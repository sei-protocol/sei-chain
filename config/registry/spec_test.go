package registry_test

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/config/registry"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
)

// PR4 of the ConfigManager stack: the stable registry and mode-based defaults.
//
// One registration per section carries the struct and a baseline function of node mode. The
// dotted key, the canonical environment spelling and the schema fingerprint all derive from that
// one declaration, so nobody hand-writes a key string.
//
// What this PR is NOT. The registry is the authoring, validation and declaration surface, not
// the boot input. The approved design forbids feeding app.New from an in-memory struct, because
// a struct silently drops keys it does not model and a round-trip test passes while being wrong.
// The transport stays a source carrying every resolved key, and no appOpts.Get call site changes.
//
// Gate 3, that resolution runs in the declared order, needs a resolver and is not in this slice.
// Precedence is declared here as data; the resolver that honours it is the next one.

// gigaSection mirrors what the giga executor's own package would register. The struct under test
// is the real one, so gate 6 measures the live reader rather than a copy of it.
const gigaSection = "giga_executor"

func registerGiga(t *testing.T) {
	t.Helper()
	registry.Reset()
	registry.RegisterSection(gigaSection, &gigaconfig.Config{}, func(m registry.Mode) any {
		// The design's own worked example: OCC is off on an archive node.
		return gigaconfig.Config{Enabled: true, OCCEnabled: m != registry.ModeArchive}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering %s produced a defect: %v", d.Section, d.Err)
	}
}

// TestGate1KeyIdentityIsDerivedNeverHandTyped holds the property the registry exists for.
//
// The dotted key comes from the section name and the field's mapstructure tag. Measured against
// the live reader's own flag constants, which is the only comparison that proves the derivation
// agrees with what actually resolves today.
func TestGate1KeyIdentityIsDerivedNeverHandTyped(t *testing.T) {
	registerGiga(t)

	s, ok := registry.Lookup(gigaSection)
	if !ok {
		t.Fatal("the section did not register")
	}
	got := strings.Join(s.Keys, " ")
	for _, live := range []string{gigaconfig.FlagEnabled, gigaconfig.FlagOCCEnabled} {
		if !strings.Contains(got, live) {
			t.Errorf("the derivation did not produce %q, the key this section's reader actually "+
				"resolves. Derived keys are %v. A derivation that disagrees with the live reader "+
				"renames a key operators already have in their files", live, s.Keys)
		}
	}
	if len(s.Keys) != 2 {
		t.Errorf("derived %d keys from a two-field struct: %v. An extra key is one no operator "+
			"writes; a missing one is a setting the registry cannot see", len(s.Keys), s.Keys)
	}
}

// TestGate1AnUntaggedFieldIsADefectNotAFallback is the guard that keeps the tag authoritative.
//
// V1 falls back to the field's own name, which is why an embedded srvconfig.Config with no tag
// put whole cosmos sections under a type-name prefix nothing writes, and why 92 operator-facing
// keys reach their field only through a spelling the tags do not produce. Refusing to guess is
// what makes the tag the single spelling.
//
// Recorded as a defect rather than panicked, because this package is imported by every feature
// and a panic at init takes down every seid invocation including --help.
func TestGate1AnUntaggedFieldIsADefectNotAFallback(t *testing.T) {
	type untagged struct {
		Tagged   bool `mapstructure:"tagged"`
		Untagged bool
	}
	registry.Reset()

	registry.RegisterSection("probe", &untagged{}, func(registry.Mode) any { return untagged{} })

	defects := registry.Defects()
	if len(defects) != 1 {
		t.Fatalf("an untagged field produced %d defects, want 1. A silent fallback to the field "+
			"name is how the legacy path's keys became unreachable", len(defects))
	}
	if !strings.Contains(defects[0].Err.Error(), "Untagged") {
		t.Errorf("the defect does not name the offending field: %v", defects[0].Err)
	}
	if _, ok := registry.Lookup("probe"); ok {
		t.Error("the section registered despite the defect, so a caller would read derived keys " +
			"that silently omit the untagged field")
	}
}

// TestGate2ABaselineVariesByModeAndAnAbsentKeyTracksIt is requirement 2.
//
// Held on a key whose baseline actually differs between two modes, since a key with one baseline
// everywhere would pass against a registry that ignored mode entirely.
func TestGate2ABaselineVariesByModeAndAnAbsentKeyTracksIt(t *testing.T) {
	registerGiga(t)
	s, _ := registry.Lookup(gigaSection)

	archive, ok := s.Defaults(registry.ModeArchive).(gigaconfig.Config)
	if !ok {
		t.Fatalf("the baseline returned %T rather than the section's own type", s.Defaults(registry.ModeArchive))
	}
	validator := s.Defaults(registry.ModeValidator).(gigaconfig.Config)

	if archive.OCCEnabled {
		t.Error("occ_enabled is true on an archive node; the design's worked example turns it off there")
	}
	if !validator.OCCEnabled {
		t.Error("occ_enabled is false on a validator, so the baseline does not vary by mode and " +
			"this gate would hold for a registry that ignored mode")
	}
	// And a key that does not vary still resolves, or the assertion above would be the only shape
	// a baseline could take.
	if !archive.Enabled || !validator.Enabled {
		t.Error("enabled differs by mode; it is the same on both in this section's baseline")
	}
}

// TestGate2EveryModeHasABaseline holds that no mode is unreachable.
//
// A mode with no baseline would resolve an absent key to the type's zero rather than to what the
// binary intends, silently, on exactly the nodes running that mode.
func TestGate2EveryModeHasABaseline(t *testing.T) {
	registerGiga(t)
	s, _ := registry.Lookup(gigaSection)

	for _, m := range registry.Modes() {
		if s.Defaults(m) == nil {
			t.Errorf("mode %q has no baseline, so an absent key on that node resolves to a zero "+
				"rather than to the binary's judgement", m)
		}
	}
}

// TestModesMatchTheNodeModes holds registry.Mode against the modes a node actually declares.
//
// registry.Mode is declared locally so this package stays a leaf every feature can import. That
// duplication is only safe while the two sets agree, and this is what makes a mode added to
// app/params fail here rather than silently having no baseline anywhere.
func TestModesMatchTheNodeModes(t *testing.T) {
	node := map[string]bool{
		string(params.NodeModeValidator): true,
		string(params.NodeModeFull):      true,
		string(params.NodeModeSeed):      true,
		string(params.NodeModeArchive):   true,
	}
	for _, m := range registry.Modes() {
		if !node[string(m)] {
			t.Errorf("registry declares mode %q, which app/params does not; a baseline for a mode "+
				"no node runs is dead, and a mode with no baseline is worse", m)
		}
		delete(node, string(m))
	}
	for left := range node {
		t.Errorf("app/params declares mode %q and the registry has no baseline input for it, so a "+
			"section cannot vary its baseline on the mode some nodes actually run", left)
	}
}

// TestGate4TheFingerprintMovesOnAStableChangeAndOnlyThen is what makes forgetting a schema bump
// impossible.
//
// Both directions, because either alone is useless. A key added, renamed or retyped moves it, so
// CI can demand the bump and the migration in the same change. Registering the identical shape
// twice does not, or the gate would fire on every unrelated commit.
func TestGate4TheFingerprintMovesOnAStableChangeAndOnlyThen(t *testing.T) {
	registerGiga(t)
	base := registry.Fingerprint()

	registerGiga(t)
	if again := registry.Fingerprint(); again != base {
		t.Errorf("the same registration produced two fingerprints, %s and %s. A gate that fires "+
			"on an unchanged shape is one a reviewer learns to ignore", base[:12], again[:12])
	}

	for _, tc := range []struct {
		name  string
		proto any
	}{
		{"a renamed key", &struct {
			Enabled    bool `mapstructure:"enabled"`
			OCCEnabled bool `mapstructure:"occ_workers"`
		}{}},
		{"an added key", &struct {
			Enabled    bool `mapstructure:"enabled"`
			OCCEnabled bool `mapstructure:"occ_enabled"`
			Extra      bool `mapstructure:"extra"`
		}{}},
		{"a retyped key", &struct {
			Enabled    bool `mapstructure:"enabled"`
			OCCEnabled int  `mapstructure:"occ_enabled"`
		}{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry.Reset()
			registry.RegisterSection(gigaSection, tc.proto, func(registry.Mode) any { return struct{}{} })
			if got := registry.Fingerprint(); got == base {
				t.Errorf("%s left the fingerprint unchanged, so CI cannot demand the schema bump "+
					"and the migration that a shape change owes", tc.name)
			}
		})
	}
}

// TestGate4AChangedBaselineMovesTheFingerprint holds the half a key-only hash would miss.
//
// A default is the value every node that never wrote the key runs, so changing one changes what
// the fleet does. That is a contract change even though no key moved.
func TestGate4AChangedBaselineMovesTheFingerprint(t *testing.T) {
	registerGiga(t)
	base := registry.Fingerprint()

	registry.Reset()
	registry.RegisterSection(gigaSection, &gigaconfig.Config{}, func(registry.Mode) any {
		return gigaconfig.Config{Enabled: false, OCCEnabled: false} // both flipped
	})

	if got := registry.Fingerprint(); got == base {
		t.Error("a changed baseline left the fingerprint unchanged. Every node that never wrote " +
			"the key would silently run the new value with no schema bump to notice")
	}
}

// TestGate5TheEnvironmentSpellingIsCanonicalAndPinned closes a measured legacy defect.
//
// The legacy path answers to three environment universes, and derives its prefix from the running
// binary's filename through path.Base(os.Executable()), so renaming seid moves the whole
// namespace. One spelling, derived from the key, with a pinned prefix.
func TestGate5TheEnvironmentSpellingIsCanonicalAndPinned(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"giga_executor.occ_enabled", "SEID_GIGA_EXECUTOR_OCC_ENABLED"},
		{"state-store.ss-keep-recent", "SEID_STATE_STORE_SS_KEEP_RECENT"},
	} {
		if got := registry.EnvName(tc.key); got != tc.want {
			t.Errorf("EnvName(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
	if !strings.HasPrefix(registry.EnvName("a.b"), registry.EnvPrefix+"_") {
		t.Error("the prefix is not applied, so the namespace is not pinned")
	}
}

// TestGate5EnvSpellingCollisionsAreDetectable records the cost of the replacer.
//
// Dots and hyphens both become underscores, so two keys differing only in that punctuation
// collapse onto one variable. The derivation cannot prevent it; a check over the registry can see
// it, and this is what makes that check's input honest.
func TestGate5EnvSpellingCollisionsAreDetectable(t *testing.T) {
	a := registry.EnvName("giga_executor.occ_enabled")
	b := registry.EnvName("giga-executor.occ-enabled")
	if a != b {
		t.Fatalf("expected these to collide onto one variable, got %q and %q; if the replacer has "+
			"changed then the collision check the registry owes is no longer needed", a, b)
	}
}

// ---------------------------------------------------------------------------------------
// Derivation agreement. V1 resolves by explicit dotted string, V2 by walking a struct, so
// comparing values is not enough: a tag typo produces a key V1 never reads, and a value-only
// comparison catches that only if a test happens to drive that key.
//
// DECIDED: V2 keeps every existing operator-facing name. Where an inert V1 tag would have
// addressed a cleaner spelling, the cleaner spelling is abandoned rather than migrated. The
// design's exemplar table left four of these open, and this is the answer to all four.
// ---------------------------------------------------------------------------------------

// keptSpellings are the operator-facing keys whose inert V1 tag would derive something else.
// Read from the tree by the design's Appendix E. Each carries the spelling that was abandoned,
// so the diff shows what the decision gave up.
var keptSpellings = []struct {
	operatorWrites string
	inertTagWould  string
	why            string
}{
	{"state-store.ss-keep-recent", "state-store.keep-recent", "prefix-strip"},
	{"state-store.ss-enable", "state-store.enable", "prefix-strip"},
	{"state-store.ss-prune-interval", "state-store.prune-interval-seconds", "word substitution"},
	{"state-store.evm-ss-split", "state-store.evm-split", "infix deletion; they cannot both be intended"},
	{"state-commit.sc-async-commit-buffer", "state-commit.async-commit-buffer", "the inert tag names a dead field"},
}

// TestGate6EveryDerivedKeyEqualsTheSpellingV1Resolves is the derivation-agreement gate.
//
// Held per key rather than per value, because that is the failure a value-only differential
// cannot see. A struct tagged with the kept spelling has to derive exactly it, and must not
// derive the spelling the decision abandoned.
func TestGate6EveryDerivedKeyEqualsTheSpellingV1Resolves(t *testing.T) {
	for _, k := range keptSpellings {
		t.Run(k.operatorWrites, func(t *testing.T) {
			section, leaf, ok := strings.Cut(k.operatorWrites, ".")
			if !ok {
				t.Fatalf("%q is not section.leaf", k.operatorWrites)
			}

			registry.Reset()
			registry.RegisterSection(section, taggedLeaf(leaf), func(registry.Mode) any { return struct{}{} })
			for _, d := range registry.Defects() {
				t.Fatalf("the kept spelling %q is not registrable: %v", k.operatorWrites, d.Err)
			}

			keys := registry.Keys()
			if len(keys) != 1 || keys[0] != k.operatorWrites {
				t.Fatalf("derived %v, want [%s]. A difference here renames a key operators already "+
					"have in their files (%s)", keys, k.operatorWrites, k.why)
			}
			if keys[0] == k.inertTagWould {
				t.Errorf("derived the abandoned spelling %q; the decision was to keep %q",
					k.inertTagWould, k.operatorWrites)
			}
		})
	}
}

// taggedLeaf returns a one-field struct whose mapstructure tag is leaf, so the derivation is
// exercised on the decided spelling rather than on a hand-written key string.
func taggedLeaf(leaf string) any {
	switch leaf {
	case "ss-keep-recent":
		return &struct {
			V int `mapstructure:"ss-keep-recent"`
		}{}
	case "ss-enable":
		return &struct {
			V bool `mapstructure:"ss-enable"`
		}{}
	case "ss-prune-interval":
		return &struct {
			V int `mapstructure:"ss-prune-interval"`
		}{}
	case "evm-ss-split":
		return &struct {
			V bool `mapstructure:"evm-ss-split"`
		}{}
	case "sc-async-commit-buffer":
		return &struct {
			V int `mapstructure:"sc-async-commit-buffer"`
		}{}
	}
	return nil
}

// TestGate7ARenameRequiresAnExplicitRatifiedEntry is the escape hatch, and why gate 6 is a gate
// rather than a prohibition.
//
// Keeping every name is today's decision, not a law. A rename is permitted through an explicit
// ratified entry, and the list starts empty so adding one is a deliberate act visible in a diff.
func TestGate7ARenameRequiresAnExplicitRatifiedEntry(t *testing.T) {
	if len(ratifiedRenames) != 0 {
		t.Errorf("the ratified-rename list is not empty: %v. Every entry renames a key operators "+
			"have in their files, so each needs its own decision and its own migration",
			ratifiedRenames)
	}
}

// ratifiedRenames names every key whose operator-facing spelling a decision deliberately changed.
// Empty by decision. An entry here is what permits gate 6 to see a difference without failing,
// and it owes a migration in the same change.
var ratifiedRenames = map[string]string{}

// TestGate8TheDeadFieldIsNotReachableUnderTheLiveKey is the trap the design calls settled, and it
// is tracked as PLT-945.
//
// state-commit.sc-async-commit-buffer resolves today to the live MemIAVLConfig field.
// StateCommitConfig.AsyncCommitBuffer carries the inert tag async-commit-buffer and nothing reads
// it. A tag-driven binder that inherited that tag would bind an operator's value to the dead field
// while the spelling they actually write reached no field at all.
//
// The trap is invisible today precisely because nothing in the tree reads by tag, and it goes live
// the moment a registry does. Held by asserting the two tags derive different keys, so inheriting
// the wrong one cannot silently produce the right-looking path.
func TestGate8TheDeadFieldIsNotReachableUnderTheLiveKey(t *testing.T) {
	const section = "state-commit"

	registry.Reset()
	registry.RegisterSection(section, &struct {
		Live int `mapstructure:"sc-async-commit-buffer"`
	}{}, func(registry.Mode) any { return struct{}{} })
	live := registry.Keys()

	registry.Reset()
	registry.RegisterSection(section, &struct {
		Dead int `mapstructure:"async-commit-buffer"`
	}{}, func(registry.Mode) any { return struct{}{} })
	dead := registry.Keys()

	if len(live) != 1 || live[0] != "state-commit.sc-async-commit-buffer" {
		t.Fatalf("the live spelling derived %v, want [state-commit.sc-async-commit-buffer]", live)
	}
	if len(dead) != 1 || dead[0] != "state-commit.async-commit-buffer" {
		t.Fatalf("the inert spelling derived %v, want [state-commit.async-commit-buffer]", dead)
	}
	if live[0] == dead[0] {
		t.Fatal("the live and inert tags derive the same key, so a struct that inherited the inert " +
			"one would bind an operator's value to the field nothing reads while looking correct")
	}
}

// ---------------------------------------------------------------------------------------
// Gate 3: resolution runs in the declared order.
// ---------------------------------------------------------------------------------------

// TestGate3ResolutionRunsInTheDeclaredOrder is the gate, and shuffling is what makes it falsifiable.
//
// If precedence comes from Precedence, the order layers are passed in cannot matter. If it comes
// from argument order or from a merge where the last writer wins, this fails. The legacy path fails
// it by construction: its answer depends on which viper instance a caller asked, which is why two
// different orders are observable across its key set.
func TestGate3ResolutionRunsInTheDeclaredOrder(t *testing.T) {
	registerGiga(t)

	file := registry.FileLayer(map[string]any{"giga_executor.occ_enabled": "file"})
	env := registry.Layer{Source: "env", Values: map[string]any{"giga_executor.occ_enabled": "env"}}
	flag := registry.Layer{Source: "flag", Values: map[string]any{"giga_executor.occ_enabled": "flag"}}

	// Every ordering of the same three layers.
	for _, order := range [][]registry.Layer{
		{file, env, flag}, {flag, env, file}, {env, flag, file}, {flag, file, env},
	} {
		got, err := registry.Resolve(registry.ModeValidator, order...)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		res, ok := got.From("giga_executor.occ_enabled")
		if !ok {
			t.Fatal("the key did not resolve at all")
		}
		if res.Value != "flag" || res.From != "flag" {
			var names []string
			for _, l := range order {
				names = append(names, l.Source)
			}
			t.Errorf("passed in the order %v the key resolved to %#v from %q, want flag from flag. "+
				"Precedence is %v, and a resolver whose answer depends on argument order has an "+
				"emergent precedence rather than a declared one",
				names, res.Value, res.From, registry.Precedence)
		}
	}
}

// TestGate3EachLayerWinsOverTheOneBelowIt walks the order one step at a time.
//
// The gate above only proves the top layer wins. This proves the ordering is the declared one at
// every step, so a resolver that happened to prefer "flag" for an unrelated reason would still fail.
func TestGate3EachLayerWinsOverTheOneBelowIt(t *testing.T) {
	const key = "giga_executor.occ_enabled"

	for _, tc := range []struct {
		name   string
		layers []registry.Layer
		want   string
	}{
		{"baseline alone", nil, "default"},
		{"file over baseline", []registry.Layer{
			{Source: "file", Values: map[string]any{key: "file"}}}, "file"},
		{"env over file", []registry.Layer{
			{Source: "file", Values: map[string]any{key: "file"}},
			{Source: "env", Values: map[string]any{key: "env"}}}, "env"},
		{"flag over env", []registry.Layer{
			{Source: "env", Values: map[string]any{key: "env"}},
			{Source: "flag", Values: map[string]any{key: "flag"}}}, "flag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registerGiga(t)

			got, err := registry.Resolve(registry.ModeValidator, tc.layers...)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			res, _ := got.From(key)
			if res.From != tc.want {
				t.Errorf("%s resolved from %q, want %q. Precedence is %v",
					tc.name, res.From, tc.want, registry.Precedence)
			}
		})
	}
}

// TestGate3AnAbsentKeyTracksItsModeBaseline is why the baseline is a layer rather than a fallback.
//
// A key no layer mentions still resolves, to the baseline for the running mode. That is what makes
// an absent key track the binary's judgement rather than a zero value, and it is the property the
// design states as "defaults are baselines, not state".
func TestGate3AnAbsentKeyTracksItsModeBaseline(t *testing.T) {
	registerGiga(t)

	archive, err := registry.Resolve(registry.ModeArchive)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	validator, err := registry.Resolve(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	a, _ := archive.From("giga_executor.occ_enabled")
	v, _ := validator.From("giga_executor.occ_enabled")
	if a.From != "default" || v.From != "default" {
		t.Fatalf("an unmentioned key resolved from %q and %q, want default from both", a.From, v.From)
	}
	if a.Value == v.Value {
		t.Errorf("both modes resolved the same value %#v for a key whose baseline varies by mode, so "+
			"the mode is not reaching the baseline", a.Value)
	}
	if a.Value != false || v.Value != true {
		t.Errorf("archive=%#v validator=%#v, want false and true from this section's baseline", a.Value, v.Value)
	}
}

// TestGate3ProvenanceIsRecoverable is the property the legacy path cannot offer at all.
//
// Its layers combine inside one viper before anything observes them, so no value's source is
// recoverable and it can never tell an operator which one won. Overrides is what a diff renders: the
// keys an operator has taken responsibility for, as distinct from those tracking the binary.
func TestGate3ProvenanceIsRecoverable(t *testing.T) {
	registerGiga(t)

	got, err := registry.Resolve(registry.ModeValidator,
		registry.Layer{Source: "env", Values: map[string]any{"giga_executor.occ_enabled": "env"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	overrides := got.Overrides()
	if len(overrides) != 1 || overrides[0] != "giga_executor.occ_enabled" {
		t.Errorf("Overrides returned %v, want the one key a layer supplied. A resolver that cannot "+
			"separate a written value from a baseline cannot tell an operator what they changed", overrides)
	}
	if res, _ := got.From("giga_executor.enabled"); res.From != "default" {
		t.Errorf("the untouched key reports From=%q, so every key would render as an override", res.From)
	}
}

// TestGate3AKeyNoSectionDeclaresIsReportedNotDropped holds the unknown-key path.
//
// Silently dropping one is how an operator's typo becomes invisible. Reported rather than an error,
// because what to do about it differs: a generate path may refuse, while a boot on an operator's
// existing file must not.
func TestGate3AKeyNoSectionDeclaresIsReportedNotDropped(t *testing.T) {
	registerGiga(t)

	got, err := registry.Resolve(registry.ModeValidator,
		registry.Layer{Source: "file", Values: map[string]any{
			"giga_executor.occ_enabled": true,
			"giga_executor.typo":        1,
		}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(got.Unknown) != 1 || got.Unknown[0] != "giga_executor.typo" {
		t.Errorf("Unknown = %v, want the one undeclared key. Dropped silently, an operator's typo is "+
			"invisible and their intended value never applies", got.Unknown)
	}
	if _, ok := got.From("giga_executor.typo"); ok {
		t.Error("the undeclared key resolved anyway, so it would reach a consumer that cannot use it")
	}
}

// TestGate3ALayerWithNoDeclaredPriorityIsAnError is the other half of "declared".
//
// A layer whose source is absent from Precedence has no defined priority. Ignoring it silently is
// worse than refusing: nothing downstream could tell the layer had contributed nothing.
func TestGate3ALayerWithNoDeclaredPriorityIsAnError(t *testing.T) {
	registerGiga(t)

	for _, tc := range []struct{ name, source string }{
		{"unknown source", "cli-somewhere"},
		{"the reserved baseline", "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := registry.Resolve(registry.ModeValidator,
				registry.Layer{Source: tc.source, Values: map[string]any{"giga_executor.enabled": true}})
			if err == nil {
				t.Errorf("a layer named %q was accepted; it has no defined priority, so it would either "+
					"be dropped silently or resolved at a priority the resolver invented", tc.source)
			}
		})
	}
}

// TestGate3EnvLayerIsDrivenByTheDeclaredSet holds the direction that makes it complete.
//
// An environment cannot be enumerated for a prefix: a variable is findable only if its name is
// already known. Asking for every declared key's canonical spelling is therefore the only way to
// build a complete env layer, and it is why the derivation lives beside the registry.
func TestGate3EnvLayerIsDrivenByTheDeclaredSet(t *testing.T) {
	registerGiga(t)
	set := map[string]string{
		registry.EnvName("giga_executor.occ_enabled"): "false",
		"SEID_SOMETHING_UNDECLARED":                   "1",
	}

	l := registry.EnvLayer(func(name string) (string, bool) {
		v, ok := set[name]
		return v, ok
	})

	if l.Source != "env" {
		t.Errorf("EnvLayer names source %q, want env", l.Source)
	}
	if len(l.Values) != 1 {
		t.Errorf("EnvLayer collected %v, want only the declared key. A layer built by scanning the "+
			"environment would pick up the undeclared variable and report it as an unknown key that "+
			"no operator wrote in a file", l.Values)
	}
	if l.Values["giga_executor.occ_enabled"] != "false" {
		t.Errorf("the declared key resolved to %#v, want false", l.Values["giga_executor.occ_enabled"])
	}
}
