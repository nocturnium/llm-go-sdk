package httpclient

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestPoll_Backoff(t *testing.T) {
	policy := PollPolicy{Initial: 5 * time.Millisecond, Max: 20 * time.Millisecond, Multiplier: 2}
	var times []time.Time
	err := Poll(context.Background(), policy, func(context.Context) (bool, error) { times = append(times, time.Now()); return len(times) == 5, nil })
	if err != nil {
		t.Fatal(err)
	}
	for i, minDelay := range []time.Duration{5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond} {
		if got := times[i+1].Sub(times[i]); got < minDelay {
			t.Fatalf("interval %d: %v < %v", i, got, minDelay)
		}
	}
}

func TestPoll_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Poll(ctx, PollPolicy{}, func(context.Context) (bool, error) { t.Fatal("called after cancel"); return false, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	go func() { <-started; time.Sleep(time.Millisecond); cancel() }()
	if err := Poll(ctx, PollPolicy{}, func(context.Context) (bool, error) { close(started); return false, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPoll_Timeout(t *testing.T) {
	for _, callbackWait := range []bool{false, true} {
		err := Poll(context.Background(), PollPolicy{Timeout: time.Millisecond}, func(ctx context.Context) (bool, error) {
			if callbackWait {
				<-ctx.Done()
				return false, ctx.Err()
			}
			return false, nil
		})
		if !errors.Is(err, ErrPollTimeout) {
			t.Fatalf("timeout: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := Poll(ctx, PollPolicy{Timeout: time.Hour}, func(ctx context.Context) (bool, error) { <-ctx.Done(); return false, ctx.Err() }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestPoll_CallbackResult(t *testing.T) {
	cause := errors.New("callback failed")
	if err := Poll(context.Background(), PollPolicy{}, func(context.Context) (bool, error) { return false, cause }); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	if err := Poll(context.Background(), PollPolicy{}, func(context.Context) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestPollPolicy_DefaultsAndBounds(t *testing.T) {
	want := PollPolicy{Initial: time.Second, Max: 10 * time.Second, Multiplier: 1.5, Jitter: 0.2}
	if got := DefaultPollPolicy(); got != want {
		t.Fatalf("defaults: %+v", got)
	}
	if got := normalizePollPolicy(PollPolicy{}); got != want {
		t.Fatalf("zero defaults: %+v", got)
	}
	for _, p := range []PollPolicy{
		{Initial: -1, Max: -1, Multiplier: -1, Jitter: math.NaN()},
		{Initial: time.Hour, Max: time.Second, Multiplier: math.Inf(1), Jitter: 2},
		{Initial: time.Second, Multiplier: math.NaN(), Jitter: -1},
	} {
		got := normalizePollPolicy(p)
		if got.Initial <= 0 || got.Initial > got.Max || got.Multiplier != 1.5 || got.Jitter < 0 || got.Jitter > 1 {
			t.Fatalf("normalization: %+v", got)
		}
	}
	for i := 0; i < 100; i++ {
		got := pollDelay(time.Second, PollPolicy{Max: time.Second, Jitter: 0.2})
		if got < 800*time.Millisecond || got > time.Second {
			t.Fatalf("jitter: %v", got)
		}
	}
}

func TestPoll_DoneOnDeadlinePoll(t *testing.T) {
	calls := 0
	err := Poll(context.Background(), PollPolicy{Timeout: 10 * time.Millisecond}, func(ctx context.Context) (bool, error) {
		calls++
		<-ctx.Done()
		return true, nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("completed deadline poll: calls=%d error=%v", calls, err)
	}
}
