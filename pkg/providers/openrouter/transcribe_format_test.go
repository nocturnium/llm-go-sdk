package openrouter

import (
	"context"
	"errors"
	"net/http"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestClient_Transcribe_NonStringResponseFormatRejected(t *testing.T) {
	called := false
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { called = true })
	_, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("ID3audio"), MIMEType: "audio/mpeg"}, llms.WithTranscribeExtra(map[string]any{"response_format": true}))
	if !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatalf("err = %v, want ErrInvalidParameters", err)
	}
	if called {
		t.Error("a rejected format must not reach the server")
	}
}
