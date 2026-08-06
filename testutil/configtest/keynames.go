package configtest

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// This file pins the half of a manifest row that the row's own assertions cannot reach.
//
// A row claims the reader resolves spec.Key into spec.Path, and CheckRow holds it to that
// by driving the reader with an AppOpts holding spec.Key. Both sides of that comparison
// take the key from the row, so a row that names its key through the same symbol the
// reader uses — `{Key: FlagSSImportNumWorkers}` against `opts.Get(FlagSSImportNumWorkers)`
// — moves with the reader. Editing that constant's value is exactly how an
// operator-facing app.toml key gets renamed, and it leaves every row assertion, and the
// discriminating-seed check, passing: both halves moved together, so the comparison still
// carries information, just about a key no node has ever been configured with. Thirty-one
// rows are spelled through a constant. How many rows there are in total is left unstated:
// it changes whenever anyone adds a key, and a count in a comment goes stale silently
// where the records themselves cannot.
//
// So the resolved string is recorded a second time, in a file no Go symbol reaches. A
// rename then moves the manifest away from the record and fails with the old and the new
// spelling in a diff, whichever way the row was spelled. This is CheckDefaults' mechanism
// pointed at key names instead of default values, and for the same reason: a self-
// comparison passes for whatever the value happens to be, and only an independent
// recording makes the change visible to a reviewer.

// keyNameRecordSuffix names the per-section record. It is a separate file from
// <section>.golden rather than an addition to it because a changed key name and a changed
// default are different review conversations. A default moving changes what nodes that do
// not configure the key resolve; a key name moving strands every app.toml already on disk
// and has to be shipped with the template that renders it. A reviewer seeing this
// filename in a diff knows which of the two they are looking at before reading a line of
// it.
//
// Two files, one -update flag. The flag is process-global, so an unfiltered
// `go test ./<pkg>/ -update` rewrites both records in the same run, which is how a pending
// rename gets absorbed into a defaults commit. Regenerating one without touching the other is
// what the -run filter in each check's instructions is for.
const keyNameRecordSuffix = ".keys.golden"

// KeyName is a key recorded for its spelling alone: one a section resolves through a target of
// its own rather than through a manifest row.
//
// A row carries a prediction — Path, Cast, Unguarded, Checked — and CheckRow holds the reader to
// it. Some keys cannot be described that way and have a fuzz target each instead: one that
// panics on a value its parser rejects, one that adopts a cast result only when it is positive,
// one that writes a second field. Those targets assert the behavior, and what they did not
// assert was the key's spelling, so editing the constant a target and a reader share moved both
// and left the suite green about a key no node has ever carried.
//
// It is its own type rather than a KeySpec with empty columns so that the compiler keeps the
// distinction. A []KeyName cannot be passed to CheckRow, to Pick or to
// CheckEveryRowHasADiscriminatingSeed, so a name recorded here cannot be mistaken for a row that
// predicts a resolved value. What it claims is the operator-facing spelling and nothing else;
// the assertion on the resolution stays in the target that can express it.
type KeyName string

// CheckKeyNames asserts that the keys a section's manifest names still resolve to the
// strings recorded in testdata/<section>.keys.golden.
//
// The check is on the resolved string, so it is indifferent to how the row spells it. A
// row written as a literal and a row written as a reference to the reader's own flag
// constant are held to the same recorded name, which is the point: the protection must not
// depend on which spelling an author happened to choose, and a literal row converted to a
// constant later must not quietly lose it.
//
// It is deliberately blind to Path, Cast, Unguarded and Checked. Those columns describe the
// reader, editing them to match a reader you changed is one of the four ways of silencing a
// failure the harness guide forbids, and a check that read them could be silenced the same
// way. Deleting a row — another of the four — fails here whichever shape it takes: on its own it
// removes a line from the record, and paired with a KeyName recording the same key it moves a line
// across keyNameSectionMarker.
//
// Regenerate with `go test ./<pkg>/ -run TestKeyNames -update` once the rename is
// deliberate, and keep the diff in the review rather than running -update to clear it.
//
// It reports through Errorf where CheckDefaults reports through Fatalf, because a package
// can hold several sections — app holds four — and each calls this once. A fatal verdict
// on the first would hide a rename in the second until the first was dealt with, and a
// rename that appears only after another is fixed is a rename someone lands twice.
//
// alsoRecorded carries the section's keys that have no row, recorded after the rows and below
// keyNameSectionMarker. It is the same list CheckEveryRowHasADiscriminatingSeed is given, and it
// has to be, since that check compares this record too: the two agreeing with the file is what
// keeps a name from being recorded in one place and forgotten in the other.
//
// specs may be empty where the reader in front of this call admits no row, which is how
// [state-sync] is recorded in cmd/seid/cmd: NewApp builds a baseapp rather than resolving an
// AppOpts, so no row can predict what it does with a key. A second reader of the same three keys
// does resolve them into a struct and does have rows, in sei-cosmos/server/config, so what decides
// this is the reader and not the section. A call with no specs gets no tie from the seeds side,
// since that check needs a manifest, and is then the only thing holding its record.
func CheckKeyNames(t testing.TB, name string, specs []KeySpec, alsoRecorded ...KeyName) {
	t.Helper()

	// An empty record would be written, compared against itself and pass, reporting a healthy
	// harness for a section that has lost its rows. Panics as Pick does, because an empty table is
	// a defect in the test that called this rather than in the behavior under test.
	if len(specs)+len(alsoRecorded) == 0 {
		panic("configtest.CheckKeyNames: no keys to record, the section's manifest has no rows")
	}

	path := goldenFilePath(t, name, keyNameRecordSuffix)
	got := keyNameRecord(specs, alsoRecorded)

	if goldenUpdateRequested() {
		writeGolden(t, name, path, got)
		return
	}

	raw, err := os.ReadFile(path) //nolint:gosec // testdata/<section>.keys.golden; the whole file name is validated by goldenFilePath
	if err != nil {
		t.Errorf("%s: cannot read %s (%v).\nEvery manifest records the key names it resolves, so "+
			"that renaming a key through the constant the reader shares with the manifest cannot "+
			"pass unnoticed. If this section is new, create the record with "+
			"`go test ./<pkg>/ -run TestKeyNames -update` and review the recorded names as part of "+
			"the change.", name, path, err)
		return
	}
	// Compared as text rather than as bytes, so an editor stripping the trailing newline or a
	// CRLF checkout does not read as a renamed key. recordText says why trimming the trailing
	// run is not enough on its own, and this record is the file that defect was found on.
	want := recordText(raw)
	got = strings.TrimRight(got, "\n")
	if got == want {
		return
	}
	t.Errorf("%s: the manifest's key names no longer match %s.\n%s\n"+
		"A key name is the operator-facing contract. Every app.toml already on disk addresses the "+
		"recorded spelling, so a renamed key stops resolving on every existing node and silently "+
		"falls back to its in-code default — and the app.toml template that renders the old "+
		"spelling, the flag registration, and the documentation are all separate places that this "+
		"check does not reach and that have to move in the same change. If the rename is "+
		"deliberate, regenerate with `go test ./<pkg>/ -run TestKeyNames -update` and keep the "+
		"diff in the review so the old and new name are what gets discussed.",
		name, path, keyNameDiff(splitRecord(want), splitRecord(got), len(specs)))
}

// keyNameSectionMarker separates a record's rows from the keys recorded for their name alone.
//
// The two halves carry different guarantees — a row is held to the value it resolves on every value
// the fuzzer reaches and has to carry a seed that discriminates it, a KeyName states one string —
// and rendered as one flat list the record cannot tell them apart. Deleting the last row and
// recording that same key as a KeyName then produces a byte-identical file: the manifest loses a
// row and its CheckRow assertion, nothing in the tree moves, and because each such deletion makes
// the next row the last one, a manifest can be emptied one green run at a time. Interior rows were
// always caught, because dropping one reorders the rest; only the tail was silent, and the tail
// regenerates. With a line between the halves the demoted key crosses it and the comparison reports
// the crossing.
//
// It is written on every record, including the sections whose keys all have rows, so the file has
// one shape rather than two and the first demotion in such a section is reported the same way as
// the next one. It cannot be mistaken for a recorded key: every key line is strconv.Quote's output
// and so opens with a quote.
const keyNameSectionMarker = "# keys with a target of their own"

// keyNameRecord renders a section's recorded key names, one quoted key per line: the manifest's
// rows in manifest order, then keyNameSectionMarker, then the keys recorded for their name alone.
//
// Quoted because Dump quotes strings for the same reason: a key with a trailing space or an
// invisible character is a real typo, and an unquoted record would hide the one thing the
// file exists to show. Manifest order rather than sorted, so a row inserted in the middle
// shows as one added line at the position it was inserted, and a reordering — which
// rebinds every `seeds.AddRow(uint(i), …)` index to a different row — shows as a change
// rather than as nothing. The rowless names go after the rows for that same reason: appending
// leaves every row at the index its seeds already bind to, where interleaving them into
// app.toml order would rebind all of them. The marker sits after the last row for the same reason
// again, so adding it rebinds nothing.
func keyNameRecord(specs []KeySpec, alsoRecorded []KeyName) string {
	lines := make([]string, 0, len(specs)+len(alsoRecorded)+1)
	for _, s := range specs {
		lines = append(lines, strconv.Quote(s.Key))
	}
	lines = append(lines, keyNameSectionMarker)
	for _, k := range alsoRecorded {
		lines = append(lines, strconv.Quote(string(k)))
	}
	return strings.Join(lines, "\n")
}

// markerAt reports where keyNameSectionMarker sits in a record, or -1 in a record without one.
//
// A record generated before the marker existed, or one hand-edited to delete it, has none.
// Reporting -1 keeps such a record comparing unequal to every record that has one, so it is
// regenerated rather than accepted with its two halves still indistinguishable.
func markerAt(lines []string) int {
	for i, l := range lines {
		if l == keyNameSectionMarker {
			return i
		}
	}
	return -1
}

// splitRecord turns a record's text back into its lines, dropping the blank one a
// trailing newline leaves behind.
func splitRecord(record string) []string {
	if record == "" {
		return nil
	}
	return strings.Split(record, "\n")
}

// keyNameDiff reports how a manifest's key names differ from the record, in the terms the
// change was made in. rows is the manifest's row count, which is how many of the record's lines
// sit above the marker and therefore have a row index a seed can bind to.
//
// The comparison is a multiset difference rather than a line-by-line one so that the
// common edits each produce the diff that names them: one key gone and one arrived is a
// rename and is reported as one, an arrival on its own is a key added to the section, a
// departure on its own is a row deleted, and the same names in a different order is one of the
// three edits sameLinesHeadline tells apart.
func keyNameDiff(want, got []string, rows int) string {
	var b strings.Builder

	removed := missingFrom(want, got)
	added := missingFrom(got, want)

	switch {
	case len(removed) == 1 && len(added) == 1:
		b.WriteString("  renamed: " + shown(removed[0]) + "\n       to: " + shown(added[0]) + "\n")
	case len(removed) == 0 && len(added) == 0:
		moved := differingPositions(want, got)
		b.WriteString(sameLinesHeadline(want, got, rows, moved))
		for _, i := range moved {
			b.WriteString("  " + position(i, rows) + ": was " + shown(at(want, i)) +
				", now " + shown(at(got, i)) + "\n")
		}
	default:
		for _, k := range removed {
			b.WriteString("  removed: " + shown(k) + "\n")
		}
		for _, k := range added {
			b.WriteString("    added: " + shown(k) + "\n")
		}
	}
	return b.String()
}

// sameLinesHeadline names what happened when the record and the manifest hold the same lines in a
// different order, which is three edits with three different consequences.
//
// Where the marker moved, a key crossed between the halves, and the direction is the whole of the
// diagnosis: one way a key lost its row, the other way it gained one. Where the marker did not
// move, the order changed within one half, and only the half above it has row indices for a seed
// to bind to.
func sameLinesHeadline(want, got []string, rows int, moved []int) string {
	switch wantMarker, gotMarker := markerAt(want), markerAt(got); {
	case gotMarker < wantMarker:
		lost := fmt.Sprintf("%d keys that had a row are now recorded below the marker, so the "+
			"manifest holds %d rows fewer", wantMarker-gotMarker, wantMarker-gotMarker)
		if wantMarker-gotMarker == 1 {
			lost = "a key that had a row is now recorded below the marker, so the manifest holds one " +
				"row fewer"
		}
		return "  " + lost + " while the record holds the same names. That is a row deleted, which is " +
			"one of the four moves the harness guide forbids: the key keeps its recorded spelling and " +
			"loses the assertion on what it resolves to, on every value the fuzzer reaches, along " +
			"with the seed that had to discriminate it. Give the row back, or — if no row can predict " +
			"the key's resolution — write the fuzz target that asserts it and name that target in the " +
			"comment beside the KeyName:\n"
	case gotMarker > wantMarker:
		subject := fmt.Sprintf("%d keys recorded below the marker now have", gotMarker-wantMarker)
		if gotMarker-wantMarker == 1 {
			subject = "a key recorded below the marker now has"
		}
		return "  " + subject + " a row. That is the direction worth having, since the key gains an " +
			"assertion on what it resolves to and a seed that has to discriminate it. Record it, and " +
			"retire the target that was asserting the key on its own or say why it stays:\n"
	}
	if len(moved) > 0 && moved[0] < rows {
		return "  the same key names in a different order. A section's seeds select rows by " +
			"index, so moving a row rebinds every seeds.AddRow that names a position at or after it:\n"
	}
	return "  the same key names in a different order, below the marker. No seed binds to those " +
		"positions, so nothing was rebound and recording the new order is the whole repair:\n"
}

// differingPositions returns the positions at which two records disagree.
//
// It compares the recorded lines rather than their rendered forms, because shown cuts a line past
// maxShownKeyBytes: two long lines sharing a prefix would render identically and the move being
// reported would print as one line against itself.
func differingPositions(want, got []string) []int {
	var out []int
	for i := range max(len(want), len(got)) {
		if at(want, i) != at(got, i) {
			out = append(out, i)
		}
	}
	return out
}

// position labels a record line for the report: its line number, and its row index as well where
// the line is one of the manifest's rows.
//
// Both numbers are needed and they are not the same number. The line number is where to look in the
// file. The row index is what seeds.AddRow binds to, and only the lines above the marker have one,
// so labelling every line "row N" over the combined list said of a rowless name that moving it
// rebinds a seed, where nothing binds to those positions at all.
func position(i, rows int) string {
	if i < rows {
		return "line " + strconv.Itoa(i+1) + " (row " + strconv.Itoa(i) + ")"
	}
	return "line " + strconv.Itoa(i+1)
}

// shown renders a recorded line for the report, quoted, so that nothing on the line can act on
// the terminal reading it.
//
// A line this package wrote is already one key in canonical quoted form, and the round trip is
// what establishes that: unquote it, requote it, and accept the result only if it reproduces the
// line. Such a line prints exactly as the file holds it, which is what makes a rename diff read
// like the record. Anything else — a line hand-edited to clear a failure, a stray \r from a CRLF
// checkout, a byte sequence that is not UTF-8 — is quoted as the raw text it is, so the escapes
// become visible rather than executed. Without that, a record carrying ANSI escapes would choose
// what a failing check looks like, up to clearing the screen and printing a green success line.
//
// A long line is cut, because a key past this length is not a key an operator renamed. An empty
// line is named rather than printed as nothing after the label, where a line holding a quoted
// empty string is a real key that is empty and prints as one.
func shown(line string) string {
	if line == "" {
		return "(a blank line)"
	}
	text := line
	if key, err := strconv.Unquote(line); err == nil && strconv.Quote(key) == line {
		text = key
	}
	if len(text) > maxShownKeyBytes {
		return strconv.Quote(text[:maxShownKeyBytes]) + "… (" + strconv.Itoa(len(text)) +
			" bytes on this line)"
	}
	return strconv.Quote(text)
}

// maxShownKeyBytes bounds how much of one recorded line reaches a report. An app.toml key is a
// section and a name, so anything past this is not a key that got renamed.
const maxShownKeyBytes = 256

// missingFrom returns the entries of a that b does not account for, respecting
// multiplicity so a duplicated key is not swallowed by its twin.
func missingFrom(a, b []string) []string {
	remaining := make(map[string]int, len(b))
	for _, k := range b {
		remaining[k]++
	}
	var out []string
	for _, k := range a {
		if remaining[k] > 0 {
			remaining[k]--
			continue
		}
		out = append(out, k)
	}
	return out
}

// at returns the recorded line at position i, or the empty string past the end.
//
// The branch that calls it has already established that the two sides hold the same lines, so their
// lengths are equal and the absent case does not arise. The bound is structural rather than
// reachable: it is here so this helper does not depend on a caller's invariant that a later edit
// could drop, and an absent line then compares and renders as a blank one, which shown names.
func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}
