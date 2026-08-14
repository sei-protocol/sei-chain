package app

import (
	"reflect"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

func TestTheRegisteredLightInvarianceBaselineIsWhatANodeRunsToday(t *testing.T) {
	section, ok := registry.Lookup(LightInvarianceSectionName)
	if !ok {
		t.Fatalf("%s did not register, so nothing below measures anything", LightInvarianceSectionName)
	}
	for _, mode := range registry.Modes() {
		got, isConfig := section.Defaults(mode).(LightInvarianceConfig)
		if !isConfig {
			t.Fatalf("the baseline for %q is %T, not this package's own type", mode, section.Defaults(mode))
		}
		if got != DefaultLightInvarianceConfig {
			t.Errorf("the baseline for %q mode is %+v and this package's default is %+v. Registering a "+
				"section must not change what a node runs, and a difference here reads as a refactor",
				mode, got, DefaultLightInvarianceConfig)
		}
		if !got.SupplyEnabled {
			t.Errorf("the baseline for %q mode turns the supply check off. That check is what tells a "+
				"node its recorded total supply no longer matches what its store holds", mode)
		}
	}
}

func TestTheDerivedLightInvarianceKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(LightInvarianceSectionName)
	if !ok {
		t.Fatalf("%s did not register", LightInvarianceSectionName)
	}
	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	if !derived[flagSupplyEnabled] {
		t.Errorf("this package's reader resolves %q and the registry derives %v. An operator's value "+
			"reaches one of those spellings and not the other", flagSupplyEnabled, section.Keys)
	}
	if len(section.Keys) != 1 {
		t.Errorf("the registry derived %d keys from a one-field struct: %v", len(section.Keys), section.Keys)
	}
}

func TestRegisteringLightInvarianceProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == LightInvarianceSectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}

func TestNoExperimentalKeyShadowsLightInvarianceAfterRegistration(t *testing.T) {
	section, ok := registry.Lookup(LightInvarianceSectionName)
	if !ok {
		t.Fatalf("%s did not register", LightInvarianceSectionName)
	}
	specs := make([]configtest.KeySpec, 0, len(section.Keys))
	for _, key := range section.Keys {
		specs = append(specs, configtest.KeySpec{Key: key})
	}
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, LightInvarianceSectionName, specs)
}

// TestTheGenesisSchemaDescribesTheReaderItStandsInFor is what holds the two apart-ness together.
//
// The schema declares the spelling and genesistypes.GenesisImportConfig holds the values, and nothing
// in the code connects a schema field to the setting it stands for. This writes a value under each
// declared key, asks the reader which setting changed, and checks the baseline against what the reader
// leaves that setting at when nothing is written. A field paired with the wrong setting fails here
// rather than resolving one operator's value into another's setting.
func TestTheGenesisSchemaDescribesTheReaderItStandsInFor(t *testing.T) {
	// The section name stays a literal here. The wiring record reads it from this call's second
	// argument, and a constant or a table entry would record every schema check under one placeholder
	// row, so removing three of four would not show up as lost coverage.
	configtest.CheckSchemaMatchesTheReader(t, "genesis", configtest.SchemaCheck{
		Read: func(opts configtest.AppOpts) (any, error) {
			return ReadGenesisImportConfig(opts)
		},
		Probe: map[string]any{
			flagGenesisStreamImport: true,
			flagGenesisImportFile:   "/mnt/genesis/stream.json",
		},
	})
}

func TestTheDerivedGenesisKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(GenesisSectionName)
	if !ok {
		t.Fatalf("%s did not register", GenesisSectionName)
	}
	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	for _, live := range []string{flagGenesisStreamImport, flagGenesisImportFile} {
		if !derived[live] {
			t.Errorf("this package's reader resolves %q and the registry derives %v. An operator's "+
				"value reaches one of those spellings and not the other", live, section.Keys)
		}
	}
	if len(section.Keys) != 2 {
		t.Errorf("the registry derived %d keys from a two-field schema: %v", len(section.Keys), section.Keys)
	}
}

func TestRegisteringGenesisProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == GenesisSectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}

func TestNoExperimentalKeyShadowsGenesisAfterRegistration(t *testing.T) {
	section, ok := registry.Lookup(GenesisSectionName)
	if !ok {
		t.Fatalf("%s did not register", GenesisSectionName)
	}
	specs := make([]configtest.KeySpec, 0, len(section.Keys))
	for _, key := range section.Keys {
		specs = append(specs, configtest.KeySpec{Key: key})
	}
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, GenesisSectionName, specs)
}

// TestTheStateStoreSchemaDescribesTheReaderItStandsInFor holds the schema against parseSSConfigs.
//
// This is the section where the two halves are furthest apart. config.StateStoreConfig tags every one
// of its fields with a name the reader does not look up, so the schema is the only statement of the
// real spelling, and nothing in the code pairs a schema field with the setting it stands for. This
// writes a value under each declared key and asks the reader which setting changed.
//
// The baseline half is recorded rather than asserted, because parseSSConfigs guards none of its reads
// and so resolves an absent key to zero rather than to the default beside it. See
// testdata/state-store.absent.golden.
func TestTheStateStoreSchemaDescribesTheReaderItStandsInFor(t *testing.T) {
	// The section name stays a literal. The wiring record reads it from this call's second argument.
	configtest.CheckSchemaMatchesTheReader(t, "state-store", configtest.SchemaCheck{
		Read: func(opts configtest.AppOpts) (any, error) {
			return parseSSConfigs(opts), nil
		},
		// Each probe differs from what an absent key resolves to, which for this reader is zero. That
		// is why the two booleans whose default is already false are probed with true.
		Probe: map[string]any{
			FlagSSEnable:            true,
			FlagSSDirectory:         "/mnt/ss",
			FlagSSBackend:           "rocksdb",
			FlagSSAsyncWriterBuffer: 250,
			FlagSSKeepRecent:        7777,
			FlagSSPruneInterval:     900,
			FlagSSImportNumWorkers:  4,
			FlagSSReadWriteMetrics:  true,
			FlagEVMSSDirectory:      "/mnt/evm-ss",
			FlagEVMSSSeparateDBs:    true,
			FlagEVMSSSplit:          true,
		},
	})
}

func TestTheDerivedStateStoreKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(StateStoreSectionName)
	if !ok {
		t.Fatalf("%s did not register", StateStoreSectionName)
	}
	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	live := []string{
		FlagSSEnable, FlagSSDirectory, FlagSSBackend, FlagSSAsyncWriterBuffer, FlagSSKeepRecent,
		FlagSSPruneInterval, FlagSSImportNumWorkers, FlagSSReadWriteMetrics,
		FlagEVMSSDirectory, FlagEVMSSSeparateDBs, FlagEVMSSSplit,
	}
	for _, key := range live {
		if !derived[key] {
			t.Errorf("this reader resolves %q and the registry derives %v. An operator's value reaches "+
				"one of those spellings and not the other", key, section.Keys)
		}
	}
	if len(section.Keys) != len(live) {
		t.Errorf("the registry derived %d keys and this reader resolves %d state-store settings: %v",
			len(section.Keys), len(live), section.Keys)
	}
}

// TestTheStateStoreStructsOwnTagsAreNotTheLiveKeys is why this section needs a schema at all.
//
// config.StateStoreConfig tags its Enable field "enable", so a section derived from that type would
// declare state-store.enable and an operator writing state-store.ss-enable would reach nothing. The
// tags are left alone on purpose: correcting them renames keys operators already have in their files,
// which is what the migration machinery is for. This says so, so that correcting them later is a
// failure here rather than a schema nobody notices has become redundant.
func TestTheStateStoreStructsOwnTagsAreNotTheLiveKeys(t *testing.T) {
	field, ok := reflect.TypeOf(config.StateStoreConfig{}).FieldByName("Enable")
	if !ok {
		t.Fatal("config.StateStoreConfig has no Enable field, so the schema's first key stands for nothing")
	}
	tag := field.Tag.Get("mapstructure")
	if tag == "ss-enable" {
		t.Error("config.StateStoreConfig now tags Enable as ss-enable. If its tags have been corrected " +
			"to the keys the reader resolves, this section no longer needs a schema and should register " +
			"the struct directly")
	}
	if tag == "" {
		t.Errorf("config.StateStoreConfig's Enable field carries no mapstructure tag; this test compares "+
			"against %q and an empty tag matches nothing", "ss-enable")
	}
}

func TestRegisteringStateStoreProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == StateStoreSectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}

func TestNoExperimentalKeyShadowsStateStoreAfterRegistration(t *testing.T) {
	section, ok := registry.Lookup(StateStoreSectionName)
	if !ok {
		t.Fatalf("%s did not register", StateStoreSectionName)
	}
	specs := make([]configtest.KeySpec, 0, len(section.Keys))
	for _, key := range section.Keys {
		specs = append(specs, configtest.KeySpec{Key: key})
	}
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, StateStoreSectionName, specs)
}

// TestTheStateCommitSchemaDescribesTheReaderItStandsInFor holds the schema against parseSCConfigs.
//
// config.StateCommitConfig nests its settings under MemIAVLConfig, FlatKVConfig and HashLogger while
// the keys the reader looks up are flat names on the section, so no derivation from that type produces
// them. This writes a value under each declared key and asks the reader which setting changed.
//
// Two keys need saying out loud, and both come from the write mode being derived rather than stored.
// The written mode is ignored while automatic mode is on, so probing it without turning that off changes
// nothing at all; and turning automatic mode off moves the mode along with it. Context supplies the
// companion for the first and AlsoDerives names the second, so the one-setting rule keeps its meaning
// for the other eighteen.
func TestTheStateCommitSchemaDescribesTheReaderItStandsInFor(t *testing.T) {
	// The section name stays a literal. The wiring record reads it from this call's second argument.
	configtest.CheckSchemaMatchesTheReader(t, "state-commit", configtest.SchemaCheck{
		Read: func(opts configtest.AppOpts) (any, error) {
			return parseSCConfigs(opts), nil
		},
		Probe: map[string]any{
			FlagSCEnable:                     true,
			FlagSCDirectory:                  "/mnt/sc",
			FlagSCAsyncCommitBuffer:          250,
			FlagSCSnapshotKeepRecent:         uint32(9),
			FlagSCSnapshotInterval:           uint32(5000),
			FlagSCSnapshotMinTimeInterval:    uint32(1800),
			FlagSCSnapshotWriterLimit:        16,
			FlagSCSnapshotPrefetchThreshold:  0.5,
			FlagSCSnapshotWriteRateMBps:      250,
			FlagSCHistoricalProofMaxInFlight: 8,
			FlagSCHistoricalProofRateLimit:   12.5,
			FlagSCHistoricalProofBurst:       6,
			FlagSCWriteMode:                  "flatkv_only",
			FlagSCWriteModeEnableAuto:        false,
			FlagSCHashLoggerEnable:           false,
			FlagSCHashLoggerDirectory:        "/mnt/hashlog",
			FlagSCHashLoggerBlocksToRetain:   uint(500),
			FlagSCHashLoggerTargetFileSize:   uint(1 << 22),
			FlagSCHashLoggerMaxDiskSize:      uint(1 << 35),
			FlagSCFlatKVReadWriteMetrics:     true,
		},
		Context: map[string]configtest.AppOpts{
			// Automatic mode overrides the written mode, so without turning it off this key reaches
			// nothing observable and would read as a key the reader never looks up.
			FlagSCWriteMode: {FlagSCWriteModeEnableAuto: false},
		},
		AlsoDerives: map[string][]string{
			// Turning automatic mode off also settles what the write mode becomes, because the reader
			// computes the mode from both keys rather than storing what was written.
			FlagSCWriteModeEnableAuto: {"WriteMode"},
		},
	})
}

func TestTheDerivedStateCommitKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(StateCommitSectionName)
	if !ok {
		t.Fatalf("%s did not register", StateCommitSectionName)
	}
	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	live := []string{
		FlagSCEnable, FlagSCDirectory, FlagSCAsyncCommitBuffer, FlagSCSnapshotKeepRecent,
		FlagSCSnapshotInterval, FlagSCSnapshotMinTimeInterval, FlagSCSnapshotWriterLimit,
		FlagSCSnapshotPrefetchThreshold, FlagSCSnapshotWriteRateMBps, FlagSCHistoricalProofMaxInFlight,
		FlagSCHistoricalProofRateLimit, FlagSCHistoricalProofBurst, FlagSCWriteMode,
		FlagSCWriteModeEnableAuto, FlagSCHashLoggerEnable, FlagSCHashLoggerDirectory,
		FlagSCHashLoggerBlocksToRetain, FlagSCHashLoggerTargetFileSize, FlagSCHashLoggerMaxDiskSize,
		FlagSCFlatKVReadWriteMetrics,
	}
	for _, key := range live {
		if !derived[key] {
			t.Errorf("this reader resolves %q and the registry derives %v. An operator's value reaches "+
				"one of those spellings and not the other", key, section.Keys)
		}
	}
	if len(section.Keys) != len(live) {
		t.Errorf("the registry derived %d keys and this reader resolves %d state-commit settings: %v",
			len(section.Keys), len(live), section.Keys)
	}
}

// TestTheRegistryRefusesAnUnknownWriteMode holds the one rule this section states.
//
// parseSCConfigs stops the node with a panic when it cannot recognise the written mode. A diagnostic
// that refuses the same value is what lets an operator find out before a restart rather than during one.
func TestTheRegistryRefusesAnUnknownWriteMode(t *testing.T) {
	section, ok := registry.Lookup(StateCommitSectionName)
	if !ok {
		t.Fatalf("%s did not register", StateCommitSectionName)
	}
	if section.Validate == nil {
		t.Fatal("the registry states no rule for this section, so a write mode this binary cannot " +
			"recognise reads as usable and stops the node at the next restart")
	}
	if err := section.Validate(map[string]any{"sc-write-mode": "sideways"}); err == nil {
		t.Error("an unrecognised write mode was accepted; the reader panics on it, so a diagnostic " +
			"reporting the file as usable sends an operator into a node that will not start")
	}
	// An absent key arrives as an empty name and the reader keeps its default for it, so refusing that
	// would refuse the configuration every node resolves to.
	if err := section.Validate(map[string]any{"sc-write-mode": ""}); err != nil {
		t.Errorf("an absent write mode was refused: %v", err)
	}
	for _, accepted := range []string{"memiavl_only", "flatkv_only", "cosmos_only"} {
		if err := section.Validate(map[string]any{"sc-write-mode": accepted}); err != nil {
			t.Errorf("the reader accepts %q and the registry refused it: %v", accepted, err)
		}
	}
}

func TestRegisteringStateCommitProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == StateCommitSectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}

func TestNoExperimentalKeyShadowsStateCommitAfterRegistration(t *testing.T) {
	section, ok := registry.Lookup(StateCommitSectionName)
	if !ok {
		t.Fatalf("%s did not register", StateCommitSectionName)
	}
	specs := make([]configtest.KeySpec, 0, len(section.Keys))
	for _, key := range section.Keys {
		specs = append(specs, configtest.KeySpec{Key: key})
	}
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, StateCommitSectionName, specs)
}

// The value a migration writes for a key an operator's files do not carry, held against what each reader
// actually does with an absent key. The section name stays a literal, since the wiring record reads it from
// the call's second argument.

func TestTheStateStoreZeroWhenAbsentDeclarationMatchesItsReader(t *testing.T) {
	configtest.CheckZeroWhenAbsentMatchesTheReader(t, "state-store",
		func(o configtest.AppOpts) (any, error) { return parseSSConfigs(o), nil })
}

func TestTheStateCommitZeroWhenAbsentDeclarationMatchesItsReader(t *testing.T) {
	configtest.CheckZeroWhenAbsentMatchesTheReader(t, "state-commit",
		func(o configtest.AppOpts) (any, error) { return parseSCConfigs(o), nil })
}

func TestTheGenesisZeroWhenAbsentDeclarationMatchesItsReader(t *testing.T) {
	configtest.CheckZeroWhenAbsentMatchesTheReader(t, "genesis",
		func(o configtest.AppOpts) (any, error) { return ReadGenesisImportConfig(o) })
}

func TestTheLightInvarianceZeroWhenAbsentDeclarationMatchesItsReader(t *testing.T) {
	configtest.CheckZeroWhenAbsentMatchesTheReader(t, "light_invariance",
		func(o configtest.AppOpts) (any, error) { return ReadLightInvarianceConfig(o) })
}
