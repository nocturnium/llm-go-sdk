package httpclient

import (
	"net"
	"testing"
)

// TestValidateNotPrivateIP_ReservedBlocks pins the address classes that reach
// infrastructure without being private by Go's definition. Each of these used
// to pass validation.
func TestValidateNotPrivateIP_ReservedBlocks(t *testing.T) {
	refused := []string{
		"0.0.0.1",        // this-network
		"192.0.0.1",      // IETF protocol assignment
		"198.18.0.1",     // benchmarking
		"240.0.0.1",      // reserved
		"fec0::1",        // site-local IPv6
		"2002:c0a8:1::1", // 6to4
		"224.0.0.1",      // multicast
		"ff02::1",        // interface-local multicast
	}
	for _, addr := range refused {
		if err := validateNotPrivateIP(net.ParseIP(addr)); err == nil {
			t.Errorf("%s passed validation", addr)
		}
	}

	allowed := []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"}
	for _, addr := range allowed {
		if err := validateNotPrivateIP(net.ParseIP(addr)); err != nil {
			t.Errorf("%s was refused: %v", addr, err)
		}
	}
}
