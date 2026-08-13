package appopts_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/config/appopts"
	"github.com/sei-protocol/sei-chain/config/registry"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
)

// existing is the configuration a node reads today, controlled key by key.
type existing map[string]any

func (e existing) Get(key string) any { return e[key] }

func (e existing) AllKeys() []string {
	out := make([]string, 0, len(e))
	for k := range e {
		out = append(out, k)
	}
	return out
}

// probe is a section with one key whose baseline varies by mode.
type probe struct {
	Workers int  `mapstructure:"workers"`
	Enabled bool `mapstructure:"enabled"`
}

// registerProbe registers that section.
func registerProbe(t *testing.T) {
	t.Helper()
	registry.Reset()
	registry.RegisterSection("probe", &probe{}, func(m registry.Mode) any {
		return probe{Workers: 4, Enabled: m != registry.ModeArchive}
	})
	for _, d := range registry.Defects() {
		t.Fatalf("registering probe produced a defect: %v", d.Err)
	}
}

// resolve returns the baselines for a mode.
func resolve(t *testing.T, mode registry.Mode) registry.Resolved {
	t.Helper()
	got, err := registry.Resolve(mode)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return got
}

// TestEveryKeyTheNodeReadsTodaySurvives is the property an in-memory struct cannot offer.
//
// A struct silently drops any key it does not model, so a round-trip test over one passes while the
// node has lost a setting. Holding the whole key space is what lets a key that no section declares
// keep working while the migration proceeds around it.
func TestEveryKeyTheNodeReadsTodaySurvives(t *testing.T) {
	registerProbe(t)
	current := existing{
		"probe.workers":          64,  // a section declares this one
		"nothing-declares-this":  "x", // and nothing declares these
		"another.undeclared.key": 7,
		"a-third":                true,
	}

	built, report, err := appopts.Build(current, resolve(t, registry.ModeValidator))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, key := range current.AllKeys() {
		if built.Get(key) == nil {
			t.Errorf("%q is gone from what the node reads. A key nothing has migrated yet has to keep "+
				"working, or moving one section breaks every reader that was not part of the move", key)
		}
	}
	// And the declared keys are present too, so nothing was lost in either direction.
	for key := range resolve(t, registry.ModeValidator).Keys {
		if built.Get(key) == nil {
			t.Errorf("declared key %q is absent from what the node reads", key)
		}
	}
	// Every key on either side is accounted for exactly once, so the report describes the whole key
	// space rather than a sample of it.
	union := map[string]bool{}
	for _, key := range current.AllKeys() {
		union[key] = true
	}
	for key := range resolve(t, registry.ModeValidator).Keys {
		union[key] = true
	}
	if report.Total() != len(union) {
		t.Errorf("the report accounts for %d keys and the two sides hold %d between them: %+v. A "+
			"report that does not cover the key space cannot say what is left to migrate",
			report.Total(), len(union), report)
	}
	if got := len(built.AllKeys()); got != len(union) {
		t.Errorf("the built source carries %d keys and the two sides hold %d between them. A key "+
			"neither dropped nor reported is one nobody knows is gone", got, len(union))
	}
}

// TestADeclaredKeyReadsFromTheRegistryNotTheOldConfiguration is what makes a migration real.
//
// Moving a key into a section has to change where its value comes from. If the old configuration still
// won, a section could be declared and the node would carry on reading what it always read, and the
// migration would be a no-op nothing detected.
func TestADeclaredKeyReadsFromTheRegistryNotTheOldConfiguration(t *testing.T) {
	registerProbe(t)
	// The old configuration says 64; the validator baseline says 4.
	built, report, err := appopts.Build(existing{"probe.workers": 64}, resolve(t, registry.ModeValidator))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if got := built.Get("probe.workers"); got != 4 {
		t.Errorf("probe.workers reads %#v, want the 4 the registry resolved. A declared key still "+
			"reading the old configuration makes declaring the section a no-op", got)
	}
	if len(report.Migrated) != 1 || report.Migrated[0] != "probe.workers" {
		t.Errorf("the report says %v moved across, want probe.workers", report.Migrated)
	}
	if len(report.Passthrough) != 0 {
		t.Errorf("the report says %v is still read from the old configuration, want none", report.Passthrough)
	}
	// probe.enabled is declared and the old configuration never had it.
	if len(report.Added) != 1 || report.Added[0] != "probe.enabled" {
		t.Errorf("the report says %v was added, want probe.enabled", report.Added)
	}
}

// TestTheReportNamesWhatIsLeftToMigrate is what makes the remaining work visible.
//
// The backlog is the set of keys still read from the old configuration. Reported per build, it shrinks
// as sections are declared, and nobody has to reason about how far along the migration is.
func TestTheReportNamesWhatIsLeftToMigrate(t *testing.T) {
	registerProbe(t)
	current := existing{"probe.workers": 64, "storage.keep": 1, "rpc.addr": "x"}

	_, report, err := appopts.Build(current, resolve(t, registry.ModeValidator))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if strings.Join(report.Passthrough, ",") != "rpc.addr,storage.keep" {
		t.Errorf("the backlog is %v, want the two keys no section declares, sorted", report.Passthrough)
	}
	if !strings.Contains(report.Summary(), "still read from the legacy configuration") {
		t.Errorf("the summary does not say what is left: %q", report.Summary())
	}
}

// TestNoResolutionRunsWhenTheNodeReads is why every value goes in at override precedence.
//
// Anything lower that viper would otherwise consult must not be able to change an answer. Held by
// setting an environment variable that matches a key by every convention viper would use, and
// requiring the installed value to win.
func TestNoResolutionRunsWhenTheNodeReads(t *testing.T) {
	registerProbe(t)
	// The spellings viper reaches for, so this cannot pass by the variable simply being missed.
	for _, name := range []string{"PROBE_WORKERS", "SEID_PROBE_WORKERS", "probe.workers", "PROBE.WORKERS"} {
		t.Setenv(name, "999")
	}

	built, _, err := appopts.Build(existing{"legacy.key": "kept"}, resolve(t, registry.ModeValidator))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if got := built.Get("probe.workers"); got != 4 {
		t.Errorf("probe.workers reads %#v with an environment variable set, want the resolved 4. A "+
			"value that can still be changed by the environment at read time was not resolved, it was "+
			"deferred", got)
	}
	if got := built.Get("legacy.key"); got != "kept" {
		t.Errorf("the passthrough key reads %#v, want kept", got)
	}
	// Nothing consults a config file either, or a stray sei.toml in the working directory would win.
	if built.ConfigFileUsed() != "" {
		t.Errorf("the built source is bound to the config file %q", built.ConfigFileUsed())
	}
}

// TestAPrefixCollisionIsRefusedRatherThanResolvedByOrder holds the one shape that loses data silently.
//
// Installing a key and another key that extends it destroys one of them: a leaf written before its
// parent becomes a map, and a parent written before its leaf leaves the leaf unreadable. Which one
// survives depends only on the order they went in, so neither answer is correct and the reader cannot
// tell anything was lost.
func TestAPrefixCollisionIsRefusedRatherThanResolvedByOrder(t *testing.T) {
	registry.Reset()

	_, _, err := appopts.Build(existing{"a.b": 1, "a.b.c": 2}, registry.Resolved{})
	if err == nil {
		t.Fatal("a key space where one key is a prefix of another was installed. One of the two values " +
			"is gone and which one depends on the order they were written")
	}
	for _, want := range []string{"a.b", "a.b.c"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	// A key that merely shares a leading string is not a collision, or ordinary key spaces would fail.
	if _, _, err := appopts.Build(existing{"a.b": 1, "a.bc": 2, "a.b_c": 3}, registry.Resolved{}); err != nil {
		t.Errorf("keys sharing a leading string but no dotted prefix were refused: %v", err)
	}
}

// TestTheKeySpaceANodeReadsTodayHasNoPrefixCollisions pins the fact the refusal above relies on.
//
// The refusal is only tolerable because no such pair exists in what a node reads. If one appears, this
// is the test that says so, rather than a node discovering it by reading a key that is no longer there.
func TestTheKeySpaceANodeReadsTodayHasNoPrefixCollisions(t *testing.T) {
	observed, err := os.ReadFile("../../app/testdata/app_new.observed.golden")
	if err != nil {
		t.Skipf("the observed key record is not readable from here: %v", err)
	}

	var keys []string
	for _, line := range strings.Split(string(observed), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "(") {
			keys = append(keys, line)
		}
	}
	if len(keys) == 0 {
		t.Fatal("no keys were read from the record, so this comparison holds for any key space")
	}

	for _, a := range keys {
		for _, b := range keys {
			if a != b && strings.HasPrefix(b, a+".") {
				t.Errorf("%q and %q cannot both be installed, and a node reads both. One of the two "+
					"values would be lost depending on write order", a, b)
			}
		}
	}
	t.Logf("%d keys a construction reads, no prefix collisions among them", len(keys))
}

// TestEveryValueTypeSurvivesInstallation keeps the transport from reshaping what it carries.
//
// The node reads these values back through casts that expect what the old configuration produced, so
// a transport that changed an int to a string, or a duration to a number, would break a reader that
// had nothing to do with the migration.
func TestEveryValueTypeSurvivesInstallation(t *testing.T) {
	registry.Reset()
	current := existing{
		"an.int":       42,
		"a.string":     "text",
		"a.bool":       true,
		"a.float":      1.5,
		"a.duration":   30 * time.Second,
		"a.stringlist": []string{"x", "y"},
		"a.uint64":     uint64(1) << 40,
	}

	built, _, err := appopts.Build(current, registry.Resolved{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for key, want := range current {
		got := built.Get(key)
		if list, ok := want.([]string); ok {
			gotList, ok := got.([]string)
			if !ok || len(gotList) != len(list) {
				t.Errorf("%s came back as %#v, want %#v", key, got, want)
			}
			continue
		}
		if got != want {
			t.Errorf("%s came back as %#v (%T), want %#v (%T). A reader casting this value expects what "+
				"the old configuration produced", key, got, got, want, want)
		}
	}
}

// TestBuildRefusesWithNothingToBuildFrom keeps an empty result from reading as a successful one.
func TestBuildRefusesWithNothingToBuildFrom(t *testing.T) {
	registry.Reset()

	if _, _, err := appopts.Build(nil, registry.Resolved{}); err == nil {
		t.Error("Build accepted no existing configuration, which would hand a node an empty source " +
			"while reporting success")
	}
}

// TestTheBuiltSourceIsWhatAppNewTakes is what keeps every read site unchanged.
//
// The design's constraint is that no appOpts.Get call site changes as keys move across. That holds
// only while the built source is the same interface those sites already read.
func TestTheBuiltSourceIsWhatAppNewTakes(t *testing.T) {
	registerProbe(t)

	built, _, err := appopts.Build(existing{"legacy.key": 1}, resolve(t, registry.ModeValidator))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var opts servertypes.AppOptions = built
	if opts.Get("probe.workers") != 4 {
		t.Errorf("read through the interface a node uses, probe.workers is %#v", opts.Get("probe.workers"))
	}
	if opts.Get("legacy.key") != 1 {
		t.Errorf("read through the interface a node uses, legacy.key is %#v", opts.Get("legacy.key"))
	}
}

// answersMoreThanItLists is a source that answers for a key it does not enumerate.
//
// Exactly what a boot viper does. It resolves an environment variable on Get whether or not the key
// appears in a file, and AllKeys lists only what a file or a flag put there, so a value an operator
// delivered through the environment is readable and invisible at the same time.
type answersMoreThanItLists struct {
	listed map[string]any
	hidden map[string]any
}

func (s answersMoreThanItLists) AllKeys() []string {
	out := make([]string, 0, len(s.listed))
	for k := range s.listed {
		out = append(out, k)
	}
	return out
}

func (s answersMoreThanItLists) Get(key string) any {
	if v, ok := s.listed[key]; ok {
		return v
	}
	return s.hidden[key]
}

// TestAKeyTheSourceAnswersButDoesNotListIsNotCarried is the limit that stops this replacing the boot
// source, and it is here so nobody discovers it on a node.
//
// Build walks AllKeys, so it can only carry what the source enumerates. A boot viper resolves an
// environment variable on Get without listing the key, which means an operator who delivered a value
// that way has it read today and would have it silently replaced by a code default if this source were
// installed in place of that viper. Measured on a real boot: of the 117 keys a construction reads, 12
// are not enumerable, and setting one of those through the environment reads 4242 from the boot viper
// and nil from what this builds.
//
// Closing it needs the key space to come from somewhere other than AllKeys, since an environment
// cannot be enumerated for a prefix. The recorded set of keys a construction reads is that somewhere,
// which makes the record load-bearing at run time rather than only in tests, and that is a decision
// rather than a detail.
func TestAKeyTheSourceAnswersButDoesNotListIsNotCarried(t *testing.T) {
	registry.Reset()
	source := answersMoreThanItLists{
		listed: map[string]any{"in.the.file": 1},
		hidden: map[string]any{"only.in.the.environment": "4242"},
	}

	built, report, err := appopts.Build(source, registry.Resolved{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if got := built.Get("in.the.file"); got != 1 {
		t.Errorf("an enumerated key reads %#v, want 1", got)
	}
	if source.Get("only.in.the.environment") != "4242" {
		t.Fatal("the fixture does not answer for its hidden key, so this test measures nothing")
	}
	if got := built.Get("only.in.the.environment"); got != nil {
		t.Errorf("the unlisted key reads %#v from what Build produced. If that has become possible, "+
			"the key space no longer comes from AllKeys alone and this test should say what it does "+
			"come from", got)
	}
	if report.Total() != 1 {
		t.Errorf("the report accounts for %d keys, want the 1 the source lists. A key the source never "+
			"listed cannot appear in the report either, which is why the report cannot be the thing "+
			"that notices this", report.Total())
	}
}
