package openai

import (
	"context"
	"strings"
	"testing"

	"net/http"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

// TestRegister_AllowHTTPDecoupledFromPrivateIPs proves the v5 decoupling through
// the real llms.New / llms.Config FACTORY path (distinct from ssrf_test.go, which
// exercises the never-coupled Option layer). AllowPrivateIPs and AllowHTTP are
// independent gates: the endpoint http://10.0.0.5 is simultaneously a private IP
// AND plain HTTP, so each flag alone must still be rejected by the other gate.
//
// The first subtest is the load-bearing regression: under the old coupled
// register.go (`if cfg.AllowPrivateIPs { WithAllowPrivateIPs(), WithAllowHTTP() }`)
// setting AllowPrivateIPs also enabled plain HTTP, so the request would have
// reached the transport instead of being rejected. (recordingTransport is defined
// in ssrf_test.go.)
func TestRegister_AllowHTTPDecoupledFromPrivateIPs(t *testing.T) {
	call := func(cfg llms.Config) (*recordingTransport, error) {
		rt := &recordingTransport{}
		cfg.APIKey = "test-key"
		cfg.BaseURL = "http://10.0.0.5/v1"
		cfg.HTTPClient = &http.Client{Transport: rt}
		llm, err := llms.New("openai", cfg)
		if err != nil {
			t.Fatalf("llms.New: %v", err)
		}
		_, genErr := llm.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
		return rt, genErr
	}

	t.Run("AllowPrivateIPs alone does not permit plain HTTP", func(t *testing.T) {
		rt, err := call(llms.Config{AllowPrivateIPs: true})
		if err == nil || !strings.Contains(err.Error(), "HTTP not allowed") {
			t.Fatalf("want HTTP-not-allowed rejection, got err=%v", err)
		}
		if rt.hit {
			t.Fatal("request reached the transport: AllowPrivateIPs must NOT enable plain HTTP (coupling regression)")
		}
	})

	t.Run("AllowHTTP alone does not permit a private IP", func(t *testing.T) {
		rt, err := call(llms.Config{AllowHTTP: true})
		if err == nil || !strings.Contains(err.Error(), "private IP") {
			t.Fatalf("want private-IP rejection, got err=%v", err)
		}
		if rt.hit {
			t.Fatal("request reached the transport: AllowHTTP must NOT enable private-IP access")
		}
	})

	t.Run("both flags together permit the private HTTP endpoint", func(t *testing.T) {
		rt, err := call(llms.Config{AllowPrivateIPs: true, AllowHTTP: true})
		if err != nil {
			t.Fatalf("both flags set: unexpected error: %v", err)
		}
		if !rt.hit {
			t.Fatal("both flags set: the request should have passed validation and reached the transport")
		}
	})

	t.Run("neither flag set rejects the endpoint", func(t *testing.T) {
		rt, err := call(llms.Config{})
		if err == nil {
			t.Fatal("default (secure) config must reject a private http endpoint")
		}
		if rt.hit {
			t.Fatal("default config must not reach the transport")
		}
	})
}
