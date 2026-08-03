package runpod

import (
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestRegisterNewMissingEndpointID(t *testing.T) {
	_, err := llms.New("runpod", llms.Config{})
	if err == nil {
		t.Fatal("New returned nil error without endpoint_id")
	}
	if !strings.Contains(err.Error(), "runpod") {
		t.Fatalf("error %q does not include provider name", err.Error())
	}
	if !strings.Contains(err.Error(), llms.ExtraRunPodEndpointID) {
		t.Fatalf("error %q does not include endpoint_id key", err.Error())
	}
}
