package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const header = "patch\tpackages\tverdict\tsubstring\trequirement\tnote\n"

// TestAnEmptyColumnDoesNotShiftTheOnesAfterIt is the parser's central obligation.
//
// A row with an empty substring is the common case: every EXPECTED-GREEN row has one. Splitting such a
// row on runs of tabs rather than on single tabs moves every later column one place left, so the note
// arrives as the requirement and the requirement as the substring — and the result still looks like a
// valid row, which is why nothing downstream can detect it.
func TestAnEmptyColumnDoesNotShiftTheOnesAfterIt(t *testing.T) {
	rows, err := ParseExpectations(strings.NewReader(header +
		"p.patch\t./pkg/\tEXPECTED-GREEN\t\tFR-123\twhy the gap is open\n"))
	if err != nil {
		t.Fatalf("ParseExpectations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("parsed %d rows, want 1", len(rows))
	}

	got := rows[0]
	if got.Substring != "" {
		t.Errorf("Substring = %q, want empty", got.Substring)
	}
	if got.Requirement != "FR-123" {
		t.Errorf("Requirement = %q, want FR-123 — an empty column shifted the ones after it", got.Requirement)
	}
	if got.Note != "why the gap is open" {
		t.Errorf("Note = %q, want the note", got.Note)
	}
}

func TestWrongFieldCountIsRejectedNamingTheLine(t *testing.T) {
	_, err := ParseExpectations(strings.NewReader(header + "p.patch\t./pkg/\tEXPECTED-RED\n"))
	if err == nil {
		t.Fatal("a row with three fields was accepted; its columns would each be read as a neighbour")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("the error does not name the line: %v", err)
	}
}

// TestARowNamedLikeTheHeaderIsNotSkipped pins that the header is recognised by position.
//
// Recognising it by prefix silently discards any real row whose patch file is named with that prefix,
// which is a lost observation rather than an error — and the row would be missing from the run with
// nothing to say so.
func TestARowNamedLikeTheHeaderIsNotSkipped(t *testing.T) {
	rows, err := ParseExpectations(strings.NewReader(header +
		"patchX.patch\t./pkg/\tEXPECTED-RED\tboom\tFR-1\t\n"))
	if err != nil {
		t.Fatalf("ParseExpectations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("parsed %d rows, want 1: a row whose name starts with the header's first field "+
			"was dropped", len(rows))
	}
	if rows[0].Patch != "patchX.patch" {
		t.Errorf("Patch = %q, want patchX.patch", rows[0].Patch)
	}
}

func TestCarriageReturnsAreTrimmed(t *testing.T) {
	rows, err := ParseExpectations(strings.NewReader(
		strings.ReplaceAll(header, "\n", "\r\n") +
			"p.patch\t./pkg/\tEXPECTED-RED\tboom\tFR-1\tnote\r\n"))
	if err != nil {
		t.Fatalf("ParseExpectations: %v", err)
	}
	if len(rows) != 1 || rows[0].Note != "note" {
		t.Fatalf("a CRLF checkout left the last field as %q", rows[0].Note)
	}
}

// TestARowWithNoPackagesIsRejected pins the parse-time refusal of an empty package list.
//
// go test with no packages runs at the repository root, reports no test files, and exits 0 — so the
// row would record a passing observation of nothing.
func TestARowWithNoPackagesIsRejected(t *testing.T) {
	_, err := ParseExpectations(strings.NewReader(header +
		"p.patch\t\tEXPECTED-RED\tboom\tFR-1\t\n"))
	if err == nil {
		t.Fatal("a row with no packages was accepted")
	}
	if !strings.Contains(err.Error(), "repository root") {
		t.Errorf("the error does not say what would happen: %v", err)
	}
}

func TestAnUnknownVerdictIsRejected(t *testing.T) {
	_, err := ParseExpectations(strings.NewReader(header +
		"p.patch\t./pkg/\tPROBABLY-FINE\tboom\tFR-1\t\n"))
	if err == nil {
		t.Fatal("an unknown verdict was accepted")
	}
	if !strings.Contains(err.Error(), "PROBABLY-FINE") {
		t.Errorf("the error does not quote the verdict: %v", err)
	}
}

func TestBlankAndCommentLinesAreSkipped(t *testing.T) {
	rows, err := ParseExpectations(strings.NewReader(header +
		"\n# a comment\np.patch\t./pkg/\tEXPECTED-RED\tboom\tFR-1\t\n"))
	if err != nil {
		t.Fatalf("ParseExpectations: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("parsed %d rows, want 1", len(rows))
	}
}

// TestTheCommittedExpectationsFileParses reads the file the gate actually runs on.
//
// A parser that satisfies its unit tests and rejects the real file is a parser nobody can use, and the
// counts are the ones the run reports, so a silent change to them would change what the gate claims to
// have observed.
func TestTheCommittedExpectationsFileParses(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "expectations.tsv"))
	if err != nil {
		t.Fatalf("open the committed expectations: %v", err)
	}
	defer f.Close() //nolint:errcheck // read-only

	rows, err := ParseExpectations(f)
	if err != nil {
		t.Fatalf("the committed expectations file does not parse: %v", err)
	}

	counts := map[Verdict]int{}
	for _, row := range rows {
		counts[row.Verdict]++
		if row.Verdict == ExpectedRed && row.Substring == "" {
			t.Errorf("line %d (%s) is EXPECTED-RED with no substring, so any red run would satisfy it",
				row.Line, row.Patch)
		}
		if row.Verdict != ExpectedRed && row.Note == "" {
			t.Errorf("line %d (%s) is %s with no note, so the reason it is not caught is unrecorded",
				row.Line, row.Patch, row.Verdict)
		}
	}

	if len(rows) == 0 {
		t.Fatal("the committed expectations file parsed to no rows")
	}
	t.Logf("parsed %d rows: %d red, %d green, %d not-observable",
		len(rows), counts[ExpectedRed], counts[ExpectedGreen], counts[NotObservable])
}

func TestRequirementsSkipCommentsAndBlanks(t *testing.T) {
	ids, err := ParseRequirements(strings.NewReader("# why some are absent\n\nFR-005\n  FR-006  \n"))
	if err != nil {
		t.Fatalf("ParseRequirements: %v", err)
	}
	want := []string{"FR-005", "FR-006"}
	if len(ids) != len(want) {
		t.Fatalf("parsed %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}
