package llms

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config contains common provider construction settings.
//
// Extra carries provider-specific construction parameters that do not have a
// common SDK field. Recognized keys:
//   - runpod: ExtraRunPodEndpointID sets the required serverless endpoint ID.
//   - zai: ExtraZAICoding set to "true", "1", or "yes" enables the Coding API.
type Config struct {
	APIKey  string
	Model   string
	BaseURL string
	Timeout time.Duration
	// AllowPrivateIPs permits requests to private, loopback, and link-local
	// addresses (e.g. a self-hosted model on a private network). It relaxes only
	// the destination-host SSRF check and does NOT permit plain HTTP — set
	// AllowHTTP for that. Default false (secure).
	AllowPrivateIPs bool
	// AllowHTTP permits plain-HTTP (non-TLS) request URLs. It is independent of
	// AllowPrivateIPs: enabling private-IP access no longer implies cleartext
	// HTTP, so an API key cannot leak over an http:// URL (a BaseURL typo or a
	// downgrade redirect) unless AllowHTTP is set explicitly. Default false
	// (HTTPS enforced).
	AllowHTTP  bool
	HTTPClient *http.Client
	Extra      map[string]string
}

// Recognized Config.Extra keys for provider-specific construction settings.
const (
	// ExtraRunPodEndpointID is the Config.Extra key for the required RunPod serverless endpoint ID.
	ExtraRunPodEndpointID = "endpoint_id"

	// ExtraZAICoding is the Config.Extra key for enabling the optional Z.AI Coding API endpoint.
	ExtraZAICoding = "coding"
)

// RequireExtra returns the non-blank Config.Extra value for key.
//
// If the key is absent or blank, RequireExtra returns an error that names both
// the provider and key and wraps ErrInvalidParameters.
func (c Config) RequireExtra(provider, key string) (string, error) {
	value := c.Extra[key]
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("llms: provider %q: missing required config key %q: %w", provider, key, ErrInvalidParameters)
	}
	return value, nil
}

// ProviderFactory constructs an LLM provider from common configuration.
type ProviderFactory func(Config) (LLM, error)

var providerRegistry = struct {
	sync.RWMutex
	factories map[string]ProviderFactory
}{
	factories: make(map[string]ProviderFactory),
}

// RegisterProvider registers a provider factory by name.
//
// Names are case-insensitive. Registering the same name more than once
// overwrites the previous factory.
func RegisterProvider(name string, factory ProviderFactory) {
	key := normalizeProviderName(name)
	if key == "" || factory == nil {
		return
	}

	providerRegistry.Lock()
	defer providerRegistry.Unlock()
	providerRegistry.factories[key] = factory
}

// New constructs a registered provider by name.
//
// The name lookup is case-insensitive. It returns an error when the provider
// has not been registered, including the current registered provider list.
// Prefer direct provider constructors such as openai.New or anthropic.New when
// the provider is known at compile time. Use pkg/openaicompat.NewClient plus
// pkg/openaicompat.NewBaseProvider when authoring a new OpenAI-compatible
// provider, then register it here if callers should construct it by name.
func New(name string, cfg Config) (LLM, error) {
	key := normalizeProviderName(name)

	providerRegistry.RLock()
	factory, ok := providerRegistry.factories[key]
	providerRegistry.RUnlock()
	if !ok {
		return nil, fmt.Errorf("llms: unknown provider %q (registered providers: %s)", name, strings.Join(RegisteredProviders(), ", "))
	}

	return factory(cfg)
}

// NewFromEnv constructs a provider from environment variables.
//
// LLM_PROVIDER is required. LLM_MODEL is optional. Provider-specific API key
// environment variables are resolved by the registered provider constructor.
func NewFromEnv() (LLM, error) {
	provider := strings.TrimSpace(os.Getenv("LLM_PROVIDER"))
	if provider == "" {
		return nil, fmt.Errorf("llms: LLM_PROVIDER is required")
	}

	return New(provider, Config{
		Model: os.Getenv("LLM_MODEL"),
	})
}

// RegisteredProviders returns the sorted names of registered providers.
func RegisteredProviders() []string {
	providerRegistry.RLock()
	defer providerRegistry.RUnlock()

	names := make([]string, 0, len(providerRegistry.factories))
	for name := range providerRegistry.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
