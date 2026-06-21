package httpclient

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	schemeHTTP = "http"
)

var nat64WellKnownPrefix = net.IPNet{
	IP:   net.ParseIP("64:ff9b::"),
	Mask: net.CIDRMask(96, 128),
}

// DefaultAllowedHosts contains the default list of allowed API hosts.
var DefaultAllowedHosts = []string{
	"api.openai.com",
	"api.anthropic.com",
	"generativelanguage.googleapis.com",
	"api.together.xyz",
	"api.featherless.ai",
}

// URLValidationOptions configures URL validation behavior
type URLValidationOptions struct {
	// AllowedHosts is a list of allowed hostnames (empty allows all non-private)
	AllowedHosts []string

	// AllowPrivateIPs allows requests to private/internal IP addresses
	AllowPrivateIPs bool

	// AllowHTTP allows non-HTTPS URLs (not recommended)
	AllowHTTP bool

	// AllowCustomPorts allows non-standard ports (not 443 for HTTPS, 80 for HTTP)
	AllowCustomPorts bool
}

// DefaultURLValidationOptions returns secure default validation options
func DefaultURLValidationOptions() *URLValidationOptions {
	return &URLValidationOptions{
		AllowedHosts:     nil, // Allow any non-private host by default
		AllowPrivateIPs:  false,
		AllowHTTP:        false,
		AllowCustomPorts: true, // Allow custom ports for testing
	}
}

// StrictURLValidationOptions returns strict validation options with only known API hosts
func StrictURLValidationOptions() *URLValidationOptions {
	return &URLValidationOptions{
		AllowedHosts:     DefaultAllowedHosts,
		AllowPrivateIPs:  false,
		AllowHTTP:        false,
		AllowCustomPorts: false,
	}
}

// ValidateURL checks if a URL is safe to use for API requests.
// It returns an error if the URL is invalid, uses an unsafe scheme,
// or points to a private/internal address.
func ValidateURL(rawURL string, opts *URLValidationOptions) error {
	if opts == nil {
		opts = DefaultURLValidationOptions()
	}

	if rawURL == "" {
		return errors.New("URL cannot be empty")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Check scheme
	if u.Scheme == "" {
		return errors.New("URL must have a scheme (https://)")
	}

	if u.Scheme != "https" {
		if u.Scheme == schemeHTTP && !opts.AllowHTTP {
			return errors.New("URL must use HTTPS (HTTP not allowed)")
		}
		if u.Scheme != schemeHTTP {
			return fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
		}
	}

	// Check host
	host := u.Hostname()
	if host == "" {
		return errors.New("URL must have a host")
	}

	// Check port
	if !opts.AllowCustomPorts {
		port := u.Port()
		if port != "" {
			expectedPort := "443"
			if u.Scheme == "http" {
				expectedPort = "80"
			}
			if port != expectedPort {
				return fmt.Errorf("custom ports not allowed (port %s)", port)
			}
		}
	}

	// Check against allowed hosts if specified
	if len(opts.AllowedHosts) > 0 {
		allowed := false
		for _, allowedHost := range opts.AllowedHosts {
			if strings.EqualFold(host, allowedHost) {
				allowed = true
				break
			}
			// Allow subdomains of allowed hosts
			if strings.HasSuffix(strings.ToLower(host), "."+strings.ToLower(allowedHost)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("host %q not in allowed list", host)
		}
	}

	// Check for private/internal IPs
	if !opts.AllowPrivateIPs {
		if err := validateNotPrivateHost(host); err != nil {
			return err
		}
	}

	return nil
}

// validateNotPrivateHost checks if a host resolves to a private or internal IP
func validateNotPrivateHost(host string) error {
	// Check if it's a direct IP address
	if ip := net.ParseIP(host); ip != nil {
		return validateNotPrivateIP(ip)
	}
	if ip := parseIPv4Literal(host); ip != nil {
		return validateNotPrivateIP(ip)
	}

	// Check for localhost variations
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".localhost") {
		return errors.New("localhost not allowed")
	}

	// Check for common internal hostnames
	if strings.HasSuffix(lowerHost, ".local") ||
		strings.HasSuffix(lowerHost, ".internal") ||
		strings.HasSuffix(lowerHost, ".localdomain") {
		return fmt.Errorf("internal hostname not allowed: %s", host)
	}

	// Note: We don't resolve DNS here to check the IP because:
	// 1. It adds latency to every request
	// 2. DNS can change between validation and request (TOCTOU)
	// 3. The HTTP client should be configured to not follow redirects to private IPs

	return nil
}

func validateNotPrivateHostFromAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	return validateNotPrivateHost(host)
}

func parseIPv4Literal(host string) net.IP {
	parts := strings.Split(host, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return nil
	}
	values := make([]uint64, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil
		}
		value, err := strconv.ParseUint(part, 0, 32)
		if err != nil {
			return nil
		}
		values[i] = value
	}

	var ip uint64
	switch len(values) {
	case 1:
		if values[0] > 0xffffffff {
			return nil
		}
		ip = values[0]
	case 2:
		if values[0] > 0xff || values[1] > 0xffffff {
			return nil
		}
		ip = values[0]<<24 | values[1]
	case 3:
		if values[0] > 0xff || values[1] > 0xff || values[2] > 0xffff {
			return nil
		}
		ip = values[0]<<24 | values[1]<<16 | values[2]
	case 4:
		for _, value := range values {
			if value > 0xff {
				return nil
			}
		}
		ip = values[0]<<24 | values[1]<<16 | values[2]<<8 | values[3]
	}

	return net.IPv4(byte(ip>>24), byte(ip>>16), byte(ip>>8), byte(ip))
}

// validateNotPrivateIP checks if an IP is private, loopback, or otherwise internal
func validateNotPrivateIP(ip net.IP) error {
	if ip == nil {
		return errors.New("invalid IP address")
	}

	if ip.IsLoopback() {
		return errors.New("loopback addresses not allowed")
	}

	if ip.IsPrivate() {
		return errors.New("private IP addresses not allowed")
	}

	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return errors.New("link-local addresses not allowed")
	}

	if ip.IsUnspecified() {
		return errors.New("unspecified addresses not allowed")
	}

	if ip4 := nat64EmbeddedIPv4(ip); ip4 != nil {
		if err := validateNotPrivateIP(ip4); err != nil {
			return fmt.Errorf("NAT64-embedded IPv4 address not allowed: %w", err)
		}
	}

	// Check for IPv6-mapped IPv4 addresses that are private
	if ip4 := ip.To4(); ip4 != nil {
		if ip4.IsLoopback() || ip4.IsPrivate() {
			return errors.New("private/loopback IPv4 addresses not allowed")
		}
	}

	// Check for common internal IP ranges not covered by IsPrivate
	// 100.64.0.0/10 (Carrier-grade NAT)
	cgnat := net.IPNet{
		IP:   net.ParseIP("100.64.0.0"),
		Mask: net.CIDRMask(10, 32),
	}
	if cgnat.Contains(ip) {
		return errors.New("carrier-grade NAT addresses not allowed")
	}

	return nil
}

func nat64EmbeddedIPv4(ip net.IP) net.IP {
	ip16 := ip.To16()
	if ip16 == nil || ip.To4() != nil {
		return nil
	}
	if !nat64WellKnownPrefix.Contains(ip16) {
		return nil
	}
	return net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15])
}

// SanitizeModelName removes potentially dangerous characters from model names
// that could be used in URL path traversal attacks
func SanitizeModelName(model string) string {
	// Remove path traversal sequences
	model = strings.ReplaceAll(model, "..", "")
	model = strings.ReplaceAll(model, "/", "-")
	model = strings.ReplaceAll(model, "\\", "-")

	// Remove null bytes and other control characters
	var sanitized strings.Builder
	for _, r := range model {
		if r >= 32 && r != 127 { // Printable ASCII and valid UTF-8
			sanitized.WriteRune(r)
		}
	}

	return sanitized.String()
}
