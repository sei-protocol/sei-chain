package config

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestTheDeclaredKeysAreTheKeysThisReaderResolves holds the declaration against the reader.
//
// Seven keys, which is every key the reader resolves and takes a value from. Two of the struct's fields are
// excluded from configuration and declare nothing, because the app layer assigns them after this reader
// has returned and a key for either is one an operator writes that the assignment discards. The reader
// resolves an eighth key, the retired spelling of the backend, only to refuse to start; a key whose one
// outcome is a stopped node is not one to offer.
func TestTheDeclaredKeysAreTheKeysThisReaderResolves(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == ReceiptStoreSectionName {
			t.Fatalf("%s was refused: %v", ReceiptStoreSectionName, defect.Err)
		}
	}
	section, ok := registry.Lookup(ReceiptStoreSectionName)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", ReceiptStoreSectionName)
	}

	// The constants this package's own reader passes to Get, rather than the same strings written again.
	// A second list agrees with itself while the reader asks for something else.
	want := []string{
		flagRSAsyncWriteBuffer,
		flagRSDBDirectory,
		flagRSEnable,
		flagRSReadWriteMetrics,
		flagRSLogFilterParallelism,
		flagRSPruneIntervalSeconds,
		flagRSBackend,
	}
	sort.Strings(want)
	if got := section.Keys; !reflect.DeepEqual(got, want) {
		t.Errorf("declared keys are\n  %v\nwant\n  %v", got, want)
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
