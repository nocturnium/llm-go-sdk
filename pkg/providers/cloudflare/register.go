package cloudflare

import llms "github.com/nocturnium/llm-go-sdk/v6"

func init() {
	llms.RegisterProvider("cloudflare", func(cfg llms.Config) (llms.LLM, error) {
		opts := make([]Option, 0, 9)
		if cfg.APIKey != "" {
			opts = append(opts, WithAPIKey(cfg.APIKey))
		}
		if account := cfg.Extra[llms.ExtraCloudflareAccountID]; account != "" {
			opts = append(opts, WithAccountID(account))
		}
		if cfg.Model != "" {
			opts = append(opts, WithModel(cfg.Model))
		}
		if embed := cfg.Extra[llms.ExtraEmbeddingModel]; embed != "" {
			opts = append(opts, WithEmbeddingModel(embed))
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
