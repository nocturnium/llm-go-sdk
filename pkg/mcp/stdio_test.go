package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"
)

// With more than one request in flight, an injected id:null error frame must not
// be broadcast to every pending channel: doing so would hand an unrelated request
// a bogus early frame and then drop its real id-correlated response. The fix
// treats an uncorrelatable id:null error as a transport-level protocol failure, so
// every caller observes one consistent error rather than a poisoned result.
func TestStdioTransport_NullIDErrorDoesNotPoisonConcurrentRequests(t *testing.T) {
	tr, serverReads, serverWrites := newPipeStdioTransport(t)
	defer func() { _ = tr.close() }()

	// The fake server reads both requests (so both register as pending) and then
	// emits a single id:null error WITHOUT ever sending the real id-correlated
	// responses, modeling a parse/invalid-request error from the server.
	bothRead := make(chan struct{})
	go func() {
		defer func() { _ = serverWrites.Close() }()
		br := bufio.NewReader(serverReads)
		for i := 0; i < 2; i++ {
			if _, err := br.ReadBytes('\n'); err != nil {
				return
			}
		}
		close(bothRead)
		_, _ = serverWrites.Write([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"invalid request"}}` + "\n"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		raw []byte
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, id := range []int64{1, 2} {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			payload, err := encodeRequest(id, "slow", nil)
			if err != nil {
				results <- result{err: err}
				return
			}
			raw, err := tr.request(ctx, id, payload)
			results <- result{raw: raw, err: err}
		}(id)
	}

	select {
	case <-bothRead:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not read both requests")
	}

	wg.Wait()
	close(results)

	got := 0
	for r := range results {
		got++
		// Neither request may resolve with a successful (bogus) result: the id:null
		// error is uncorrelatable, so both must surface an error.
		if r.err == nil {
			t.Fatalf("request resolved with a bogus result %q and no error; an uncorrelatable id:null error must not be delivered as a success", r.raw)
		}
		if ctx.Err() != nil {
			t.Fatalf("request returned because the context expired (%v); the transport should have failed fast instead of looping", r.err)
		}
	}
	if got != 2 {
		t.Fatalf("expected both requests to return, got %d", got)
	}
}

// TestStdioTransport_DeliveredResponseWinsOverClose reproduces the answer-then-EOF
// race: a response is delivered to the pending channel at the same instant the
// transport's done channel closes, so request()'s select has both cases ready.
// The delivered response must win — returning "transport closed" for a call the
// server actually completed would make a non-idempotent tool look failed and be
// retried, duplicating its side effect. An io.Pipe stdin makes write() block
// until BOTH conditions are set up, so the both-ready state is deterministic
// rather than timing-dependent; pre-fix, select's pseudo-random pick fails ~half
// the iterations.
func TestStdioTransport_DeliveredResponseWinsOverClose(t *testing.T) {
	const iterations = 50
	for iter := 0; iter < iterations; iter++ {
		pr, pw := io.Pipe()
		tr := &stdioTransport{
			cmd:     &exec.Cmd{},
			stdin:   pw,
			pending: make(map[int64]chan []byte),
			done:    make(chan struct{}),
		}

		const id = int64(1)
		type outcome struct {
			line []byte
			err  error
		}
		res := make(chan outcome, 1)
		go func() {
			line, err := tr.request(context.Background(), id, []byte(`{"jsonrpc":"2.0","id":1}`))
			res <- outcome{line, err}
		}()

		// request() registers pending[id] before it calls write(), which then
		// blocks on the still-unread pipe. Wait for the registration.
		var ch chan []byte
		for ch == nil {
			tr.mu.Lock()
			ch = tr.pending[id]
			tr.mu.Unlock()
			if ch == nil {
				runtime.Gosched()
			}
		}

		// Make BOTH select cases ready before request() selects: buffer a delivered
		// response, then close done.
		ch <- []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
		close(tr.done)

		// Now unblock write() by draining the pipe; request() proceeds to select
		// with both ch and done ready.
		drained := make(chan struct{})
		go func() {
			_, _ = io.Copy(io.Discard, pr)
			close(drained)
		}()

		got := <-res
		_ = pw.Close()
		<-drained

		if got.err != nil {
			t.Fatalf("iter %d: delivered response lost, got err %v", iter, got.err)
		}
		if len(got.line) == 0 {
			t.Fatalf("iter %d: empty response line", iter)
		}
	}
}

// A blocking notification handler must not stall delivery of the originating
// request's response: handlers run off the transport read path. Here a progress
// notification is interleaved before the response and its handler blocks; the
// request must still resolve promptly.
func TestStdioClient_BlockingProgressHandlerDoesNotStallResponse(t *testing.T) {
	clientReads, serverWrites := io.Pipe() // server -> client
	serverReads, clientWrites := io.Pipe() // client -> server

	tr := &stdioTransport{
		cmd:     &exec.Cmd{},
		stdin:   clientWrites,
		pending: make(map[int64]chan []byte),
		done:    make(chan struct{}),
	}
	go tr.readLoop(clientReads)

	// Fake server: answer initialize, then for tools/call emit a progress
	// notification (whose handler will block) immediately before the response.
	go func() {
		defer func() { _ = serverWrites.Close() }()
		br := bufio.NewReader(serverReads)
		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 {
				var probe struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
					Params struct {
						Meta struct {
							ProgressToken string `json:"progressToken"`
						} `json:"_meta"`
					} `json:"params"`
				}
				_ = json.Unmarshal(line, &probe)
				switch probe.Method {
				case methodInitialize:
					writeStdioResult(serverWrites, probe.ID, InitializeResult{
						ProtocolVersion: protocolVersion,
						ServerInfo:      Implementation{Name: "blk", Version: "1.0"},
					})
				case methodInitialized:
					// notification, no response
				case methodToolsCall:
					// Echo the client's progress token so the per-call handler (keyed by
					// that token) actually receives the notification.
					tok, _ := json.Marshal(probe.Params.Meta.ProgressToken)
					_, _ = serverWrites.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":` + string(tok) + `,"progress":1}}` + "\n"))
					writeStdioResult(serverWrites, probe.ID, CallToolResult{Content: []ContentBlock{{Type: "text", Text: "done"}}})
				}
			}
			if err != nil {
				return
			}
		}
	}()

	c, err := newClient(context.Background(), tr, buildConfig(nil))
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := c.CallToolWithProgress(ctx, "slow", json.RawMessage(`{}`),
		WithProgress(func(ProgressNotification) {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release // block the handler for the duration of the test
		}))
	if err != nil {
		t.Fatalf("CallToolWithProgress did not resolve while its progress handler was blocked: %v", err)
	}
	if res.Text() != "done" {
		t.Errorf("result text = %q, want done", res.Text())
	}

	// The blocking handler must actually have been invoked (proving the off-path
	// dispatch ran it), yet the call above still returned.
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("progress handler was never invoked")
	}
}

func writeStdioResult(w io.Writer, id json.RawMessage, result any) {
	b, _ := json.Marshal(result)
	out, _ := json.Marshal(rpcResponse{JSONRPC: jsonRPCVersion, ID: id, Result: b})
	_, _ = w.Write(append(out, '\n'))
}
