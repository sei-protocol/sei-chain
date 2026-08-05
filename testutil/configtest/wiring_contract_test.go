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

	wired, err := packagesCallingACheck(root)
	if err != nil {
		t.Fatalf("walk the tree: %v", err)
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
	t.Logf("checked %d package(s) importing this helper", len(wired))
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

// packagesCallingACheck returns the directories that call at least one configtest check.
//
// Keyed on calling a check rather than on importing the package. Several packages import this helper
// for Isolate, AppOpts or Home and assert nothing through a section, so requiring a wiring record of
// them would mean a record with nothing in it. The package holding the helper is excluded too: its own
// tests drive the checks against fakes and temporary directories rather than against sections of its
// own.
func packagesCallingACheck(root string) ([]string, error) {
	seen := make(map[string]bool)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
		if seen[dir] || dir == thisPackage(root) {
			return nil
		}
		pairs, err := wiringOf(dir)
		if err != nil {
			return err //nolint:wrapcheck // returned to WalkDir
		}
		if len(pairs) > 0 {
			seen[dir] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	dirs := make([]string, 0, len(seen))
	for dir := range seen {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// callsCheckWiring reports whether the package in dir calls configtest.CheckWiring.
//
// Parsed rather than grepped, so the string appearing in a comment does not satisfy it.
func callsCheckWiring(dir string) (bool, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return false, err
	}
	found := false
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			if !strings.HasSuffix(path, "_test.go") {
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
				if pkgIdent, ok := selector.X.(*ast.Ident); ok && pkgIdent.Name == "configtest" {
					found = true
				}
				return true
			})
		}
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
