package configtest

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoldenFilePathConfinesBothHalvesOfTheFileName pins what the //nolint:gosec suppressions on
// the two golden reads rest on.
//
// Each suppression says the path it reads is testdata/<section><suffix> and nothing else, and
// what makes that true is this function refusing everything else. Both halves are checked
// because both are joined: a validated section name with an unvalidated suffix appended to it is
// not a validated path, and `goldenFilePath(t, "app", "/../../../../../../etc/crontab")` used to
// return a path outside the repository. No call site can reach that today — the suffix is a
// constant at both of them — which is the reason to hold it here rather than in an argument about
// reachability that the next call site invalidates.
func TestGoldenFilePathConfinesBothHalvesOfTheFileName(t *testing.T) {
	for _, accepted := range []struct{ name, suffix string }{
		{"app", ".golden"},
		{"state-commit", keyNameRecordSuffix},
		{"light_invariance", keyNameRecordSuffix},
	} {
		want := filepath.Join("testdata", accepted.name+accepted.suffix)
		if got := goldenFilePath(t, accepted.name, accepted.suffix); got != want {
			t.Errorf("goldenFilePath(%q, %q) = %q, want %q", accepted.name, accepted.suffix, got, want)
		}
	}

	for _, rejected := range []struct{ what, name, suffix string }{
		{"a traversal in the suffix", "app", "/../../../../../../etc/crontab"},
		{"a traversal in the name", "../../etc/crontab", ".golden"},
		{"a separator in the name", "app/nested", ".golden"},
		{"a relative prefix", "./app", ".golden"},
		{"an absolute name", "/etc/crontab", ".golden"},
		{"the parent directory", "..", ""},
		{"no name at all", "", ".golden"},
	} {
		reported := capture(t, func(tb testing.TB) { goldenFilePath(tb, rejected.name, rejected.suffix) })
		if !reported.fatal {
			t.Errorf("%s: goldenFilePath(%q, %q) returned instead of giving up, so a golden read or "+
				"write could leave testdata", rejected.what, rejected.name, rejected.suffix)
			continue
		}
		if msg := reported.only(t); !strings.Contains(msg, "testdata") {
			t.Errorf("%s: the rejection does not say where a golden file may live:\n%s",
				rejected.what, msg)
		}
	}
}

// TestRecordWriteIsRefusedUnderCI pins the refusal that keeps -update from rewriting a checked-in
// record where nobody reviews the diff.
//
// The three cases are the three records one process-global switch used to silence together. Two are
// writers; the third is a comparison requireKeyNameRecord skips, which is why its absence reads as
// a pass rather than a failure and why it needs its own case. A test covering only the writers
// would pass while the cross-check went on quietly returning early.
func TestRecordWriteIsRefusedUnderCI(t *testing.T) {
	t.Setenv("CI", "true")

	// Not withUpdateFlag: that helper sets allowRecordWriteUnderCI, which is exactly what this test
	// needs left alone. The flag is set and restored the same way the helper does it.
	f := flag.Lookup("update")
	if f == nil {
		t.Fatal("the -update flag is not registered, so the refusal cannot be exercised")
	}
	previous := f.Value.String()
	if err := f.Value.Set("true"); err != nil {
		t.Fatalf("set -update: %v", err)
	}
	t.Cleanup(func() {
		if err := f.Value.Set(previous); err != nil {
			t.Errorf("restore -update to %q: %v", previous, err)
		}
	})

	if !recordWriteRefused() {
		t.Fatal("recordWriteRefused() is false under CI with -update and no in-process override, " +
			"so the refusal would never fire and a CI run would rewrite every record it touched")
	}

	t.Chdir(t.TempDir())
	if err := os.MkdirAll("testdata", 0o750); err != nil {
		t.Fatalf("create testdata: %v", err)
	}

	specs := []KeySpec{{Key: "refusal.enabled", Path: "Enabled", Cast: CastBool}}
	recordPath := filepath.Join("testdata", "refusal"+keyNameRecordSuffix)

	for _, c := range []struct {
		what string
		call func(testing.TB)
	}{
		{"CheckDefaults", func(tb testing.TB) {
			CheckDefaults(tb, "refusal", struct{ Enabled bool }{Enabled: true})
		}},
		{"CheckKeyNames", func(tb testing.TB) { CheckKeyNames(tb, "refusal", specs) }},
		{"requireKeyNameRecord", func(tb testing.TB) {
			requireKeyNameRecord(tb, "refusal", recordPath, specs, nil)
		}},
	} {
		t.Run(c.what, func(t *testing.T) {
			got := capture(t, c.call)
			reported := strings.Join(got.failures, "\n")

			if len(got.failures) == 0 {
				t.Fatalf("%s reported nothing under CI with -update, so the record was rewritten "+
					"or the check was skipped where nobody reviews the diff", c.what)
			}
			if !strings.Contains(reported, "refusing to rewrite") {
				t.Errorf("%s failed but not with the refusal: %q", c.what, reported)
			}
			// The message must name the record file, not just the flag: the reader's next question is
			// which file was about to be overwritten.
			//
			// Asserted on a path token rather than on the section name. The section here is called
			// "refusal", and refuseRecordWrite interpolates name before path, so a message that had
			// dropped path entirely would still contain "refusal" — the assertion would have passed
			// while proving nothing about the file being named. "testdata/" can only come from path.
			if !strings.Contains(reported, "testdata/") {
				t.Errorf("%s refused without naming the record file: %q", c.what, reported)
			}
		})
	}

	// Nothing was written. A refusal that failed the test but left a rewritten file behind would be
	// half a guard, since the file is the thing that matters.
	//
	// What this does not cover is a package that writes a golden without routing through
	// writeGolden. sei-tendermint/internal/p2p/conn declares its own -update flag and writes
	// directly, so the refusal cannot see it. That is out of scope here rather than covered
	// elsewhere, and worth knowing before anyone reads this as protecting every record in the tree.
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the refusal left files behind: %v", names)
	}
}

// TestInProcessOverrideReEnablesTheWrite pins the one escape, and that it is the only one.
//
// The refusal has to be defeatable from inside the package, or this package's own contract tests
// could not observe a writer writing. It must not be defeatable from anywhere else, which is why
// the override is an unexported variable rather than an environment variable or a second flag: a CI
// job that could set it would reinstate the hole the refusal closes.
func TestInProcessOverrideReEnablesTheWrite(t *testing.T) {
	t.Setenv("CI", "true")
	withUpdateFlag(t)

	if recordWriteRefused() {
		t.Fatal("withUpdateFlag did not set the in-process override, so this package's own " +
			"contract tests cannot observe a write and would fail under CI instead")
	}

	t.Chdir(t.TempDir())
	if err := os.MkdirAll("testdata", 0o750); err != nil {
		t.Fatalf("create testdata: %v", err)
	}

	CheckKeyNames(t, "override", []KeySpec{{Key: "override.enabled", Path: "Enabled", Cast: CastBool}})

	if _, err := os.Stat(filepath.Join("testdata", "override"+keyNameRecordSuffix)); err != nil {
		t.Fatalf("the override did not re-enable the write: %v", err)
	}
}

// TestRefusalSurvivesIsolate pins the one coupling that would make the refusal silently inert.
//
// recordWriteRefused reads the environment, and Isolate unsets everything outside envAllowlist. So a
// check that isolated before writing would find CI unset, write the record, and report success —
// which is the hole the refusal exists to close, reopened by the hermeticity helper rather than by
// anything a reviewer would look at. CI is on the allowlist for this reason, and this test is what
// keeps it there: dropping the entry makes this fail rather than making a future writer unguarded.
func TestRefusalSurvivesIsolate(t *testing.T) {
	t.Setenv("CI", "true")
	if !runningUnderCI() {
		t.Fatal("CI is set but runningUnderCI() is false")
	}

	Isolate(t)

	if !runningUnderCI() {
		t.Fatal("Isolate stripped CI, so the record refusal would silently no-op in every test that " +
			"isolates before writing. Add CI back to envAllowlist in env.go with the reason.")
	}
	if !recordWriteRefused() {
		t.Fatal("the refusal does not fire after Isolate, so an isolated writer would rewrite a " +
			"checked-in record on CI")
	}
}

// TestNoTestInThisPackageIsParallel enforces the invariant allowRecordWriteUnderCI depends on.
//
// That override is a mutable package variable, and withUpdateFlag turns it on for the duration of one
// test. That is only safe while no test in this package runs concurrently with another: a parallel
// test driving a writer under CI would observe the window in which the refusal is off. Nothing
// enforced that until now — it was a convention in a comment, which is the shape this suite exists
// to stop trusting.
//
// Parsed rather than grepped. A textual search for "t.Parallel()" matches the string literal in this
// function's own failure message, and matched it on the first run — a check that counts occurrences
// in text is satisfied by prose, which is the defect one level up from the one being prevented. The
// AST sees calls only.
func TestNoTestInThisPackageIsParallel(t *testing.T) {
	// Read the directory and parse each file rather than using go/parser.ParseDir, for the reason
	// wiringOf gives: the standard library deprecates ParseDir for ignoring build tags, and ignoring
	// them is what this wants. A t.Parallel behind a build tag is still a t.Parallel.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	scanned := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		name := entry.Name()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Parallel" {
				return true
			}
			t.Errorf("%s calls %s.Parallel() at %s. allowRecordWriteUnderCI is a mutable package "+
				"variable that withUpdateFlag turns on for one test at a time, so a concurrent test "+
				"can observe the window in which the record refusal is off. Either drop the call or "+
				"replace the override with something a concurrent test cannot see.",
				name, types.ExprString(sel.X), fset.Position(call.Pos()))
			return true
		})
	}

	// A scan that parsed nothing would pass while checking nothing — the same defect this package
	// exists to catch, one level up.
	if scanned == 0 {
		t.Fatal("parsed no _test.go files, so this check proved nothing about the package")
	}
}
