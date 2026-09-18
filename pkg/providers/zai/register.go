package zai

import (
	"fmt"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func init() {
	llms.RegisterProvider("zai", func(cfg llms.Config) (llms.LLM, error) {
		opts := make([]Option, 0, 7)
		if cfg.APIKey != "" {
			opts = append(opts, WithAPIKey(cfg.APIKey))
		}
		coding, err := parseExtraEnabled(cfg.Extra[llms.ExtraZAICoding])
		if err != nil {
			return nil, err
		}
		if coding {
			opts = append(opts, WithUseCodingAPI())
		}
		if cfg.Model != "" {
			opts = append(opts, WithModel(cfg.Model))
		}
		if cfg.BaseURL != "" {
			opts = append(opts, WithBaseURL(cfg.BaseURL))
		}
		if cfg.Timeout != 0 {
			opts = append(opts, WithTimeout(cfg.Timeout))
		}
		if cfg.HTTPClient != nil {
			opts = append(opts, WithHTTPClient(cfg.HTTPClient))
		}
		if cfg.AllowPrivateIPs {
			opts = append(opts, WithAllowPrivateIPs())
		}
		if cfg.AllowHTTP {
			opts = append(opts, WithAllowHTTP())
		}
		return New(opts...)
	})
}

// parseExtraEnabled reads a boolean Config.Extra value. An unrecognized value
// is an error rather than a silent false: a user writing "on" or "enabled" to
// reach the Coding API would otherwise be routed to the standard endpoint with
// no signal that the setting was ignored.
func parseExtraEnabled(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return false, nil
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("zai: %s must be true or false, got %q: %w", llms.ExtraZAICoding, value, llms.ErrInvalidParameters)
	}
}
