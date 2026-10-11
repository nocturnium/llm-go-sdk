package llms

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"
)

func TestValidateEmbedInput(t *testing.T) {
	tests := []struct {
		name    string
		texts   []string
		wantErr bool
	}{
		{
			name:    "valid single text",
			texts:   []string{"Hello, world!"},
			wantErr: false,
		},
		{
			name:    "valid multiple texts",
			texts:   []string{"Hello", "World", "Test"},
			wantErr: false,
		},
		{
			name:    "empty slice",
			texts:   []string{},
			wantErr: true,
		},
		{
			name:    "nil slice",
			texts:   nil,
			wantErr: true,
		},
		{
			name:    "contains empty string",
			texts:   []string{"Hello", "", "World"},
			wantErr: true,
		},
		{
			name:    "only empty string",
			texts:   []string{""},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEmbedInput(tc.texts)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateEmbedInput() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestApplyEmbedOptions(t *testing.T) {
	opts := ApplyEmbedOptions(
		WithEmbedModel("custom-model"),
		WithDimensions(256),
		WithEncodingFormat("base64"),
		WithEmbedUser("user123"),
		WithTaskType(TaskTypeRetrievalQuery),
	)

	if opts.Model != "custom-model" {
		t.Errorf("Model = %s, want custom-model", opts.Model)
	}
	if opts.Dimensions != 256 {
		t.Errorf("Dimensions = %d, want 256", opts.Dimensions)
	}
	if opts.EncodingFormat != "base64" {
		t.Errorf("EncodingFormat = %s, want base64", opts.EncodingFormat)
	}
	if opts.User != "user123" {
		t.Errorf("User = %s, want user123", opts.User)
	}
	if opts.TaskType != TaskTypeRetrievalQuery {
		t.Errorf("TaskType = %s, want %s", opts.TaskType, TaskTypeRetrievalQuery)
	}
}

func TestApplyEmbedOptions_Defaults(t *testing.T) {
	opts := ApplyEmbedOptions()

	if opts.Model != "" {
		t.Errorf("Model = %s, want empty", opts.Model)
	}
	if opts.Dimensions != 0 {
		t.Errorf("Dimensions = %d, want 0", opts.Dimensions)
	}
	if opts.EncodingFormat != "" {
		t.Errorf("EncodingFormat = %s, want empty", opts.EncodingFormat)
	}
	if opts.User != "" {
		t.Errorf("User = %s, want empty", opts.User)
	}
	if opts.TaskType != "" {
		t.Errorf("TaskType = %s, want empty", opts.TaskType)
	}
}

// An embedding response is consumed through EmbedDocuments, so that is what this
// pins: every vector comes back, in the order the provider returned them.
func TestEmbeddingResponse_Structure(t *testing.T) {
	resp := &EmbeddingResponse{
		Embeddings: []Embedding{
			{Index: 1, Vector: []float32{0.4, 0.5, 0.6}, Object: "embedding"},
			{Index: 0, Vector: []float32{0.1, 0.2, 0.3}, Object: "embedding"},
		},
		Model: "text-embedding-3-small",
		Usage: EmbeddingUsage{PromptTokens: 10, TotalTokens: 10},
	}

	vectors, err := EmbedDocuments(context.Background(), &stubEmbedder{resp: resp}, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 {
		t.Fatalf("EmbedDocuments returned %d vectors, want 2", len(vectors))
	}
	// Response order, which is what the documented contract promises.
	if vectors[0][0] != 0.4 || vectors[1][0] != 0.1 {
		t.Errorf("vectors = %v, want them in response order", vectors)
	}
}

// The standard OpenAI wire form puts vectors in `data`, while some providers
// and fixtures use `embeddings`. Both must decode; only recognizing one would
// silently leave Embeddings empty and make EmbedQuery report an incomplete
// response.
func TestEmbeddingResponse_DecodesDataAndEmbeddingsKeys(t *testing.T) {
	var fromData EmbeddingResponse
	if err := json.Unmarshal([]byte(`{"model":"m","data":[{"index":0,"embedding":[1,2],"object":"embedding"}]}`), &fromData); err != nil {
		t.Fatal(err)
	}
	if len(fromData.Embeddings) != 1 || fromData.Embeddings[0].Vector[0] != 1 {
		t.Errorf("data key: %+v", fromData)
	}

	var fromEmbeddings EmbeddingResponse
	if err := json.Unmarshal([]byte(`{"model":"m","embeddings":[{"index":0,"embedding":[3,4]}]}`), &fromEmbeddings); err != nil {
		t.Fatal(err)
	}
	if len(fromEmbeddings.Embeddings) != 1 || fromEmbeddings.Embeddings[0].Vector[0] != 3 {
		t.Errorf("embeddings key: %+v", fromEmbeddings)
	}
}

// WithEncodingFormat("base64") makes OpenAI return each vector as a base64
// string of little-endian float32s. Decoding only the float-array form would
// leave the vector empty or fail the response for that caller.
func TestEmbeddingResponse_DecodesBase64Vectors(t *testing.T) {
	var payload []byte
	for _, v := range []float32{0.5, -1.25} {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		payload = append(payload, b[:]...)
	}
	encoded := base64.StdEncoding.EncodeToString(payload)

	var resp EmbeddingResponse
	body := `{"model":"m","data":[{"index":0,"embedding":"` + encoded + `","object":"embedding"}]}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Embeddings) != 1 {
		t.Fatalf("embeddings = %+v, want one", resp.Embeddings)
	}
	vector := resp.Embeddings[0].Vector
	if len(vector) != 2 || vector[0] != 0.5 || vector[1] != -1.25 {
		t.Errorf("vector = %v, want [0.5 -1.25]", vector)
	}
}

func TestTaskTypeConstants(t *testing.T) {
	// Verify task type constants have expected values
	tests := []struct {
		constant string
		expected string
	}{
		{TaskTypeRetrievalQuery, "RETRIEVAL_QUERY"},
		{TaskTypeRetrievalDocument, "RETRIEVAL_DOCUMENT"},
		{TaskTypeSemantic, "SEMANTIC_SIMILARITY"},
		{TaskTypeClassification, "CLASSIFICATION"},
		{TaskTypeClustering, "CLUSTERING"},
	}

	for _, tc := range tests {
		if tc.constant != tc.expected {
			t.Errorf("TaskType constant = %s, want %s", tc.constant, tc.expected)
		}
	}
}

func TestEmbeddingsErrors(t *testing.T) {
	if ErrEmbeddingsNotSupported == nil {
		t.Error("ErrEmbeddingsNotSupported should not be nil")
	}
	if ErrEmptyInput == nil {
		t.Error("ErrEmptyInput should not be nil")
	}

	// Verify error messages
	if ErrEmbeddingsNotSupported.Error() != "embeddings not supported by this provider" {
		t.Errorf("unexpected error message: %s", ErrEmbeddingsNotSupported.Error())
	}
	if ErrEmptyInput.Error() != "input text is empty" {
		t.Errorf("unexpected error message: %s", ErrEmptyInput.Error())
	}
}
