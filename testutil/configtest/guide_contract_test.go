package configtest

import (
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
