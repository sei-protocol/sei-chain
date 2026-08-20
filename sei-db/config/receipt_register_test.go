package config

import (
	"reflect"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestTheDeclaredKeysAreTheKeysThisReaderResolves holds the declaration against the reader.
//
// Two of the struct's fields are excluded from configuration and must declare nothing. KeepRecent is
// derived from the global min-retain-blocks flag at the app layer and ExternalPruning is set by whatever
// constructs the collector, so a key for either would be written at override precedence over the value
// that code assigns.
func TestTheDeclaredKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(ReceiptStoreSectionName)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", ReceiptStoreSectionName)
	}

	want := []string{
		"receipt-store.async-write-buffer",
		"receipt-store.db-directory",
		"receipt-store.enable-read-write-metrics",
		"receipt-store.log-filter-parallelism",
		"receipt-store.prune-interval-seconds",
		"receipt-store.rs-backend",
	}
	if got := section.Keys; !reflect.DeepEqual(got, want) {
		t.Errorf("declared keys are\n  %v\nwant\n  %v", got, want)
	}
	for _, excluded := range []string{"receipt-store.keep-recent", "receipt-store.external-pruning"} {
		for _, key := range section.Keys {
			if key == excluded {
				t.Errorf("%s is declared. Nothing sources it from configuration, so installing it would "+
					"put a default over the value the app layer assigns", excluded)
			}
		}
	}
}

// TestTheDefaultsAreWhatTheNodeAlreadyRuns keeps the section from restating the values by hand.
func TestTheDefaultsAreWhatTheNodeAlreadyRuns(t *testing.T) {
	for _, mode := range registry.Modes() {
		got, ok := receiptStoreDefaults(mode).(ReceiptStoreConfig)
		if !ok {
			t.Fatalf("mode %q: defaults returned %T, want ReceiptStoreConfig", mode, receiptStoreDefaults(mode))
		}
		if !reflect.DeepEqual(got, DefaultReceiptStoreConfig()) {
			t.Errorf("mode %q resolves to %+v, want the package default %+v",
				mode, got, DefaultReceiptStoreConfig())
		}
	}
}
