package configtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// wiringRecordSuffix names the per-package record of which checks each section is wired to.
const wiringRecordSuffix = ".golden"

// wiringRecordName is the section name the wiring record is filed under.
//
// One record per package rather than per section, because the thing being recorded is which checks a
// package calls, and that is a fact about the package. A per-section record would also need a call per
// section, and each of those calls would be as deletable as the calls it exists to protect.
const wiringRecordName = "wiring"

// CheckWiring records which checks each of this package's sections is wired to, and fails when the
// wiring changes without the record changing with it.
//
// This closes the one gap every other check in this package shares: deleting a call to any of them is
// silent. A section wired to five checks and then wired to four still passes everything that remains,
// so coverage can be removed without anything reporting it, and the suite reads exactly as it did
// before. Two instances of that were found by experiment, in evmrpc/config and in giga/executor/config,
// where three calls were removed from a fully wired section and every package stayed green.
//
// The wiring is read from the package's own source rather than recorded as calls happen. A run-time
// recorder would have to be consulted after every check had run, which makes the verdict depend on
// test order, and -shuffle=on is a run mode this suite is expected to survive. Reading the source is
// order-independent and gives the same answer under every mode.
//
// What it therefore establishes is that a call is written, not that it executed. That is the right
// property for this: the failure being prevented is a deleted line, and a call wrapped in a condition
// that never fires is a far more visible edit than a line that is simply gone.
//
// Call it once per package. Deleting that call is the one deletion this cannot catch, which is why
// testutil/configtest's own suite asserts that every package importing this helper calls it.
func CheckWiring(t testing.TB) {
	t.Helper()

	got, err := wiringOf(".")
	if err != nil {
		t.Fatalf("read this package's wiring: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no configtest checks were found in this package, so the wiring record would be " +
			"empty and would assert nothing. CheckWiring reads the package in the working directory, " +
			"which for a test is the package's own directory.")
	}

	path := goldenFilePath(t, wiringRecordName, wiringRecordSuffix)
	record := strings.Join(append(wiringRecordHeader(), got...), "\n")

	if recordRewriteInProgress(t, wiringRecordName, path) {
		writeGolden(t, wiringRecordName, path, record)
		return
	}

	raw, err := os.ReadFile(path) //nolint:gosec // testdata/wiring.golden; the file name is validated by goldenFilePath
	if err != nil {
		t.Fatalf("cannot read %s (%v).\nThis file records which checks each section in this package "+
			"is wired to, so that removing a check fails rather than passing quietly. Create it with "+
			"`go test ./<pkg>/ -run TestWiringMatchesTheRecord -update` and read what it lists.",
			path, err)
	}
	if record == recordText(raw) {
		return
	}
	t.Fatalf("this package's wiring no longer matches %s.\n%s\n"+
		"A line that disappeared is a check that was removed, which is the one edit the remaining "+
		"checks cannot report. If the removal is deliberate, regenerate the record with `-update` so "+
		"the lost coverage appears in the diff.", path, wiringDiff(recordText(raw), record))
}

// wiringRecordHeader says what the record is, so it reads without the reader having to find this
// file.
//
// Part of the recorded text rather than a comment the comparison strips, because a header that is
// not compared is a header someone can edit into something untrue.
func wiringRecordHeader() []string {
	return []string{
		"# Which checks each of this package's configuration sections is wired to.",
		"# One line per (section, check) pair, tab-separated, read from this package's own test files.",
		"# A line that disappears is a check that was deleted, which is the one edit the remaining",
		"# checks cannot report. Regenerate with `go test ./<pkg>/ -run TestWiringMatchesTheRecord -update`.",
		"",
	}
}

// wiringOf returns the sorted "section\tcheck" pairs the package in dir calls.
func wiringOf(dir string) ([]string, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			if !strings.HasSuffix(path, "_test.go") {
				continue
			}
			for _, pair := range wiringIn(file) {
				seen[pair] = true
			}
		}
	}

	pairs := make([]string, 0, len(seen))
	for pair := range seen {
		pairs = append(pairs, pair)
	}
	sort.Strings(pairs)
	return pairs, nil
}

// wiringIn collects the checks one file calls, with the section each names.
//
// Matched on the call's shape rather than on a list of check names, so a check added to this package
// later is recorded without anyone remembering to extend a list here.
func wiringIn(file *ast.File) []string {
	var pairs []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		check, ok := configtestCheckName(call)
		if !ok {
			return true
		}
		pairs = append(pairs, sectionOf(call)+"\t"+check)
		return true
	})
	return pairs
}

// configtestCheckName reports the name of the configtest check a call invokes.
func configtestCheckName(call *ast.CallExpr) (string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "configtest" || !strings.HasPrefix(selector.Sel.Name, "Check") {
		return "", false
	}
	// CheckWiring records the others; recording itself would make the file describe its own presence
	// rather than the coverage it protects.
	if selector.Sel.Name == "CheckWiring" {
		return "", false
	}
	return selector.Sel.Name, true
}

// sectionOf returns the section name a check call names.
//
// Every check takes the section as its second argument, after the testing.TB or testing.F. A call whose
// section is not a literal is recorded under a placeholder rather than skipped, because skipping it
// would let a section drop out of the record by being named through a variable.
func sectionOf(call *ast.CallExpr) string {
	const sectionArg = 1
	if len(call.Args) <= sectionArg {
		return "(no section argument)"
	}
	literal, ok := call.Args[sectionArg].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "(section not a literal)"
	}
	name, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "(section not a literal)"
	}
	return name
}

// wiringDiff reports which pairs were lost and which were added, so a failure names the change rather
// than printing two lists for a reader to compare by eye.
func wiringDiff(want, got string) string {
	inGot := make(map[string]bool)
	for _, line := range strings.Split(got, "\n") {
		inGot[line] = true
	}
	inWant := make(map[string]bool)
	for _, line := range strings.Split(want, "\n") {
		inWant[line] = true
	}

	var b strings.Builder
	for _, line := range strings.Split(want, "\n") {
		if line != "" && !inGot[line] {
			fmt.Fprintf(&b, "  removed: %s\n", strings.ReplaceAll(line, "\t", " -> "))
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if line != "" && !inWant[line] {
			fmt.Fprintf(&b, "  added:   %s\n", strings.ReplaceAll(line, "\t", " -> "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
