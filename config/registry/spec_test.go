package registry_test

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/config/registry"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
)

// Registry behaviour: one declaration per section, and defaults that vary by node mode.
//
// One registration per section carries the struct and a baseline function of node mode. The
// dotted key, the canonical environment spelling and the schema fingerprint all derive from that
// one declaration, so nobody hand-writes a key string.
//
// What this package is not. It is where a key is declared, validated and resolved for reporting,
// not where a running node reads from. app.New must not be handed an in-memory struct: a struct
// silently drops any key it does not model, and a round-trip test over it passes while being
// wrong. What the node reads stays a source carrying every resolved key, whether a section
// declares it or not, so no appOpts.Get call site changes.
//
// Resolve therefore answers for declared keys and nothing more. It feeds a diagnostic or an
// authoring check, and it reads its order from Precedence rather than from its caller's argument
// order, so no caller can reorder its way to a different answer.

// gigaSection mirrors what the giga executor's own package would register. The struct under test
// is the real one, so the key comparison below measures the live reader rather than a copy of it.
const gigaSection = "giga_executor"

func registerGiga(t *testing.T) {
	t.Helper()
	registry.Reset()
	// The baseline this section actually runs, for every mode. An invented mode-varying default here
	// would have tests asserting behaviour no node has, and the registry supporting mode-varying
	// baselines is held below on a section built for that purpose instead.
	registry.RegisterSection(gigaSection, &gigaconfig.Config{}, func(registry.Mode) any {
		return gigaconfig.DefaultConfig
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering %s produced a defect: %v", d.Section, d.Err)
	}
}

// varying is a section whose baseline differs by mode, which giga's does not.
type varying struct {
	PerMode bool `mapstructure:"per_mode"`
	Fixed   bool `mapstructure:"fixed"`
}

// registerVarying registers that section.
//
// Separate from any real section on purpose. Holding the registry's mode support against a real
// section's baseline would make the test fail the day that section's defaults change, and would let a
// behaviour change enter as the fix.
func registerVarying(t *testing.T) {
	t.Helper()
	registry.Reset()
	registry.RegisterSection("varying", &varying{}, func(m registry.Mode) any {
		return varying{PerMode: m != registry.ModeArchive, Fixed: true}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering varying produced a defect: %v", d.Err)
	}
}

// TestKeyIdentityIsDerivedNeverHandTyped holds the property the registry exists for.
//
// The dotted key comes from the section name and the field's mapstructure tag. Measured against
// the live reader's own flag constants, which is the only comparison that proves the derivation
// agrees with what actually resolves today.
func TestKeyIdentityIsDerivedNeverHandTyped(t *testing.T) {
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

// TestAnUntaggedFieldIsADefectNotAFallback is the guard that keeps the tag authoritative.
//
// The node reads configuration today through a binder that falls back to the field's own name.
// That fallback is why an embedded srvconfig.Config with no tag
// put whole cosmos sections under a type-name prefix nothing writes, and why 92 operator-facing
// keys reach their field only through a spelling the tags do not produce. Refusing to guess is
// what makes the tag the single spelling.
//
// Recorded as a defect rather than panicked, because this package is imported by every feature
// and a panic at init takes down every seid invocation including --help.
func TestAnUntaggedFieldIsADefectNotAFallback(t *testing.T) {
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

// TestABaselineVariesByModeAndAnAbsentKeyTracksIt holds that node mode reaches the baseline.
//
// Held on a key whose baseline actually differs between two modes, since a key with one baseline
// everywhere would pass against a registry that ignored mode entirely.
func TestABaselineVariesByModeAndAnAbsentKeyTracksIt(t *testing.T) {
	registerVarying(t)
	s, _ := registry.Lookup("varying")

	archive, ok := s.Defaults(registry.ModeArchive).(varying)
	if !ok {
		t.Fatalf("the baseline returned %T rather than the section's own type", s.Defaults(registry.ModeArchive))
	}
	validator := s.Defaults(registry.ModeValidator).(varying)

	if archive.PerMode {
		t.Error("per_mode is true on an archive node; this section's baseline turns it off there")
	}
	if !validator.PerMode {
		t.Error("per_mode is false on a validator, so the baseline does not vary by mode and " +
			"this test would hold for a registry that ignored mode")
	}
	// And a key that does not vary still resolves, or the assertion above would be the only shape
	// a baseline could take.
	if !archive.Fixed || !validator.Fixed {
		t.Error("fixed differs by mode; it is the same on both in this section's baseline")
	}
}

// TestEveryModeHasABaseline holds that no mode is unreachable.
//
// A mode with no baseline would resolve an absent key to the type's zero rather than to what the
// binary intends, silently, on exactly the nodes running that mode.
func TestEveryModeHasABaseline(t *testing.T) {
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

// TestTheFingerprintMovesOnASchemaChangeAndOnlyThen is what makes forgetting a schema bump
// impossible.
//
// Both directions, because either alone is useless. A key added, renamed or retyped moves it, so
// CI can demand the bump and the migration in the same change. Registering the identical shape
// twice does not, or it would fire on every unrelated commit.
func TestTheFingerprintMovesOnASchemaChangeAndOnlyThen(t *testing.T) {
	registerGiga(t)
	base := registry.Fingerprint()

	registerGiga(t)
	if again := registry.Fingerprint(); again != base {
		t.Errorf("the same registration produced two fingerprints, %s and %s. A check that fires "+
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

// TestAChangedBaselineMovesTheFingerprint holds the half a key-only hash would miss.
//
// A default is the value every node that never wrote the key runs, so changing one changes what
// the fleet does. That is a contract change even though no key moved.
func TestAChangedBaselineMovesTheFingerprint(t *testing.T) {
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

// TestTheEnvironmentSpellingIsCanonicalAndPinned closes a measured legacy defect.
//
// The legacy path answers to three environment universes, and derives its prefix from the running
// binary's filename through path.Base(os.Executable()), so renaming seid moves the whole
// namespace. One spelling, derived from the key, with a pinned prefix.
func TestTheEnvironmentSpellingIsCanonicalAndPinned(t *testing.T) {
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

// TestEnvSpellingCollisionsAreDetectable records the cost of the replacer.
//
// Dots and hyphens both become underscores, so two keys differing only in that punctuation
// collapse onto one variable. The derivation cannot prevent it; a check over the registry can see
// it, and this is what makes that check's input honest.
func TestEnvSpellingCollisionsAreDetectable(t *testing.T) {
	a := registry.EnvName("giga_executor.occ_enabled")
	b := registry.EnvName("giga-executor.occ-enabled")
	if a != b {
		t.Fatalf("expected these to collide onto one variable, got %q and %q; if the replacer has "+
			"changed then the collision check the registry owes is no longer needed", a, b)
	}
}

// ---------------------------------------------------------------------------------------
// Derived keys have to match the keys the node resolves today. Today's readers name each key as an
// explicit dotted string; this package walks a struct instead, so comparing resolved values is not
// enough. A tag typo produces a key no reader ever asks for, and comparing values catches that
// only if a test happens to drive that key.
//
// Every existing operator-facing name is kept. Where an unused tag in the current tree would
// have addressed a cleaner spelling, the cleaner spelling is abandoned rather than migrated,
// because migrating one renames a key operators already have in their files.
// ---------------------------------------------------------------------------------------

// keptSpellings are the operator-facing keys whose unused struct tag would derive something else.
// Each carries the spelling that was abandoned, so the diff shows what the decision gave up.
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

// TestEveryDerivedKeyMatchesTheSpellingOperatorsAlreadyWrite compares keys, not values.
//
// Held per key rather than per value, because that is the failure a value-only comparison cannot
// see: a tag typo produces a key nothing reads, and comparing values catches that only if a test
// happens to drive that key. A struct tagged with the kept spelling has to derive exactly it, and
// must not derive the spelling that was abandoned.
func TestEveryDerivedKeyMatchesTheSpellingOperatorsAlreadyWrite(t *testing.T) {
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

// TestARenameRequiresAnExplicitApprovedEntry is the escape hatch, and the reason the check above
// compares against a list rather than forbidding renames outright.
//
// Keeping every name is today's decision, not a law. A rename is permitted through an explicit
// entry below, and the list starts empty so adding one is a deliberate act visible in a diff.
func TestARenameRequiresAnExplicitApprovedEntry(t *testing.T) {
	if len(approvedRenames) != 0 {
		t.Errorf("the approved-rename list is not empty: %v. Every entry renames a key operators "+
			"have in their files, so each needs its own decision and its own migration",
			approvedRenames)
	}
}

// approvedRenames names every key whose operator-facing spelling a decision deliberately changed.
// Empty by decision. An entry here permits the derived key to differ from what operators write
// today, and it owes a migration in the same change.
var approvedRenames = map[string]string{}

// TestTheDeadFieldIsNotReachableUnderTheLiveKey holds a trap tracked as PLT-945.
//
// state-commit.sc-async-commit-buffer resolves today to the live MemIAVLConfig field.
// StateCommitConfig.AsyncCommitBuffer carries the inert tag async-commit-buffer and nothing reads
// it. A tag-driven binder that inherited that tag would bind an operator's value to the dead field
// while the spelling they actually write reached no field at all.
//
// The trap is invisible today precisely because nothing in the tree reads by tag, and it goes live
// the moment a registry does. Held by asserting the two tags derive different keys, so inheriting
// the wrong one cannot silently produce the right-looking path.
func TestTheDeadFieldIsNotReachableUnderTheLiveKey(t *testing.T) {
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
// Resolution: which layer's value wins, and where a resolved value came from.
// ---------------------------------------------------------------------------------------

// TestResolutionRunsInTheDeclaredOrder shuffles the layers, which is what makes it falsifiable.
//
// If precedence comes from Precedence, the order layers are passed in cannot matter. If it comes
// from argument order or from a merge where the last writer wins, this fails. The legacy path fails
// it by construction: its answer depends on which viper instance a caller asked, which is why two
// different orders are observable across its key set.
func TestResolutionRunsInTheDeclaredOrder(t *testing.T) {
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

// TestEachLayerWinsOverTheOneBelowIt walks the order one step at a time.
//
// The test above only proves the top layer wins. This proves the ordering is the declared one at
// every step, so a resolver that happened to prefer "flag" for an unrelated reason would still fail.
func TestEachLayerWinsOverTheOneBelowIt(t *testing.T) {
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

// TestAnAbsentKeyTracksItsModeBaseline is why the baseline is a layer rather than a fallback.
//
// A key no layer mentions still resolves, to the baseline for the running mode. That is what makes
// an absent key track the binary's judgement rather than a zero value: a default lives in the
// binary and is never written into an operator's file.
func TestAnAbsentKeyTracksItsModeBaseline(t *testing.T) {
	registerVarying(t)

	archive, err := registry.Resolve(registry.ModeArchive)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	validator, err := registry.Resolve(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	a, _ := archive.From("varying.per_mode")
	v, _ := validator.From("varying.per_mode")
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

// TestProvenanceIsRecoverable is the property the legacy path cannot offer at all.
//
// Its layers combine inside one viper before anything observes them, so no value's source is
// recoverable and it can never tell an operator which one won. Overrides is what a diff renders: the
// keys an operator has taken responsibility for, as distinct from those tracking the binary.
func TestProvenanceIsRecoverable(t *testing.T) {
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

// TestAKeyNoSectionDeclaresIsReportedNotDropped covers a key nothing declares.
//
// Silently dropping one is how an operator's typo becomes invisible. Reported rather than an error,
// because what to do about it differs: a generate path may refuse, while a boot on an operator's
// existing file must not.
func TestAKeyNoSectionDeclaresIsReportedNotDropped(t *testing.T) {
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

// TestALayerWithNoDeclaredPriorityIsAnError refuses a layer whose priority is undefined.
//
// A layer whose source is absent from Precedence has no defined priority. Ignoring it silently is
// worse than refusing: nothing downstream could tell the layer had contributed nothing.
func TestALayerWithNoDeclaredPriorityIsAnError(t *testing.T) {
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

// TestEnvLayerIsDrivenByTheDeclaredSet holds the direction that makes the layer complete.
//
// An environment cannot be enumerated for a prefix: a variable is findable only if its name is
// already known. Asking for every declared key's canonical spelling is therefore the only way to
// build a complete env layer, and it is why the derivation lives beside the registry.
func TestEnvLayerIsDrivenByTheDeclaredSet(t *testing.T) {
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

// TestAFieldMarkedNotFromConfigContributesNoKey holds mapstructure's own skip idiom.
//
// A dash is how mapstructure says a field is not populated from configuration. Such a field has no key,
// so deriving one would declare a key nothing reads and leave doctor refusing an operator's file. Read as
// an error instead, it refuses the whole section, and every one of that section's keys silently falls
// back to the legacy path.
//
// Both are silent in their own way, which is why this is held rather than left to whichever reading the
// code happened to take. receipt-store carries two such fields, documented as derived at the app layer
// rather than read from configuration, and it was the first section that could not register because of it.
func TestAFieldMarkedNotFromConfigContributesNoKey(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &struct {
		Read       bool `mapstructure:"read"`
		NotFromCfg int  `mapstructure:"-"`
		AlsoRead   bool `mapstructure:"also_read"`
	}{}, func(registry.Mode) any { return struct{}{} })

	for _, d := range registry.Defects() {
		t.Fatalf("a field marked as not from configuration was refused: %v.\n\nThe section does not "+
			"register at all, so every key it declares silently reads from the legacy path instead", d.Err)
	}
	s, ok := registry.Lookup("probe")
	if !ok {
		t.Fatal("the section did not register")
	}
	if strings.Join(s.Keys, ",") != "probe.also_read,probe.read" {
		t.Errorf("derived %v, want only the two fields that are read from configuration. A key derived "+
			"for a field nothing populates is one doctor would refuse in an operator's file", s.Keys)
	}
}

// TestAnEmptyMapstructureNameIsStillAnError keeps the fix above from swallowing the real mistake.
//
// A dash says something deliberate. An empty name says nothing, and a key with an empty final segment
// matches no written key, so it stays a defect.
//
// The probe carries a validly named field alongside the nameless one on purpose. With only the
// nameless field, a rule that quietly skipped it would still be refused, for declaring no keys at
// all, and this would pass while proving nothing. With two fields the section registers under that
// rule and the nameless field vanishes without a word, which is the outcome being refused here.
func TestAnEmptyMapstructureNameIsStillAnError(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &struct {
		Read     bool `mapstructure:"read"`
		Nameless bool `mapstructure:""`
	}{}, func(registry.Mode) any { return struct{}{} })

	defects := registry.Defects()
	if len(defects) != 1 {
		t.Fatalf("an empty mapstructure name produced %d defects, want 1. It is not the skip idiom and "+
			"a key ending in nothing matches no written key", len(defects))
	}
	if !strings.Contains(defects[0].Err.Error(), "Nameless") {
		t.Errorf("the defect is %q and does not name the field at fault, so whoever reads it has to "+
			"find which of the struct's fields is the problem", defects[0].Err)
	}
	if _, ok := registry.Lookup("probe"); ok {
		t.Error("the section registered despite the defect, so the nameless field is absent from the " +
			"key space and nothing says so")
	}
}

// TestOnlyAFlagTheOperatorChangedContributes is the whole of the flag layer's contract.
//
// A registered flag always answers, with its registration default when nobody typed it. A layer that
// carried those would put every default at the top of the order, above the file, and an operator's
// written value would lose to a default they never chose. That is the same inversion as ignoring the
// flag, arriving from the other side.
func TestOnlyAFlagTheOperatorChangedContributes(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &struct {
		Typed   int `mapstructure:"typed"`
		Untyped int `mapstructure:"untyped"`
	}{}, func(registry.Mode) any {
		return struct {
			Typed   int `mapstructure:"typed"`
			Untyped int `mapstructure:"untyped"`
		}{Typed: 1, Untyped: 2}
	})

	// A command where the operator typed one flag and left the other at its registration default.
	layer := registry.FlagLayer(func(key string) (string, bool) {
		if key == "probe.typed" {
			return "99", true
		}
		return "", false
	})

	if layer.Source != "flag" {
		t.Errorf("the layer names source %q, so Resolve cannot place it in the declared order", layer.Source)
	}
	if got, ok := layer.Values["probe.typed"]; !ok || got != "99" {
		t.Errorf("the flag the operator set contributed %v (present=%v), want \"99\"", got, ok)
	}
	if _, ok := layer.Values["probe.untyped"]; ok {
		t.Error("a flag nobody changed contributed a value. Its registration default would then sit " +
			"above the operator's file, so a value they wrote down would lose to one they never chose")
	}
}

// TestAFlagBeatsTheFileAndTheFileBeatsTheBaseline is the order an operator is promised.
func TestAFlagBeatsTheFileAndTheFileBeatsTheBaseline(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &struct {
		Both  int `mapstructure:"both"`
		File  int `mapstructure:"file"`
		Plain int `mapstructure:"plain"`
	}{}, func(registry.Mode) any {
		return struct {
			Both  int `mapstructure:"both"`
			File  int `mapstructure:"file"`
			Plain int `mapstructure:"plain"`
		}{Both: 1, File: 1, Plain: 1}
	})

	resolved, err := registry.Resolve(registry.ModeFull,
		registry.FileLayer(map[string]any{"probe.both": 2, "probe.file": 2}),
		registry.FlagLayer(func(key string) (string, bool) {
			if key == "probe.both" {
				return "3", true
			}
			return "", false
		}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	for _, want := range []struct {
		key   string
		value any
		from  string
	}{
		{"probe.both", "3", "flag"},
		{"probe.file", 2, "file"},
		{"probe.plain", 1, "default"},
	} {
		got := resolved.Keys[want.key]
		if got.Value != want.value || got.From != want.from {
			t.Errorf("%s resolved to %#v from %q, want %#v from %q. An operator cannot be told which "+
				"source won if the order is not this one", want.key, got.Value, got.From, want.value, want.from)
		}
	}
}
