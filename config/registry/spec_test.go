package registry_test

import (
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/config/registry"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
)

// Registry behaviour: one declaration per section, and defaults that vary by node mode.
//
// One registration per section carries the struct and a default that varies by node mode. The
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
// authoring check, and it reads its order from Source's declaration rather than from its caller's
// argument order, so no caller can reorder its way to a different answer.

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

// TestADefaultVariesByModeAndAnAbsentKeyTracksIt holds that node mode reaches the default.
//
// Held on a key whose default actually differs between two modes, since a key with one default
// everywhere would pass against a registry that ignored mode entirely.
func TestADefaultVariesByModeAndAnAbsentKeyTracksIt(t *testing.T) {
	registerGiga(t)
	s, _ := registry.Lookup(gigaSection)

	archive, ok := s.Defaults(registry.ModeArchive).(gigaconfig.Config)
	if !ok {
		t.Fatalf("the default returned %T rather than the section's own type", s.Defaults(registry.ModeArchive))
	}
	validator := s.Defaults(registry.ModeValidator).(gigaconfig.Config)

	if archive.OCCEnabled {
		t.Error("occ_enabled is true on an archive node; this section's default turns it off there")
	}
	if !validator.OCCEnabled {
		t.Error("occ_enabled is false on a validator, so the default does not vary by mode and " +
			"this test would hold for a registry that ignored mode")
	}
	// And a key that does not vary still resolves, or the assertion above would be the only shape
	// a default could take.
	if !archive.Enabled || !validator.Enabled {
		t.Error("enabled differs by mode; it is the same on both in this section's default")
	}
}

// TestEveryModeHasADefault holds that no mode is unreachable.
//
// A mode with no default would resolve an absent key to the type's zero rather than to what the
// binary intends, silently, on exactly the nodes running that mode.
func TestEveryModeHasADefault(t *testing.T) {
	registerGiga(t)
	s, _ := registry.Lookup(gigaSection)

	for _, m := range registry.Modes() {
		if s.Defaults(m) == nil {
			t.Errorf("mode %q has no default, so an absent key on that node resolves to a zero "+
				"rather than to the binary's judgement", m)
		}
	}
}

// TestModesMatchTheNodeModes holds registry.Mode against the modes a node actually declares.
//
// registry.Mode is declared locally so this package stays a leaf every feature can import. That
// duplication is only safe while the two sets agree, and this is what makes a mode added to
// app/params fail here rather than silently having no default anywhere.
func TestModesMatchTheNodeModes(t *testing.T) {
	node := map[string]bool{
		string(params.NodeModeValidator): true,
		string(params.NodeModeFull):      true,
		string(params.NodeModeSeed):      true,
		string(params.NodeModeArchive):   true,
	}
	for _, m := range registry.Modes() {
		if !node[string(m)] {
			t.Errorf("registry declares mode %q, which app/params does not; a default for a mode "+
				"no node runs is dead, and a mode with no default is worse", m)
		}
		delete(node, string(m))
	}
	for left := range node {
		t.Errorf("app/params declares mode %q and the registry has no default input for it, so a "+
			"section cannot vary its default on the mode some nodes actually run", left)
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
// Dots and hyphens both become underscores, so two keys differing only in that punctuation collapse
// onto one variable. The derivation cannot prevent it, which is why registration refuses the pair;
// this holds the input that refusal reads. TestAKeySharingAnEnvironmentSpellingIsRefused holds the
// refusal itself.
func TestEnvSpellingCollisionsAreDetectable(t *testing.T) {
	a := registry.EnvName("giga_executor.occ_enabled")
	b := registry.EnvName("giga-executor.occ-enabled")
	if a != b {
		t.Fatalf("expected these to collide onto one variable, got %q and %q; if the replacer has "+
			"changed then the registration refusal is guarding a collision that no longer exists", a, b)
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
// Resolution: which source's value wins, and what a caller is told about how a key got its value.
// ---------------------------------------------------------------------------------------

// TestEachSourceWinsOverTheOneBelowIt walks the precedence one step at a time.
//
// Each case adds the next source up and expects its value, so a resolver that happened to prefer the
// top source for an unrelated reason still fails on the middle steps. The value each source supplies
// is its own name, so which one won is readable from the resolved value alone.
func TestEachSourceWinsOverTheOneBelowIt(t *testing.T) {
	const key = "giga_executor.occ_enabled"
	env := func(v string) func(string) (string, bool) {
		return func(name string) (string, bool) {
			if name == registry.EnvName(key) {
				return v, true
			}
			return "", false
		}
	}

	for _, tc := range []struct {
		name string
		from registry.Sources
		want any
	}{
		{"the default alone", registry.Sources{}, true},
		{"the file over the default", registry.Sources{
			File: map[string]any{key: "file"}}, "file"},
		{"the environment over the file", registry.Sources{
			File: map[string]any{key: "file"}, LookupEnv: env("env")}, "env"},
		{"a flag over the environment", registry.Sources{
			LookupEnv: env("env"), Flags: map[string]any{key: "flag"}}, "flag"},
		{"a flag over every source below it", registry.Sources{
			File: map[string]any{key: "file"}, LookupEnv: env("env"),
			Flags: map[string]any{key: "flag"}}, "flag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registerGiga(t)

			got, err := registry.Resolve(registry.ModeValidator, tc.from)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.Values[key] != tc.want {
				t.Errorf("%s resolved to %#v, want %#v; a source lower in the precedence won",
					tc.name, got.Values[key], tc.want)
			}
		})
	}
}

// TestAnAbsentKeyTracksItsModeDefault is why the default is a layer rather than a fallback.
//
// A key no layer mentions still resolves, to the default for the running mode. That is what makes
// an absent key track the binary's judgement rather than a zero value: a default lives in the
// binary and is never written into an operator's file.
func TestAnAbsentKeyTracksItsModeDefault(t *testing.T) {
	registerGiga(t)

	archive, err := registry.Resolve(registry.ModeArchive, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	validator, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	const key = "giga_executor.occ_enabled"
	if len(archive.Overrides) != 0 || len(validator.Overrides) != 0 {
		t.Fatalf("no source was passed and %v and %v are reported as overrides, so a key tracking the "+
			"binary would render as one an operator chose", archive.Overrides, validator.Overrides)
	}
	a, v := archive.Values[key], validator.Values[key]
	if a == v {
		t.Errorf("both modes resolved the same value %#v for a key whose default varies by mode, so "+
			"the mode is not reaching the default", a)
	}
	if a != false || v != true {
		t.Errorf("archive=%#v validator=%#v, want false and true from this section's default", a, v)
	}
}

// TestAWrittenValueIsSeparableFromADefault is the property the legacy path cannot offer at all.
//
// Its layers combine inside one viper before anything observes them, so a written value and a default
// are indistinguishable afterwards. Overrides is what a diff renders: the keys an operator has taken
// responsibility for, as distinct from those tracking the binary.
func TestAWrittenValueIsSeparableFromADefault(t *testing.T) {
	registerGiga(t)

	const written = "giga_executor.occ_enabled"
	got, err := registry.Resolve(registry.ModeValidator, registry.Sources{
		File: map[string]any{written: false},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if !reflect.DeepEqual(got.Overrides, []string{written}) {
		t.Errorf("Overrides is %v, want the one key a source supplied. A resolver that cannot separate "+
			"a written value from a default cannot tell an operator what they changed", got.Overrides)
	}
	// Written and equal to the mode's own default, so a resolver comparing values rather than tracking
	// which source supplied the key would leave this out.
	if got.Values[written] != false {
		t.Errorf("the written key resolved to %#v, want false", got.Values[written])
	}
	if _, declared := got.Values["giga_executor.enabled"]; !declared {
		t.Error("the untouched key did not resolve, so an override was recorded by dropping the rest")
	}
}

// TestAFileKeyIsMatchedRegardlessOfCase covers the normalisation a written file needs.
//
// A source enumerates lower-cased while an operator's file may not be written that way. Matched
// as-written, a key differing only in case resolves as unknown while the operator's value goes
// nowhere, which is the shape of failure where the file looks right and the node ignores it.
func TestAFileKeyIsMatchedRegardlessOfCase(t *testing.T) {
	registerGiga(t)

	got, err := registry.Resolve(registry.ModeValidator, registry.Sources{
		File: map[string]any{"GIGA_EXECUTOR.OCC_Enabled": "written"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Unknown) != 0 {
		t.Errorf("the file's key was reported unknown %v; an operator whose only mistake was case would "+
			"be told their key does not exist", got.Unknown)
	}
	if got.Values["giga_executor.occ_enabled"] != "written" {
		t.Errorf("the key resolved to %#v, want the file's value", got.Values["giga_executor.occ_enabled"])
	}
}

// TestAKeyNoSectionDeclaresIsReportedNotDropped covers a key nothing declares.
//
// Silently dropping one is how an operator's typo becomes invisible. Reported rather than an error,
// because what to do about it differs: a generate path may refuse, while a boot on an operator's
// existing file must not.
func TestAKeyNoSectionDeclaresIsReportedNotDropped(t *testing.T) {
	registerGiga(t)

	got, err := registry.Resolve(registry.ModeValidator, registry.Sources{
		File: map[string]any{
			"giga_executor.occ_enabled": true,
			"giga_executor.typo":        1,
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if !reflect.DeepEqual(got.Unknown, []string{"giga_executor.typo"}) {
		t.Errorf("Unknown is %v, want the one undeclared key. Dropped silently, an operator's typo is "+
			"invisible and their intended value never applies", got.Unknown)
	}
	if _, ok := got.Values["giga_executor.typo"]; ok {
		t.Error("the undeclared key resolved anyway, so it would reach a consumer that cannot use it")
	}
}

// TestTheEnvironmentIsReadByTheDeclaredSet holds the direction that makes the environment complete.
//
// An environment cannot be enumerated for a prefix: a variable is findable only if its name is
// already known. Asking for every declared key's canonical spelling is therefore the only way to read
// a complete one, and it is why the derivation lives beside the registry.
func TestTheEnvironmentIsReadByTheDeclaredSet(t *testing.T) {
	registerGiga(t)
	set := map[string]string{
		registry.EnvName("giga_executor.occ_enabled"): "false",
		"SEID_SOMETHING_UNDECLARED":                   "1",
	}

	var asked []string
	got, err := registry.Resolve(registry.ModeValidator, registry.Sources{
		LookupEnv: func(name string) (string, bool) {
			asked = append(asked, name)
			v, ok := set[name]
			return v, ok
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Asked for exactly the declared spellings, so the undeclared variable is never seen rather than
	// seen and discarded. Read the other way it would surface as an unknown key no operator wrote.
	var want []string
	for _, key := range registry.Keys() {
		want = append(want, registry.EnvName(key))
	}
	sort.Strings(asked)
	sort.Strings(want)
	if !reflect.DeepEqual(asked, want) {
		t.Errorf("the environment was asked for %v, want the declared spellings %v", asked, want)
	}
	if len(got.Unknown) != 0 {
		t.Errorf("the environment produced unknown keys %v", got.Unknown)
	}
	if !reflect.DeepEqual(got.Overrides, []string{"giga_executor.occ_enabled"}) {
		t.Errorf("Overrides is %v, want the one declared key the environment supplied", got.Overrides)
	}
	if got.Values["giga_executor.occ_enabled"] != "false" {
		t.Errorf("the declared key resolved to %#v, want false", got.Values["giga_executor.occ_enabled"])
	}
}

// TestEveryDeclaredKeyResolves holds Resolve to the property its documentation states.
//
// Two halves, because the property has two: a section whose default states every declared key
// resolves all of them, and a section whose default falls short is refused by name rather than
// resolving with a hole. Only asserting the second would pass on a Resolve that refused everything.
func TestEveryDeclaredKeyResolves(t *testing.T) {
	type inner struct {
		Cert string `mapstructure:"cert"`
	}
	type optional struct {
		Name string `mapstructure:"name"`
		TLS  *inner `mapstructure:"tls"`
	}

	t.Run("a default stating every declared key", func(t *testing.T) {
		registry.Reset()
		registry.RegisterSection("svc", &optional{}, func(registry.Mode) any {
			return optional{Name: "n", TLS: &inner{Cert: "c"}}
		})
		requireNoDefects(t)

		for _, m := range registry.Modes() {
			res, err := registry.Resolve(m, registry.Sources{})
			if err != nil {
				t.Fatalf("mode %s: %v", m, err)
			}
			declared := registry.Keys()
			if len(declared) == 0 {
				t.Fatalf("mode %s: the probe declared nothing, so this test asserts nothing", m)
			}
			for _, key := range declared {
				if _, ok := res.Values[key]; !ok {
					t.Errorf("mode %s: %q is declared and has no resolution, so a caller iterating Keys "+
						"and calling From is handed an absence the documentation rules out", m, key)
				}
			}
		}
	})

	t.Run("a default whose optional subtree is nil", func(t *testing.T) {
		registry.Reset()
		registry.RegisterSection("svc", &optional{}, func(registry.Mode) any {
			return optional{Name: "n"} // TLS left nil, which is how a default arrives short
		})
		requireNoDefects(t)

		// The type walk unwraps the pointer to derive svc.tls.cert and the value walk skips a nil one,
		// so the default states one fewer key than the section declares.
		_, err := registry.Resolve(registry.ModeFull, registry.Sources{})
		if err == nil {
			t.Fatal("a default that states no value for a declared key resolved, so a caller holds a " +
				"resolution missing a key From answers ok=false for")
		}
		if !strings.Contains(err.Error(), "svc.tls.cert") {
			t.Errorf("the refusal is %q, which does not name svc.tls.cert; a caller cannot fix a "+
				"default from a message that does not say which key is missing", err)
		}
	})
}

// requireNoDefects fails when a test's own probe registration was refused, so a test cannot pass by
// having registered nothing.
func requireNoDefects(t *testing.T) {
	t.Helper()
	for _, d := range registry.Defects() {
		t.Fatalf("registering the probe section produced a defect: %v", d.Err)
	}
}

// TestADivergentDefaultIsRefusedRatherThanPanicking pins the other half of the same seam.
//
// The two walks share tagOf and are meant to traverse the same type, and nothing held them to it: the
// prototype's type derives the keys while whatever Defaults returns supplies the values. A default
// whose type squashes a non-struct reached reflect.NumField on a string, so Resolve panicked in a
// package whose stated posture is that a bad registration never does.
func TestADivergentDefaultIsRefusedRatherThanPanicking(t *testing.T) {
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
			t.Fatalf("Resolve panicked on a divergent default: %v", r)
		}
	}()
	if _, err := registry.Resolve(registry.ModeFull, registry.Sources{}); err == nil {
		t.Error("a default whose type is not the registered struct's resolved without complaint, so " +
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
	// becomes a group, because a time.Duration is an int64 and takes the leaf path on its kind.
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

	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{})
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
		got, ok := resolved.Values[key]
		if !ok {
			t.Errorf("%s declared and did not resolve", key)
			continue
		}
		if got != want {
			t.Errorf("%s resolved to %#v, want %#v", key, got, want)
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
	type cyclic struct {
		Child *cyclic `mapstructure:"child"`
		N     string  `mapstructure:"n"`
	}
	type Shared struct {
		Laddr string `mapstructure:"laddr"`
	}
	type collide struct {
		Shared `mapstructure:",squash"`
		Laddr  string `mapstructure:"laddr"`
	}
	type hiddenSquash struct {
		A      string `mapstructure:"a"`
		shared Shared `mapstructure:",squash"` //nolint:unused // the point is that it is refused
	}
	type dottedName struct {
		A string `mapstructure:"a.b"`
	}
	type spacedName struct {
		A string `mapstructure:"a b"`
	}
	type hollow struct{}
	type carriesHollow struct {
		A string `mapstructure:"a"`
		H hollow `mapstructure:"h"`
	}
	type stamp time.Time
	type carriesStamp struct {
		A string `mapstructure:"a"`
		S stamp  `mapstructure:"s"`
	}

	for _, tc := range []struct {
		name      string
		section   string
		prototype any
		defaults  func(registry.Mode) any
		want      string
	}{
		{"a squashed field that also names a segment", "s", &squashNamed{}, anyDefault, "one or the other"},
		{"an empty mapstructure name", "s", &emptyName{}, anyDefault, "empty mapstructure name"},
		{"a dash mapstructure name", "s", &dashName{}, anyDefault, "empty mapstructure name"},
		{"an upper-case key", "s", &upperName{}, anyDefault, "not lower case"},
		{"a squashed scalar", "s", &squashScalar{}, anyDefault, "not a struct"},
		{"a struct declaring nothing", "s", &noKeys{}, anyDefault, "declares no keys"},
		{"an empty section name", "", &Good{}, anyDefault, "section name is empty"},
		{"an upper-case section name", "Sec", &Good{}, anyDefault, "not lower case"},
		{"a dotted section name", "a.b", &Good{}, anyDefault, `carries "."`},
		{"a spaced section name", "a b", &Good{}, anyDefault, `carries " "`},
		{"a struct that contains itself", "s", &cyclic{}, anyDefault, "contains itself"},
		{"two fields declaring one key", "s", &collide{}, anyDefault, "both declare \"s.laddr\""},
		{"a dotted key name", "s", &dottedName{}, anyDefault, `carries "."`},
		{"a spaced key name", "s", &spacedName{}, anyDefault, `carries " "`},
		{"a subtree with no exported field", "s", &carriesHollow{}, anyDefault, "declares no key"},
		{"a defined type over an opaque leaf", "s", &carriesStamp{}, anyDefault, "declares no key"},
		{"an unexported field carrying a tag", "s", &hiddenSquash{}, anyDefault, "is unexported and carries"},
		{"no struct at all", "s", nil, anyDefault, "no struct"},
		{"a non-struct prototype", "s", "string", anyDefault, "not a struct"},
		{"no defaults function", "s", &Good{}, nil, "no defaults function"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry.Reset()
			registry.RegisterSection(tc.section, tc.prototype, tc.defaults)

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

// anyDefault stands in where the table's subject is the struct rather than the default. The
// registration is refused before a default is ever asked for, so its shape cannot matter.
func anyDefault(registry.Mode) any { return struct{}{} }

// TestRegistrationAndResolutionAreConcurrencySafe drives registration against resolution.
//
// Registration runs at package initialisation, which is when a diagnostic or an authoring check may
// already be resolving, so the two overlap in real use. Run under -race, which is what asserts the
// shared state is actually guarded; the checks below hold the answer's shape.
//
// The part of the single-snapshot correction this cannot see is a section arriving between two reads
// of the defaults. From outside the package that is indistinguishable from one arriving after the call
// returns. The part it can see has its own test below, because reading the registry again to build the
// environment leaves a symptom: a key reported as one no section declares, that a section declares.
func TestRegistrationAndResolutionAreConcurrencySafe(t *testing.T) {
	type one struct {
		A string `mapstructure:"a"`
	}
	defaults := func(registry.Mode) any { return one{A: "v"} }

	for round := 0; round < 20; round++ {
		registry.Reset()
		registry.RegisterSection("first", &one{}, defaults)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			registry.RegisterSection("second", &one{}, defaults)
		}()

		res, err := registry.Resolve(registry.ModeFull, registry.Sources{})
		wg.Wait()
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if _, ok := res.Values["first.a"]; !ok {
			t.Fatalf("round %d: first.a was registered before the call and did not resolve", round)
		}
		if len(res.Unknown) != 0 {
			t.Fatalf("round %d: no source was passed and %v is reported unknown", round, res.Unknown)
		}
	}
}

// TestNoUnknownKeyIsOneTheRegistryDeclares holds every source against one snapshot of the registry.
//
// Resolve derives the declared set once and checks every source against it. A source built from a
// second read of the registry can carry a key the first read did not hold, and that key comes back
// reported as one no section declares. An operator whose environment variable is real would be told it
// matches nothing, and their value would be dropped.
//
// The trigger is a section registering while Resolve runs, which is deterministic here rather than
// raced: Resolve calls each section's Defaults between reading the registry and reading the
// environment, and Defaults is caller-supplied code. A goroutine cannot be relied on to land in that
// window, so a concurrent version of this test passes whether the defect is present or not.
func TestNoUnknownKeyIsOneTheRegistryDeclares(t *testing.T) {
	type one struct {
		A string `mapstructure:"a"`
	}
	late := func(registry.Mode) any { return one{A: "v"} }

	registry.Reset()
	registry.RegisterSection("first", &one{}, func(m registry.Mode) any {
		// Lands after Resolve has taken its snapshot, so a source that reads the registry again sees a
		// section the declared set does not hold.
		if _, already := registry.Lookup("second"); !already {
			registry.RegisterSection("second", &one{}, late)
		}
		return one{A: "v"}
	})
	requireNoDefects(t)

	env := map[string]string{
		registry.EnvName("first.a"):  "from the environment",
		registry.EnvName("second.a"): "from the environment",
	}
	res, err := registry.Resolve(registry.ModeFull, registry.Sources{
		LookupEnv: func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	declared := map[string]bool{}
	for _, key := range registry.Keys() {
		declared[key] = true
	}
	if !declared["second.a"] {
		t.Fatal("the late section did not register, so this test cannot see the defect it exists for")
	}
	for _, key := range res.Unknown {
		if declared[key] {
			t.Errorf("%q is reported as a key no section declares, and the registry declares it. An "+
				"operator setting it would be told it matches nothing and their value would be dropped", key)
		}
	}
}

// TestAKeySharingAnEnvironmentSpellingIsRefused holds EnvName's collision claim to a real check.
//
// A dot and a hyphen both become an underscore, so two keys differing only in that punctuation answer
// to one variable. Without a refusal the second key is simply unsettable from the environment, and
// nothing in the resolved output says so: the environment is asked for both spellings and answers once.
func TestAKeySharingAnEnvironmentSpellingIsRefused(t *testing.T) {
	type WithHyphen struct {
		Under string `mapstructure:"a-b"`
	}
	type withDot struct {
		Sub struct {
			B string `mapstructure:"b"`
		} `mapstructure:"a"`
	}
	type nested struct {
		B struct {
			Sub struct {
				B string `mapstructure:"b"`
			} `mapstructure:"a"`
		} `mapstructure:"b"`
	}

	t.Run("within one section", func(t *testing.T) {
		registry.Reset()
		registry.RegisterSection("s", &struct {
			WithHyphen `mapstructure:",squash"`
			Sub        struct {
				B string `mapstructure:"b"`
			} `mapstructure:"a"`
		}{}, anyDefault)
		requireEnvCollisionRefused(t, "SEID_S_A_B")
	})

	// Two section names that differ only in punctuation. A section name cannot carry a dot, so this
	// is the only shape the cross-section collision takes.
	t.Run("across two sections", func(t *testing.T) {
		registry.Reset()
		registry.RegisterSection("a-b", &withDot{}, anyDefault)
		if len(registry.Defects()) != 0 {
			t.Fatalf("the first section was refused: %v", registry.Defects())
		}
		registry.RegisterSection("a", &nested{}, anyDefault)
		defects := registry.Defects()
		if len(defects) != 1 {
			t.Fatalf("got %d defects, want exactly one; a-b.a.b and a.b.a.b both answer to "+
				"SEID_A_B_A_B", len(defects))
		}
		if msg := defects[0].Err.Error(); !strings.Contains(msg, "SEID_A_B_A_B") {
			t.Errorf("the refusal reads %q and does not name the shared spelling", msg)
		}
	})

	t.Run("distinct spellings register", func(t *testing.T) {
		registry.Reset()
		registry.RegisterSection("a", &withDot{}, anyDefault)
		registry.RegisterSection("b", &withDot{}, anyDefault)
		if d := registry.Defects(); len(d) != 0 {
			t.Fatalf("two sections whose keys have distinct spellings were refused: %v", d[0].Err)
		}
	})
}

func requireEnvCollisionRefused(t *testing.T, spelling string) {
	t.Helper()
	defects := registry.Defects()
	if len(defects) != 1 {
		t.Fatalf("got %d defects, want exactly one naming the shared spelling", len(defects))
	}
	if msg := defects[0].Err.Error(); !strings.Contains(msg, spelling) {
		t.Errorf("the refusal reads %q and does not name %s, so a caller cannot tell which variable "+
			"the two keys are fighting over", msg, spelling)
	}
	if len(registry.Keys()) != 0 {
		t.Errorf("the section registered anyway, so one of its keys is unsettable from the environment " +
			"with nothing reporting it")
	}
}

// TestAReadKeySliceCannotReachTheRegistry holds Lookup and Sections to handing out copies.
//
// A caller that sorted, truncated or appended to the returned slice would be writing into the
// registry's own storage from outside the mutex, so one caller's convenience would silently change
// every other caller's declared set.
func TestAReadKeySliceCannotReachTheRegistry(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("s", &struct {
		B string `mapstructure:"b"`
		A string `mapstructure:"a"`
	}{}, func(registry.Mode) any {
		return struct {
			B string `mapstructure:"b"`
			A string `mapstructure:"a"`
		}{}
	})
	requireNoDefects(t)

	want := []string{"s.a", "s.b"}
	for _, read := range []struct {
		name string
		keys func() []string
	}{
		{"Lookup", func() []string { s, _ := registry.Lookup("s"); return s.Keys }},
		{"Sections", func() []string { return registry.Sections()[0].Keys }},
	} {
		t.Run(read.name, func(t *testing.T) {
			got := read.keys()
			for i := range got {
				got[i] = "clobbered"
			}
			if after := read.keys(); !reflect.DeepEqual(after, want) {
				t.Errorf("writing into the slice %s returned left the registry declaring %v, want %v",
					read.name, after, want)
			}
		})
	}
}

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

// TestAStructThatIsAValueStaysOneKey pins the stop the walk needs at a decoded-whole type.
//
// time.Time carries exported fields, so walking into it would declare keys for its internals that no
// operator writes and no decoder fills. It has to arrive as one value under its own key.
//
// The walk asks isLeaf only after finding a struct, which is why the list names only struct types. A
// time.Duration is an int64 and never reaches it.
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
	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got, ok := resolved.Values["stamped.at"]
	if !ok {
		t.Fatal("stamped.at declared and did not resolve")
	}
	if got != stamp {
		t.Errorf("stamped.at resolved to %#v, want the whole time %#v", got, stamp)
	}
}

// TestADefaultThatIsNotAStructIsRefused covers the guards on what Defaults hands back.
//
// A default is a func returning any, so nothing at the type level stops it returning nil or a
// scalar. Both would otherwise reach the value walk, which expects a struct.
func TestADefaultThatIsNotAStructIsRefused(t *testing.T) {
	type one struct {
		A string `mapstructure:"a"`
	}
	for _, tc := range []struct {
		name     string
		defaults func(registry.Mode) any
		want     string
	}{
		{"nil", func(registry.Mode) any { return nil }, "no value"},
		{"a scalar", func(registry.Mode) any { return "string" }, "not a struct"},
		{"a nil pointer", func(registry.Mode) any { return (*one)(nil) }, "nil *"},
		{"an untagged field", func(registry.Mode) any {
			return struct{ A string }{}
		}, "has no mapstructure tag"},
		{"an unexported field carrying a tag", func(registry.Mode) any {
			return struct {
				a string `mapstructure:"a"` //nolint:unused // the point is that it is refused
			}{}
		}, "is unexported and carries"},
		{"a squashed scalar", func(registry.Mode) any {
			return struct {
				S string `mapstructure:",squash"`
			}{}
		}, "not a struct"},
		{"a bad field inside a squashed struct", func(registry.Mode) any {
			return struct {
				Base struct{ A string } `mapstructure:",squash"`
			}{}
		}, "has no mapstructure tag"},
		{"a bad field inside a named subtree", func(registry.Mode) any {
			return struct {
				Sub struct{ A string } `mapstructure:"sub"`
			}{}
		}, "has no mapstructure tag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry.Reset()
			registry.RegisterSection("probe", &one{}, tc.defaults)
			for _, d := range registry.Defects() {
				t.Fatalf("the prototype is valid and should register: %v", d.Err)
			}
			_, err := registry.Resolve(registry.ModeFull, registry.Sources{})
			if err == nil {
				t.Fatalf("a default returning %s resolved without complaint", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal reads %q, which does not mention %q", err, tc.want)
			}
		})
	}
}

// TestADefaultCarryingAnUndeclaredKeyIsRefused covers the other direction of matchesDeclaration.
//
// A default whose type is not the prototype's can state keys the section never declared as well as
// omit ones it did. Reported as its own case, because the cause is the same and a reader chasing one
// message should not have to guess that the other exists.
func TestADefaultCarryingAnUndeclaredKeyIsRefused(t *testing.T) {
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
	_, err := registry.Resolve(registry.ModeFull, registry.Sources{})
	if err == nil {
		t.Fatal("a default stating an undeclared key resolved without complaint")
	}
	if !strings.Contains(err.Error(), "does not declare") {
		t.Errorf("the refusal reads %q, which does not say the default is the wrong type", err)
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

// TestSectionsAreSortedAndADefaultMayBeAPointer covers the shapes a caller is free to choose.
//
// Sections promises an order, and a caller reading two sections in registration order would see one
// that depends on map iteration. A default may hand back a pointer, since a section holding a large
// struct has no reason to copy it, and the value walk has to follow that.
func TestSectionsAreSortedAndADefaultMayBeAPointer(t *testing.T) {
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

	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{})
	if err != nil {
		t.Fatalf("a pointer default was refused: %v", err)
	}
	if got, ok := resolved.Values["alpha.shown"]; !ok || got != "a" {
		t.Errorf("alpha.shown resolved to (%#v, %v) through a pointer default, want \"a\"", got, ok)
	}
	if keys := registry.Keys(); !reflect.DeepEqual(keys, []string{"alpha.shown", "zulu.b"}) {
		t.Errorf("declared keys are %v; the unexported field must not become one", keys)
	}
}

// TestADefaultOfAWhollyDifferentTypeSaysSoOnce covers the message for a default that both omits a
// declared key and states one that was never declared.
//
// Two separate errors would send a reader chasing the second after fixing the first, when the cause is
// one thing: the default is not the registered struct's type.
func TestADefaultOfAWhollyDifferentTypeSaysSoOnce(t *testing.T) {
	type declared struct {
		A string `mapstructure:"a"`
	}
	type unrelated struct {
		Z string `mapstructure:"z"`
	}
	registry.Reset()
	registry.RegisterSection("probe", &declared{}, func(registry.Mode) any { return unrelated{Z: "z"} })

	_, err := registry.Resolve(registry.ModeFull, registry.Sources{})
	if err == nil {
		t.Fatal("a default of an unrelated type resolved without complaint")
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
