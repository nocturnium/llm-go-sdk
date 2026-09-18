package llamacpp

import llms "github.com/nocturnium/llm-go-sdk/v6"

// Nothing here can turn the SSRF relaxations off: llamacpp serves a loopback
// endpoint by default, so this provider defaults AllowPrivateIPs and AllowHTTP
// to true, a Config leaving them at their zero value is indistinguishable from
// one setting them false, and New has no option that clears either flag. A
// deployment that needs private-IP and plain-HTTP requests refused has to wrap
// the client's transport itself.
func init() {
	llms.RegisterProvider("llamacpp", func(cfg llms.Config) (llms.LLM, error) {
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
