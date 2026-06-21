package zai

import (
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

func init() {
	llms.RegisterProvider("zai", func(cfg llms.Config) (llms.LLM, error) {
		opts := make([]Option, 0, 7)
		if cfg.APIKey != "" {
			opts = append(opts, WithAPIKey(cfg.APIKey))
		}
		if isExtraEnabled(cfg.Extra[llms.ExtraZAICoding]) {
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
			opts = append(opts, WithAllowPrivateIPs(), WithAllowHTTP())
		}
		return New(opts...)
	})
}

func isExtraEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}
