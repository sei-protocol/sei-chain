package configtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
		scanned[dir] = true

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
