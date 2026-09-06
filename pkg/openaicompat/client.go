package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// Client is an OpenAI-compatible API client
type Client struct {
	mediaPaths   MediaCapabilities
	httpClient   *httpclient.Client
	baseURL      string
	apiKey       string
	headers      map[string]string
	azureAPIKey  bool   // Use api-key header instead of Authorization
	azureVersion string // Azure API version (appended to URLs)
}

// ClientConfig configures the client
type ClientConfig struct {
	// Media route overrides; empty values use OpenAI defaults.
	ImagesPath, ImageEditsPath, SpeechPath, TranscriptionsPath, VideosPath string
	BaseURL                                                                string
	APIKey                                                                 string
	Headers                                                                map[string]string
	HTTPClient                                                             *http.Client
	Timeout                                                                time.Duration // Timeout for the underlying HTTP client; 0 uses the default.
	AzureAPIKey                                                            bool          // Use api-key header instead of Authorization
	AzureVersion                                                           string        // Azure API version (appended to URLs)

	// AllowPrivateIPs allows requests to private/loopback IPs (SSRF opt-out).
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
}

// NewClient creates a new OpenAI-compatible client
func NewClient(config ClientConfig) *Client {
	var hcOpts []httpclient.ClientOption
	if config.HTTPClient != nil {
		hcOpts = append(hcOpts, httpclient.WithHTTPClient(config.HTTPClient))
	}
	if config.Timeout > 0 {
		hcOpts = append(hcOpts, httpclient.WithTimeout(config.Timeout))
	}
	hcOpts = append(hcOpts,
		httpclient.WithAllowPrivateIPs(config.AllowPrivateIPs),
		httpclient.WithAllowHTTP(config.AllowHTTP),
	)
	httpClient := httpclient.NewClient(hcOpts...)

	return &Client{
		httpClient:   httpClient,
		mediaPaths:   MediaCapabilities{ImagesPath: config.ImagesPath, ImageEditsPath: config.ImageEditsPath, SpeechPath: config.SpeechPath, TranscriptionsPath: config.TranscriptionsPath, VideosPath: config.VideosPath},
		baseURL:      config.BaseURL,
		apiKey:       config.APIKey,
		headers:      config.Headers,
		azureAPIKey:  config.AzureAPIKey,
		azureVersion: config.AzureVersion,
	}
}

// CreateChatCompletion sends a chat completion request
func (c *Client) CreateChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	headers := c.getHeaders()
	var response ChatCompletionResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     c.buildURL("/chat/completions"),
		Headers: headers,
		Body:    req,
	}, &response)

	if err != nil {
		return nil, err
	}

	return &response, nil
}

// CreateChatCompletionStream sends a streaming chat completion request
func (c *Client) CreateChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (*StreamReader, error) {
	req.Stream = true
	headers := c.getHeaders()

	body, err := c.httpClient.DoStream(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     c.buildURL("/chat/completions"),
		Headers: headers,
		Body:    req,
	})

	if err != nil {
		return nil, err
	}

	return &StreamReader{
		sseReader: httpclient.NewSSEReader(body),
	}, nil
}

func (c *Client) getHeaders() map[string]string {
	headers := make(map[string]string)

	if c.azureAPIKey {
		// Azure uses api-key header
		headers["api-key"] = c.apiKey
	} else if c.apiKey != "" {
		// Standard OpenAI uses Bearer token
		headers["Authorization"] = "Bearer " + c.apiKey
	}

	for k, v := range c.headers {
		headers[k] = v
	}

	return headers
}

// buildURL constructs the full URL, appending Azure API version if needed.
func (c *Client) buildURL(path string) string {
	fullURL, err := appendPath(c.baseURL, path)
	if err != nil {
		fullURL = strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	}
	if c.azureVersion == "" {
		return fullURL
	}

	u, err := url.Parse(fullURL)
	if err != nil {
		return fullURL
	}
	values := u.Query()
	values.Set("api-version", c.azureVersion)
	u.RawQuery = values.Encode()
	return u.String()
}

// StreamReader reads streaming responses
type StreamReader struct {
	sseReader *httpclient.SSEReader
}

// Read reads the next chunk from the stream
// Returns io.EOF when the stream is done
func (r *StreamReader) Read() (*StreamChunk, error) {
	for {
		event, err := r.sseReader.Read()
		if err != nil {
			return nil, err
		}

		// Check for [DONE] marker
		if event.Data == "[DONE]" {
			return nil, io.EOF
		}

		// Skip empty data
		if event.Data == "" {
			continue
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return nil, fmt.Errorf("failed to unmarshal chunk: %w", err)
		}

		return &chunk, nil
	}
}

// Close closes the stream
func (r *StreamReader) Close() error {
	return r.sseReader.Close()
}

// CreateEmbedding sends an embedding request
func (c *Client) CreateEmbedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	headers := c.getHeaders()
	var response EmbeddingResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     c.buildURL("/embeddings"),
		Headers: headers,
		Body:    req,
	}, &response)

	if err != nil {
		return nil, err
	}

	return &response, nil
}

// ListModels retrieves the list of available models.
// Handles both standard OpenAI format {"object": "list", "data": [...]}
// and raw array format [...] returned by some providers like TogetherAI.
func (c *Client) ListModels(ctx context.Context) (*ModelsListResponse, error) {
	headers := c.getHeaders()

	resp, err := c.doRawRequest(ctx, http.MethodGet, c.buildURL("/models"), headers, nil)
	if err != nil {
		return nil, err
	}

	return c.parseModelsResponse(resp)
}

// ListModelsWithQuery retrieves models with optional query parameters.
// Query parameters are properly URL-encoded to handle special characters safely.
// Handles both standard OpenAI format and raw array format.
func (c *Client) ListModelsWithQuery(ctx context.Context, queryParams map[string]string) (*ModelsListResponse, error) {
	headers := c.getHeaders()

	requestURL := c.buildURL("/models")
	if len(queryParams) > 0 {
		u, err := url.Parse(requestURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse request URL: %w", err)
		}
		values := url.Values{}
		for k, existing := range u.Query() {
			for _, v := range existing {
				values.Add(k, v)
			}
		}
		for k, v := range queryParams {
			values.Add(k, v)
		}
		u.RawQuery = values.Encode()
		requestURL = u.String()
	}

	resp, err := c.doRawRequest(ctx, http.MethodGet, requestURL, headers, nil)
	if err != nil {
		return nil, err
	}

	return c.parseModelsResponse(resp)
}

// doRawRequest performs an HTTP request and returns the raw response body.
func (c *Client) doRawRequest(ctx context.Context, method, url string, headers map[string]string, body any) ([]byte, error) {
	return c.httpClient.DoRaw(ctx, httpclient.Request{
		Method:  method,
		URL:     url,
		Headers: headers,
		Body:    body,
	})
}

// parseModelsResponse handles both OpenAI format {"object": "list", "data": [...]}
// and raw array format [...] returned by some providers like TogetherAI.
func (c *Client) parseModelsResponse(data []byte) (*ModelsListResponse, error) {
	// Trim whitespace to check the first character
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	// Check if response is an array (starts with '[')
	if trimmed[0] == '[' {
		// Parse as raw array of models
		var models []ModelResponse
		if err := json.Unmarshal(data, &models); err != nil {
			return nil, fmt.Errorf("failed to decode response as array: %w", err)
		}
		return &ModelsListResponse{
			Object: "list",
			Data:   models,
		}, nil
	}

	// Parse as standard OpenAI format
	var response ModelsListResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

func appendPath(baseURL, suffix string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimRight(u.Path, "/")
	suffixPath := strings.TrimLeft(suffix, "/")
	if suffixPath == "" {
		return u.String(), nil
	}
	u.Path = path.Join(basePath, suffixPath)
	return u.String(), nil
}

func (c *Client) mediaURL(override, fallback string) string {
	if override == "" {
		override = fallback
	}
	return c.buildURL(override)
}

// CreateImage posts an image generation request and returns a wire response or transport error.
func (c *Client) CreateImage(ctx context.Context, req *ImageGenerationRequest) (*ImageResponse, error) {
	var out ImageResponse
	err := c.httpClient.DoJSON(ctx, httpclient.Request{Method: http.MethodPost, URL: c.mediaURL(c.mediaPaths.ImagesPath, "/images/generations"), Headers: c.getHeaders(), Body: req}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateImageStream returns image SSE events. Consumers must drain the channel
// or cancel ctx. Parsing and transport failures appear in the event's Err field.
func (c *Client) CreateImageStream(ctx context.Context, req *ImageGenerationRequest) (<-chan ImageStreamEvent, error) {
	copied := *req
	copied.Stream = true
	// Streaming must win over any non-streaming ExtraBody value.
	copied.ExtraBody = make(map[string]any, len(req.ExtraBody))
	for k, v := range req.ExtraBody {
		copied.ExtraBody[k] = v
	}
	copied.ExtraBody["stream"] = true
	return mediaStream(ctx, c, c.mediaURL(c.mediaPaths.ImagesPath, "/images/generations"), &copied,
		func(e *ImageStreamEvent, typ string, err error) bool {
			e.Err = err
			if e.Type == "" {
				e.Type = typ
			}
			return e.Type == "image_generation.completed" || e.Type == "image_edit.completed"
		})
}

// EditImage uploads inline images and an optional mask as multipart data.
// URL/FileID uploads and malformed inputs return a validation error.
func (c *Client) EditImage(ctx context.Context, req *ImageEditRequest) (*ImageResponse, error) {
	fields, files, err := multipartMediaFields(req, req.ExtraBody)
	if err != nil {
		return nil, err
	}
	if len(req.Images) == 0 || len(req.Images) > 16 {
		return nil, fmt.Errorf("image edits require 1-16 images")
	}
	for _, input := range req.Images {
		file, err := mediaUpload("image[]", input)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if req.Mask != nil {
		file, err := mediaUpload("mask", *req.Mask)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	var out ImageResponse
	err = c.httpClient.DoMultipart(ctx, http.MethodPost, c.mediaURL(c.mediaPaths.ImageEditsPath, "/images/edits"), fields, files, c.getHeaders(), &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSpeech returns audio bytes and the response content type, or a transport error.
func (c *Client) CreateSpeech(ctx context.Context, req *SpeechRequest) ([]byte, string, error) {
	out, err := c.httpClient.DoBinary(ctx, http.MethodPost, c.mediaURL(c.mediaPaths.SpeechPath, "/audio/speech"), req, c.getHeaders())
	if err != nil {
		return nil, "", err
	}
	return out.Data, out.ContentType, nil
}

// CreateSpeechStream returns SSE audio events, closing after done or [DONE].
// Consumers must drain the channel or cancel ctx. Audio remains base64 encoded.
func (c *Client) CreateSpeechStream(ctx context.Context, req *SpeechRequest) (<-chan SpeechStreamEvent, error) {
	copied := *req
	copied.StreamFormat = "sse"
	return mediaStream(ctx, c, c.mediaURL(c.mediaPaths.SpeechPath, "/audio/speech"), &copied,
		func(e *SpeechStreamEvent, typ string, err error) bool {
			e.Err = err
			if e.Type == "" {
				e.Type = typ
			}
			return e.Type == "speech.audio.done"
		})
}

func mediaStream[T any](ctx context.Context, c *Client, endpoint string, req any, finish func(*T, string, error) bool) (<-chan T, error) {
	body, err := c.httpClient.DoStream(ctx, httpclient.Request{Method: http.MethodPost, URL: endpoint, Headers: c.getHeaders(), Body: req})
	if err != nil {
		return nil, err
	}
	events := make(chan T, 8)
	go func() {
		defer close(events)
		reader := httpclient.NewSSEReader(body)
		defer func() { _ = reader.Close() }()
		for {
			event, err := reader.Read()
			if err == nil && event.Data == "[DONE]" {
				return
			}
			if errors.Is(err, io.EOF) {
				err = fmt.Errorf("media stream ended before completion: %w", errors.Join(io.ErrUnexpectedEOF, llms.ErrStreamInterrupted))
			}
			if err == nil && event.Data == "" {
				continue
			}
			var value T
			typ := ""
			if err == nil {
				typ = event.Event
				err = json.Unmarshal([]byte(event.Data), &value)
			}
			done := finish(&value, typ, err)
			select {
			case events <- value:
			case <-ctx.Done():
				return
			}
			if err != nil || done {
				return
			}
		}
	}()
	return events, nil
}

// CreateTranscription uploads audio and decodes the selected response format.
// Streaming transcription is unsupported. Uploads must contain at most 25 MB of inline data.
func (c *Client) CreateTranscription(ctx context.Context, req *TranscriptionRequest) (*TranscriptionResponse, error) {
	fields, files, err := multipartMediaFields(req, req.ExtraBody)
	if err != nil {
		return nil, err
	}
	if fields["stream"] == "true" {
		return nil, fmt.Errorf("streaming transcription is unsupported")
	}
	if fields["response_format"] == "" {
		fields["response_format"] = "json"
	}
	if err := validateTranscriptionFormat(fields["model"], fields["response_format"]); err != nil {
		return nil, err
	}
	file, err := mediaUpload("file", req.File)
	if err != nil {
		return nil, err
	}
	if len(file.Data) > 25*1024*1024 {
		return nil, fmt.Errorf("transcription file exceeds 25 MB")
	}
	files = append(files, file)
	var out TranscriptionResponse
	var raw []byte
	var target any = &out
	switch fields["response_format"] {
	case contentTypeText, "srt", "vtt":
		target = &raw
	}
	err = c.httpClient.DoMultipart(ctx, http.MethodPost, c.mediaURL(c.mediaPaths.TranscriptionsPath, "/audio/transcriptions"), fields, files, c.getHeaders(), target)
	if err != nil {
		return nil, err
	}
	if target == &raw {
		out.Text = string(raw)
	}
	return &out, nil
}

func multipartMediaFields(req any, extra map[string]any) (map[string]string, []httpclient.MultipartFile, error) {
	for key, value := range extra {
		switch value.(type) {
		case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		default:
			return nil, nil, fmt.Errorf("%w: multipart extra %q must be scalar", llms.ErrInvalidParameters, key)
		}
	}
	data, err := marshalMediaExtra(req, extra)
	if err != nil {
		return nil, nil, err
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, nil, err
	}
	fields := make(map[string]string, len(values))
	var repeated []httpclient.MultipartFile
	for key, value := range values {
		if array, ok := value.([]any); ok {
			for _, item := range array {
				// An empty filename makes this a regular form field, allowing repeated keys.
				repeated = append(repeated, httpclient.MultipartFile{Field: strings.TrimSuffix(key, "[]") + "[]", Data: []byte(fmt.Sprint(item))})
			}
		} else {
			fields[key] = fmt.Sprint(value)
		}
	}
	return fields, repeated, nil
}

func mediaUpload(field string, input llms.MediaInput) (httpclient.MultipartFile, error) {
	if err := input.Validate(); err != nil {
		return httpclient.MultipartFile{}, err
	}
	if len(input.Data) == 0 {
		return httpclient.MultipartFile{}, fmt.Errorf("multipart upload requires inline Data: %w", llms.ErrInvalidParameters)
	}
	extensions := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "audio/wav": ".wav", "audio/x-wav": ".wav", "audio/mpeg": ".mp3", "audio/mp3": ".mp3", "audio/mp4": ".m4a", "audio/flac": ".flac", "audio/ogg": ".ogg", "audio/webm": ".webm"}
	return httpclient.MultipartFile{Field: field, Filename: "upload" + extensions[input.MIMEType], ContentType: input.MIMEType, Data: input.Data}, nil
}

// CreateVideo submits a video job, returning its initial state or a transport error.
func (c *Client) CreateVideo(ctx context.Context, req *VideoCreateRequest) (*VideoObject, error) {
	var out VideoObject
	err := c.httpClient.DoJSON(ctx, httpclient.Request{Method: http.MethodPost, URL: c.mediaURL(c.mediaPaths.VideosPath, "/videos"), Headers: c.getHeaders(), Body: req}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) videoURL(id string, content bool) (string, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\?#%") {
		return "", fmt.Errorf("invalid video ID: %w", llms.ErrInvalidParameters)
	}
	endpoint, err := url.Parse(c.mediaURL(c.mediaPaths.VideosPath, "/videos"))
	if err != nil {
		return "", err
	}
	endpoint.Path = path.Join(endpoint.Path, id)
	if content {
		endpoint.Path = path.Join(endpoint.Path, "content")
		query := endpoint.Query()
		query.Set("variant", "video")
		endpoint.RawQuery = query.Encode()
	}
	return endpoint.String(), nil
}

// GetVideo retrieves the current job state. Invalid IDs fail before any request.
func (c *Client) GetVideo(ctx context.Context, id string) (*VideoObject, error) {
	endpoint, err := c.videoURL(id, false)
	if err != nil {
		return nil, err
	}
	var out VideoObject
	err = c.httpClient.DoJSON(ctx, httpclient.Request{Method: http.MethodGet, URL: endpoint, Headers: c.getHeaders()}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetVideoContent downloads the completed MP4 bytes or returns a transport error.
func (c *Client) GetVideoContent(ctx context.Context, id string) ([]byte, error) {
	endpoint, err := c.videoURL(id, true)
	if err != nil {
		return nil, err
	}
	out, err := c.httpClient.DoBinary(ctx, http.MethodGet, endpoint, nil, c.getHeaders())
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}
