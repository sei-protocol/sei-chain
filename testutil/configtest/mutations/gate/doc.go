// Package gate decides, per named falsifier, whether the configuration characterization suite
// would actually catch a change to production code.
//
// # Why this exists
//
// A test that cannot fail is indistinguishable from one that works, by reading. An audit of this
// suite renamed 27 operator-facing app.toml key names and the whole suite stayed green, so "the
// suite covers these keys" and "the suite would catch a change to them" turned out to be different
// claims. This package settles the second claim by experiment: it breaks production code in a
// recorded way and observes what the suite does about it.
//
// # The three-part observation
//
// Each row of expectations.tsv names a patch, the packages that must react to it, the verdict
// recorded for it, and a substring its failure output must contain. A row is observed in three
// parts, all required:
//
//  1. Clean. The named packages must PASS before the patch is applied.
//  2. Patched. With the patch applied, the named packages must FAIL.
//  3. Attributed. The failure output must contain the row's recorded substring.
//
// Part 1 is not ceremony. A non-zero exit on its own cannot distinguish "the assertion under test
// caught this" from "these packages were already failing for some unrelated reason", and this tree
// contains a live example: one patch reddens evmrpc/config only through a hand-written require.NotNil
// that has nothing to do with the configuration suite. Part 3 is the same defence one step finer, for
// a package that goes red for a reason other than the row's own assertion.
//
// # Verdicts
//
// EXPECTED-RED means the suite catches the mutation today; all three parts must hold.
//
// EXPECTED-GREEN means it does not. The patched run must pass, and the row carries the requirement
// that will close the gap. Recording a gap is the point: an inventoried gap and a hidden one behave
// identically on a green CI run, and only one of them is being worked on.
//
// NOT-OBSERVABLE means no input can produce the divergence, so no patch is applied. The row must name
// the change that would make it observable, because a NOT-OBSERVABLE row with nothing named is
// indistinguishable from a failing row someone downgraded to silence it.
//
// A row moving from EXPECTED-GREEN to EXPECTED-RED is the unit of progress for the coverage work this
// package serves.
//
// # Reporting nothing is a failure
//
// The gate refuses to report success over zero observations. An absent or empty expectations file, or
// a file of rows that apply no patch, is a broken instrument rather than an instrument with nothing to
// do — and an instrument that green-lights over nothing is the artefact this whole effort exists to
// eliminate.
//
// # Where the effects live
//
// Every mutation, revert and test run goes through [Tree]. The real implementation works inside a
// throwaway git worktree, so an interrupted run cannot leave a patch applied in anyone's checkout, and
// the gate always measures the committed state rather than whatever is currently being edited. Tests
// substitute a fake [Tree] and drive the verdict logic without a subprocess.
package gate
