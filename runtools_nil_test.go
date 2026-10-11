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

func TestRunTools_NilProviderResponseReturnsError(t *testing.T) {
	_, _, err := RunTools(context.Background(), &nilResponseLLM{}, []Message{{Role: RoleUser, Content: "hi"}}, NewToolRegistry())
	if err == nil {
		t.Fatal("a (nil, nil) provider result must be an error, not a panic")
	}
}

func TestRunTools_NilLLMReturnsError(t *testing.T) {
	if _, _, err := RunTools(context.Background(), nil, []Message{{Role: RoleUser, Content: "hi"}}, NewToolRegistry()); err == nil {
		t.Fatal("RunTools with a nil LLM must return an error")
	}
}
