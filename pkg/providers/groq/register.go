package groq

import llms "github.com/nocturnium/llm-go-sdk/v3"

func init() {
	llms.RegisterProvider("groq", func(cfg llms.Config) (llms.LLM, error) {
		opts := make([]Option, 0, 6)
		if cfg.APIKey != "" {
			opts = append(opts, WithAPIKey(cfg.APIKey))
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
