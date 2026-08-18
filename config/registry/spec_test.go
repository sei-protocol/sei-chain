package registry_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/config/registry"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
)

// Registry behaviour: one declaration per section, and defaults that vary by node mode.
//
// One registration per section carries the struct and a baseline function of node mode. The
// dotted key, the canonical environment spelling and the read site all derive from that one
// declaration, so nobody hand-writes a key string.
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
	registry.RegisterSection(gigaSection, &gigaconfig.Config{}, func(m registry.Mode) any {
		// The design's own worked example: OCC is off on an archive node.
		return gigaconfig.Config{Enabled: true, OCCEnabled: m != registry.ModeArchive}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering %s produced a defect: %v", d.Section, d.Err)
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
	registerGiga(t)
	s, _ := registry.Lookup(gigaSection)

	archive, ok := s.Defaults(registry.ModeArchive).(gigaconfig.Config)
	if !ok {
		t.Fatalf("the baseline returned %T rather than the section's own type", s.Defaults(registry.ModeArchive))
	}
	validator := s.Defaults(registry.ModeValidator).(gigaconfig.Config)

	if archive.OCCEnabled {
		t.Error("occ_enabled is true on an archive node; this section's baseline turns it off there")
	}
	if !validator.OCCEnabled {
		t.Error("occ_enabled is false on a validator, so the baseline does not vary by mode and " +
			"this test would hold for a registry that ignored mode")
	}
	// And a key that does not vary still resolves, or the assertion above would be the only shape
	// a baseline could take.
	if !archive.Enabled || !validator.Enabled {
		t.Error("enabled differs by mode; it is the same on both in this section's baseline")
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
	if !strings.HasPrefix(registry.EnvName("a.b"), "SEID_") {
		t.Error("the namespace is not applied, so a key could answer to a variable outside it")
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

// TestEveryDeclaredKeyResolves holds Resolve to the property its documentation states.
//
// A section whose baseline leaves an optional subtree nil declares keys the baseline states no value
// for, because the type walk unwraps a pointer to derive its keys and the value walk skips a nil one.
// Either every declared key resolves, or Resolve names the ones that cannot. A silent hole is the
// worse of the two: Overrides never reports the key and From answers ok=false, so a diagnostic that
// renders Keys and calls From prints nothing for it and looks correct.
func TestEveryDeclaredKeyResolves(t *testing.T) {
	type inner struct {
		Cert string `mapstructure:"cert"`
	}
	type optional struct {
		Name string `mapstructure:"name"`
		TLS  *inner `mapstructure:"tls"`
	}

	registry.Reset()
	registry.RegisterSection("svc", &optional{}, func(registry.Mode) any {
		return optional{Name: "n"} // TLS left nil, which is how a baseline arrives short
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering the probe section produced a defect: %v", d.Err)
	}

	for _, m := range registry.Modes() {
		res, err := registry.Resolve(m)
		if err != nil {
			// A named refusal is an answer. A resolution missing a declared key is not.
			continue
		}
		for _, key := range registry.Keys() {
			if _, ok := res.From(key); !ok {
				t.Errorf("mode %s: %q is declared and has no resolution, so a caller iterating Keys "+
					"and calling From is handed an absence the documentation rules out", m, key)
			}
		}
	}
}

// TestADivergentBaselineIsRefusedRatherThanPanicking pins the other half of the same seam.
//
// The two walks share tagOf and are meant to traverse the same type, and nothing held them to it: the
// prototype's type derives the keys while whatever Defaults returns supplies the values. A baseline
// whose type squashes a non-struct reached reflect.NumField on a string, so Resolve panicked in a
// package whose stated posture is that a bad registration never does.
func TestADivergentBaselineIsRefusedRatherThanPanicking(t *testing.T) {
	// Both embedded types are exported on purpose. An unexported embed is skipped before its squash
	// tag is read, so a probe using one would pass while exercising nothing.
	type Good struct {
		Addr string `mapstructure:"addr"`
	}
	type prototype struct {
		Good `mapstructure:",squash"`
		Name string `mapstructure:"name"`
	}
	type NotAStruct string
	type divergent struct {
		NotAStruct `mapstructure:",squash"`
		Name       string `mapstructure:"name"`
	}

	registry.Reset()
	registry.RegisterSection("svc", &prototype{}, func(registry.Mode) any { return divergent{Name: "n"} })
	for _, d := range registry.Defects() {
		t.Fatalf("the prototype is valid and should register cleanly, got: %v", d.Err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Resolve panicked on a divergent baseline: %v", r)
		}
	}()
	if _, err := registry.Resolve(registry.ModeFull); err == nil {
		t.Error("a baseline whose type is not the registered struct's resolved without complaint, so " +
			"every declared key silently carried no value")
	}
}

// TestARealisticSectionShapeDerivesAndResolvesEveryKey drives the shapes real sections have.
//
// The tests above register flat structs of scalars, which is the one shape no upstream configuration
// struct actually is. A section carries a nested struct for a sub-table, an embedded base it squashes
// so the base adds no segment, a duration that must stay a value rather than becoming a group of keys,
// and sometimes a pointer that is set. Each of those takes a different branch of the walk, and both
// walks have to agree on all of them or a key derives with no value behind it.
func TestARealisticSectionShapeDerivesAndResolvesEveryKey(t *testing.T) {
	type Base struct {
		Laddr string `mapstructure:"laddr"`
	}
	type sub struct {
		Size int           `mapstructure:"size"`
		Wait time.Duration `mapstructure:"wait"`
	}
	type shaped struct {
		Base    `mapstructure:",squash"`
		Enabled bool          `mapstructure:"enabled"`
		Timeout time.Duration `mapstructure:"timeout"`
		Cache   sub           `mapstructure:"cache"`
		Spill   *sub          `mapstructure:"spill"`
	}

	registry.Reset()
	registry.RegisterSection("shaped", &shaped{}, func(registry.Mode) any {
		return shaped{
			Base:    Base{Laddr: "tcp://0.0.0.0:1"},
			Enabled: true,
			Timeout: 30 * time.Second,
			Cache:   sub{Size: 64, Wait: time.Second},
			Spill:   &sub{Size: 8, Wait: 2 * time.Second}, // set, so the pointer branch is taken
		}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering a realistic shape produced a defect: %v", d.Err)
	}

	// The squashed base adds no segment, the nested structs each add one, and neither duration
	// becomes a group. A duration walked into would declare time.Duration's own fields instead.
	want := []string{
		"shaped.cache.size", "shaped.cache.wait",
		"shaped.enabled",
		"shaped.laddr",
		"shaped.spill.size", "shaped.spill.wait",
		"shaped.timeout",
	}
	if got := registry.Keys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("declared keys are\n  %v\nwant\n  %v", got, want)
	}

	resolved, err := registry.Resolve(registry.ModeFull)
	if err != nil {
		t.Fatalf("resolving a realistic shape: %v", err)
	}
	for key, want := range map[string]any{
		"shaped.laddr":      "tcp://0.0.0.0:1",
		"shaped.enabled":    true,
		"shaped.timeout":    30 * time.Second,
		"shaped.cache.size": 64,
		"shaped.cache.wait": time.Second,
		"shaped.spill.size": 8,
		"shaped.spill.wait": 2 * time.Second,
	} {
		res, ok := resolved.From(key)
		if !ok {
			t.Errorf("%s declared and did not resolve", key)
			continue
		}
		if res.Value != want {
			t.Errorf("%s resolved to %#v, want %#v", key, res.Value, want)
		}
	}
}

// TestEveryRefusalIsReportedAsADefect drives the refusals this package's posture rests on.
//
// The doc says a registration it cannot use is recorded rather than panicked, and that refusing to
// guess is what keeps the tag authoritative. Only the untagged field was covered. A refusal nothing
// exercises is a refusal that can stop working, and each of these is the difference between a key an
// operator writes reaching its field and reaching nothing.
func TestEveryRefusalIsReportedAsADefect(t *testing.T) {
	// Exported on purpose. walk skips an unexported field before tagOf reads its tag, so an
	// unexported embed would report "declares no keys" and this row would pass while proving nothing
	// about the squash rule.
	type Good struct {
		N string `mapstructure:"n"`
	}
	type squashNamed struct {
		Good `mapstructure:"base,squash"`
	}
	type emptyName struct {
		N string `mapstructure:""`
	}
	type dashName struct {
		N string `mapstructure:"-"`
	}
	type upperName struct {
		N string `mapstructure:"N"`
	}
	type squashScalar struct {
		Alias string `mapstructure:",squash"`
		N     string `mapstructure:"n"`
	}
	type noKeys struct {
		unexported string //nolint:unused // the point is that nothing is declared
	}

	for _, tc := range []struct {
		name    string
		section string
		proto   any
		base    func(registry.Mode) any
		want    string
	}{
		{"a squashed field that also names a segment", "s", &squashNamed{}, ok1, "one or the other"},
		{"an empty mapstructure name", "s", &emptyName{}, ok1, "empty mapstructure name"},
		{"a dash mapstructure name", "s", &dashName{}, ok1, "empty mapstructure name"},
		{"an upper-case key", "s", &upperName{}, ok1, "not lower case"},
		{"a squashed scalar", "s", &squashScalar{}, ok1, "not a struct"},
		{"a struct declaring nothing", "s", &noKeys{}, ok1, "declares no keys"},
		{"an empty section name", "", &Good{}, ok1, "section name is empty"},
		{"an upper-case section name", "Sec", &Good{}, ok1, "not lower case"},
		{"no struct at all", "s", nil, ok1, "no struct"},
		{"a non-struct prototype", "s", "string", ok1, "not a struct"},
		{"no baseline function", "s", &Good{}, nil, "no baseline function"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry.Reset()
			registry.RegisterSection(tc.section, tc.proto, tc.base)

			if _, registered := registry.Lookup(tc.section); registered {
				t.Errorf("the section registered despite %s, so its keys join the declared set", tc.name)
			}
			defects := registry.Defects()
			if len(defects) != 1 {
				t.Fatalf("got %d defects, want exactly one naming %s", len(defects), tc.name)
			}
			if msg := defects[0].Err.Error(); !strings.Contains(msg, tc.want) {
				t.Errorf("the refusal reads %q, which does not mention %q", msg, tc.want)
			}
		})
	}
}

// ok1 is a baseline for the refusal table, where the baseline is not what is under test.
func ok1(registry.Mode) any { return struct{}{} }

// TestASectionRegisteredTwiceIsRefused pins the one refusal a table cannot express, since it needs
// two calls under one name.
//
// The second registration losing silently would leave whichever struct registered first deciding the
// key space, and which one that is depends on package initialisation order.
func TestASectionRegisteredTwiceIsRefused(t *testing.T) {
	type first struct {
		A string `mapstructure:"a"`
	}
	type second struct {
		B string `mapstructure:"b"`
	}
	registry.Reset()
	registry.RegisterSection("dup", &first{}, func(registry.Mode) any { return first{} })
	registry.RegisterSection("dup", &second{}, func(registry.Mode) any { return second{} })

	defects := registry.Defects()
	if len(defects) != 1 {
		t.Fatalf("got %d defects, want one naming the repeat", len(defects))
	}
	if msg := defects[0].Err.Error(); !strings.Contains(msg, "registered twice") {
		t.Errorf("the refusal reads %q, which does not mention the repeat", msg)
	}
	if keys := registry.Keys(); !reflect.DeepEqual(keys, []string{"dup.a"}) {
		t.Errorf("the declared set is %v; the first registration should stand alone", keys)
	}
}

// TestTwoLayersNamingOneSourceIsAnError pins the last resolution refusal.
//
// Two layers under one source name have no order between them, so one would win by map iteration and
// the other would vanish with nothing able to report it.
func TestTwoLayersNamingOneSourceIsAnError(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &struct {
		A string `mapstructure:"a"`
	}{}, func(registry.Mode) any {
		return struct {
			A string `mapstructure:"a"`
		}{A: "base"}
	})

	_, err := registry.Resolve(registry.ModeFull,
		registry.FileLayer(map[string]any{"probe.a": "one"}),
		registry.FileLayer(map[string]any{"probe.a": "two"}))
	if err == nil {
		t.Fatal("two file layers resolved without complaint, so one of them lost silently")
	}
	if !strings.Contains(err.Error(), "silently lose") {
		t.Errorf("the error reads %q, which does not say what the cost is", err)
	}
}

// TestAStructThatIsAValueStaysOneKey pins the stop the walk needs at a decoded-whole type.
//
// time.Time carries exported fields, so walking into it would declare keys for its internals that no
// operator writes and no decoder fills. It has to arrive as one value under its own key.
//
// Only the struct-kinded entries in isLeaf are reachable. The walk asks isLeaf only after finding a
// struct, and time.Duration is an int64, so it takes the leaf path on its kind alone and the
// "time.Duration" entry in that list never matches.
func TestAStructThatIsAValueStaysOneKey(t *testing.T) {
	type withTime struct {
		At   time.Time `mapstructure:"at"`
		Name string    `mapstructure:"name"`
	}
	stamp := time.Unix(0, 0).UTC()

	registry.Reset()
	registry.RegisterSection("stamped", &withTime{}, func(registry.Mode) any {
		return withTime{At: stamp, Name: "n"}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering a section with a time.Time produced a defect: %v", d.Err)
	}

	want := []string{"stamped.at", "stamped.name"}
	if got := registry.Keys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("declared keys are %v, want %v; a walked time.Time would add its own fields", got, want)
	}
	resolved, err := registry.Resolve(registry.ModeFull)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	res, ok := resolved.From("stamped.at")
	if !ok {
		t.Fatal("stamped.at declared and did not resolve")
	}
	if res.Value != stamp {
		t.Errorf("stamped.at resolved to %#v, want the whole time %#v", res.Value, stamp)
	}
}

// TestABaselineThatIsNotAStructIsRefused covers the guards on what Defaults hands back.
//
// A baseline is a func returning any, so nothing at the type level stops it returning nil or a
// scalar. Both would otherwise reach the value walk, which expects a struct.
func TestABaselineThatIsNotAStructIsRefused(t *testing.T) {
	type one struct {
		A string `mapstructure:"a"`
	}
	for _, tc := range []struct {
		name string
		base func(registry.Mode) any
		want string
	}{
		{"nil", func(registry.Mode) any { return nil }, "no value"},
		{"a scalar", func(registry.Mode) any { return "string" }, "not a struct"},
		{"a nil pointer", func(registry.Mode) any { return (*one)(nil) }, "nil *"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry.Reset()
			registry.RegisterSection("probe", &one{}, tc.base)
			for _, d := range registry.Defects() {
				t.Fatalf("the prototype is valid and should register: %v", d.Err)
			}
			_, err := registry.Resolve(registry.ModeFull)
			if err == nil {
				t.Fatalf("a baseline returning %s resolved without complaint", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal reads %q, which does not mention %q", err, tc.want)
			}
		})
	}
}

// TestABaselineCarryingAnUndeclaredKeyIsRefused covers the other direction of matchesDeclaration.
//
// A baseline whose type is not the prototype's can state keys the section never declared as well as
// omit ones it did. Reported as its own case, because the cause is the same and a reader chasing one
// message should not have to guess that the other exists.
func TestABaselineCarryingAnUndeclaredKeyIsRefused(t *testing.T) {
	type declared struct {
		A string `mapstructure:"a"`
	}
	type wider struct {
		A string `mapstructure:"a"`
		B string `mapstructure:"b"`
	}
	registry.Reset()
	registry.RegisterSection("probe", &declared{}, func(registry.Mode) any {
		return wider{A: "a", B: "b"}
	})
	_, err := registry.Resolve(registry.ModeFull)
	if err == nil {
		t.Fatal("a baseline stating an undeclared key resolved without complaint")
	}
	if !strings.Contains(err.Error(), "does not declare") {
		t.Errorf("the refusal reads %q, which does not say the baseline is the wrong type", err)
	}
}

// TestARefusalInsideANestedStructNamesItsPath pins that a refusal survives the recursion.
//
// Both walks recurse, so a bad tag can sit any depth down. If the refusal did not carry the prefix a
// reader would be told a field name with no way to find which subtree it is in, and if it did not
// propagate at all the section would register with the subtree missing.
func TestARefusalInsideANestedStructNamesItsPath(t *testing.T) {
	type deep struct {
		Untagged string // no mapstructure tag, two levels down
	}
	type mid struct {
		Deep deep `mapstructure:"deep"`
	}
	type outer struct {
		Mid  mid    `mapstructure:"mid"`
		Name string `mapstructure:"name"`
	}

	registry.Reset()
	registry.RegisterSection("nest", &outer{}, func(registry.Mode) any { return outer{} })

	if _, registered := registry.Lookup("nest"); registered {
		t.Error("the section registered with an untagged field two levels down")
	}
	defects := registry.Defects()
	if len(defects) != 1 {
		t.Fatalf("got %d defects, want one naming the nested field", len(defects))
	}
	if msg := defects[0].Err.Error(); !strings.Contains(msg, "nest.mid.deep.Untagged") {
		t.Errorf("the refusal reads %q; it should name the full path so a reader can find the field", msg)
	}
}

// TestSectionsAreSortedAndABaselineMayBeAPointer covers the shapes a caller is free to choose.
//
// Sections promises an order, and a caller reading two sections in registration order would see one
// that depends on map iteration. A baseline may hand back a pointer, since a section holding a large
// struct has no reason to copy it, and the value walk has to follow that.
func TestSectionsAreSortedAndABaselineMayBeAPointer(t *testing.T) {
	type withHidden struct {
		Shown  string `mapstructure:"shown"`
		hidden string //nolint:unused // ordinary internal state, and not a declared key
	}
	type second struct {
		B string `mapstructure:"b"`
	}

	registry.Reset()
	// Registered out of order, so the sort is what puts them in order.
	registry.RegisterSection("zulu", &second{}, func(registry.Mode) any { return second{B: "z"} })
	registry.RegisterSection("alpha", &withHidden{}, func(registry.Mode) any {
		return &withHidden{Shown: "a", hidden: "not a key"} // a pointer, and an unexported field
	})
	for _, d := range registry.Defects() {
		t.Fatalf("an unexported untagged field is ordinary state and must not be a defect: %v", d.Err)
	}

	var names []string
	for _, s := range registry.Sections() {
		names = append(names, s.Name)
	}
	if !reflect.DeepEqual(names, []string{"alpha", "zulu"}) {
		t.Errorf("Sections returned %v, want them sorted; registration order must not decide it", names)
	}

	resolved, err := registry.Resolve(registry.ModeFull)
	if err != nil {
		t.Fatalf("a pointer baseline was refused: %v", err)
	}
	if res, ok := resolved.From("alpha.shown"); !ok || res.Value != "a" {
		t.Errorf("alpha.shown resolved to (%#v, %v) through a pointer baseline, want \"a\"", res.Value, ok)
	}
	if keys := registry.Keys(); !reflect.DeepEqual(keys, []string{"alpha.shown", "zulu.b"}) {
		t.Errorf("declared keys are %v; the unexported field must not become one", keys)
	}
}

// TestABaselineOfAWhollyDifferentTypeSaysSoOnce covers the message for a baseline that both omits a
// declared key and states one that was never declared.
//
// Two separate errors would send a reader chasing the second after fixing the first, when the cause is
// one thing: the baseline is not the registered struct's type.
func TestABaselineOfAWhollyDifferentTypeSaysSoOnce(t *testing.T) {
	type declared struct {
		A string `mapstructure:"a"`
	}
	type unrelated struct {
		Z string `mapstructure:"z"`
	}
	registry.Reset()
	registry.RegisterSection("probe", &declared{}, func(registry.Mode) any { return unrelated{Z: "z"} })

	_, err := registry.Resolve(registry.ModeFull)
	if err == nil {
		t.Fatal("a baseline of an unrelated type resolved without complaint")
	}
	msg := err.Error()
	if !strings.Contains(msg, "not the registered struct's type") {
		t.Errorf("the refusal reads %q; it should name the cause rather than the two symptoms", msg)
	}
	for _, want := range []string{"probe.a", "probe.z"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not mention %q, so a reader cannot see both halves", want)
		}
	}
}

// TestARefusalInsideASquashedBaseIsReported is the squash arm of the nested case above.
//
// A squashed base promotes its fields to the section's own level, so a bad tag inside one is a bad key
// at the top of the section rather than in a subtree. Squash and the named-subtree case recurse
// through different branches, so covering one says nothing about the other.
func TestARefusalInsideASquashedBaseIsReported(t *testing.T) {
	type Base struct {
		Untagged string // promoted to the section's own level, and unaddressable
	}
	type squashed struct {
		Base `mapstructure:",squash"`
		Name string `mapstructure:"name"`
	}

	registry.Reset()
	registry.RegisterSection("sq", &squashed{}, func(registry.Mode) any { return squashed{} })

	if _, registered := registry.Lookup("sq"); registered {
		t.Error("the section registered with an untagged field in its squashed base")
	}
	defects := registry.Defects()
	if len(defects) != 1 {
		t.Fatalf("got %d defects, want one naming the promoted field", len(defects))
	}
	// The prefix is the section, not a subtree, because squash adds no segment.
	if msg := defects[0].Err.Error(); !strings.Contains(msg, "sq.Untagged") {
		t.Errorf("the refusal reads %q; a squashed field's path is the section's own", msg)
	}
}
