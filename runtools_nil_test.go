package llms

import (
	"context"
	"testing"
)

func TestRunTools_NilRegistryReturnsError(t *testing.T) {
	msgs := []Message{{Role: RoleUser, Content: "hi"}}
	resp, transcript, err := RunTools(context.Background(), nil, msgs, nil)
	if err == nil {
		t.Fatal("RunTools with a nil registry must return an error")
	}
	if resp != nil || len(transcript) != 1 {
		t.Errorf("resp=%v transcript=%v, want nil response and the input transcript", resp, transcript)
	}
}
