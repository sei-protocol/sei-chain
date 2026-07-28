package config

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
)

// receiptKeys covers the [receipt-store] keys whose resolution is a plain guarded
// checked cast. Three read sites in this section are not plain and get their own
// targets: db-directory (trimmed), rs-backend (lower-cased, trimmed and
// allowlisted), and the misnamed legacy backend key (a fail-closed detector).
var receiptKeys = []configtest.KeySpec{
	{
		Key: "receipt-store.async-write-buffer", Path: "AsyncWriteBuffer", Cast: configtest.CastInt,
		Checked: true,
		Why:     "default 100; <= 0 means synchronous writes, so an absent key must not clobber it to 0",
	},
	{
		Key: "receipt-store.prune-interval-seconds", Path: "PruneIntervalSeconds", Cast: configtest.CastInt,
		Checked: true,
	},
	{
		Key: "receipt-store.enable-read-write-metrics", Path: "EnableReadWriteMetrics", Cast: configtest.CastBool,
		Checked: true,
	},
	{
		Key: "receipt-store.log-filter-parallelism", Path: "LogFilterParallelism", Cast: configtest.CastInt,
		Checked: true,
		Why:     "default 16; littidx eth_getLogs fan-out, <= 0 falls back at the consumer",
	},
}

func readReceipt(opts configtest.AppOpts) (any, error) { return ReadReceiptConfig(opts) }

func FuzzReadReceiptConfig(f *testing.F) {
	f.Add(uint(0), uint8(3), "", int64(100), false)
	f.Add(uint(0), uint8(3), "", int64(0), false)
	f.Add(uint(1), uint8(7), "", int64(600), false)
	f.Add(uint(2), uint8(2), "", int64(0), true)
	f.Add(uint(3), uint8(3), "", int64(16), false)
	f.Add(uint(0), uint8(1), "many", int64(0), false)
	f.Add(uint(1), uint8(0), "", int64(0), false)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(receiptKeys, keyIdx)
		configtest.CheckRow(t, "receipt-store", readReceipt, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

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
	f.Add(uint8(1), "pebbledb", int64(0), false)
	f.Add(uint8(0), "", int64(0), false)
	f.Add(uint8(2), "", int64(0), true)
	f.Add(uint8(3), "", int64(0), false)

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

// TestReadReceiptConfigAbsentKeysKeepDefaults pins the section baseline.
func TestReadReceiptConfigAbsentKeysKeepDefaults(t *testing.T) {
	configtest.CheckAbsent(t, "receipt-store", readReceipt, DefaultReceiptStoreConfig())
}

// TestDefaultsMatchTheRecordedValues pins the receipt_store defaults themselves.
//
// The absent-keys coverage in this file proves the reader returns the declared defaults; it
// cannot prove which values those are, because both sides of that comparison come from the
// same package. This compares them against testdata/receipt_store.golden, an independent
// recording, so a default that moves shows the new value in a diff instead of passing
// silently.
func TestDefaultsMatchTheRecordedValues(t *testing.T) {
	configtest.CheckDefaults(t, "receipt_store", DefaultReceiptStoreConfig())
}
