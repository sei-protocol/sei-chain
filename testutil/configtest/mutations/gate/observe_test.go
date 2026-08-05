package gate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// call is one expected interaction with the tree, and the answer the fake gives it.
type call struct {
	method string // "RunTests", "Apply", "Reset", "Dirty"
	arg    string // the joined package list, or the patch path
	out    string // RunTests output
	passed bool   // RunTests verdict
	dirty  string // Dirty answer
	err    error
}

// fakeTree answers a declared sequence of calls, in order.
//
// Ordered rather than keyed, because the clean arm and the patched arm call RunTests with the same
// package list and must receive different answers. A fake keyed on arguments cannot express that, and
// the ways of working around it — a call counter buried in the fake, an "any" answer — all end with
// the fake agreeing with whatever the gate did. Position is what distinguishes the two, so position is
// what the script records.
//
// Any call that does not match the next entry fails the test. That is what proves an arm exists: a
// gate that skips the clean run does not produce the call the script expects next, so the omission
// fails rather than passing quietly.
type fakeTree struct {
	t      *testing.T
	script []call
	at     int
}

func (f *fakeTree) next(method, arg string) call {
	f.t.Helper()
	if f.at >= len(f.script) {
		f.t.Fatalf("unexpected %s(%q): the script declared %d calls and this is number %d",
			method, arg, len(f.script), f.at+1)
	}
	want := f.script[f.at]
	if want.method != method || want.arg != arg {
		f.t.Fatalf("call %d is %s(%q), want %s(%q)", f.at+1, method, arg, want.method, want.arg)
	}
	f.at++
	return want
}

func (f *fakeTree) RunTests(_ context.Context, packages []string) (string, bool, error) {
	c := f.next("RunTests", strings.Join(packages, " "))
	return c.out, c.passed, c.err
}

func (f *fakeTree) Apply(_ context.Context, patchPath string) error {
	return f.next("Apply", patchPath).err
}

func (f *fakeTree) Reset(_ context.Context) error { return f.next("Reset", "").err }

func (f *fakeTree) Dirty(_ context.Context) (string, error) {
	c := f.next("Dirty", "")
	return c.dirty, c.err
}

// exhausted reports whether every declared call happened, so a gate that stops early is caught.
func (f *fakeTree) exhausted() bool { return f.at == len(f.script) }

// redRow is the row every arm test starts from: one EXPECTED-RED falsifier over one package.
func redRow() Row {
	return Row{
		Patch:       "p.patch",
		Packages:    []string{"./pkg/"},
		Verdict:     ExpectedRed,
		Substring:   "RECORDED FAILURE",
		Requirement: "FR-TEST",
		Line:        2,
	}
}

// observeOne runs the gate over a single row against a scripted tree.
func observeOne(t *testing.T, row Row, script []call) (Result, *fakeTree) {
	t.Helper()
	tree := &fakeTree{t: t, script: script}
	got := Run(context.Background(), Config{
		Tree:     tree,
		Rows:     []Row{row},
		PatchDir: "patches",
	})
	if !tree.exhausted() {
		t.Errorf("the gate made %d of %d declared calls, so it stopped short of the contract",
			tree.at, len(script))
	}
	return got, tree
}

// caughtScript is the sequence a healthy EXPECTED-RED observation produces.
func caughtScript() []call {
	return []call{
		{method: "RunTests", arg: "-count=1 ./pkg/", passed: true},
		{method: "Apply", arg: "patches/p.patch"},
		{method: "RunTests", arg: "-count=1 ./pkg/", out: "--- FAIL: X\n    x.go:1: RECORDED FAILURE\n"},
		{method: "Reset"},
		{method: "Dirty"},
	}
}

// TestExpectedRedIsAcceptedWhenAllThreePartsHold is the positive half of the arm tests. Each negative
// test below changes exactly one thing about this scenario, so the failure it provokes is attributable
// to that one change.
func TestExpectedRedIsAcceptedWhenAllThreePartsHold(t *testing.T) {
	got, _ := observeOne(t, redRow(), caughtScript())

	if len(got.Failures) != 0 {
		t.Fatalf("a row whose three parts all hold was reported as failing: %v", got.Failures)
	}
	if got.Observed != 1 || got.RowsRead != 1 {
		t.Errorf("Observed=%d RowsRead=%d, want 1 and 1", got.Observed, got.RowsRead)
	}
}

// TestCleanArmRefusesAnAlreadyRedPackage is the clean arm's negative half.
//
// Without this arm a row certifies on a package that was already failing, and the patched run's
// non-zero exit says nothing about the assertion under test.
func TestCleanArmRefusesAnAlreadyRedPackage(t *testing.T) {
	got, _ := observeOne(t, redRow(), []call{
		{method: "RunTests", arg: "-count=1 ./pkg/", passed: false, out: "--- FAIL: Unrelated\n"},
	})

	requireFailureMentioning(t, got, "ALREADY RED")
	if got.Observed != 0 {
		t.Errorf("Observed=%d, want 0: a row refused at the clean arm observed nothing", got.Observed)
	}
}

// TestAttributionArmRefusesTheWrongFailure is the attribution arm's negative half.
//
// The package goes red, but for a different reason than the row records. Without this arm any red run
// satisfies any EXPECTED-RED row.
func TestAttributionArmRefusesTheWrongFailure(t *testing.T) {
	script := caughtScript()
	script[2].out = "--- FAIL: Something\n    other.go:9: a failure nobody recorded\n"

	got, _ := observeOne(t, redRow(), script)

	requireFailureMentioning(t, got, "not with its recorded failure")
	if got.Observed != 1 {
		t.Errorf("Observed=%d, want 1: the row was observed, then judged against its substring", got.Observed)
	}
}

// TestExpectedRedRefusesAPassingPatchedRun is the patched arm's negative half.
func TestExpectedRedRefusesAPassingPatchedRun(t *testing.T) {
	script := caughtScript()
	script[2].passed = true
	script[2].out = "ok\n"

	got, _ := observeOne(t, redRow(), script)

	requireFailureMentioning(t, got, "did NOT catch it")
}

// TestExpectedRedWithNoSubstringIsRefused pins that the attribution arm cannot be skipped by leaving
// the column blank, which would otherwise be a quieter way to disable it than deleting the check.
func TestExpectedRedWithNoSubstringIsRefused(t *testing.T) {
	row := redRow()
	row.Substring = ""

	got, _ := observeOne(t, row, caughtScript())

	requireFailureMentioning(t, got, "no recorded substring")
}

func TestExpectedGreenIsAcceptedWhenThePatchedRunPasses(t *testing.T) {
	row := redRow()
	row.Verdict = ExpectedGreen
	row.Substring = ""
	row.Note = "FR-999 closes this"

	script := caughtScript()
	script[2].passed = true
	script[2].out = "ok\n"

	got, _ := observeOne(t, row, script)

	if len(got.Failures) != 0 {
		t.Fatalf("an inventoried gap was reported as failing: %v", got.Failures)
	}
	if got.Observed != 1 {
		t.Errorf("Observed=%d, want 1", got.Observed)
	}
}

// TestExpectedGreenReportsAClosedGap is the other side of an inventoried gap: when the suite starts
// catching a mutation, the stale record has to be promoted rather than left as a passing row.
func TestExpectedGreenReportsAClosedGap(t *testing.T) {
	row := redRow()
	row.Verdict = ExpectedGreen
	row.Substring = ""
	row.Note = "FR-999 closes this"

	got, _ := observeOne(t, row, caughtScript())

	requireFailureMentioning(t, got, "the suite CAUGHT it")
}

func TestNotObservableRequiresAnEnablingChange(t *testing.T) {
	unobservable := func(note string) Row {
		return Row{Patch: "u", Verdict: NotObservable, Requirement: "FR-TEST", Note: note, Line: 2}
	}

	// Asserted at the row level. A lone NOT-OBSERVABLE row also trips the run-level "observed
	// nothing" check, which is correct and separately covered, so this looks for the absence of the
	// row's own complaint rather than the absence of every failure.
	withNote, _ := observeOne(t, unobservable("FR-011 makes it reachable"), nil)
	if joined := strings.Join(withNote.Failures, "\n"); strings.Contains(joined, "no enabling change") {
		t.Errorf("a NOT-OBSERVABLE row naming its enabling change was refused: %s", joined)
	}
	if withNote.Unobservable != 1 {
		t.Errorf("Unobservable=%d, want 1", withNote.Unobservable)
	}

	withoutNote, _ := observeOne(t, unobservable(""), nil)
	requireFailureMentioning(t, withoutNote, "no enabling change named")
}

// TestReportingNothingIsAFailure covers the ways a run reaches the end having observed no mutation.
func TestReportingNothingIsAFailure(t *testing.T) {
	t.Run("no rows at all", func(t *testing.T) {
		got := Run(context.Background(), Config{Tree: &fakeTree{t: t}})
		requireFailureMentioning(t, got, "produced no rows")
	})

	t.Run("only rows that apply no patch", func(t *testing.T) {
		got, _ := observeOne(t, Row{
			Patch: "u", Verdict: NotObservable, Requirement: "FR-TEST", Note: "unreachable", Line: 2,
		}, nil)
		requireFailureMentioning(t, got, "no row applied a patch")
	})
}

func TestRequirementWithoutARowFailsTheRun(t *testing.T) {
	tree := &fakeTree{t: t, script: caughtScript()}
	got := Run(context.Background(), Config{
		Tree:         tree,
		Rows:         []Row{redRow()},
		Requirements: []string{"FR-TEST", "FR-ORPHAN"},
		PatchDir:     "patches",
	})

	requireFailureMentioning(t, got, "FR-ORPHAN names a falsifier but has no row")
	if strings.Contains(strings.Join(got.Failures, "\n"), "FR-TEST names") {
		t.Error("FR-TEST has a row, so it must not be reported as uncovered")
	}
}

// TestAPatchThatDoesNotApplyIsReportedNotSkipped pins that a stale patch is a failure rather than a
// silently absent observation.
func TestAPatchThatDoesNotApplyIsReportedNotSkipped(t *testing.T) {
	got, _ := observeOne(t, redRow(), []call{
		{method: "RunTests", arg: "-count=1 ./pkg/", passed: true},
		{method: "Apply", arg: "patches/p.patch", err: errors.New("patch does not apply")},
		{method: "Reset"},
		{method: "Dirty"},
	})

	requireFailureMentioning(t, got, "does not apply")
	if got.Observed != 0 {
		t.Errorf("Observed=%d, want 0", got.Observed)
	}
}

// TestATreeStillDirtyAfterResetStopsTheRow pins that a failed revert is not carried into the next row.
func TestATreeStillDirtyAfterResetStopsTheRow(t *testing.T) {
	script := caughtScript()
	script[4].dirty = " M production.go"

	got, _ := observeOne(t, redRow(), script)

	requireFailureMentioning(t, got, "still carries changes after reverting")
}

// TestTheCleanArmIsMeasuredOncePerPackageSet pins the memoisation: rows sharing a package set must not
// each pay for the same clean run, and the script is what proves the second row did not repeat it.
func TestTheCleanArmIsMeasuredOncePerPackageSet(t *testing.T) {
	first, second := redRow(), redRow()
	second.Patch = "q.patch"

	tree := &fakeTree{t: t, script: []call{
		{method: "RunTests", arg: "-count=1 ./pkg/", passed: true}, // the only clean run
		{method: "Apply", arg: "patches/p.patch"},
		{method: "RunTests", arg: "-count=1 ./pkg/", out: "x.go:1: RECORDED FAILURE"},
		{method: "Reset"},
		{method: "Dirty"},
		{method: "Apply", arg: "patches/q.patch"}, // straight to the patch, no second clean run
		{method: "RunTests", arg: "-count=1 ./pkg/", out: "x.go:1: RECORDED FAILURE"},
		{method: "Reset"},
		{method: "Dirty"},
	}}
	got := Run(context.Background(), Config{
		Tree: tree, Rows: []Row{first, second}, PatchDir: "patches",
	})

	if !tree.exhausted() {
		t.Errorf("made %d of %d declared calls", tree.at, len(tree.script))
	}
	if len(got.Failures) != 0 {
		t.Fatalf("unexpected failures: %v", got.Failures)
	}
	if got.Observed != 2 {
		t.Errorf("Observed=%d, want 2", got.Observed)
	}
}

func requireFailureMentioning(t *testing.T, got Result, want string) {
	t.Helper()
	if len(got.Failures) == 0 {
		t.Fatalf("no failure reported, want one mentioning %q", want)
	}
	if joined := strings.Join(got.Failures, "\n"); !strings.Contains(joined, want) {
		t.Fatalf("no failure mentions %q; got:\n%s", want, joined)
	}
}
