package elevenlabs

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// Client is a native media-only ElevenLabs client, safe for concurrent use.
type Client struct {
	transport *httpclient.Client
	options   *options
	base      *url.URL
	headers   map[string]string
}

// New constructs a client. Missing credentials return ErrMissingAPIKey;
// invalid base URLs, voices or settings return ErrInvalidParameters.
func New(opts ...Option) (*Client, error) {
	o := apply(opts...)
	if o.Timeout <= 0 {
		o.Timeout = defaultOptions().Timeout
	}
	key, err := llms.RequireAPIKey("elevenlabs", o.APIKey, llms.EnvElevenLabsAPIKey)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(o.BaseURL)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, invalid("invalid base URL")
	}
	if err = validateID(o.Voice); err != nil {
		return nil, err
	}
	if err = validateSettings(o.VoiceSettings); err != nil {
		return nil, err
	}
	transportOpts := []httpclient.ClientOption{}
	if o.HTTPClient != nil {
		transportOpts = append(transportOpts, httpclient.WithHTTPClient(o.HTTPClient))
	}
	transportOpts = append(transportOpts, httpclient.WithTimeout(o.Timeout), httpclient.WithAllowPrivateIPs(o.AllowPrivateIPs), httpclient.WithAllowHTTP(o.AllowHTTP))
	return &Client{options: o, base: base, headers: map[string]string{"xi-api-key": key}, transport: httpclient.NewClient(transportOpts...)}, nil
}

// Provider returns ElevenLabs' identifier.
func (c *Client) Provider() llms.Provider { return llms.ProviderElevenLabs }

// Model returns the default speech model.
func (c *Client) Model() string { return c.options.Model }

// Capabilities reports provider-level media support; individual model support varies.
func (c *Client) Capabilities() llms.Capabilities {
	return llms.Capabilities{Speech: true, Transcription: true, ImageGeneration: true, VideoGeneration: true}
}
func (c *Client) endpoint(route string, q url.Values) string {
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/" + route
	u.RawQuery = q.Encode()
	return u.String()
}
func (c *Client) request(ctx context.Context, method, route string, body, out any) (err error) {
	model := ""
	if fields, ok := body.(map[string]any); ok {
		model, _ = fields["model_id"].(string)
	}
	ctx, finish := c.startOperation(ctx, "request", model)
	defer func() { finish(err) }()
	return WrapError("request", c.transport.DoJSON(ctx, httpclient.Request{Method: method, URL: c.endpoint(route, nil), Body: body, Headers: c.headers}, out))
}
func invalid(message string) error {
	return fmt.Errorf("elevenlabs: %s: %w", message, llms.ErrInvalidParameters)
}
func validateID(id string) error {
	if id == "" || len(id) > 256 || strings.Trim(id, ".") == "" {
		return invalid("invalid voice or generation ID")
	}
	for _, r := range id {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
		if !valid {
			return invalid("invalid voice or generation ID")
		}
	}
	return nil
}

var (
	_ llms.SpeechSynthesizer = (*Client)(nil)
	_ llms.Transcriber       = (*Client)(nil)
	_ llms.ImageGenerator    = (*Client)(nil)
	_ llms.ImageEditor       = (*Client)(nil)
	_ llms.VideoGenerator    = (*Client)(nil)
	_ llms.ModelLister       = (*Client)(nil)
)

func (c *Client) startOperation(ctx context.Context, operation, model string) (context.Context, func(error)) {
	ctx, span := otel.Tracer("llms").Start(ctx, "elevenlabs."+operation)
	span.SetAttributes(attribute.String("llm.provider", "elevenlabs"), attribute.String("llm.model", model), attribute.String("llm.operation", operation))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}
