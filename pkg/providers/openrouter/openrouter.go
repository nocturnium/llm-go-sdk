package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

const defaultBaseURL = "https://openrouter.ai/api/v1"

const (
	// DefaultModel is the default chat model, verified in discovery on 2026-09-05.
	DefaultModel = "google/gemini-3.5-flash-lite"
	// DefaultImageModel is the default image generation model.
	DefaultImageModel = "google/gemini-3.1-flash-lite-image"
	// DefaultSpeechModel was live-verified via /models?output_modalities=speech on 2026-09-05.
	DefaultSpeechModel = "fish-audio/s2.1-pro"
	// DefaultTranscriptionModel is the default transcription model.
	DefaultTranscriptionModel = "openai/whisper-1"
	// DefaultVideoModel has the lowest published per-second rate in discovery on 2026-09-05.
	// Token-priced models cannot be compared without their token accounting formula.
	DefaultVideoModel = "google/veo-3.1-lite"
)

var defaultProviderConfig = openaicompat.ProviderConfig{
	Provider:          llms.ProviderOpenRouter,
	DefaultImageModel: DefaultImageModel, DefaultSpeechModel: DefaultSpeechModel,
	DefaultTranscriptionModel: DefaultTranscriptionModel, DefaultVideoModel: DefaultVideoModel,
	Media: openaicompat.MediaCapabilities{Images: true, Speech: true, Transcription: true, Videos: true,
		ImagesPath: "/images", SpeechPath: "/audio/speech", TranscriptionsPath: "/audio/transcriptions"},
	// These advertise routing support; individual models may support fewer features.
	// Context and output limits remain unknown rather than assuming one model's limits.
	Capabilities: llms.Capabilities{Streaming: true, Tools: true, Vision: true, Embeddings: true, JSONMode: true},
}

// Client is an OpenRouter chat and media client, safe for concurrent use.
type Client struct {
	openaicompat.BaseProvider
	options   *options
	transport *httpclient.Client
	baseURL   *url.URL
	headers   map[string]string
}

// New constructs a client from options, returning ErrMissingAPIKey for missing
// credentials or ErrInvalidParameters for an invalid base URL or timeout.
func New(opts ...Option) (*Client, error) {
	o := apply(opts...)
	key, err := llms.RequireAPIKey("openrouter", o.APIKey, llms.EnvOpenRouterAPIKey)
	if err != nil {
		return nil, err
	}
	o.APIKey = key
	if o.BaseURL == "" {
		o.BaseURL = defaultBaseURL
	}
	u, err := url.Parse(o.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || o.Timeout < 0 {
		return nil, fmt.Errorf("openrouter: invalid base URL or timeout: %w", llms.ErrInvalidParameters)
	}
	headers := map[string]string{}
	if o.SiteURL != "" {
		headers["HTTP-Referer"] = o.SiteURL
	}
	if o.AppName != "" {
		headers["X-Title"] = o.AppName
	}
	compat := openaicompat.NewClient(openaicompat.ClientConfig{BaseURL: o.BaseURL, APIKey: key, Headers: headers, HTTPClient: o.HTTPClient, Timeout: o.Timeout, AllowPrivateIPs: o.AllowPrivateIPs, AllowHTTP: o.AllowHTTP})
	cfg := defaultProviderConfig
	cfg.DefaultModel = o.Model
	cfg.DefaultEmbeddingModel = o.EmbeddingModel
	transportOptions := []httpclient.ClientOption{httpclient.WithAllowPrivateIPs(o.AllowPrivateIPs), httpclient.WithAllowHTTP(o.AllowHTTP)}
	if o.HTTPClient != nil {
		transportOptions = append(transportOptions, httpclient.WithHTTPClient(o.HTTPClient))
	}
	if o.Timeout > 0 {
		transportOptions = append(transportOptions, httpclient.WithTimeout(o.Timeout))
	}
	transport := httpclient.NewClient(transportOptions...)
	nativeHeaders := map[string]string{"Authorization": "Bearer " + key}
	for k, v := range headers {
		nativeHeaders[k] = v
	}
	return &Client{BaseProvider: openaicompat.NewBaseProvider(compat, cfg), options: o, transport: transport, baseURL: u, headers: nativeHeaders}, nil
}

func (c *Client) endpoint(route string, query url.Values) string {
	u := c.baseURL.JoinPath(route)
	u.RawQuery = query.Encode()
	return u.String()
}

func (c *Client) request(ctx context.Context, method, route string, query url.Values, body, out any) error {
	if err := ctx.Err(); err != nil {
		return openaicompat.WrapError(c.Provider(), route, err)
	}
	err := c.transport.DoJSON(ctx, httpclient.Request{Method: method, URL: c.endpoint(route, query), Headers: c.headers, Body: body}, out)
	return openaicompat.WrapError(c.Provider(), route, err)
}

// GenerateImage maps AspectRatio and Seed to native body keys and delegates to
// BaseProvider. Extra (including resolution and input_references) merges last,
// overriding typed options without mutating the caller's map. Returns validation
// or provider errors, and preserves reported MIME types and costs.
func (c *Client) GenerateImage(ctx context.Context, prompt string, opts ...llms.ImageOption) (*llms.ImageResponse, error) {
	o := llms.ApplyImageOptions(opts...)
	extra := map[string]any{}
	if o.AspectRatio != "" {
		extra["aspect_ratio"] = o.AspectRatio
	}
	if o.Seed != nil {
		extra["seed"] = *o.Seed
	}
	for k, v := range o.Extra {
		extra[k] = v
	}
	o.Extra = extra
	return c.BaseProvider.GenerateImage(ctx, prompt, func(target *llms.ImageOptions) { *target = *o })
}

// Synthesize returns speech bytes using the compatible speech request and mp3
// default. WithUsageLookup adds a generation lookup using X-Generation-Id;
// lookup failures leave cost unknown and preserve successful audio. Metadata
// includes generation_id when reported, for a later GenerationCost call.
func (c *Client) Synthesize(ctx context.Context, text string, opts ...llms.SpeechOption) (*llms.SpeechResponse, error) {
	o := llms.ApplySpeechOptions(opts...)
	req := openaicompat.BuildSpeechRequest(DefaultSpeechModel, text, o)
	req.Voice = o.Voice
	req.ExtraBody = o.Extra
	body := speechBody{req}
	if strings.TrimSpace(text) == "" {
		return nil, openaicompat.WrapError(c.Provider(), "synthesize", llms.ErrEmptyText)
	}
	if utf8.RuneCountInString(text) > 4096 || (req.Speed != nil && (math.IsNaN(*req.Speed) || *req.Speed < 0.25 || *req.Speed > 4)) {
		return nil, openaicompat.WrapError(c.Provider(), "synthesize", llms.ErrInvalidParameters)
	}
	data, err := c.transport.DoBinary(ctx, http.MethodPost, c.endpoint("audio/speech", nil), body, c.headers)
	if err != nil {
		return nil, openaicompat.WrapError(c.Provider(), "synthesize", err)
	}
	out := openaicompat.ConvertSpeechResponse(data.Data, data.ContentType, req, nil)
	if id := data.Header.Get("X-Generation-Id"); id != "" {
		out.Metadata = map[string]any{"generation_id": id}
		if c.options.UsageLookup {
			if cost, lookupErr := c.GenerationCost(ctx, id); lookupErr == nil {
				out.Usage.Cost = cost
			}
		}
	}

	return out, nil
}

var (
	_ llms.LLM               = (*Client)(nil)
	_ llms.CapableProvider   = (*Client)(nil)
	_ llms.ModelLister       = (*Client)(nil)
	_ llms.Embedder          = (*Client)(nil)
	_ llms.ImageGenerator    = (*Client)(nil)
	_ llms.SpeechSynthesizer = (*Client)(nil)
	_ llms.Transcriber       = (*Client)(nil)
	_ llms.VideoGenerator    = (*Client)(nil)
)

// speechBody preserves compatible marshaling but omits OpenRouter's unset voice.
type speechBody struct{ request *openaicompat.SpeechRequest }

func (b speechBody) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(b.request)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if b.request.Voice == "" {
		delete(fields, "voice")
	}
	return json.Marshal(fields)
}

// GenerationCost retrieves reported USD cost for a generation ID. Missing costs
// return nil, nil; invalid IDs and transport errors return wrapped errors.
func (c *Client) GenerationCost(ctx context.Context, generationID string) (*float64, error) {
	if strings.TrimSpace(generationID) == "" {
		return nil, openaicompat.WrapError(c.Provider(), "generation cost", llms.ErrInvalidParameters)
	}
	var usage struct {
		Data struct {
			TotalCost *float64 `json:"total_cost"`
		} `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "generation", url.Values{"id": {generationID}}, nil, &usage); err != nil {
		return nil, err
	}
	return usage.Data.TotalCost, nil
}
