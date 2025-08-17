//go:build unit
// +build unit

package device

import (
	"strings"
	"testing"
	"unicode"

	config "github/setera/pkg"
)

func TestFirstIP_IPv4_Normal(t *testing.T) {
	n := mustCIDR(t, "10.0.0.0/24")
	got, err := FirstIP(n)
	if err != nil {
		t.Fatalf("FirstIP error: %v", err)
	}
	if got.String() != "10.0.0.1/24" {
		t.Fatalf("want 10.0.0.1/24, got %s", got)
	}
}

func TestFirstIP_IPv6_Normal(t *testing.T) {
	n := mustCIDR(t, "2001:db8::/64")
	got, err := FirstIP(n)
	if err != nil {
		t.Fatalf("FirstIP error: %v", err)
	}
	if got.String() != "2001:db8::1/64" {
		t.Fatalf("want 2001:db8::1/64, got %s", got)
	}
}

func TestFirstIP_NonNetworkInput_IsMasked(t *testing.T) {
	// Provide an IP that isn't the network address; FirstIP should mask first.
	n := mustCIDR(t, "10.1.2.3/16") // network is 10.1.0.0/16
	got, err := FirstIP(n)
	if err != nil {
		t.Fatalf("FirstIP error: %v", err)
	}
	if got.String() != "10.1.0.1/16" {
		t.Fatalf("want 10.1.0.1/16, got %s", got)
	}
}

func TestFirstIP_Err_OnFullMaskV4(t *testing.T) {
	n := mustCIDR(t, "10.0.0.1/32") // increment escapes network
	if _, err := FirstIP(n); err == nil {
		t.Fatalf("expected error for /32, got nil")
	}
}

func TestFirstIP_Err_OnFullMaskV6(t *testing.T) {
	n := mustCIDR(t, "2001:db8::1/128")
	if _, err := FirstIP(n); err == nil {
		t.Fatalf("expected error for /128, got nil")
	}
}

func TestHostIP_IPv4(t *testing.T) {
	n := mustCIDR(t, "10.9.8.7/20")
	got, err := HostIP(n)
	if err != nil {
		t.Fatalf("HostIP error: %v", err)
	}
	if got.String() != "10.9.0.0/20" {
		t.Fatalf("want 10.9.0.0/20, got %s", got)
	}
}

func TestHostIP_IPv6(t *testing.T) {
	n := mustCIDR(t, "2001:db8::1234/64")
	got, err := HostIP(n)
	if err != nil {
		t.Fatalf("HostIP error: %v", err)
	}
	if got.String() != "2001:db8::/64" {
		t.Fatalf("want 2001:db8::/64, got %s", got)
	}
}

func TestGenerateDeviceName_LengthAndPrefix(t *testing.T) {
	res, err := GenerateDeviceName("br-", "tenant-abc")
	if err != nil {
		t.Fatalf("GenerateDeviceName error: %v", err)
	}
	if !strings.HasPrefix(res, "br-") {
		t.Fatalf("expected prefix br-, got %q", res)
	}
	if len(res) != config.MaxDeviceNameLength {
		t.Fatalf("want len=%d, got %d (%q)", config.MaxDeviceNameLength, len(res), res)
	}
	suffix := res[len("br-"):]
	if len(suffix) != config.MaxDeviceNameLength-len("br-") {
		t.Fatalf("suffix length mismatch")
	}
	// suffix should be lowercase hex (sha1 hex encoding)
	for _, r := range suffix {
		if !(unicode.IsDigit(r) || (r >= 'a' && r <= 'f')) {
			t.Fatalf("suffix has non-hex char %q in %q", r, suffix)
		}
	}
}

func TestGenerateDeviceName_Deterministic(t *testing.T) {
	a, _ := GenerateDeviceName("vx-", "tenant-xyz")
	b, _ := GenerateDeviceName("vx-", "tenant-xyz")
	if a != b {
		t.Fatalf("determinism failed: %q vs %q", a, b)
	}
}

func TestGenerateDeviceName_DifferentInputsDiffer(t *testing.T) {
	a, _ := GenerateDeviceName("vx-", "t-1")
	b, _ := GenerateDeviceName("vx-", "t-2")
	if a == b {
		t.Fatalf("different names produced same output: %q", a)
	}
}

func TestGenerateDeviceName_ErrorWhenPrefixTooLong(t *testing.T) {
	longPrefix := strings.Repeat("x", config.MaxDeviceNameLength+1)
	if _, err := GenerateDeviceName(longPrefix, "name"); err == nil {
		t.Fatalf("expected error for prefix>MaxDeviceNameLength")
	}
}

func TestGenerateDeviceName_PrefixEqualMax_OK(t *testing.T) {
	prefix := strings.Repeat("p", config.MaxDeviceNameLength)
	got, err := GenerateDeviceName(prefix, "ignored")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With zero hash room, result equals prefix.
	if got != prefix {
		t.Fatalf("want exact prefix back, got %q", got)
	}
}
