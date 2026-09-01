package config

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
)

// FuzzReceiptBackend pins the backend allowlist, which is fail-closed by design:
// only pebbledb, pebble and littidx boot, and anything else is a startup error
// rather than a fallback. Opening a receipt store on the wrong engine would read
// an empty database and serve empty eth_getTransactionReceipt responses on a node
// that otherwise looks healthy, so silently defaulting is the one behavior this
// key must never have.
//
// Case and surrounding whitespace are normalized away before the allowlist check,
// so "  PebbleDB " is accepted and resolves lower-cased.
func FuzzReceiptBackend(f *testing.F) {
	f.Add("pebbledb")
	f.Add("pebble")
	f.Add("littidx")
	f.Add("PebbleDB")
	f.Add("  littidx  ")
	f.Add("rocksdb")
	f.Add("")
	f.Add("pebbledb littidx")

	f.Fuzz(func(t *testing.T, raw string) {
		cfg, err := ReadReceiptConfig(configtest.AppOpts{"receipt-store.rs-backend": raw})

		normalized := strings.ToLower(strings.TrimSpace(raw))
		allowed := normalized == "pebbledb" || normalized == "pebble" || normalized == "littidx"

		if !allowed {
			if err == nil {
				t.Fatalf("receipt-store.rs-backend = %q is not on the allowlist and must fail the boot", raw)
			}
			if !strings.Contains(err.Error(), "unsupported receipt-store backend") {
				t.Fatalf("receipt-store.rs-backend = %q rejected with an unhelpful error: %v", raw, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("receipt-store.rs-backend = %q is allowed and must be accepted, got %v", raw, err)
		}
		if cfg.Backend != normalized {
			t.Fatalf("receipt-store.rs-backend = %q resolved to %q, want the normalized %q", raw, cfg.Backend, normalized)
		}
	})
}

// FuzzReceiptDBDirectory pins the trim applied to the directory. An entry of only
// whitespace must resolve to the empty string, because empty is the sentinel the
// app layer replaces with the derived per-backend path — whereas a literal " "
// would be taken as a real relative directory and put the receipt database
// somewhere nobody intended.
func FuzzReceiptDBDirectory(f *testing.F) {
	f.Add("/var/lib/sei/receipt")
	f.Add("")
	f.Add("   ")
	f.Add("\t/tmp/receipt\n")

	f.Fuzz(func(t *testing.T, raw string) {
		cfg, err := ReadReceiptConfig(configtest.AppOpts{"receipt-store.db-directory": raw})
		if err != nil {
			t.Fatalf("receipt-store.db-directory = %q must be accepted, got %v", raw, err)
		}
		if want := strings.TrimSpace(raw); cfg.DBDirectory != want {
			t.Fatalf("receipt-store.db-directory = %q resolved to %q, want %q", raw, cfg.DBDirectory, want)
		}
	})
}

// FuzzReceiptMisnamedBackendKey pins the fail-closed detector for
// receipt-store.backend, the retired spelling of rs-backend. Its presence at all —
// whatever it holds, and even alongside a valid rs-backend — is a hard boot error,
// so an operator whose app.toml still carries it is told rather than silently
// running the default engine.
func FuzzReceiptMisnamedBackendKey(f *testing.F) {
	f.Add(fuzzing.KindString, "pebbledb", int64(0), false)
	f.Add(fuzzing.KindNil, "", int64(0), false)
	f.Add(fuzzing.KindBool, "", int64(0), true)
	f.Add(fuzzing.KindInt64, "", int64(0), false)

	f.Fuzz(func(t *testing.T, kind uint8, s string, n int64, b bool) {
		value := fuzzing.ConfigValue(kind, s, n, b)
		opts := configtest.AppOpts{
			"receipt-store.backend":    value,
			"receipt-store.rs-backend": "pebbledb",
		}
		_, err := ReadReceiptConfig(opts)

		// A nil value is indistinguishable from an absent key, so only a non-nil
		// value trips the detector.
		if value == nil {
			if err != nil {
				t.Fatalf("a nil receipt-store.backend is indistinguishable from absent and must not fail: %v", err)
			}
			return
		}
		if err == nil {
			t.Fatalf("receipt-store.backend = %#v is the retired spelling and must fail the boot", value)
		}
		if !strings.Contains(err.Error(), "unsupported receipt-store config key") {
			t.Fatalf("receipt-store.backend = %#v rejected with an unhelpful error: %v", value, err)
		}
	})
}

// TestReadReceiptConfigAbsentKeysKeepDefaults pins the section baseline: no
// [receipt-store] section means the store boots on the declared defaults rather
// than the zero value, where an async write buffer of 0 would silence the writer
// into synchronous mode and a backend of "" would fail the allowlist. Both sides
// move together when a default changes, so this asserts the reader's behavior
// rather than the values themselves.
func TestReadReceiptConfigAbsentKeysKeepDefaults(t *testing.T) {
	cfg, err := ReadReceiptConfig(configtest.AppOpts{})
	if err != nil {
		t.Fatalf("an absent [receipt-store] section must read cleanly, got %v", err)
	}
	if want := DefaultReceiptStoreConfig(); cfg != want {
		t.Fatalf("an absent [receipt-store] section resolved to %+v, want the declared defaults %+v",
			cfg, want)
	}
}
