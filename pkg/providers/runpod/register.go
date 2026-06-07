package runpod

import llms "github.com/nocturnium/llm-go-sdk"

func init() {
	llms.RegisterProvider("runpod", func(cfg llms.Config) (llms.LLM, error) {
		opts := make([]Option, 0, 7)
		if cfg.APIKey != "" {
			opts = append(opts, WithAPIKey(cfg.APIKey))
		}
		if cfg.Extra != nil && cfg.Extra["endpoint_id"] != "" {
			opts = append(opts, WithEndpointID(cfg.Extra["endpoint_id"]))
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
