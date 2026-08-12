package configtest

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/experimental"
)

// CheckExperimentalDeclarations asserts every rule about a declaration that needs only the
// registry.
//
// It lives here rather than being exported from config/experimental because a testing-taking
// helper exported from a package every feature imports would drag testing, flag and
// runtime/pprof into the seid binary, breaking the containment this package's golden.go states.
//
// It takes the declared set as data rather than reading package state, so a test can drive an
// arbitrary set without touching the global registry. Call it from a suite whose binary links the
// node's feature packages: config/experimental's own import graph contains zero of them, so two
// teams declaring one name would pass a leaf-package test clean.
//
// It runs the caller-supplied Check, which is exactly what registration must not do. A Check that
// panics is a test failure with a stack trace and one that hangs is a test timeout, where at init
// either would take down every seid invocation including --help.
func CheckExperimentalDeclarations(
	t testing.TB,
	declarations []experimental.Declaration,
	defects []*experimental.DeclarationError,
	checkers []experimental.Checker,
	tombstones []experimental.Tombstone,
) {
	t.Helper()

	// Registration already refused every rule it could judge without running caller code, and
	// recorded each as a defect. A defect reaching a binary is inert plus a boot report, so this is
	// where it becomes a build failure instead.
	for _, d := range defects {
		t.Errorf("experimental declaration %q was refused: %s\n\nA refused declaration is inert: "+
			"every read of it returns the declared default, so an operator who sets it is silently "+
			"ignored. Fix the declaration rather than the check.", d.Name, d.Reason)
	}

	checkDeclarationMetadata(t, declarations)
	checkDefaultsSatisfyTheirOwnCheck(t, checkers)
	checkEnvSpellingCollisions(t, declarations)
	checkTombstonesDoNotCollide(t, declarations, tombstones)
}

// checkDeclarationMetadata asserts the per-declaration rules the registry records.
//
// Duplicated deliberately against register's own refusal. register cannot fail a build, and this
// cannot see a declaration that registration rejected outright, so the two together are what make
// every rule both inert at runtime and fatal at build time.
func checkDeclarationMetadata(t testing.TB, declarations []experimental.Declaration) {
	t.Helper()
	for _, d := range declarations {
		switch {
		case d.Owner == "":
			t.Errorf("experimental key %q has no Owner. A key nobody owns is one nobody can decide "+
				"to promote or delete, and a report naming it has nobody to route to.", d.Name)
		case d.Since == "":
			t.Errorf("experimental key %q has no Since. Without it a report cannot tell an operator "+
				"whether their binary predates the key, which is the difference between a typo and a "+
				"key from a later release.", d.Name)
		}
		if d.Name != strings.ToLower(d.Name) {
			t.Errorf("experimental key %q is not lower case. A configuration source enumerates "+
				"lower-cased, so this key would be reported undeclared forever while Get happened to "+
				"work through case folding.", d.Name)
		}
		segs := strings.Split(d.Name, ".")
		if len(segs) < 2 {
			t.Errorf("experimental key %q has one segment. A name needs at least two, because "+
				"promotion drops the namespace prefix and what remains has to be a section path.", d.Name)
		}
		if len(segs) > experimental.MaxKeySegments {
			t.Errorf("experimental key %q has %d segments, more than the %d the sweep resolves, so "+
				"the key would be skipped forever while Get worked.", d.Name, len(segs), experimental.MaxKeySegments)
		}
		for _, s := range segs {
			if s == "" {
				t.Errorf("experimental key %q has an empty segment.", d.Name)
			}
		}
		if segs[0] == experimental.Namespace {
			t.Errorf("experimental key %q starts with %q. The namespace is a prefix the package "+
				"adds, and repeating it here would survive promotion.", d.Name, experimental.Namespace)
		}
	}
}

// checkDefaultsSatisfyTheirOwnCheck runs each declaration's own Check against its own default.
//
// This is the rule registration cannot assert, because asserting it means running caller code, and
// a Check that wrote to a nil map would panic before main. A default violating its own domain is
// worse than a bad operator value: it is wrong everywhere by default, and no operator action
// reveals it.
func checkDefaultsSatisfyTheirOwnCheck(t testing.TB, checkers []experimental.Checker) {
	t.Helper()
	for _, c := range checkers {
		if ve, ok := c.RejectDefault(); !ok {
			t.Errorf("experimental key %s declares a default its own Check rejects: %v\n\nThis is the "+
				"value every node without an override runs, so it is wrong everywhere by default and "+
				"no operator action reveals it.", c.Path(), ve)
		}
	}
}

// checkEnvSpellingCollisions asserts no two declarations collapse onto one environment variable.
//
// Dots, hyphens and underscores all become underscores, so a.b_c and a.b-c yield one variable
// name. One key's value would then be delivered under a name the other also answers to, and
// nothing at runtime could tell them apart.
func checkEnvSpellingCollisions(t testing.TB, declarations []experimental.Declaration) {
	t.Helper()
	byVar := map[string][]string{}
	for _, d := range declarations {
		v := envify(experimental.Namespace + "." + d.Name)
		byVar[v] = append(byVar[v], d.Name)
	}
	for v, names := range byVar {
		if len(names) > 1 {
			sort.Strings(names)
			t.Errorf("experimental keys %v all collapse onto the environment variable %s. Dots, "+
				"hyphens and underscores are all replaced with underscores, so one operator variable "+
				"would deliver a value to several keys and nothing could tell which was meant.", names, v)
		}
	}
}

// envify renders a dotted path the way the boot's replacer does.
func envify(path string) string {
	return strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(path))
}

// checkTombstonesDoNotCollide asserts no tombstone names a key that is still declared.
//
// A collision makes the sweep's classification order decide the answer: the same key would be
// reported as promoted or as live depending on which check ran first, and an operator would be
// told to move a value that still works.
func checkTombstonesDoNotCollide(
	t testing.TB,
	declarations []experimental.Declaration,
	tombstones []experimental.Tombstone,
) {
	t.Helper()
	live := make(map[string]bool, len(declarations))
	for _, d := range declarations {
		live[d.Name] = true
	}
	for _, tomb := range tombstones {
		if live[tomb.Name] {
			t.Errorf("experimental key %q is both declared and tombstoned. The sweep would classify "+
				"it by whichever check ran first, so an operator could be told to move a value that "+
				"still works.", tomb.Name)
		}
	}
}

// CheckExperimentalGolden records the registry and its tombstones, keyed by name.
//
// Keyed by name rather than written as a list, so inserting one key does not rewrite every later
// line and a review sees only what changed.
//
// An empty registry produces an empty record, which is not self-validating: it would compare clean
// even if registration never appended. The caller therefore also asserts that a key declared in a
// test file reaches Declarations.
func CheckExperimentalGolden(
	t testing.TB,
	name string,
	declarations []experimental.Declaration,
	tombstones []experimental.Tombstone,
) {
	t.Helper()

	var b strings.Builder
	b.WriteString("# experimental registry. Regenerate with -update.\n")
	b.WriteString("# One line per key, name-keyed, so inserting a key does not rewrite the rest.\n")
	b.WriteString("#\n")
	b.WriteString("# A change here is a change to what this binary recognizes. It is deliberately\n")
	b.WriteString("# outside the schema fingerprint: an experimental key may change shape in a patch\n")
	b.WriteString("# release, so this record exists to make that change visible, not to freeze it.\n\n")

	rows := make([]string, 0, len(declarations)+len(tombstones))
	for _, d := range declarations {
		rows = append(rows, fmt.Sprintf("declared %s type=%s default=%s owner=%s since=%s",
			d.Name, d.Type, d.Default, d.Owner, d.Since))
	}
	for _, tomb := range tombstones {
		promoted := tomb.PromotedTo
		if promoted == "" {
			promoted = "-"
		}
		rows = append(rows, fmt.Sprintf("retired  %s type=%s owner=%s since=%s retired_in=%s promoted_to=%s",
			tomb.Name, tomb.Type, tomb.Owner, tomb.Since, tomb.RetiredIn, promoted))
	}
	sort.Strings(rows)
	if len(rows) == 0 {
		b.WriteString("(no experimental keys are declared in this binary)\n")
	}
	for _, r := range rows {
		b.WriteString(r + "\n")
	}

	got := strings.TrimRight(b.String(), "\n")
	path := goldenFilePath(t, name, ".experimental.golden")

	if goldenUpdateRequested() {
		writeGolden(t, name, path, got)
		return
	}
	raw, err := os.ReadFile(path) //nolint:gosec // testdata/<name>.experimental.golden; goldenFilePath validates the whole name
	if err != nil {
		t.Fatalf("%s: cannot read %s (%v).\nIf this record is new, create it with "+
			"`go test ./cmd/seid/cmd/ -run TestExperimentalRegistryMatchesTheRecord -update` and "+
			"review the recorded keys as part of the change.", name, path, err)
	}
	if want := recordText(raw); got != want {
		t.Fatalf("%s: the experimental registry no longer matches %s.\n%s\n"+
			"A key added, removed, renamed, re-typed, re-owned or re-defaulted changes what this "+
			"binary recognizes, which is what an operator's file is written against. Regenerate with "+
			"`go test ./cmd/seid/cmd/ -run TestExperimentalRegistryMatchesTheRecord -update` and keep "+
			"the diff in the review.", name, path, goldenDiff(want, got))
	}
}

// CheckNoExperimentalKeyShadowsThisSection asserts no declared experimental key would collide with
// one of this section's first-class keys once promoted.
//
// It has to be called from the section's own test binary. A KeySpec manifest is an unexported
// package-level var in a _test.go file, and Go compiles that file only into its own package's test
// binary, so no test elsewhere can reference it. The only binary that sees both a section's
// manifest and Declarations() is that section's own.
//
// Two limits, stated rather than implied, because a check whose reach is misread is worse than one
// that is absent.
//
// It compares whole paths, so it catches an identical spelling and nothing else. A semantic
// duplicate under a different name, an experimental occ_worker_count beside a live
// concurrency-workers, is invisible to any string comparison and stays a review question.
//
// A first-class key at TOML root scope is unreachable by it, and that is safe by construction
// rather than by luck: a root-scope key has one segment, and a declared experimental name needs at
// least two, so the two sets cannot intersect.
func CheckNoExperimentalKeyShadowsThisSection(t testing.TB, section string, specs []KeySpec) {
	t.Helper()

	live := make(map[string]bool, len(specs))
	for _, s := range specs {
		live[s.Key] = true
	}
	if len(live) == 0 {
		t.Fatalf("%s: the manifest passed in is empty, so this check would pass for a section whose "+
			"every key an experimental declaration had taken over", section)
	}

	for _, d := range experimental.Declarations() {
		if !live[d.Name] {
			continue
		}
		t.Errorf("experimental key %q (owner %s) declares the path %s already owns as a first-class "+
			"key.\n\nThe declared name is the path the key occupies after promotion, so promoting this "+
			"one would put two declarations on one key. Before that, an operator writing "+
			"experimental.%s and %s has written two different settings that look like one.",
			d.Name, d.Owner, section, d.Name, d.Name)
	}
}
