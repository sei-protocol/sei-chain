package gate

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Tree is the working tree the gate mutates, and the only way it reaches the outside world.
//
// An interface rather than direct calls so the verdict logic can be driven without a subprocess. Its
// predecessor kept the git and go invocations inline, which left no way to test the observation logic
// except by editing copies of the script and running them — so the tests could only assert on the
// script's output, never on the sequence of effects it was contracted to produce.
type Tree interface {
	// RunTests runs go test with these arguments and reports whether it passed, with stdout and
	// stderr combined: the failure lines a row is attributed by land on either stream.
	RunTests(ctx context.Context, args []string) (output string, passed bool, err error)
	// Apply applies a patch file to the tree.
	Apply(ctx context.Context, patchPath string) error
	// Reset returns the tree to its committed state, discarding modifications and additions alike.
	Reset(ctx context.Context) error
	// Dirty reports uncommitted changes, empty when there are none.
	Dirty(ctx context.Context) (string, error)
}

// Config is one gate run.
type Config struct {
	Tree         Tree
	Rows         []Row
	Requirements []string
	// PatchDir holds the patch files the rows name.
	PatchDir string
	// CheckRunModes runs the obligations that are properties of how the suite is invoked rather than
	// mutations of production code. Off when driving fixtures, since they say nothing about the rows.
	CheckRunModes bool
	// Log receives progress as it happens. The gate's readable account of what it observed is its
	// product, not a side effect, so a run with no Log is a silent instrument.
	Log func(string)
}

// Result is what a run observed.
type Result struct {
	// RowsRead counts rows parsed from the expectations file.
	RowsRead int
	// Observed counts rows that actually applied a patch and reached a verdict.
	Observed int
	// Unobservable counts rows that applied no patch by design.
	Unobservable int
	// Failures are review prompts, each stating what disagreed and what to do about it.
	Failures []string
	// Aborted marks a run that could not start, as distinct from one that ran and found failures.
	Aborted bool
}

// Run makes the three-part observation for every row, then checks the properties that hold over the
// run as a whole.
//
// The order is the argument: rows are observed, then the requirement set is checked for falsifiers
// nobody executed, then the run-mode obligations, and only then is a verdict reported — because a
// verdict over zero observations is the failure this gate exists to detect, and reporting it before
// counting would be that failure.
func Run(ctx context.Context, cfg Config) Result {
	g := &gate{cfg: cfg}

	g.observeEveryRow(ctx)
	g.requireEveryFalsifierHasARow()
	g.checkRunModeObligations(ctx)
	g.requireSomethingWasObserved()

	return g.result
}

// gate carries one run's configuration and the result it is accumulating.
type gate struct {
	cfg    Config
	result Result
	// cleanArm memoises the clean run per package set. Rows share package sets, and the clean run for
	// a given set cannot differ between them, so re-running it would cost minutes to learn nothing.
	cleanArm map[string]bool
}

func (g *gate) log(format string, args ...any) {
	if g.cfg.Log != nil {
		g.cfg.Log(fmt.Sprintf(format, args...))
	}
}

func (g *gate) fail(format string, args ...any) {
	g.result.Failures = append(g.result.Failures, fmt.Sprintf(format, args...))
}

// testArgs is how a row's packages become a go test invocation.
//
// -count=1 is here rather than in the Tree, so the count a run-mode obligation needs is chosen by that
// obligation instead of negotiated with the runner.
func testArgs(row Row) []string {
	return append([]string{"-count=1"}, row.Packages...)
}

func (g *gate) observeEveryRow(ctx context.Context) {
	for _, row := range g.cfg.Rows {
		g.result.RowsRead++
		g.log("")
		g.log("=== %s  [%s]  (%s)", row.Patch, row.Verdict, row.Requirement)
		g.observeRow(ctx, row)
	}
}

func (g *gate) observeRow(ctx context.Context, row Row) {
	if row.Verdict == NotObservable {
		g.recordUnobservable(row)
		return
	}
	if !g.cleanArmPasses(ctx, row) {
		return
	}
	seen := g.patchedArm(ctx, row)
	// The tree is checked either way: a patch that failed to apply can still have left part of itself
	// behind, and the next row would be observed against it.
	if !g.treeIsClean(ctx, row) {
		return
	}
	if !seen.happened {
		return
	}
	g.result.Observed++
	g.judge(row, seen)
}

// observation is what the patched arm saw, or that it saw nothing at all.
//
// The distinction is load-bearing. A patch that did not apply and a patch whose run passed both leave
// no failure to attribute, and collapsing them into one bool counted an unapplied patch as an
// observation and then judged the row against a run that never happened.
type observation struct {
	output   string
	failed   bool
	happened bool
}

// recordUnobservable accepts a row that applies no patch, provided it says what would make it
// observable.
//
// Without that, NOT-OBSERVABLE is a one-line way to silence a row that started failing — the same
// move as deleting the assertion, which is what the suite's own guide forbids.
func (g *gate) recordUnobservable(row Row) {
	if row.Note == "" {
		g.fail("%s: NOT-OBSERVABLE with no enabling change named. Name the change that would make "+
			"the divergence reachable, or give the row a verdict that can be observed.", row.Patch)
		return
	}
	g.result.Unobservable++
	g.log("    no patch applied: %s", row.Note)
}

// cleanArmPasses reports whether the row's packages pass before its patch is applied.
//
// A row whose packages are already red cannot certify anything: the patched run would exit non-zero
// either way, so the observation would record a catch that never happened.
func (g *gate) cleanArmPasses(ctx context.Context, row Row) bool {
	key := strings.Join(row.Packages, " ")
	if g.cleanArm == nil {
		g.cleanArm = make(map[string]bool)
	}
	passed, measured := g.cleanArm[key]
	if !measured {
		var err error
		_, passed, err = g.cfg.Tree.RunTests(ctx, testArgs(row))
		if err != nil {
			g.fail("%s: could not run %s on the clean tree: %v", row.Patch, key, err)
			return false
		}
		g.cleanArm[key] = passed
	}

	if !passed {
		g.fail("%s: %s is ALREADY RED before the patch, so this row cannot certify anything. A "+
			"non-zero exit under the patch would prove nothing about the assertion under test. Fix "+
			"the packages or narrow the row's package list.", row.Patch, key)
		return false
	}
	g.log("    1. clean: %s exit 0", key)
	return true
}

func (g *gate) patchedArm(ctx context.Context, row Row) observation {
	patch := filepath.Join(g.cfg.PatchDir, row.Patch)
	if err := g.cfg.Tree.Apply(ctx, patch); err != nil {
		g.fail("%s: does not apply. The construct it targets has moved, so regenerate the patch "+
			"against that construct: %v", row.Patch, err)
		g.revert(ctx, row)
		return observation{}
	}

	output, passed, err := g.cfg.Tree.RunTests(ctx, testArgs(row))
	g.revert(ctx, row)
	if err != nil {
		g.fail("%s: could not run %s under the patch: %v", row.Patch, strings.Join(row.Packages, " "), err)
		return observation{}
	}
	return observation{output: output, failed: !passed, happened: true}
}

// revert returns the tree to its committed state, reporting a revert that did not take rather than
// carrying it into the next row.
func (g *gate) revert(ctx context.Context, row Row) {
	if err := g.cfg.Tree.Reset(ctx); err != nil {
		g.fail("%s: could not revert: %v", row.Patch, err)
	}
}

// treeIsClean confirms the revert took, before the next row is observed against a tree that still
// carries this row's patch.
func (g *gate) treeIsClean(ctx context.Context, row Row) bool {
	dirty, err := g.cfg.Tree.Dirty(ctx)
	if err != nil {
		g.fail("%s: could not check the tree after reverting: %v", row.Patch, err)
		return false
	}
	if dirty != "" {
		g.fail("%s: the tree still carries changes after reverting, so no later row can be trusted:\n%s",
			row.Patch, dirty)
		return false
	}
	return true
}

func (g *gate) judge(row Row, seen observation) {
	switch row.Verdict {
	case ExpectedGreen:
		g.judgeExpectedGreen(row, seen.failed)
	case ExpectedRed:
		g.judgeExpectedRed(row, seen.output, seen.failed)
	case NotObservable:
		// Returned before reaching here; a row cannot be judged without an observation.
	}
}

func (g *gate) judgeExpectedGreen(row Row, failed bool) {
	if failed {
		g.fail("%s: recorded EXPECTED-GREEN, but the suite CAUGHT it. The gap has closed: promote "+
			"the row to EXPECTED-RED with the substring its failure prints, in the change that "+
			"closed it. Recorded gap: %s", row.Patch, row.Note)
		return
	}
	g.log("    2. patched: exit 0 as recorded — the gap is real and inventoried")
	g.log("       gap: %s", row.Note)
}

func (g *gate) judgeExpectedRed(row Row, output string, failed bool) {
	if !failed {
		g.fail("%s: recorded EXPECTED-RED, but the suite did NOT catch it. Either the assertion "+
			"meant to catch this asserts nothing, or the patch no longer breaks what it names.",
			row.Patch)
		return
	}
	g.log("    2. patched: exit non-zero")

	if row.Substring == "" {
		g.fail("%s: EXPECTED-RED with no recorded substring, so a package going red for an "+
			"unrelated reason would satisfy this row.", row.Patch)
		return
	}
	if !strings.Contains(output, row.Substring) {
		g.fail("%s: went red, but not with its recorded failure, so the reddening cannot be "+
			"attributed to the assertion under test.\n       wanted: %s\n       got: %s",
			row.Patch, row.Substring, firstFailureLines(output))
		return
	}
	g.log("    3. attributed: failure output contains %q", row.Substring)
}

// requireEveryFalsifierHasARow reports requirements that name a falsifier nobody has executed.
func (g *gate) requireEveryFalsifierHasARow() {
	g.log("")
	g.log("=== completeness: every requirement that names a falsifier has a row")

	covered := make(map[string]bool, len(g.cfg.Rows))
	for _, row := range g.cfg.Rows {
		covered[row.Requirement] = true
	}
	for _, id := range g.cfg.Requirements {
		if !covered[id] {
			g.fail("%s names a falsifier but has no row here, so that falsifier has never been "+
				"executed.", id)
		}
	}
}

// runModeObligations are properties of how the suite is invoked rather than of production code.
//
// Kept out of the rows deliberately. A row's patch mutates production, and a row that could certify
// against a mutated test would certify against a change no operator can make.
var runModeObligations = []struct {
	args []string
	why  string
}{
	{
		args: []string{"-count=2", "-shuffle=on", "./sei-cosmos/server/config/"},
		why: "a package-global written by one test and not restored makes the run order load-bearing, " +
			"and -shuffle=on is the run mode the suite's guide recommends",
	},
}

func (g *gate) checkRunModeObligations(ctx context.Context) {
	if !g.cfg.CheckRunModes {
		return
	}
	g.log("")
	g.log("=== run-mode obligations (not rows: their falsifier mutates a test, not production)")
	for _, obligation := range runModeObligations {
		if _, passed, err := g.cfg.Tree.RunTests(ctx, obligation.args); err != nil || !passed {
			g.fail("go test %s fails: %s", strings.Join(obligation.args, " "), obligation.why)
			continue
		}
		g.log("    go test %s passes", strings.Join(obligation.args, " "))
	}
}

// requireSomethingWasObserved makes an empty run a failure rather than a pass.
//
// An absence of disagreement is not a result. Both counts are checked because rows that apply no
// patch still count as rows, so a file of nothing but NOT-OBSERVABLE entries would otherwise report
// success having observed nothing.
func (g *gate) requireSomethingWasObserved() {
	if g.result.RowsRead == 0 {
		g.fail("the expectations file produced no rows, so no mutation was observed.")
	}
	if g.result.Observed == 0 {
		g.fail("no row applied a patch, so nothing was observed. %d row(s) were NOT-OBSERVABLE; a "+
			"file of only those reports success while testing nothing.", g.result.Unobservable)
	}
}

// firstFailureLines picks the lines a reader needs to see why a package went red, so a diagnostic
// does not carry a whole test log.
func firstFailureLines(output string) string {
	const want = 3
	var kept []string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--- FAIL") || strings.Contains(trimmed, ".go:") {
			kept = append(kept, trimmed)
			if len(kept) == want {
				break
			}
		}
	}
	if len(kept) == 0 {
		return "(no recognisable failure lines)"
	}
	return strings.Join(kept, " | ")
}
