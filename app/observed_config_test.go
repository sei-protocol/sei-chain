package app

import (
	"os"
	"sort"
	"strings"
	"testing"

	bam "github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configcli"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// Which keys the application construction reads is measured here rather than written down.
//
// A key reaches a reader as a dotted string built at its call site, so reading the tree cannot find
// the full set: a reader can build a key from a constant in another package, or from a prefix and a
// suffix joined at run time. The only way to know what a node asks for is to construct one and watch.
//
// This is what makes any claim about covering the configuration surface checkable. Without it, a list
// of keys some new writer must produce is a guess, and the keys missing from that guess are exactly
// the ones nobody thought of.
//
// What this covers, stated so the record is not read as more than it is: New, which is the
// application construction. Starting a node reads more, from the server command and the services it
// brings up, and none of that runs here. So the recorded set is a floor on what a node reads, not the
// whole of it.

// recordConstructionReads constructs an application through a recording source and returns what it read.
//
// New is called directly rather than through the setup helpers, because those take a concrete options
// struct while New takes the interface, and only the interface can be wrapped. The arguments mirror
// what those helpers pass, so what is measured is the construction a test app actually performs.
func recordConstructionReads(t *testing.T) *configtest.RecordingAppOpts {
	t.Helper()

	homeDir, err := os.MkdirTemp("", "sei-observed")
	if err != nil {
		t.Fatalf("temp home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(homeDir) })

	recorder := configtest.Record(TestAppOpts{})
	app := New(
		dbm.NewMemDB(),
		nil,
		true,
		map[int64]bool{},
		homeDir,
		1,
		false,
		config.TestConfig(),
		MakeEncodingConfig(),
		wasm.EnableAllProposals,
		recorder,
		EmptyWasmOpts,
		nil,
		func(_ *bam.BaseApp) {},
	)
	if app == nil {
		t.Fatal("the application was not constructed, so nothing was read and this test measures nothing")
	}
	t.Cleanup(func() {
		// Closing releases the stores the construction opened, so the temp home can be removed.
		_ = app.Close()
	})
	return recorder
}

// TestTheKeysTheConstructionReadsAreObservedNotListed is what makes a coverage claim checkable.
//
// The recorded set is the deliverable. A key added to or removed from it is a reader added or
// removed, which changes what an operator's file has to carry, so it belongs in a diff rather than
// in somebody's memory.
func TestTheKeysTheConstructionReadsAreObservedNotListed(t *testing.T) {
	recorder := recordConstructionReads(t)

	observed := recorder.Keys()
	if len(observed) == 0 {
		t.Fatal("the construction read no configuration at all. Either the recorder is not wired to the " +
			"source the application was given, or the construction did not happen, and either way " +
			"every assertion below would hold for a recorder that recorded nothing")
	}
	configtest.CheckObservedKeys(t, "app_new", observed)
}

// TestTheRecorderChangesNothingTheCallerObserves is what makes the record trustworthy.
//
// A recorder that altered an answer would produce a set of keys for a boot nobody runs. Held by
// asking both the recorder and the source it wraps for the same keys, including keys neither has.
func TestTheRecorderChangesNothingTheCallerObserves(t *testing.T) {
	inner := TestAppOpts{}
	recorder := configtest.Record(inner)

	for _, key := range []string{
		"chain-id",
		FlagSCEnable,
		FlagSCSnapshotInterval,
		"a-key-nothing-declares",
		"",
	} {
		want, got := inner.Get(key), recorder.Get(key)
		if want != got {
			t.Errorf("the recorder answered %#v for %q where the source it wraps answers %#v. A "+
				"recorder that changes an answer records the keys of a construction nobody performs", got, key, want)
		}
	}
	// And every one of those was recorded, including the ones with no value.
	if n := len(recorder.Keys()); n != 5 {
		t.Errorf("recorded %d of the 5 keys asked for: %v. A key the source has no value for is still "+
			"a key that was read", n, recorder.Keys())
	}
}

// TestTheConstructionReadsSomeKeysMoreThanOnce is a property worth knowing rather than a rule to enforce.
//
// Each read is a separate call site, and each is a separate chance for two of them to disagree about
// the spelling or the cast. Recording the count is what makes a key with several readers findable,
// and this asserts the count is real rather than always one.
func TestTheConstructionReadsSomeKeysMoreThanOnce(t *testing.T) {
	recorder := recordConstructionReads(t)

	var repeated []string
	for _, key := range recorder.Keys() {
		if recorder.Count(key) > 1 {
			repeated = append(repeated, key)
		}
	}

	if recorder.Total() <= len(recorder.Keys()) {
		t.Errorf("the construction made %d reads across %d keys, so no key was read twice. Either the "+
			"counts "+
			"are not being kept or this construction reads less than a real one",
			recorder.Total(), len(recorder.Keys()))
	}
	if len(repeated) == 0 {
		t.Error("no key was read more than once, so Count cannot distinguish a key with one reader " +
			"from a key with several")
	}
	t.Logf("%d of %d keys have more than one reader", len(repeated), len(recorder.Keys()))
}

// TestGenerateCoversExactlyWhatTheConstructionReadsForAMigratedSection closes the loop the record
// exists for.
//
// Recording what a construction reads is only useful if something compares it against what the new
// writer produces. This does that for the one section already declared, in both directions: a key the
// construction reads and generate omits is a setting an operator cannot express, and a key generate
// writes that nothing reads is a line in their file that does nothing.
//
// Measured against the observed reads rather than against a list, which is the whole point. The
// section's keys are derived from its struct, and until they were observed there was no evidence the
// derivation agreed with what the node actually asks for.
//
// This is the template each further section follows as it is declared. The keys the construction reads
// outside any declared section are the remaining work, and that count is reported below rather than
// asserted, because it shrinks one section at a time.
//
// Measured against the section this binary really registers, so a stand-in registration cannot agree
// with generate while the real one disagrees.
func TestGenerateCoversExactlyWhatTheConstructionReadsForAMigratedSection(t *testing.T) {
	const section = "giga_executor"
	recorder := recordConstructionReads(t)

	// The registration this binary carries, not one made here. giga/executor/config registers its own
	// section on import, so this measures what a node actually declares rather than a stand-in that
	// could differ from it in exactly the way the comparison is meant to catch.
	if _, registered := registry.Lookup(section); !registered {
		t.Fatalf("%s is not registered in this binary. Importing giga/executor/config is what "+
			"registers it, and without it every comparison below is vacuous", section)
	}
	for _, d := range registry.Defects() {
		if d.Section == section {
			t.Fatalf("registering %s produced a defect: %v", section, d.Err)
		}
	}

	observed := recorder.Under(section)
	if len(observed) == 0 {
		t.Fatalf("the construction read no key under %s, so this comparison would hold for a generate "+
			"that wrote nothing", section)
	}

	file, err := configcli.Generate(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	written, err := file.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}

	for _, key := range observed {
		if _, present := written[key]; !present {
			t.Errorf("the construction reads %q and generate does not write it. An operator has no "+
				"way to express that setting in the file the tool produces", key)
		}
	}
	for key := range written {
		if !strings.HasPrefix(key, section+".") {
			continue
		}
		if !holdsString(observed, key) {
			t.Errorf("generate writes %q and the construction never reads it. That is a line in an "+
				"operator's file that does nothing, and a value they may believe is taking effect", key)
		}
	}

	// The rest of what the construction reads is the work still to do, reported so it is visible and so
	// a section moving across shows up as this number falling.
	//
	// Counted against every declared key rather than against this one section. Scoped to one section it
	// measured how much sits outside that section, which stops being the backlog the moment a second
	// section lands: with two migrated it still read 115, which is true of giga alone and says nothing
	// about what remains.
	declared := map[string]bool{}
	for _, key := range registry.Keys() {
		declared[key] = true
	}
	remaining := 0
	for _, key := range recorder.Keys() {
		if !declared[key] {
			remaining++
		}
	}
	t.Logf("%d of %d keys the construction reads are not yet under any declared section",
		remaining, len(recorder.Keys()))
}

// holdsString reports whether keys contains want.
func holdsString(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// TestEveryDeclaredKeyIsOneTheNodeActuallyReads is the guard on migrating a section.
//
// Declaring a key that nothing reads is worse than useless. doctor refuses a written key no section
// declares, so a section declared with spellings that differ from the ones its readers resolve makes
// every real key unrecognized: the operator's file suddenly fails the check on every node, while the
// keys the registry does declare are read by nobody.
//
// That is not hypothetical. A section's mapstructure tags and the keys its readers resolve can differ,
// and in this tree they usually do. state-commit tags a field `async-commit-buffer` while operators
// write `sc-async-commit-buffer`, so deriving keys from those tags produces twenty keys nothing reads
// and leaves twenty real ones undeclared. giga_executor migrated cleanly because its tags happen to
// match its flag constants, which makes it the easy case rather than the representative one.
//
// Held against the observed record, because that is the only list of keys a node demonstrably reads. A
// key read outside the application construction is not in it, and the honest answer there is to widen
// the recording rather than to weaken this.
//
// Scoped to the sections this package's test binary registers. A section registers when its owning package
// is imported, and some owners are reached only through config/sections, which this package does not
// import, so their keys are absent from the count below. TestEveryDeclaredKeyIsReadBySomething in
// cmd/seid/cmd is the same check over every section a binary declares.
func TestEveryDeclaredKeyIsOneTheNodeActuallyReads(t *testing.T) {
	declared := registry.Keys()
	if len(declared) == 0 {
		t.Skip("this binary declares no sections, so there is nothing to check")
	}

	recorder := recordConstructionReads(t)
	observed := map[string]bool{}
	for _, key := range recorder.Keys() {
		observed[key] = true
	}

	for _, key := range declaredButUnread(declared, observed) {
		t.Errorf("%q is declared and the construction never reads it.\n\nEither the section's tags "+
			"derive a spelling its readers do not resolve, in which case doctor will report every "+
			"real key under that section as unrecognized on every node, or the key is read somewhere "+
			"this recording does not reach, in which case widen the recording rather than remove "+
			"this check.", key)
	}
	t.Logf("%d declared key(s) visible to this test binary, all read by the construction. Sections whose "+
		"owner this package does not import are absent from that count, and the binary-level check in "+
		"cmd/seid/cmd is what covers them", len(declared))
}

// declaredButUnread returns the declared keys nothing was observed reading, sorted.
func declaredButUnread(declared []string, observed map[string]bool) []string {
	var out []string
	for _, key := range declared {
		if !observed[key] {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// TestTheDeclaredKeyCheckCatchesASpellingItsReadersDoNotResolve makes the guard above falsifiable.
//
// That guard passes today because the one migrated section's tags happen to match its flag constants,
// so nothing about it would change if the comparison stopped working. This drives the comparison over
// the spellings a real section's tags would produce against the keys that section's readers actually
// resolve, and requires the mismatch to be reported.
//
// The pair is taken from state-commit, whose tags say async-commit-buffer while every reader and every
// operator's file says sc-async-commit-buffer. Deriving keys from those tags would declare twenty keys
// nothing reads and leave twenty real ones undeclared, which turns doctor into a check that fails on
// every node.
func TestTheDeclaredKeyCheckCatchesASpellingItsReadersDoNotResolve(t *testing.T) {
	recorder := recordConstructionReads(t)
	observed := map[string]bool{}
	for _, key := range recorder.Keys() {
		observed[key] = true
	}

	// What the node reads, and what deriving from the tags would have declared instead.
	real := []string{"state-commit.sc-enable", "state-commit.sc-async-commit-buffer"}
	fromTags := []string{"state-commit.enable", "state-commit.async-commit-buffer"}

	for _, key := range real {
		if !observed[key] {
			t.Fatalf("%q is not in the observed set, so this test's premise is wrong and it measures "+
				"nothing", key)
		}
	}
	if unread := declaredButUnread(real, observed); len(unread) != 0 {
		t.Errorf("the keys this section's readers resolve were reported as unread: %v", unread)
	}
	if unread := declaredButUnread(fromTags, observed); len(unread) != len(fromTags) {
		t.Errorf("declaring %v was accepted, and the node reads none of them. A section registered that "+
			"way would leave doctor refusing every real key under it, on every node", fromTags)
	}
}
