package appopts_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/appopts"
	"github.com/sei-protocol/sei-chain/config/registry"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
)

// probe is a section whose baseline varies by mode.
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

// bootLike returns a source shaped like the one a node boots with.
//
// A file's worth of values, plus the environment consultation the real one performs. That second part
// is what matters: it answers for a key on Get without listing it in AllKeys, which is the behaviour
// every assertion about delegation here depends on.
func bootLike(t *testing.T, prefix string, fileValues map[string]any) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.SetEnvPrefix(prefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	for key, value := range fileValues {
		// SetDefault rather than Set, so these sit below the override layer exactly as a file's values
		// do and an installed value has something real to beat.
		v.SetDefault(key, value)
	}
	return v
}

// TestAnEnvironmentValueUnderAnUndeclaredKeySurvives is the property this package exists for.
//
// A boot source answers for a key on Get whether or not it lists it, so a value an operator delivered
// through the environment is readable and unlistable at the same time. Building a fresh source from an
// enumeration drops exactly those values and replaces an operator's setting with a code default,
// silently. Leaving the key alone cannot, because the code that answers it is unchanged.
func TestAnEnvironmentValueUnderAnUndeclaredKeySurvives(t *testing.T) {
	registerProbe(t)
	t.Setenv("TESTBOOT_ONLY_IN_THE_ENVIRONMENT", "4242")
	target := bootLike(t, "TESTBOOT", map[string]any{"in.the.file": 1})

	// The premise: readable, and absent from the enumeration.
	if got := target.Get("only.in.the.environment"); got != "4242" {
		t.Fatalf("the fixture does not deliver the value through the environment (got %#v), so this "+
			"test measures nothing", got)
	}
	for _, key := range target.AllKeys() {
		if strings.EqualFold(key, "only.in.the.environment") {
			t.Fatal("the fixture enumerates the environment-delivered key, so the case this test " +
				"exists for is not being exercised")
		}
	}

	if _, err := appopts.Install(target, resolve(t, registry.ModeValidator)); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if got := target.Get("only.in.the.environment"); got != "4242" {
		t.Errorf("after installing, the environment-delivered key reads %#v and the operator set 4242. "+
			"A key nothing declares has to keep being answered by whatever answered it before, because "+
			"nothing can enumerate it to carry it across", got)
	}
	if got := target.Get("in.the.file"); got != 1 {
		t.Errorf("a key from the file reads %#v after installing, want 1", got)
	}
}

// TestADeclaredKeyReadsTheRegistryOverEverythingElse is what makes declaring a section a real change.
//
// Override precedence is the whole mechanism: viper checks it before the file, the environment and any
// bound flag. If either of those still won, a section could be declared and the node would carry on
// reading what it always read.
func TestADeclaredKeyReadsTheRegistryOverEverythingElse(t *testing.T) {
	registerProbe(t)
	// Both channels say something other than the baseline of 4.
	t.Setenv("TESTBOOT_PROBE_WORKERS", "999")
	target := bootLike(t, "TESTBOOT", map[string]any{"probe.workers": 64})

	if got := target.Get("probe.workers"); got == 4 {
		t.Fatal("the fixture already reads the baseline, so this test would pass without installing")
	}

	report, err := appopts.Install(target, resolve(t, registry.ModeValidator))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if got := target.Get("probe.workers"); got != 4 {
		t.Errorf("probe.workers reads %#v with a file value of 64 and an environment value of 999, want "+
			"the resolved 4. A declared key still answered by either of those makes declaring the "+
			"section a change with no effect", got)
	}
	if len(report.Installed) != 2 {
		t.Errorf("the report says %v was installed, want both of the section's keys", report.Installed)
	}
}

// TestNothingUndeclaredIsDisturbed holds the other half of the partition.
//
// The keys a node reads today keep reading the same values, or migrating one section breaks readers
// that had nothing to do with it.
func TestNothingUndeclaredIsDisturbed(t *testing.T) {
	registerProbe(t)
	file := map[string]any{
		"probe.workers":     64, // declared, so this one is expected to change
		"storage.keep":      100,
		"rpc.addr":          "tcp://0.0.0.0:26657",
		"a.list":            []string{"x", "y"},
		"nested.thing.here": true,
	}
	target := bootLike(t, "TESTBOOT", file)

	before := map[string]any{}
	for key := range file {
		before[key] = target.Get(key)
	}

	if _, err := appopts.Install(target, resolve(t, registry.ModeValidator)); err != nil {
		t.Fatalf("Install: %v", err)
	}

	for key, was := range before {
		if key == "probe.workers" {
			continue
		}
		if got := target.Get(key); !sameRead(got, was) {
			t.Errorf("%s read %#v before installing and %#v after. Migrating one section must not "+
				"change what any other key answers", key, was, got)
		}
	}
}

// TestTheKeySpaceOnlyGrows keeps enumeration from losing anything.
//
// The sweep that reports unrecognized experimental keys walks AllKeys, so a key space that shrank here
// would quietly narrow what that sweep can see.
func TestTheKeySpaceOnlyGrows(t *testing.T) {
	registerProbe(t)
	target := bootLike(t, "TESTBOOT", map[string]any{"storage.keep": 1, "rpc.addr": "x"})

	before := map[string]bool{}
	for _, key := range target.AllKeys() {
		before[strings.ToLower(key)] = true
	}

	if _, err := appopts.Install(target, resolve(t, registry.ModeValidator)); err != nil {
		t.Fatalf("Install: %v", err)
	}

	after := map[string]bool{}
	for _, key := range target.AllKeys() {
		after[strings.ToLower(key)] = true
	}
	for key := range before {
		if !after[key] {
			t.Errorf("%q was enumerable before installing and is not after. Anything walking AllKeys, "+
				"including the experimental sweep, would stop seeing it", key)
		}
	}
	for _, declared := range []string{"probe.workers", "probe.enabled"} {
		if !after[declared] {
			t.Errorf("%q is declared and installed but not enumerable, so a report over AllKeys would "+
				"not know the node reads it", declared)
		}
	}
}

// TestTheReportNamesWhatIsLeftToMigrate is what makes the remaining work a number rather than a guess.
func TestTheReportNamesWhatIsLeftToMigrate(t *testing.T) {
	registerProbe(t)
	target := bootLike(t, "TESTBOOT", map[string]any{"probe.workers": 64, "storage.keep": 1, "rpc.addr": "x"})

	report, err := appopts.Install(target, resolve(t, registry.ModeValidator))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if strings.Join(report.Passthrough, ",") != "rpc.addr,storage.keep" {
		t.Errorf("the backlog is %v, want the two keys no section declares", report.Passthrough)
	}
	// probe.enabled is declared and the file never had it, so a node reads it from the registry for the
	// first time. Reported apart from the backlog, since it is not work that remains.
	if len(report.Added) != 1 || report.Added[0] != "probe.enabled" {
		t.Errorf("the report says %v was added, want probe.enabled", report.Added)
	}
	if !strings.Contains(report.Summary(), "still read as they always have") {
		t.Errorf("the summary does not say what is left: %q", report.Summary())
	}
}

// TestTheReportIsTakenBeforeAnythingIsInstalled is why the order inside Install matters.
//
// Installing a declared key the source never enumerated makes it enumerable. A report built afterwards
// could not tell such a key from one the source always had, so the backlog and the added set would both
// be wrong in the same direction.
func TestTheReportIsTakenBeforeAnythingIsInstalled(t *testing.T) {
	registerProbe(t)
	target := bootLike(t, "TESTBOOT", nil) // nothing enumerated at all

	report, err := appopts.Install(target, resolve(t, registry.ModeValidator))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if len(report.Added) != 2 {
		t.Errorf("the report says %v was added, want both declared keys: the source enumerated nothing, "+
			"so a node reads both from the registry for the first time", report.Added)
	}
	if len(report.Passthrough) != 0 {
		t.Errorf("the report claims %v reads as it always has, and the source enumerated nothing. A "+
			"report taken after installing counts the keys it just installed", report.Passthrough)
	}
}

// TestACollidingDeclaredPairIsRefused holds the one shape the override layer cannot carry.
//
// Two keys where one extends the other cannot both live there: whichever is written second turns the
// other into a map or leaves it unreadable, and which survives depends only on iteration order. A
// single section cannot produce such a pair, since keys derive from struct leaves, so this is for a pair
// arriving from two sections at once.
func TestACollidingDeclaredPairIsRefused(t *testing.T) {
	registry.Reset()
	colliding := registry.Resolved{Keys: map[string]registry.Resolution{
		"a.b":   {Key: "a.b", Value: 1, From: "default"},
		"a.b.c": {Key: "a.b.c", Value: 2, From: "default"},
	}}

	_, err := appopts.Install(bootLike(t, "TESTBOOT", nil), colliding)
	if err == nil {
		t.Fatal("a declared pair where one key extends the other was installed. One of the two values " +
			"is gone and which one depends on iteration order")
	}
	for _, want := range []string{"a.b", "a.b.c"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	// Sharing a leading string is not a collision, or ordinary key spaces would fail.
	fine := registry.Resolved{Keys: map[string]registry.Resolution{
		"a.b":   {Key: "a.b", Value: 1},
		"a.bc":  {Key: "a.bc", Value: 2},
		"a.b_c": {Key: "a.b_c", Value: 3},
	}}
	if _, err := appopts.Install(bootLike(t, "TESTBOOT", nil), fine); err != nil {
		t.Errorf("keys sharing a leading string but no dotted prefix were refused: %v", err)
	}
}

// TestARefusedInstallChangesNothing keeps a rejected key space from leaving half its values behind.
func TestARefusedInstallChangesNothing(t *testing.T) {
	registry.Reset()
	target := bootLike(t, "TESTBOOT", map[string]any{"untouched": "before"})
	colliding := registry.Resolved{Keys: map[string]registry.Resolution{
		"a.b":   {Key: "a.b", Value: 1},
		"a.b.c": {Key: "a.b.c", Value: 2},
	}}

	if _, err := appopts.Install(target, colliding); err == nil {
		t.Fatal("the colliding pair was accepted")
	}

	if got := target.Get("untouched"); got != "before" {
		t.Errorf("a refused install changed the source: untouched reads %#v", got)
	}
	if got := target.Get("a.b"); got != nil {
		t.Errorf("a refused install wrote a.b = %#v, so the source now holds part of a key space that "+
			"was rejected", got)
	}
}

// TestInstallRefusesWithNoSourceToInstallInto keeps a missing source from reading as success.
func TestInstallRefusesWithNoSourceToInstallInto(t *testing.T) {
	registerProbe(t)

	if _, err := appopts.Install(nil, resolve(t, registry.ModeValidator)); err == nil {
		t.Error("Install accepted no source, which would report every key resolved while nothing was " +
			"written anywhere")
	}
}

// TestTheResultIsStillWhatAppNewTakes is what keeps every read site unchanged.
//
// The boot hands its own source to the application, so installing into that source is what lets a
// resolved value reach a reader without any appOpts.Get call site moving.
func TestTheResultIsStillWhatAppNewTakes(t *testing.T) {
	registerProbe(t)
	target := bootLike(t, "TESTBOOT", map[string]any{"legacy.key": 1})

	if _, err := appopts.Install(target, resolve(t, registry.ModeValidator)); err != nil {
		t.Fatalf("Install: %v", err)
	}

	var opts servertypes.AppOptions = target
	if opts.Get("probe.workers") != 4 {
		t.Errorf("read through the interface a node uses, probe.workers is %#v", opts.Get("probe.workers"))
	}
	if opts.Get("legacy.key") != 1 {
		t.Errorf("read through the interface a node uses, legacy.key is %#v", opts.Get("legacy.key"))
	}
}

// sameRead reports whether a value read from the source is unchanged.
//
// reflect.DeepEqual rather than a comparison written here, for the same reason: == panics on a slice
// instead of returning false, so enumerating slice types by hand is wrong as soon as one is missed.
func sameRead(a, b any) bool { return reflect.DeepEqual(a, b) }
