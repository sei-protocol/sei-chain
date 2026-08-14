package config

import (
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

func TestTheRegisteredReceiptStoreBaselineIsWhatANodeRunsToday(t *testing.T) {
	section, ok := registry.Lookup(ReceiptStoreSectionName)
	if !ok {
		t.Fatalf("%s did not register, so nothing below measures anything", ReceiptStoreSectionName)
	}
	for _, mode := range registry.Modes() {
		got, isConfig := section.Defaults(mode).(ReceiptStoreConfig)
		if !isConfig {
			t.Fatalf("the baseline for %q is %T, not this package's own type", mode, section.Defaults(mode))
		}
		if got != DefaultReceiptStoreConfig() {
			t.Errorf("the baseline for %q mode is %+v and this package's default is %+v. Registering a "+
				"section must not change what a node runs, and a difference here changes where "+
				"receipts are stored while reading as a refactor", mode, got, DefaultReceiptStoreConfig())
		}
	}
}

func TestTheDerivedReceiptStoreKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(ReceiptStoreSectionName)
	if !ok {
		t.Fatalf("%s did not register", ReceiptStoreSectionName)
	}
	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	live := []string{
		flagRSDBDirectory, flagRSBackend, flagRSAsyncWriteBuffer,
		flagRSPruneIntervalSeconds, flagRSReadWriteMetrics, flagRSLogFilterParallelism,
	}
	for _, key := range live {
		if !derived[key] {
			t.Errorf("this package's reader resolves %q and the registry derives %v. An operator's "+
				"value reaches one of those spellings and not the other", key, section.Keys)
		}
	}
	if len(section.Keys) != len(live) {
		t.Errorf("the registry derived %d keys and this reader resolves %d settings: %v",
			len(section.Keys), len(live), section.Keys)
	}
}

// TestTheMisnamedBackendKeyIsNotDeclared keeps a refusal from becoming a setting.
//
// The reader looks receipt-store.backend up only to refuse it, telling an operator who used the wrong
// name which one to use. Declaring it would turn that message into a key the registry resolves and
// stores, and the operator would get a node whose backend came from a name the reader rejects.
func TestTheMisnamedBackendKeyIsNotDeclared(t *testing.T) {
	section, ok := registry.Lookup(ReceiptStoreSectionName)
	if !ok {
		t.Fatalf("%s did not register", ReceiptStoreSectionName)
	}
	for _, key := range section.Keys {
		if key == flagRSMisnamedBackend {
			t.Errorf("the registry declared %q, which this reader accepts no value for; it exists to "+
				"tell an operator to use %q instead", flagRSMisnamedBackend, flagRSBackend)
		}
	}
}

// TestTheFieldsNotReadFromConfigurationDeclareNoKey holds the two derived fields.
//
// KeepRecent comes from the global block-retention setting and ExternalPruning from which backend is
// in use. A key for either would be one an operator could write and nothing would read.
func TestTheFieldsNotReadFromConfigurationDeclareNoKey(t *testing.T) {
	section, ok := registry.Lookup(ReceiptStoreSectionName)
	if !ok {
		t.Fatalf("%s did not register", ReceiptStoreSectionName)
	}
	for _, key := range section.Keys {
		switch key {
		case ReceiptStoreSectionName + ".keeprecent", ReceiptStoreSectionName + ".externalpruning":
			t.Errorf("the registry declared %q. That value is set by the code that constructs the "+
				"store, so a written one would be silently discarded", key)
		}
	}
}

// TestTheRegistryChecksTheBackendAllowlist holds the one rule this section states.
func TestTheRegistryChecksTheBackendAllowlist(t *testing.T) {
	section, ok := registry.Lookup(ReceiptStoreSectionName)
	if !ok {
		t.Fatalf("%s did not register", ReceiptStoreSectionName)
	}
	if section.Validate == nil {
		t.Fatal("the registry states no rule for this section, so a resolved configuration naming a " +
			"storage backend that does not exist reads as usable")
	}
	if err := section.Validate(map[string]any{"rs-backend": "rocksdb"}); err == nil {
		t.Error("a backend with no implementation was accepted; the boot refuses it, so a diagnostic " +
			"reporting it as usable sends an operator into a node that will not start")
	}
	// The reader absorbs case and surrounding space, so a diagnostic that refused them would refuse a
	// file the node accepts.
	for _, accepted := range []string{"pebbledb", "PebbleDB", " littidx ", "pebble"} {
		if err := section.Validate(map[string]any{"rs-backend": accepted}); err != nil {
			t.Errorf("the reader accepts %q and the registry refused it: %v", accepted, err)
		}
	}
}

func TestRegisteringReceiptStoreProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == ReceiptStoreSectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}

func TestNoExperimentalKeyShadowsReceiptStoreAfterRegistration(t *testing.T) {
	section, ok := registry.Lookup(ReceiptStoreSectionName)
	if !ok {
		t.Fatalf("%s did not register", ReceiptStoreSectionName)
	}
	specs := make([]configtest.KeySpec, 0, len(section.Keys))
	for _, key := range section.Keys {
		specs = append(specs, configtest.KeySpec{Key: key})
	}
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, "receipt-store", specs)
}

// TestTheZeroWhenAbsentDeclarationMatchesThisReader holds what a migration writes for a key this
// section's keys are absent from, against what the reader actually does with an absent key.
func TestTheZeroWhenAbsentDeclarationMatchesThisReader(t *testing.T) {
	configtest.CheckZeroWhenAbsentMatchesTheReader(t, "receipt-store",
		func(o configtest.AppOpts) (any, error) { return ReadReceiptConfig(o) })
}
