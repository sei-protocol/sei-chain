package admin_test

import (
	"net"
	"testing"

	"github.com/sei-protocol/sei-chain/admin"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// FuzzReadConfigAddress pins the two behaviors layered on admin_address.
//
// First, an empty string is treated as absent: the in-code default 127.0.0.1:9095
// survives rather than the server binding to ":" — so a blanked-out app.toml entry
// is a no-op, not a wildcard bind. Second, the loopback validation runs only when
// the server is enabled, and it requires a literal IP: a hostname is rejected
// even if it would resolve to loopback, because a name is resolved at dial time
// and cannot be checked here. Both directions matter, since the whole point of
// the key is that the admin gRPC service — which can change log levels on a
// running validator — is never reachable off-box.
func FuzzReadConfigAddress(f *testing.F) {
	f.Add(true, "127.0.0.1:9095")
	f.Add(true, "")
	f.Add(false, "")
	f.Add(true, "[::1]:9095")
	f.Add(true, "0.0.0.0:9095")
	f.Add(false, "0.0.0.0:9095")
	f.Add(true, "localhost:9095")
	f.Add(true, "127.0.0.1")
	f.Add(true, ":9095")
	f.Add(true, "127.0.0.1:9095:extra")

	f.Fuzz(func(t *testing.T, enabled bool, address string) {
		cfg, err := admin.ReadConfig(configtest.AppOpts{
			"admin_server.admin_enabled": enabled,
			"admin_server.admin_address": address,
		})

		wantAddress := address
		if address == "" {
			wantAddress = admin.DefaultAddress
		}
		if cfg.Address != wantAddress {
			t.Fatalf("admin_address = %q resolved to %q, want %q", address, cfg.Address, wantAddress)
		}

		if !enabled {
			if err != nil {
				t.Fatalf("admin_address = %q must not be validated while the server is disabled, got %v", address, err)
			}
			return
		}

		wantErr := !isLiteralLoopbackHostPort(wantAddress)
		if wantErr && err == nil {
			t.Fatalf("admin_address = %q is not a literal loopback host:port and must be rejected when enabled", wantAddress)
		}
		if !wantErr && err != nil {
			t.Fatalf("admin_address = %q is a literal loopback host:port and must be accepted, got %v", wantAddress, err)
		}
	})
}

// isLiteralLoopbackHostPort restates the rule ReadConfig enforces, independently
// of how it enforces it: splittable into host and port, the host parses as an IP
// literal, and that IP is loopback.
func isLiteralLoopbackHostPort(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline: no
// [admin_server] section means the server is off at the loopback default. Both
// sides move together when a default changes, so this asserts the reader's
// behavior rather than the values themselves.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	cfg, err := admin.ReadConfig(configtest.AppOpts{})
	if err != nil {
		t.Fatalf("an absent [admin_server] section must read cleanly, got %v", err)
	}
	if cfg != admin.DefaultConfig {
		t.Fatalf("an absent [admin_server] section resolved to %+v, want the declared defaults %+v",
			cfg, admin.DefaultConfig)
	}
}
