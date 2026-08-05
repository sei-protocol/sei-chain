package gate

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Verdict is what a row records about the suite's current behaviour under its patch.
type Verdict string

const (
	// ExpectedRed means the suite catches the mutation today.
	ExpectedRed Verdict = "EXPECTED-RED"
	// ExpectedGreen means it does not, and the row names the requirement that will close the gap.
	ExpectedGreen Verdict = "EXPECTED-GREEN"
	// NotObservable means no reachable input produces the divergence, so no patch is applied.
	NotObservable Verdict = "NOT-OBSERVABLE"
)

// expectationFields is the number of tab-separated columns a row must have.
//
// Enforced exactly rather than as a minimum, because a row with the wrong count is a row whose
// columns have shifted, and a shifted row still parses into plausible-looking values: a note read as
// a requirement, a requirement read as a substring. The shell predecessor split on tabs with the
// shell's own field splitting, which collapses runs of tabs, so every row with an empty column
// silently handed the next column's value to the wrong field.
const expectationFields = 6

// Row is one falsifier: a patch, the packages that must react to it, and what is recorded about the
// suite's behaviour under it.
type Row struct {
	// Patch names a file in the patch directory.
	Patch string
	// Packages are the go test package patterns the patch must affect. Never empty.
	Packages []string
	// Verdict is what the suite is recorded as doing under this patch today.
	Verdict Verdict
	// Substring must appear in the failure output of an ExpectedRed row, so that a package going red
	// for an unrelated reason cannot satisfy the row.
	Substring string
	// Requirement is the spec identifier this row belongs to.
	Requirement string
	// Note carries the reason an ExpectedGreen gap is still open, or the enabling change a
	// NotObservable row is waiting on.
	Note string
	// Line is the row's 1-based line in the source file, so a diagnostic can point at it.
	Line int
}

// ParseExpectations reads the rows the gate will observe.
//
// Errors name the line, because the alternative is a reader comparing a rejected file against a
// format description to work out which row is malformed.
func ParseExpectations(r io.Reader) ([]Row, error) {
	var rows []Row
	scanner := bufio.NewScanner(r)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimRight(scanner.Text(), "\r")
		if skippable(text, line) {
			continue
		}
		row, err := parseRow(text, line)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read expectations: %w", err)
	}
	return rows, nil
}

// skippable reports whether a line carries no row.
//
// The header is recognised by being the first line and nothing else. Matching it by prefix would
// also discard any real row whose patch file happens to be named with that prefix, which is a row
// dropped in silence rather than an error.
func skippable(text string, line int) bool {
	return text == "" || strings.HasPrefix(text, "#") || line == 1
}

func parseRow(text string, line int) (Row, error) {
	fields := strings.Split(text, "\t")
	if len(fields) != expectationFields {
		return Row{}, fmt.Errorf("expectations line %d has %d tab-separated fields, want %d: a row "+
			"with the wrong count still parses into plausible values, with each column read as its "+
			"neighbour, so it is rejected rather than guessed at", line, len(fields), expectationFields)
	}

	row := Row{
		Patch:       fields[0],
		Packages:    strings.Fields(fields[1]),
		Verdict:     Verdict(fields[2]),
		Substring:   fields[3],
		Requirement: fields[4],
		Note:        fields[5],
		Line:        line,
	}

	if row.Patch == "" {
		return Row{}, fmt.Errorf("expectations line %d names no patch", line)
	}
	// An empty package list would make go test run over the repository root, where it reports no test
	// files and exits 0 — so the row would record a passing observation of nothing.
	if len(row.Packages) == 0 && row.Verdict != NotObservable {
		return Row{}, fmt.Errorf("expectations line %d (%s) names no packages, so go test would run "+
			"at the repository root and exit 0 without observing the patch", line, row.Patch)
	}
	if err := row.Verdict.validate(line, row.Patch); err != nil {
		return Row{}, err
	}
	return row, nil
}

func (v Verdict) validate(line int, patch string) error {
	switch v {
	case ExpectedRed, ExpectedGreen, NotObservable:
		return nil
	default:
		return fmt.Errorf("expectations line %d (%s) has verdict %q, want one of %s, %s or %s",
			line, patch, v, ExpectedRed, ExpectedGreen, NotObservable)
	}
}

// ParseRequirements reads the identifiers that must each have at least one row.
//
// A requirement naming a falsifier with no row here is a falsifier nobody has executed, which is the
// state this gate exists to leave.
func ParseRequirements(r io.Reader) ([]string, error) {
	var ids []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		text := strings.TrimSpace(strings.TrimRight(scanner.Text(), "\r"))
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		ids = append(ids, text)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read requirements: %w", err)
	}
	return ids, nil
}
