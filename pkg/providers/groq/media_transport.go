package groq

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

func newMediaHTTP(o *options) *httpclient.Client {
	opts := []httpclient.ClientOption{httpclient.WithAllowHTTP(o.AllowHTTP), httpclient.WithAllowPrivateIPs(o.AllowPrivateIPs)}
	if o.HTTPClient != nil {
		opts = append(opts, httpclient.WithHTTPClient(o.HTTPClient))
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	opts = append(opts, httpclient.WithTimeout(timeout))
	return httpclient.NewClient(opts...)
}

func (c *Client) mediaEndpoint(route string) string {
	endpoint, err := url.JoinPath(c.options.BaseURL, route)
	if err != nil {
		return c.options.BaseURL
	}
	return endpoint
}

func (c *Client) mediaHeaders() map[string]string {
	return map[string]string{"Authorization": "Bearer " + c.options.APIKey}
}

func (c *Client) mediaError(err error) error {
	if err == nil {
		return nil
	}
	mapped := openaicompat.WrapError(c.Provider(), "media", err)

	return fmt.Errorf("groq: %w", errors.Join(mapped, err))
}
