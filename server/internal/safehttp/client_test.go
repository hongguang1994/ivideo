package safehttp

import (
	"net/netip"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "192.168.50.1", "169.254.169.254", "100.64.0.1", "::1", "fc00::1"} {
		if isPublicIP(netip.MustParseAddr(value)) {
			t.Fatalf("%s must not be considered public", value)
		}
	}
	if !isPublicIP(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public address should be accepted")
	}
}
