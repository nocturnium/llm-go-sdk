package openrouter

import (
	"encoding/json"
	"testing"
)

// TestBatchError_NumericCode pins that a numeric error code decodes. A bare
// integer there used to fail the whole Batch decode, so GetBatch returned a
// JSON error instead of the failed batch.
func TestBatchError_NumericCode(t *testing.T) {
	var batch Batch
	body := []byte(`{"id":"batch_1","status":"failed","error":{"code":429,"message":"rate limited","type":"rate_limit"}}`)

	if err := json.Unmarshal(body, &batch); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if batch.Error == nil {
		t.Fatal("batch carries no error")
	}
	if batch.Error.Code != "429" {
		t.Errorf("Code = %q, want %q", batch.Error.Code, "429")
	}
	if batch.Error.Message != "rate limited" {
		t.Errorf("Message = %q, want %q", batch.Error.Message, "rate limited")
	}
}

// TestBatchError_StringCode keeps the documented string form working.
func TestBatchError_StringCode(t *testing.T) {
	var e BatchError
	if err := json.Unmarshal([]byte(`{"code":"insufficient_quota","message":"no credit"}`), &e); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if e.Code != "insufficient_quota" {
		t.Errorf("Code = %q, want %q", e.Code, "insufficient_quota")
	}
}
