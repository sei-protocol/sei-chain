package configtest

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestEveryWiredPackageRecordsItsWiring closes the one deletion CheckWiring cannot catch: its own.
//
// CheckWiring makes removing any other check loud, because the removal changes the recorded wiring. It
// cannot report its own absence, and a convention that every package remembers to call it is the same
// kind of convention this whole file exists to stop relying on. So the requirement is asserted from one
// place, over every package that imports the helper, rather than repeated in each of them.
//
// Reads the repository rather than a list. A package added later is covered without anyone editing
// anything here, which is the property a checked-in list of packages would not have.
func TestEveryWiredPackageRecordsItsWiring(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Skipf("cannot locate the repository root, so the tree cannot be walked: %v", err)
	}

	wired, unparseable, err := packagesCallingACheck(root)
	if err != nil {
		t.Fatalf("walk the tree: %v", err)
	}
	for _, dir := range unparseable {
		t.Logf("skipped %s, whose sources do not parse, so no wiring could be read from it",
			mustRel(root, dir))
	}
	if len(wired) == 0 {
		t.Fatal("found no packages calling a configtest check, so this check proved nothing. It walks " +
			"the repository looking for the calls, so an empty result means the walk failed rather " +
			"than that nothing is wired.")
	}

	var missing []string
	for _, dir := range wired {
		calls, err := callsCheckWiring(dir)
		if err != nil {
			t.Errorf("read %s: %v", dir, err)
			continue
		}
		if !calls {
			missing = append(missing, mustRel(root, dir))
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("these packages use configtest checks but do not call configtest.CheckWiring, so "+
			"removing one of their checks would pass quietly:\n  %s\n"+
			"Add a test calling configtest.CheckWiring(t) and generate its record with -update.",
			strings.Join(missing, "\n  "))
	}
	requireNoOrphanedRecord(t, root, wired)
	t.Logf("checked %d package(s) calling a configtest check", len(wired))
}

// repoRoot walks up from the working directory to the directory holding go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// packagesCallingACheck returns the directories that call at least one configtest check, and
// separately the directories whose sources do not parse.
//
// Keyed on calling a check rather than on importing the package. Several packages import this helper
// for Isolate, AppOpts or Home and assert nothing through a section, so requiring a wiring record of
// them would mean a record with nothing in it. The package holding the helper is excluded too: its own
// tests drive the checks against fakes and temporary directories rather than against sections of its
// own.
//
// Whether a directory has been scanned is tracked apart from whether it is wired, because one
// directory holds many test files and wiringOf parses every .go file in it. Tracked together, a
// directory calling no check would never be marked and would be reparsed once per test file it
// contains, which across the tree is thousands of redundant parses.
//
// An unparseable directory is reported rather than returned as an error. Failing the walk on a parse
// error anywhere in the tree, the vendored sub-repos included, would fail this test over a file it
// makes no claim about and bury what it does assert. CheckWiring is right to give up on the same
// error, because there the unparseable file is the package under test.
func packagesCallingACheck(root string) (wired, unparseable []string, err error) {
	scanned := make(map[string]bool)
	callsACheck := make(map[string]bool)

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err //nolint:wrapcheck // returned to WalkDir, which reports it
		}
		if d.IsDir() {
			// vendor and .git hold no first-party tests, and testdata is data by convention.
			switch d.Name() {
			case "vendor", ".git", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir := filepath.Dir(path)
		if scanned[dir] || dir == thisPackage(root) {
			return nil
		}
		// Marked before the prefilter, not after, so a directory is examined once whichever way the
		// prefilter goes. Marking only on a hit would re-scan every unrelated directory per test file.
		scanned[dir] = true

		mentions, readErr := someFileMentionsThisPackage(dir)
		if readErr != nil {
			unparseable = append(unparseable, dir)
			return nil
		}
		if !mentions {
			return nil
		}

		pairs, parseErr := wiringOf(dir)
		if parseErr != nil {
			unparseable = append(unparseable, dir)
			return nil
		}
		if len(pairs) > 0 {
			callsACheck[dir] = true
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}

	dirs := make([]string, 0, len(callsACheck))
	for dir := range callsACheck {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	sort.Strings(unparseable)
	return dirs, unparseable, nil
}

// someFileMentionsThisPackage reports whether any test file in dir contains this package's import
// path, as bytes.
//
// A prefilter in front of wiringOf, which parses every .go file in a directory. The walk reaches around
// 440 directories holding roughly 1500 test files, the vendored sub-repos included, to assert a property
// over 11 of them, and a directory that never names this package cannot contribute a pair. Reading bytes
// to decide that is two orders of magnitude cheaper than parsing to find out.
//
// A prefilter rather than a checked-in list of packages, so the discovered-not-declared property holds.
// A package added later is still found without anyone editing this file.
func someFileMentionsThisPackage(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // a _test.go file found by walking the repo
		if err != nil {
			return false, err
		}
		if bytes.Contains(data, []byte("testutil/configtest")) {
			return true, nil
		}
	}
	return false, nil
}

// callsCheckWiring reports whether the package in dir calls configtest.CheckWiring.
//
// Parsed rather than grepped, so the string appearing in a comment does not satisfy it. Reads the
// directory and parses each test file for the same reason wiringOf does, which is that build tags are
// ignored on purpose and go/parser.ParseDir is deprecated for doing exactly that.
func callsCheckWiring(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}

	fset := token.NewFileSet()
	found := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			return false, err
		}
		// The name the file binds this package to, rather than the literal "configtest", for the same
		// reason wiringIn resolves it. A package that aliased the import would otherwise read as one
		// that never calls CheckWiring.
		local, ok := configtestPackageName(file)
		if !ok {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "CheckWiring" {
				return true
			}
			if pkgIdent, ok := selector.X.(*ast.Ident); ok && pkgIdent.Name == local {
				found = true
			}
			return true
		})
	}
	return found, nil
}

// thisPackage is the directory holding the helper itself.
func thisPackage(root string) string {
	return filepath.Join(root, "testutil", "configtest")
}

func mustRel(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return dir
	}
	return rel
}

// requireNoOrphanedRecord fails when a coverage record has no package behind it any more.
//
// The check above reaches a package by finding a check in it, so deleting every check in a package at
// once drops it from the wired set and would leave its record in the tree looking like coverage. This is
// the inverse assertion over the same walk. Every record must have a wired package behind it.
func requireNoOrphanedRecord(t *testing.T, root string, wired []string) {
	t.Helper()

	hasPackage := make(map[string]bool, len(wired))
	for _, dir := range wired {
		hasPackage[dir] = true
	}

	var orphaned []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err //nolint:wrapcheck // returned to WalkDir, which reports it
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != wiringRecordName+wiringRecordSuffix {
			return nil
		}
		// The record lives in testdata, so the package that owns it is the parent of that directory.
		pkg := filepath.Dir(filepath.Dir(path))
		if !hasPackage[pkg] {
			orphaned = append(orphaned, mustRel(root, path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree for coverage records: %v", err)
	}

	if len(orphaned) > 0 {
		sort.Strings(orphaned)
		t.Errorf("these coverage records have no package calling a configtest check behind them, so "+
			"they record coverage that no longer exists:\n  %s\n"+
			"Either restore the checks the record lists or delete the record with them.",
			strings.Join(orphaned, "\n  "))
	}
}

// TestGuideListsEveryPrimitive holds the Primitives table in AGENTS.md to the exported surface.
//
// The table is a claim about this package, so an unchecked one drifts the moment a check is added.
// That is not hypothetical for this table: the surface was miscounted twice while it was being
// written, once as seven checks and once as nine, because CheckRow hides CheckDeterministic and the
// guide listed the checks only in scattered prose.
//
// CheckRow is exempt because the table states it is CheckKey plus CheckDeterministic, both of which
// it lists. That exemption is named here rather than inferred, so a second composition added later
// fails until someone decides how the table should read.
func TestGuideListsEveryPrimitive(t *testing.T) {
	const composition = "CheckRow"

	guide, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatalf("read the package guide: %v", err)
	}
	// Scoped to the Primitives section rather than the whole guide. Scanning everything would let any
	// later markdown table whose first cell is a Check* code span satisfy this, so the required row
	// could be deleted with the test still green, which is the drift it exists to catch.
	section, ok := primitivesSection(string(guide))
	if !ok {
		t.Fatal("AGENTS.md has no `## Primitives` section, so the table this holds to the code has " +
			"been removed or renamed")
	}

	documented := map[string]bool{}
	for _, m := range regexp.MustCompile(`\| `+"`"+`(Check\w+)`+"`").FindAllStringSubmatch(section, -1) {
		documented[m[1]] = true
	}
	if len(documented) == 0 {
		t.Fatal("the guide's Primitives table lists no checks, so this proved nothing about it")
	}

	exported, err := exportedChecks(".")
	if err != nil {
		t.Fatalf("read this package's exported checks: %v", err)
	}

	var undocumented []string
	for _, name := range exported {
		if name != composition && !documented[name] {
			undocumented = append(undocumented, name)
		}
	}
	if len(undocumented) > 0 {
		sort.Strings(undocumented)
		t.Errorf("these checks are exported but absent from the Primitives table in "+
			"testutil/configtest/AGENTS.md:\n  %s\n"+
			"Add a row naming the one failure each prevents, or state why it composes others the "+
			"way %s does.", strings.Join(undocumented, "\n  "), composition)
	}

	present := map[string]bool{}
	for _, name := range exported {
		present[name] = true
	}
	for name := range documented {
		if !present[name] {
			t.Errorf("the Primitives table lists %s, which this package does not export", name)
		}
	}
}

// primitivesSection returns the guide's Primitives section, from its heading to the next one.
func primitivesSection(guide string) (string, bool) {
	const heading = "## Primitives"
	start := strings.Index(guide, heading)
	if start < 0 {
		return "", false
	}
	rest := guide[start+len(heading):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		return rest[:next], true
	}
	return rest, true
}

// exportedChecks returns the names of the exported Check functions the package in dir declares.
func exportedChecks(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Check") {
				continue
			}
			names = append(names, fn.Name.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}
