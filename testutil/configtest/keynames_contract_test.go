package configtest

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// withKeyNameRecord puts a section's key-name record where the checks look for it.
//
// Both CheckKeyNames and requireKeyNameRecord resolve testdata relative to the working
// directory, which is a package's own directory in a real suite. This package has no
// sections, so the record is written into a directory of the test's own. t.Chdir rather than
// os.Chdir because it restores on cleanup and refuses to run under t.Parallel, and the
// working directory is process-global.
func withKeyNameRecord(t *testing.T, name string, specs []KeySpec, alsoRecorded ...KeyName) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("testdata", 0o750); err != nil {
		t.Fatalf("create testdata: %v", err)
	}
	path := filepath.Join("testdata", name+keyNameRecordSuffix)
	if err := os.WriteFile(path, []byte(keyNameRecord(specs, alsoRecorded)+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// renamedKey returns specs with the key at row i changed, which is what editing the flag
// constant a reader and a manifest share does to the manifest.
func renamedKey(specs []KeySpec, i int, to string) []KeySpec {
	out := append([]KeySpec(nil), specs...)
	out[i].Key = to
	return out
}

// TestCheckKeyNamesComparesTheResolvedStringNotTheSpelling is the property the deliverable
// reduces to.
//
// The gap this check closes is that a row spelled as a reference to the reader's own flag
// constant moves when that constant's value is edited, so the row and the reader agree on a
// key no node has ever been configured with and every assertion still passes. The record
// holds the resolved string, so it is indifferent to the spelling: a row reached through a
// constant is checked against the same recorded name as a literal one, and editing the
// constant is a diff.
func TestCheckKeyNamesComparesTheResolvedStringNotTheSpelling(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)

	// A constant standing in for the reader's flag constant, holding the name the record was
	// written with. A row reaching its key through it must be accepted.
	const flagB = "probe.b"
	viaConstant := renamedKey(tableSpecs, 1, flagB)
	if reported := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, viaConstant)
	}); len(reported.failures) != 0 {
		t.Errorf("a row spelled through a constant holding the recorded name was rejected:\n%s",
			strings.Join(reported.failures, "\n"))
	}

	// Editing that constant's value is how an operator-facing key gets renamed. The row is not
	// touched, and the record must still notice.
	const renamedFlagB = "probe.b-v2"
	msg := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, renamedKey(tableSpecs, 1, renamedFlagB))
	}).only(t)

	for _, want := range []string{"probe.b", renamedFlagB, "renamed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the report does not contain %q, so a reviewer cannot see what moved:\n%s", want, msg)
		}
	}
}

// TestCheckKeyNamesReportsAnAddedRowAndADeletedOne pins the two edits that are not renames.
//
// A deleted row matters beyond bookkeeping: deleting a row rather than updating it is one of
// the four ways of silencing a failure the harness guide forbids, and it takes a key out of
// the manifest a replacement implementation reads as the contract. The record makes it a
// diff.
func TestCheckKeyNamesReportsAnAddedRowAndADeletedOne(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)

	added := append(append([]KeySpec(nil), tableSpecs...),
		KeySpec{Key: "probe.d", Path: "D", Cast: CastInt})
	msg := capture(t, func(tb testing.TB) { CheckKeyNames(tb, probeSection, added) }).only(t)
	if !strings.Contains(msg, "added") || !strings.Contains(msg, "probe.d") {
		t.Errorf("a key added to the section must be reported as added, by name:\n%s", msg)
	}

	msg = capture(t, func(tb testing.TB) { CheckKeyNames(tb, probeSection, tableSpecs[:2]) }).only(t)
	if !strings.Contains(msg, "removed") || !strings.Contains(msg, "probe.c") {
		t.Errorf("a deleted row must be reported as removed, by name:\n%s", msg)
	}
}

// TestCheckKeyNamesReportsAReorderAsARebinding pins the case where the set of names is
// unchanged.
//
// Row order is not cosmetic here: a section's seeds select rows by index, so moving a row
// silently rebinds every seeds.AddRow that names a position at or after it. The record is
// kept in manifest order so that this shows up at all, and the report says which positions
// moved rather than leaving a reviewer to diff two lists that hold the same names.
func TestCheckKeyNamesReportsAReorderAsARebinding(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)

	reordered := []KeySpec{tableSpecs[1], tableSpecs[0], tableSpecs[2]}
	msg := capture(t, func(tb testing.TB) { CheckKeyNames(tb, probeSection, reordered) }).only(t)

	if !strings.Contains(msg, "different order") {
		t.Errorf("the same names in a new order must be reported as a reorder rather than as a "+
			"rename:\n%s", msg)
	}
	for _, want := range []string{"row 0", "row 1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the report does not name the moved position %q, which is what the seeds bind "+
				"to:\n%s", want, msg)
		}
	}
}

// TestCheckKeyNamesQuotesSoInvisibleCharactersAreVisible pins the rendering.
//
// A key with a trailing space is a real typo — viper looks up the string as given — and an
// unquoted record would render it identically to the correct spelling, hiding the one thing
// the file exists to show.
func TestCheckKeyNamesQuotesSoInvisibleCharactersAreVisible(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)

	msg := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, renamedKey(tableSpecs, 0, "probe.a "))
	}).only(t)

	if !strings.Contains(msg, `"probe.a "`) {
		t.Errorf("a trailing space in a key must survive into the report, or the diff shows two "+
			"identical-looking names:\n%s", msg)
	}
}

// TestCheckKeyNamesRoundTripsThroughUpdate pins the workflow a legitimate rename follows.
//
// A check that cannot be satisfied deliberately gets worked around, so the -update path has
// to work: regenerate, and the run that follows passes with the new name recorded. The
// recorded file is inspected as well as the verdict, because "passes after -update" is also
// true of a check that wrote nothing and compared nothing.
func TestCheckKeyNamesRoundTripsThroughUpdate(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)
	renamed := renamedKey(tableSpecs, 1, "probe.b-v2")

	withUpdateFlag(t)
	if reported := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, renamed)
	}); len(reported.failures) != 0 {
		t.Fatalf("regenerating the record failed:\n%s", strings.Join(reported.failures, "\n"))
	}

	path := filepath.Join("testdata", probeSection+keyNameRecordSuffix)
	recorded, err := os.ReadFile(path) //nolint:gosec // written by this test into its own directory
	if err != nil {
		t.Fatalf("read back %s: %v", path, err)
	}
	if got, want := string(recorded), keyNameRecord(renamed, nil)+"\n"; got != want {
		t.Errorf("the regenerated record is not the manifest's names\n got: %q\nwant: %q", got, want)
	}
}

// TestCheckKeyNamesRoundTripPassesOnTheNextRun completes the round trip: the comparison the
// -update run skipped has to hold on the run after it, with -update off.
func TestCheckKeyNamesRoundTripPassesOnTheNextRun(t *testing.T) {
	renamed := renamedKey(tableSpecs, 1, "probe.b-v2")
	withKeyNameRecord(t, probeSection, renamed)

	if reported := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, renamed)
	}); len(reported.failures) != 0 {
		t.Errorf("a recorded rename does not pass on the run after -update:\n%s",
			strings.Join(reported.failures, "\n"))
	}
}

// TestCheckKeyNamesReportRendersARecordLineRatherThanObeyingIt pins that the record cannot write
// the report.
//
// This failure text is read in a terminal by someone who has just broken something. A record
// hand-edited to hold ANSI escapes would otherwise get to choose what that failure looks like, and
// clearing the screen and printing a green success line is enough. So a line that is not already
// one key in canonical quoted form is rendered as quoted raw text — the escapes become visible
// instead of executed — and a line too long to be a key is cut.
func TestCheckKeyNamesReportRendersARecordLineRatherThanObeyingIt(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)
	path := filepath.Join("testdata", probeSection+keyNameRecordSuffix)

	const hostile = "\x1b[2J\x1b[32mok  \tgithub.com/sei-protocol/sei-chain/app\t36.635s\x1b[0m"
	if err := os.WriteFile(path, []byte(hostile+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	msg := capture(t, func(tb testing.TB) { CheckKeyNames(tb, probeSection, tableSpecs) }).only(t)
	if strings.ContainsRune(msg, '\x1b') {
		t.Errorf("the record's escape bytes reached the report, so a record decides what a failing "+
			"check looks like:\n%q", msg)
	}
	if !strings.Contains(msg, `\x1b`) {
		t.Errorf("the escapes are neither printed nor escaped, so the reader cannot see what the "+
			"record holds:\n%s", msg)
	}

	// A line that is not UTF-8 at all reaches the same branch, and it is the one where rendering
	// raw text would put the bytes on a terminal verbatim.
	if err := os.WriteFile(path, []byte("\"probe.\xff\xfe\"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	msg = capture(t, func(tb testing.TB) { CheckKeyNames(tb, probeSection, tableSpecs) }).only(t)
	if strings.ContainsRune(msg, 0xfffd) || strings.Contains(msg, "\xff") {
		t.Errorf("a record line that is not UTF-8 reached the report as bytes rather than as "+
			"escapes:\n%q", msg)
	}
	if !strings.Contains(msg, `\xff`) {
		t.Errorf("the report neither prints nor escapes a non-UTF-8 line, so the reader cannot see "+
			"what the record holds:\n%s", msg)
	}

	long := strings.Repeat("k", 4*maxShownKeyBytes)
	if err := os.WriteFile(path, []byte(strconv.Quote(long)+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	msg = capture(t, func(tb testing.TB) { CheckKeyNames(tb, probeSection, tableSpecs) }).only(t)
	if strings.Contains(msg, long) {
		t.Errorf("a record line four times the length of any key reached the report whole, so one "+
			"line can bury the diff it is part of:\n%s", msg)
	}
	if !strings.Contains(msg, "bytes on this line") {
		t.Errorf("the report cut a line without saying so, which leaves a reader unable to tell how "+
			"much of it they are seeing:\n%s", msg)
	}
}

// TestCheckKeyNamesToleratesACRLFCheckout pins that a line ending is not a rename.
//
// A record checked out with CRLF endings carries a \r at the end of every interior line, and
// trimming the trailing run leaves all of them in place. Because the comparison quotes the
// manifest's key rather than the record's line, that \r lands outside the quotes: the real
// eth_replay record converted to CRLF reported three keys removed and the same three added,
// rendered character-for-character identically. Nothing about the recorded names changed, so
// nothing may be reported — and a real rename in a CRLF record still has to be.
func TestCheckKeyNamesToleratesACRLFCheckout(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)

	path := filepath.Join("testdata", probeSection+keyNameRecordSuffix)
	raw, err := os.ReadFile(path) //nolint:gosec // written by withKeyNameRecord into this test's own directory
	if err != nil {
		t.Fatalf("read back %s: %v", path, err)
	}
	crlf := strings.ReplaceAll(string(raw), "\n", "\r\n")
	if err := os.WriteFile(path, []byte(crlf), 0o600); err != nil {
		t.Fatalf("rewrite %s with CRLF endings: %v", path, err)
	}

	if reported := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, tableSpecs)
	}); len(reported.failures) != 0 {
		t.Errorf("a CRLF checkout of an unchanged record was reported as a rename:\n%s",
			strings.Join(reported.failures, "\n"))
	}

	msg := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, renamedKey(tableSpecs, 1, "probe.b-v2"))
	}).only(t)
	if !strings.Contains(msg, "renamed") {
		t.Errorf("normalizing the line endings must not also normalize away a rename:\n%s", msg)
	}
}

// TestCheckKeyNamesRecordsAKeyThatHasNoRow pins the half of a section a manifest cannot describe.
//
// Some keys have a fuzz target of their own because no row can predict them — one panics on a
// value its parser rejects, one adopts a cast result only when positive, one writes a second
// field. Those targets spell the key through the constant the reader uses, so editing that
// constant moves both halves and leaves them asserting about a key no node carries. A KeyName
// records the spelling without claiming a resolved value, and it is recorded after the rows so
// that adding one does not rebind the row index a section's seeds select by.
func TestCheckKeyNamesRecordsAKeyThatHasNoRow(t *testing.T) {
	const rowless KeyName = "probe.write-mode"
	withKeyNameRecord(t, probeSection, tableSpecs, rowless)

	if reported := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, tableSpecs, rowless)
	}); len(reported.failures) != 0 {
		t.Fatalf("a record holding a key with no row was rejected:\n%s",
			strings.Join(reported.failures, "\n"))
	}

	if recorded := keyNameRecord(tableSpecs, []KeyName{rowless}); !strings.HasSuffix(recorded,
		"\n"+strconv.Quote(string(rowless))) {
		t.Errorf("a key with no row must be recorded after the rows, or adding one rebinds every "+
			"row index the seeds select by:\n%s", recorded)
	}

	const renamed KeyName = "probe.write-mode-v2"
	msg := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, tableSpecs, renamed)
	}).only(t)
	for _, want := range []string{string(rowless), string(renamed), "renamed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("renaming a key that has no row is not reported as a rename (%q is missing):\n%s",
				want, msg)
		}
	}
}

// TestCheckKeyNamesCannotDemoteARowIntoTheRowlessHalf pins the edit the marker exists for.
//
// Deleting the last row and recording that same key as a KeyName leaves the manifest one row and
// one CheckRow assertion lighter while the set of recorded names is unchanged. Rendered as one flat
// list that produced a byte-identical record, and since each such deletion makes the next row the
// last one, a manifest could be peeled to nothing one green run at a time. So the record carries a
// line between its two halves, and a key crossing it is a diff.
func TestCheckKeyNamesCannotDemoteARowIntoTheRowlessHalf(t *testing.T) {
	withKeyNameRecord(t, probeSection, tableSpecs)

	demoted := KeyName(tableSpecs[2].Key)
	msg := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, tableSpecs[:2], demoted)
	}).only(t)

	if !strings.Contains(msg, "now recorded below the marker") {
		t.Errorf("a row demoted to a key recorded for its name alone is not reported as a crossing, "+
			"so the row and its assertion are gone with the record unchanged:\n%s", msg)
	}
	for _, want := range []string{string(demoted), "row deleted"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the report does not contain %q, so a reviewer cannot see which key lost its "+
				"row:\n%s", want, msg)
		}
	}

	// One row deeper, against the record the demotion above would leave behind. Each demotion makes
	// the next row the last one, so the first step being caught is not the same as the ratchet being
	// closed.
	withKeyNameRecord(t, probeSection, tableSpecs[:2], demoted)
	msg = capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, tableSpecs[:1], KeyName(tableSpecs[1].Key), demoted)
	}).only(t)
	if !strings.Contains(msg, "now recorded below the marker") {
		t.Errorf("the row that became the last one can be demoted in turn, so the record can be "+
			"peeled one green run at a time:\n%s", msg)
	}
}

// TestCheckKeyNamesReportsAPromotionAsWhatItIs pins the other direction across the marker.
//
// A key that gains a row gains an assertion on what it resolves to and a seed that has to
// discriminate it, which is the direction worth having. It still moves a line, so it still has to
// be recorded — and it must not be reported as the deletion its mirror image is.
func TestCheckKeyNamesReportsAPromotionAsWhatItIs(t *testing.T) {
	const rowless KeyName = "probe.write-mode"
	withKeyNameRecord(t, probeSection, tableSpecs, rowless)

	promoted := append(append([]KeySpec(nil), tableSpecs...),
		KeySpec{Key: string(rowless), Path: "D", Cast: CastInt})
	msg := capture(t, func(tb testing.TB) { CheckKeyNames(tb, probeSection, promoted) }).only(t)

	if !strings.Contains(msg, "now has a row") {
		t.Errorf("a key that gained a row is not reported as having gained one:\n%s", msg)
	}
	if strings.Contains(msg, "row deleted") {
		t.Errorf("gaining a row is reported as the deletion its mirror image is:\n%s", msg)
	}
}

// TestCheckKeyNamesLabelsARowIndexOnlyWhereThereIsOne pins the report's coordinates.
//
// A section's seeds select rows by index, so "row 2" is a claim that moving that line rebinds
// `seeds.AddRow(2, …)`. Only the lines above the marker have such an index; labelling the appended
// names the same way asserts a rebinding of seeds that do not exist.
func TestCheckKeyNamesLabelsARowIndexOnlyWhereThereIsOne(t *testing.T) {
	rowless := []KeyName{"probe.write-mode", "probe.write-mode-enable-auto"}
	withKeyNameRecord(t, probeSection, tableSpecs, rowless...)

	msg := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, tableSpecs, rowless[1], rowless[0])
	}).only(t)

	if strings.Contains(msg, "row 4") || strings.Contains(msg, "row 5") {
		t.Errorf("a reordered name below the marker is labelled with a row index, which claims a "+
			"seed binds to it:\n%s", msg)
	}
	for _, want := range []string{"line 5", "line 6"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the report does not name the line %q that moved, so it cannot be found in the "+
				"file:\n%s", want, msg)
		}
	}

	// A reordered row keeps both numbers, because the line is where to look and the row index is
	// what the seeds bind to.
	withKeyNameRecord(t, probeSection, tableSpecs)
	msg = capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, []KeySpec{tableSpecs[1], tableSpecs[0], tableSpecs[2]})
	}).only(t)
	if !strings.Contains(msg, "line 1 (row 0)") {
		t.Errorf("a moved row must carry the index its seeds bind to as well as its line:\n%s", msg)
	}
}

// TestCheckKeyNamesShowsABrokenMarkerRatherThanAcceptingIt pins that the separator cannot be
// hand-deleted to re-flatten the record.
//
// The marker is what makes a demotion visible, so a record edited to drop it has to fail, and the
// report has to show the line rather than describing it: an author who deleted it is the reader.
func TestCheckKeyNamesShowsABrokenMarkerRatherThanAcceptingIt(t *testing.T) {
	const rowless KeyName = "probe.write-mode"
	withKeyNameRecord(t, probeSection, tableSpecs, rowless)

	path := filepath.Join("testdata", probeSection+keyNameRecordSuffix)
	raw, err := os.ReadFile(path) //nolint:gosec // written by withKeyNameRecord into this test's own directory
	if err != nil {
		t.Fatalf("read back %s: %v", path, err)
	}
	flattened := strings.ReplaceAll(string(raw), keyNameSectionMarker+"\n", "")
	if err := os.WriteFile(path, []byte(flattened), 0o600); err != nil {
		t.Fatalf("rewrite %s without the marker: %v", path, err)
	}

	msg := capture(t, func(tb testing.TB) {
		CheckKeyNames(tb, probeSection, tableSpecs, rowless)
	}).only(t)
	if !strings.Contains(msg, keyNameSectionMarker) {
		t.Errorf("a record with the marker deleted is accepted or reported without showing the line "+
			"that is missing:\n%s", msg)
	}
}

// TestCheckKeyNamesRequiresARecordToExist pins that a section with no record fails rather
// than passing vacuously, and that the failure says how to create one.
func TestCheckKeyNamesRequiresARecordToExist(t *testing.T) {
	t.Chdir(t.TempDir())

	msg := capture(t, func(tb testing.TB) { CheckKeyNames(tb, probeSection, tableSpecs) }).only(t)
	if !strings.Contains(msg, "-update") {
		t.Errorf("the report must say how to create a missing record:\n%s", msg)
	}
}
