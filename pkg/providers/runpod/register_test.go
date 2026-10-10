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

// The generic constructor must forward the embedding model, like the other
// model and client settings, or a caller using llms.New with an embedding model
// gets a client whose EmbeddingModel stays empty.
func TestRegisterForwardsEmbeddingModel(t *testing.T) {
	llm, err := llms.New("runpod", llms.Config{
		APIKey: "test-key",
		Extra: map[string]string{
			llms.ExtraRunPodEndpointID: "ep-123",
			llms.ExtraEmbeddingModel:   "BAAI/bge-small-en-v1.5",
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	client, ok := llm.(*Client)
	if !ok {
		t.Fatalf("got %T, want *runpod.Client", llm)
	}
	if got := client.options.EmbeddingModel; got != "BAAI/bge-small-en-v1.5" {
		t.Errorf("EmbeddingModel = %q, want BAAI/bge-small-en-v1.5", got)
	}
}
