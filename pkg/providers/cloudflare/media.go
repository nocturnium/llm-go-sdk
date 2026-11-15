package cloudflare

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// Default media models. Each was live-verified on /ai/run on 2026-11-14, and its
// input and output schema read from /ai/models/schema.
const (
	// DefaultRerankModel takes {query, contexts[{text}], top_k} and returns
	// {response[{id, score}]}.
	DefaultRerankModel = "@cf/baai/bge-reranker-base"
	// DefaultImageModel takes {prompt (1-2048 chars), steps (<= 8)} and returns a
	// base64 JPEG in {image}.
	DefaultImageModel = "@cf/black-forest-labs/flux-1-schnell"
	// DefaultSpeechModel takes {text, speaker, encoding, container, sample_rate,
	// bit_rate} and returns raw audio bytes (audio/mpeg by default).
	DefaultSpeechModel = "@cf/deepgram/aura-2-en"
	// DefaultTranscriptionModel takes base64 {audio, language, initial_prompt} and
	// returns text, segments with word timings, and transcription_info.
	DefaultTranscriptionModel = "@cf/openai/whisper-large-v3-turbo"
)

// Media models are allowlisted: Workers AI models of one task do not share a
// wire format (melotts returns base64 JSON where aura returns bytes; nova-3
// takes a binary body where whisper takes base64), so an unlisted model returns
// ErrInvalidParameters rather than a guessed request.
var (
	rerankModels        = map[string]bool{DefaultRerankModel: true}
	imageModels         = map[string]bool{DefaultImageModel: true}
	speechModels        = map[string]bool{DefaultSpeechModel: true}
	transcriptionModels = map[string]bool{DefaultTranscriptionModel: true}
)

// Body keys owned by typed options; Extra may not set them. "model" is listed
// because the model rides in the route, not the body.
var (
	speechKeys        = map[string]bool{"model": true, "text": true, "speaker": true, "encoding": true, "container": true, "sample_rate": true, "bit_rate": true}
	transcriptionKeys = map[string]bool{"model": true, "audio": true, "language": true, "initial_prompt": true}
)

// intValue reads an int from an int or an integral float64 (JSON-decoded configs).
func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

const (
	maxImagePromptLength = 2048
	maxImageSteps        = 8
	// maxTranscriptionBytes bounds inline audio, matching the 25 MB limit other
	// providers in this module apply.
	maxTranscriptionBytes = 25 * 1024 * 1024
)

func modelOrDefault(model, def string, allowed map[string]bool, kind string) (string, error) {
	if model == "" {
		model = def
	}
	if !allowed[model] {
		return "", fmt.Errorf("cloudflare: %s model %q is not supported (supported: %s): %w", kind, model, def, llms.ErrInvalidParameters)
	}
	return model, nil
}

// Rerank scores documents against query with a Workers AI reranker. Results come
// back best first; TopN maps to top_k and ReturnDocuments copies the input text
// into each result. Empty query or documents return ErrInvalidParameters.
func (c *Client) Rerank(ctx context.Context, query string, documents []string, opts ...llms.RerankOption) (*llms.RerankResponse, error) {
	o := llms.ApplyRerankOptions(opts...)
	model, err := modelOrDefault(o.Model, DefaultRerankModel, rerankModels, "rerank")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" || len(documents) == 0 {
		return nil, fmt.Errorf("cloudflare: rerank needs a query and at least one document: %w", llms.ErrInvalidParameters)
	}
	contexts := make([]map[string]string, len(documents))
	for i, d := range documents {
		if d == "" {
			return nil, fmt.Errorf("cloudflare: rerank document %d is empty: %w", i, llms.ErrInvalidParameters)
		}
		contexts[i] = map[string]string{"text": d}
	}
	if o.TopN < 0 {
		return nil, fmt.Errorf("cloudflare: negative TopN: %w", llms.ErrInvalidParameters)
	}
	body := map[string]any{"query": query, "contexts": contexts}
	if o.TopN > 0 {
		body["top_k"] = o.TopN
	}
	var result struct {
		Response []struct {
			ID    int     `json:"id"`
			Score float64 `json:"score"`
		} `json:"response"`
		Meta struct {
			Name  string `json:"cost_metric_name_1"`
			Value int    `json:"cost_metric_value_1"`
		} `json:"meta"`
	}
	if err := c.post(ctx, runRoute(model), body, false, &result); err != nil {
		return nil, err
	}
	out := &llms.RerankResponse{Model: model, Results: make([]llms.RerankResult, 0, len(result.Response))}
	for _, r := range result.Response {
		if r.ID < 0 || r.ID >= len(documents) {
			return nil, fmt.Errorf("cloudflare: rerank result index %d out of range: %w", r.ID, llms.ErrIncompleteResponse)
		}
		res := llms.RerankResult{Index: r.ID, Score: r.Score}
		if o.ReturnDocuments {
			res.Document = documents[r.ID]
		}
		out.Results = append(out.Results, res)
	}
	// meta names its first cost metric; bge-reranker-base reports input_tokens
	// (live 2026-11-14). Any other metric is not a token count, so TotalTokens
	// stays zero rather than holding a value in an unknown unit.
	if result.Meta.Name == "input_tokens" {
		out.Usage.TotalTokens = result.Meta.Value
	}
	return out, nil
}

// GenerateImage generates one JPEG with FLUX.1 [schnell]. Extra["steps"] sets
// the diffusion steps (1-8, default 4). Count above 1, size, aspect ratio, seed,
// negative prompt, quality, output format and safety tolerance are not accepted
// by the model and return ErrInvalidParameters.
func (c *Client) GenerateImage(ctx context.Context, prompt string, opts ...llms.ImageOption) (*llms.ImageResponse, error) {
	o := llms.ApplyImageOptions(opts...)
	model, err := modelOrDefault(o.Model, DefaultImageModel, imageModels, "image")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("cloudflare: %w", llms.ErrEmptyPrompt)
	}
	if utf8.RuneCountInString(prompt) > maxImagePromptLength {
		return nil, fmt.Errorf("cloudflare: image prompt exceeds %d characters: %w", maxImagePromptLength, llms.ErrInvalidParameters)
	}
	if (o.NSet && o.N != 1) || o.Size != "" || o.AspectRatio != "" || o.Seed != nil || o.NegativePrompt != "" || o.Quality != "" || o.OutputFormat != "" || o.SafetyTolerance != nil {
		return nil, fmt.Errorf("cloudflare: %s accepts only a prompt and steps: %w", model, llms.ErrInvalidParameters)
	}
	body := map[string]any{"prompt": prompt}
	for _, k := range sortedKeys(o.Extra) {
		if k != "steps" {
			return nil, fmt.Errorf("cloudflare: image Extra key %q is not accepted by %s: %w", k, model, llms.ErrInvalidParameters)
		}
		steps, ok := intValue(o.Extra[k])
		if !ok || steps < 1 || steps > maxImageSteps {
			return nil, fmt.Errorf("cloudflare: steps must be an int from 1 to %d: %w", maxImageSteps, llms.ErrInvalidParameters)
		}
		body["steps"] = steps
	}
	var result struct {
		Image string `json:"image"`
		Usage struct {
			Neurons float64 `json:"neurons"`
		} `json:"usage"`
	}
	if err := c.post(ctx, runRoute(model), body, false, &result); err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(result.Image)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("cloudflare: image result is not base64 image data: %w", llms.ErrIncompleteResponse)
	}
	// FLUX.1 [schnell] returns JPEG (live 2026-11-14); sniff rather than assume,
	// and refuse bytes that are not an image.
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, fmt.Errorf("cloudflare: image result is %s, not an image: %w", mimeType, llms.ErrIncompleteResponse)
	}
	return &llms.ImageResponse{
		Images:   []llms.MediaAsset{{Data: data, MIMEType: mimeType}},
		Model:    model,
		Usage:    llms.MediaUsage{Unit: llms.MediaUnitImage, Quantity: 1},
		Metadata: map[string]any{"neurons": result.Usage.Neurons},
	}, nil
}

// Synthesize converts text to speech with Deepgram Aura 2. Voice maps to
// speaker; Format maps to encoding, container, sample_rate and bit_rate. The
// default output is MP3. Language, instructions, speed and timestamps are not
// accepted and return ErrInvalidParameters.
func (c *Client) Synthesize(ctx context.Context, text string, opts ...llms.SpeechOption) (*llms.SpeechResponse, error) {
	o := llms.ApplySpeechOptions(opts...)
	model, err := modelOrDefault(o.Model, DefaultSpeechModel, speechModels, "speech")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("cloudflare: %w", llms.ErrEmptyText)
	}
	if o.Language != "" || o.Instructions != "" || o.Speed != nil || o.Timestamps {
		return nil, fmt.Errorf("cloudflare: %s does not accept language, instructions, speed or timestamps: %w", model, llms.ErrInvalidParameters)
	}
	body := map[string]any{"text": text}
	if o.Voice != "" {
		body["speaker"] = o.Voice
	}
	if o.Format.Encoding != "" {
		body["encoding"] = o.Format.Encoding
	}
	if o.Format.Container != "" {
		body["container"] = o.Format.Container
	}
	if o.Format.SampleRate > 0 {
		body["sample_rate"] = o.Format.SampleRate
	}
	if o.Format.BitRate > 0 {
		body["bit_rate"] = o.Format.BitRate
	}
	for k, v := range o.Extra {
		if speechKeys[k] {
			return nil, fmt.Errorf("cloudflare: speech Extra key %q is reserved for a typed option: %w", k, llms.ErrInvalidParameters)
		}
		body[k] = v
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u := c.aiBase.JoinPath(runRoute(model)...)
	data, header, err := c.transport.DoRawWithHeaders(ctx, httpclient.Request{Method: http.MethodPost, URL: u.String(), Headers: c.headers, Body: body})
	if err != nil {
		return nil, openaicompat.WrapError(c.Provider(), "synthesize", err)
	}
	mimeType, _, _ := mime.ParseMediaType(header.Get("Content-Type"))
	if mimeType == "" {
		// Aura's schema declares audio/mpeg output.
		mimeType = "audio/mpeg"
	}
	if !strings.HasPrefix(mimeType, "audio/") || len(data) == 0 {
		return nil, fmt.Errorf("cloudflare: speech returned %s, not audio: %w", mimeType, llms.ErrIncompleteResponse)
	}
	format := o.Format
	if format.Encoding == "" {
		format.Encoding = "mp3"
	}
	return &llms.SpeechResponse{
		Audio:    llms.MediaAsset{Data: data, MIMEType: mimeType},
		Format:   format,
		Model:    model,
		Usage:    llms.MediaUsage{Unit: llms.MediaUnitKChar, Quantity: float64(utf8.RuneCountInString(text)) / 1000},
		Metadata: neuronsMetadata(header.Get("Cf-Ai-Neurons")),
	}, nil
}

// neuronsMetadata reports the Cf-Ai-Neurons header as a float64, the same type
// GenerateImage reports, or no metadata when the header is absent or malformed.
func neuronsMetadata(header string) map[string]any {
	n, err := strconv.ParseFloat(header, 64)
	if err != nil {
		return nil
	}
	return map[string]any{"neurons": n}
}

// StreamSpeech is not supported; Aura on Workers AI returns the whole file.
func (c *Client) StreamSpeech(context.Context, string, ...llms.SpeechOption) (<-chan llms.AudioChunk, error) {
	return nil, fmt.Errorf("cloudflare: %w", llms.ErrSpeechStreamNotSupported)
}

type whisperResult struct {
	Text              string `json:"text"`
	TranscriptionInfo struct {
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	} `json:"transcription_info"`
	Segments []struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Text  string  `json:"text"`
		Words []struct {
			Word  string  `json:"word"`
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		} `json:"words"`
	} `json:"segments"`
}

// Transcribe converts inline audio (at most 25 MB) to text with Whisper large-v3
// turbo. Language and Prompt map to language and initial_prompt. Words are
// returned when WordTimestamps is set. Diarization and keyterms are not
// supported, and URL or FileID audio returns ErrInvalidParameters.
func (c *Client) Transcribe(ctx context.Context, audio llms.MediaInput, opts ...llms.TranscribeOption) (*llms.Transcription, error) {
	o := llms.ApplyTranscribeOptions(opts...)
	model, err := modelOrDefault(o.Model, DefaultTranscriptionModel, transcriptionModels, "transcription")
	if err != nil {
		return nil, err
	}
	if err := audio.Validate(); err != nil {
		return nil, fmt.Errorf("cloudflare: %w", err)
	}
	if len(audio.Data) == 0 || len(audio.Data) > maxTranscriptionBytes {
		return nil, fmt.Errorf("cloudflare: transcription needs inline audio up to 25 MB: %w", llms.ErrInvalidParameters)
	}
	if o.Diarize || len(o.Keyterms) > 0 {
		return nil, fmt.Errorf("cloudflare: %s does not support diarization or keyterms: %w", model, llms.ErrInvalidParameters)
	}
	body := map[string]any{"audio": base64.StdEncoding.EncodeToString(audio.Data)}
	if o.Language != "" {
		body["language"] = o.Language
	}
	if o.Prompt != "" {
		body["initial_prompt"] = o.Prompt
	}
	for k, v := range o.Extra {
		if transcriptionKeys[k] {
			return nil, fmt.Errorf("cloudflare: transcription Extra key %q is reserved for a typed option: %w", k, llms.ErrInvalidParameters)
		}
		body[k] = v
	}
	var result whisperResult
	if err := c.post(ctx, runRoute(model), body, false, &result); err != nil {
		return nil, err
	}
	out := &llms.Transcription{
		Text:            strings.TrimSpace(result.Text),
		Language:        result.TranscriptionInfo.Language,
		DurationSeconds: result.TranscriptionInfo.Duration,
		Model:           model,
		Segments:        make([]llms.TranscriptSegment, 0, len(result.Segments)),
		Words:           make([]llms.TranscriptWord, 0),
		Usage:           llms.MediaUsage{Unit: llms.MediaUnitMinute, Quantity: result.TranscriptionInfo.Duration / 60},
	}
	for _, s := range result.Segments {
		out.Segments = append(out.Segments, llms.TranscriptSegment{Start: s.Start, End: s.End, Text: strings.TrimSpace(s.Text)})
		if o.WordTimestamps {
			for _, w := range s.Words {
				out.Words = append(out.Words, llms.TranscriptWord{Start: w.Start, End: w.End, Word: strings.TrimSpace(w.Word)})
			}
		}
	}
	return out, nil
}

var (
	_ llms.Reranker          = (*Client)(nil)
	_ llms.ImageGenerator    = (*Client)(nil)
	_ llms.SpeechSynthesizer = (*Client)(nil)
	_ llms.Transcriber       = (*Client)(nil)
)
