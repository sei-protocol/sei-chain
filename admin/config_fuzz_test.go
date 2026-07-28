package admin_test

import (
	"net"
	"testing"

	"github.com/sei-protocol/sei-chain/admin"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
)

// adminKeys covers the one [admin_server] key whose resolution is a plain
// guarded cast. admin_address is not: it carries a second guard that treats the
// empty string as absent, and a conditional loopback validation, so it gets its
// own target below.
var adminKeys = []configtest.KeySpec{
	{
		Key: "admin_server.admin_enabled", Path: "Enabled", Cast: configtest.CastBool,
		Why: "default false; the read is guarded but unchecked, so a malformed value is silently false",
	},
}

func readAdmin(opts configtest.AppOpts) (any, error) { return admin.ReadConfig(opts) }

// FuzzReadConfigEnabled pins admin_enabled. The read is guarded but *unchecked*
// (cast.ToBool, not ToBoolE), so a value that will not convert resolves to false
// with no error. That is the safe direction for this particular knob — a
// malformed value leaves the admin server off rather than on — which is why it is
// pinned rather than reported as a defect.
func FuzzReadConfigEnabled(f *testing.F) {
	f.Add(uint8(2), "true", int64(1), true)
	f.Add(uint8(8), "false", int64(0), false)
	f.Add(uint8(1), "yes-please", int64(0), false)
	f.Add(uint8(0), "", int64(0), false)
	f.Add(uint8(11), "", int64(0), false)

	f.Fuzz(func(t *testing.T, kind uint8, s string, n int64, b bool) {
		spec := adminKeys[0]
		configtest.CheckRow(t, "admin_server", readAdmin, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

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
// [admin_server] section means the server is off at the loopback default.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	configtest.CheckAbsent(t, "admin_server", readAdmin, admin.DefaultConfig)
}

// TestDefaultsMatchTheRecordedValues pins the admin_server defaults themselves.
//
// The absent-keys row above proves the reader returns the declared defaults; it cannot prove
// which values those are, because both sides of that comparison come from this package. This
// compares them against testdata/admin_server.golden, an independent recording, so a default that
// moves shows the new value in a diff instead of passing silently.
func TestDefaultsMatchTheRecordedValues(t *testing.T) {
	configtest.CheckDefaults(t, "admin_server", admin.DefaultConfig)
}
