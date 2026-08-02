package runpod

import llms "github.com/nocturnium/llm-go-sdk/v6"

func init() {
	llms.RegisterProvider("runpod", func(cfg llms.Config) (llms.LLM, error) {
		opts := make([]Option, 0, 7)
		if cfg.APIKey != "" {
			opts = append(opts, WithAPIKey(cfg.APIKey))
		}
		id, err := cfg.RequireExtra("runpod", llms.ExtraRunPodEndpointID)
		if err != nil {
			return nil, err
		}
		opts = append(opts, WithEndpointID(id))
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
