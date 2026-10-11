package ollama

import llms "github.com/nocturnium/llm-go-sdk/v6"

// Ollama serves a loopback endpoint by default, so this provider defaults
// AllowPrivateIPs and AllowHTTP to true. Config's boolean zero value cannot
// distinguish "unset" from "false", so Extra[llms.ExtraDisallowPrivateIPs]
// and Extra[llms.ExtraDisallowHTTP] exist to force the shared SSRF/HTTPS
// restrictions back on for deployments that need them.
func init() {
	llms.RegisterProvider("ollama", func(cfg llms.Config) (llms.LLM, error) {
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
		// Apply the zero-value-compatible allowances first, then the Extra
		// opt-outs last so an explicit disallow key wins over AllowPrivateIPs or
		// AllowHTTP being set.
		if cfg.AllowPrivateIPs {
			opts = append(opts, WithAllowPrivateIPs())
		}
		if cfg.AllowHTTP {
			opts = append(opts, WithAllowHTTP())
		}
		if _, ok := cfg.Extra[llms.ExtraDisallowPrivateIPs]; ok {
			opts = append(opts, WithDisallowPrivateIPs())
		}
		if _, ok := cfg.Extra[llms.ExtraDisallowHTTP]; ok {
			opts = append(opts, WithDisallowHTTP())
		}
		return New(opts...)
	})
}
