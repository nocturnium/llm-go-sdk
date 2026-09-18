package ollama

import llms "github.com/nocturnium/llm-go-sdk/v6"

// The registry cannot turn the SSRF relaxations off: ollama serves a loopback
// endpoint by default, so this provider defaults AllowPrivateIPs and AllowHTTP
// to true and a Config leaving them at their zero value is indistinguishable
// from one setting them false. Construct the provider with New and no relaxing
// option to run it against a remote host under the strict defaults.
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
		if cfg.AllowPrivateIPs {
			opts = append(opts, WithAllowPrivateIPs())
		}
		if cfg.AllowHTTP {
			opts = append(opts, WithAllowHTTP())
		}
		return New(opts...)
	})
}
