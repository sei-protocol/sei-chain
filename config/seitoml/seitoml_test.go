package seitoml_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/config/seitoml"
)

// commented is a file written the way an operator writes one: a heading comment, a reason beside a
// value, and a blank line for legibility.
const commented = `schema_version = 1
node_mode = "validator"

# The giga executor. Turned on after the load test in March.
[giga_executor]
enabled = true
# Off deliberately: this node serves historical queries and OCC cost us more than it saved.
occ_enabled = false
`

func parse(t *testing.T, body string) *seitoml.File {
	t.Helper()
	f, err := seitoml.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

// render returns the file's current text.
func render(t *testing.T, f *seitoml.File) string {
	t.Helper()
	raw, err := f.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return string(raw)
}

// TestEditingPreservesAnOperatorsComments is the property that decides how this package is built.
//
// An operator's comments are how they explain a choice to whoever reads the file next. Rewriting
// the file from a decoded map would drop all of them, and the operator would have no way to get
// that reasoning back. Held by editing a value that has a comment explaining it.
func TestEditingPreservesAnOperatorsComments(t *testing.T) {
	f := parse(t, commented)

	if err := f.Set("giga_executor.occ_enabled", true); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := render(t, f)
	for _, comment := range []string{
		"# The giga executor. Turned on after the load test in March.",
		"# Off deliberately: this node serves historical queries and OCC cost us more than it saved.",
	} {
		if !strings.Contains(got, comment) {
			t.Errorf("editing one value dropped a comment:\n  %s\n\nThe file now reads:\n%s\n\n"+
				"An operator cannot recover the reasoning they recorded, and nothing warned them",
				comment, got)
		}
	}
	if !strings.Contains(got, "occ_enabled = true") {
		t.Errorf("the value was not written. The file reads:\n%s", got)
	}
	if strings.Contains(got, "occ_enabled = false") {
		t.Errorf("the old value is still present, so the key is written twice:\n%s", got)
	}
}

// TestEditingChangesOnlyTheValueItWasAsked holds the rest of that property.
//
// Preserving comments is not enough if the content moves. A save that rewrote other values, or
// reordered them, would make every change unreviewable, because the diff would show the whole file
// rather than the one value that moved.
//
// Compared on the lines that carry content. Blank lines are excluded because the formatter
// normalizes vertical spacing, which the test below pins as a one-time change.
func TestEditingChangesOnlyTheValueItWasAsked(t *testing.T) {
	f := parse(t, commented)

	if err := f.Set("giga_executor.occ_enabled", true); err != nil {
		t.Fatalf("Set: %v", err)
	}

	before, after := contentLines(commented), contentLines(render(t, f))
	if len(before) != len(after) {
		t.Fatalf("the file went from %d lines of content to %d:\n%s",
			len(before), len(after), strings.Join(after, "\n"))
	}
	var moved []string
	for i := range before {
		if before[i] != after[i] {
			moved = append(moved, "  -"+before[i]+"\n  +"+after[i])
		}
	}
	if len(moved) != 1 {
		t.Errorf("setting one value changed %d lines of content, want 1:\n%s",
			len(moved), strings.Join(moved, "\n"))
	}
}

// contentLines returns the lines that carry content, in order.
func contentLines(body string) []string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestFormattingNormalizesOnceAndThenHoldsSteady is what makes the normalization safe to accept.
//
// Rendering a hand-written file adjusts its vertical spacing, so the first save of a file nobody
// has saved before shows a blank line the operator did not add. That is tolerable only if it does
// not repeat: a file that gained a line on every save would grow without bound, and every diff
// after the first would carry noise nobody chose.
func TestFormattingNormalizesOnceAndThenHoldsSteady(t *testing.T) {
	first := render(t, parse(t, commented))
	second := render(t, parse(t, first))

	if second != first {
		t.Errorf("rendering a rendered file changed it again, so each save moves the file:\n"+
			"first:\n%s\nsecond:\n%s", first, second)
	}
	// The normalization is spacing only, so nothing that carries content may differ.
	if a, b := contentLines(commented), contentLines(first); strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Errorf("rendering changed the file's content, not just its spacing:\n%s", first)
	}
}

// TestAnAbsentSchemaVersionIsAnError holds that the file's shape is never guessed.
//
// A migration chain reads the version to decide which steps to run. Defaulting an absent one to
// zero would run every step in history against a file nobody established the shape of, and the
// result would look like a successful upgrade.
func TestAnAbsentSchemaVersionIsAnError(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"absent", "[giga_executor]\nenabled = true\n"},
		{"not an integer", "schema_version = \"1\"\n"},
		{"a float", "schema_version = 1.0\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parse(t, tc.body).Version(); err == nil {
				t.Errorf("a %s schema version was accepted. A migration would then run against a file "+
					"whose shape nobody established, and report success", tc.name)
			}
		})
	}
	if v, err := parse(t, commented).Version(); err != nil || v != 1 {
		t.Errorf("a well-formed version read (%d, %v), want (1, nil)", v, err)
	}
}

// TestTheSchemaVersionIsNotAConfigurationKey keeps file metadata out of the key space.
//
// It describes the file rather than configuring the node, so a check comparing written keys against
// the declared set would report it as a key no section owns, on every node, forever.
func TestTheSchemaVersionIsNotAConfigurationKey(t *testing.T) {
	values, err := parse(t, commented).Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}

	if _, present := values[seitoml.VersionKey]; present {
		t.Errorf("%s appears in the written key space: %v. Every node would then be told it has a "+
			"key no section declares", seitoml.VersionKey, values)
	}
	if len(values) != 2 {
		t.Errorf("read %d keys, want the section's 2: %v", len(values), values)
	}
	if values["giga_executor.occ_enabled"] != false {
		t.Errorf("occ_enabled read %#v, want false", values["giga_executor.occ_enabled"])
	}
}

// TestSetRoundTripsEveryTypeItAccepts is what keeps the writer and the reader from disagreeing.
//
// Writing renders a Go value as TOML text and reading parses it back, and the two are separate
// enumerations. Without this, a type could be written in a form that reads back as something else,
// and the file would look correct while the node ran a different value.
func TestSetRoundTripsEveryTypeItAccepts(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  any
		want any
	}{
		{"bool", true, true},
		{"bool false", false, false},
		{"int", 16, int64(16)},
		{"negative int", -3, int64(-3)},
		{"int64", int64(1 << 40), int64(1 << 40)},
		{"uint", uint(7), int64(7)},
		{"int32", int32(1 << 20), int64(1 << 20)},
		{"uint32", uint32(4294967295), int64(4294967295)},
		{"uint64", uint64(1 << 40), int64(1 << 40)},
		{"float", 1.5, 1.5},
		// An integral float has to keep its type. TOML tells a float from an integer by the fractional
		// part, and the shortest form of 1.0 is "1", which reads back as an integer.
		{"integral float", float64(1), float64(1)},
		{"negative integral float", float64(-2), float64(-2)},
		{"float needing an exponent", 1e21, 1e21},
		// The shape reading an array back produces, which anything that reads a list and writes it again
		// hands straight back to Set.
		{"list read back as any", []any{"a", int64(2), true}, []any{"a", int64(2), true}},
		{"string", "hello", "hello"},
		{"string with a quote", `say "hi"`, `say "hi"`},
		{"string with a backslash", `C:\sei\data`, `C:\sei\data`},
		{"empty string", "", ""},
		{"duration", 90 * time.Second, "1m30s"},
		{"string list", []string{"a", "b"}, []any{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := seitoml.New("validator", "v6.7.0")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := f.Set("probe.value", tc.set); err != nil {
				t.Fatalf("Set(%#v): %v", tc.set, err)
			}

			// Re-parsed rather than read back from the same document, so this measures what a
			// later process reads off disk rather than what is still in memory.
			reread := parse(t, render(t, f))
			got, ok, err := reread.Get("probe.value")
			if err != nil || !ok {
				t.Fatalf("Get after a round trip: (%#v, %v, %v)\nfile:\n%s", got, ok, err, render(t, f))
			}

			if !equal(got, tc.want) {
				t.Errorf("wrote %#v and read back %#v, want %#v.\nfile:\n%s\n\nA value that does not "+
					"survive a round trip means the file looks correct while the node runs something "+
					"else", tc.set, got, tc.want, render(t, f))
			}
		})
	}
}

// equal compares two read values, including lists.
func equal(a, b any) bool {
	as, aok := a.([]any)
	bs, bok := b.([]any)
	if aok || bok {
		if !aok || !bok || len(as) != len(bs) {
			return false
		}
		for i := range as {
			if as[i] != bs[i] {
				return false
			}
		}
		return true
	}
	return a == b
}

// TestALiteralStringIsTakenAsWritten holds the difference between TOML's two string forms.
//
// A basic string carries escapes and a literal string does not, which is why TOML has both.
// Decoding a literal string as though it had escapes turns a Windows path's separators into
// control characters, and the value the node runs is not the one in the file.
func TestALiteralStringIsTakenAsWritten(t *testing.T) {
	f := parse(t, "schema_version = 1\n[probe]\nliteral = 'C:\\sei\\data'\nbasic = \"a\\tb\"\n")

	values, err := f.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}

	if got := values["probe.literal"]; got != `C:\sei\data` {
		t.Errorf("a literal string read %#v, want the text as written. Escapes were processed in the "+
			"form that does not have them", got)
	}
	if got := values["probe.basic"]; got != "a\tb" {
		t.Errorf("a basic string read %#v, want its escape decoded to a tab", got)
	}
}

// TestUnsetRemovesTheKeyRatherThanWritingItsBaseline holds what unset means.
//
// An absent key resolves to the running binary's baseline. Writing the baseline value instead
// looks identical in the file but is a commitment that survives a release changing that baseline,
// which is the opposite of what the operator asked for.
func TestUnsetRemovesTheKeyRatherThanWritingItsBaseline(t *testing.T) {
	f := parse(t, commented)

	removed, err := f.Unset("giga_executor.occ_enabled")
	if err != nil {
		t.Fatalf("Unset: %v", err)
	}
	if !removed {
		t.Fatal("Unset reported no change for a key the file carries")
	}

	values, err := f.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if _, present := values["giga_executor.occ_enabled"]; present {
		t.Errorf("the key is still written after unset: %v. It would keep overriding the baseline the "+
			"operator asked to fall back to", values)
	}
	if strings.Contains(render(t, f), "occ_enabled") {
		t.Errorf("the key is still in the file text:\n%s", render(t, f))
	}
	// The other key is untouched, or this would pass for an unset that emptied the section.
	if values["giga_executor.enabled"] != true {
		t.Errorf("unsetting one key disturbed another: %v", values)
	}

	again, err := f.Unset("giga_executor.occ_enabled")
	if err != nil {
		t.Fatalf("Unset on an absent key: %v", err)
	}
	if again {
		t.Error("Unset reported a change for a key that was already gone, so a caller cannot tell " +
			"whether it had anything to remove")
	}
}

// TestSetCreatesTheTableWhenTheSectionIsNew holds the first-key case.
//
// Without it, writing the first key of a section would need the operator to add the heading by
// hand, and set would fail on exactly the file a new node starts from.
func TestSetCreatesTheTableWhenTheSectionIsNew(t *testing.T) {
	f, err := seitoml.New("validator", "v6.7.0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := f.Set("state-store.ss-keep-recent", 100000); err != nil {
		t.Fatalf("Set into a section that does not exist: %v", err)
	}

	got := render(t, f)
	if !strings.Contains(got, "[state-store]") {
		t.Errorf("no table heading was written:\n%s", got)
	}
	values, err := parse(t, got).Values()
	if err != nil {
		t.Fatalf("Values after a round trip: %v", err)
	}
	if values["state-store.ss-keep-recent"] != int64(100000) {
		t.Errorf("the key read %#v after a round trip, want 100000. file:\n%s",
			values["state-store.ss-keep-recent"], got)
	}
}

// TestValuesFlattensAnInlineTable keeps an inline table's leaves visible.
//
// An inline table is one written line holding several keys. Left nested, its leaves are invisible
// to any check that walks declared keys, so an operator could write a setting nothing validates.
func TestValuesFlattensAnInlineTable(t *testing.T) {
	f := parse(t, "schema_version = 1\n[state-commit]\nflatkv = { enable = true, dir = \"/data\" }\n")

	values, err := f.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}

	if values["state-commit.flatkv.enable"] != true {
		t.Errorf("the inline table's leaf is not reachable as a dotted key: %v.\n\nA key nothing can "+
			"see is a setting nothing validates", values)
	}
	if values["state-commit.flatkv.dir"] != "/data" {
		t.Errorf("the second leaf read %#v, want /data", values["state-commit.flatkv.dir"])
	}
	if _, nested := values["state-commit.flatkv"]; nested {
		t.Errorf("the table itself is also reported as a value, so a check would see a key whose "+
			"value is a map: %v", values)
	}
}

// TestAnUnsupportedTypeIsRefused keeps a wrong guess out of an operator's file.
//
// A formatter that rendered anything would put a plausible-looking line in the file that reads
// back as something else. Refusing is what makes the round-trip guarantee above hold for every
// type this accepts.
func TestAnUnsupportedTypeIsRefused(t *testing.T) {
	f, err := seitoml.New("validator", "v6.7.0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := f.Set("probe.value", map[string]string{"a": "b"}); err == nil {
		t.Error("a map was written to the file. Whatever line that produced, nothing guarantees it " +
			"reads back as the value the caller meant")
	}
	if err := f.Set("", true); err == nil {
		t.Error("an empty key was accepted")
	}
	if err := f.Set("probe..value", true); err == nil {
		t.Error("a key with an empty segment was accepted")
	}
}

// TestSaveLandsInFullOrNotAtAll holds the atomicity the boot depends on.
//
// A configuration file truncated by a crash mid-write is one the node cannot parse, so a save that
// cannot complete must leave the previous file exactly as it was. Driven by making the install step
// fail, which is the part that would otherwise have already replaced the file.
func TestSaveLandsInFullOrNotAtAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sei.toml")
	if err := os.WriteFile(path, []byte(commented), 0o600); err != nil {
		t.Fatalf("seed the file: %v", err)
	}

	f := parse(t, commented)
	if err := f.Set("giga_executor.occ_enabled", true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// A directory the process cannot write is how the temporary file, and therefore the install,
	// is made to fail without the previous file being touched.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := f.Save(path); err == nil {
		t.Fatal("Save reported success in a directory it cannot write, so a caller would believe " +
			"the new configuration is on disk")
	}

	raw, err := os.ReadFile(path) //nolint:gosec // a path this test created under t.TempDir
	if err != nil {
		t.Fatalf("the previous file is unreadable after a failed save: %v", err)
	}
	if string(raw) != commented {
		t.Errorf("a failed save changed the file on disk. It now reads:\n%s\n\nThe node would boot "+
			"from something nobody wrote", raw)
	}
}

// TestSaveLeavesNoTemporaryFileBehind keeps the directory clean on both paths.
//
// The temporary file has to sit beside the destination so the rename stays on one filesystem.
// Left behind, a partial configuration accumulates next to the real one, and a reader globbing the
// directory can find it.
func TestSaveLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sei.toml")
	f := parse(t, commented)

	if err := f.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "sei.toml" {
			t.Errorf("a save left %q beside the configuration. A partial file next to the real one is "+
				"something a reader can find", e.Name())
		}
	}
}

// TestSaveKeepsAnExistingFilesPermissions holds that a save never widens access.
//
// A configuration may name a private endpoint or carry a token. An operator who narrowed the file
// deliberately would have that undone by a save, silently, and nothing about the change is visible
// in the file's contents.
func TestSaveKeepsAnExistingFilesPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sei.toml")
	if err := os.WriteFile(path, []byte(commented), 0o640); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	f := parse(t, commented)
	if err := f.Set("giga_executor.enabled", false); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := f.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the file's mode moved from 0600 to %#o. A save that widens access undoes a "+
			"restriction an operator chose, and the file's contents do not show it", got)
	}
}

// TestANewFileIsNotWorldReadable holds the mode a file created here gets.
//
// The usual 0644 would be wrong for a file that may name a private endpoint, and a new file has no
// previous mode to inherit, so the choice has to be made here.
func TestANewFileIsNotWorldReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sei.toml")
	f, err := seitoml.New("validator", "v6.7.0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := f.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("a new configuration file was created with mode %#o, readable beyond its owner", got)
	}
}

// TestNewCarriesThisBinarysSchemaVersion holds that a generated file is never version-less.
//
// A file written without one cannot be migrated later, and the failure appears at the first
// upgrade rather than at the write that caused it.
func TestNewCarriesThisBinarysSchemaVersion(t *testing.T) {
	f, err := seitoml.New("validator", "v6.7.0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	v, err := parse(t, render(t, f)).Version()
	if err != nil {
		t.Fatalf("a new file has no readable schema version: %v\nfile:\n%s", err, render(t, f))
	}
	if v != seitoml.SchemaVersion {
		t.Errorf("a new file records version %d, want %d", v, seitoml.SchemaVersion)
	}
}

// TestANewFileRecordsItsNodeMode holds the field every value in the file depends on.
//
// The mode selects which defaults the values were chosen against, so a file that omits it cannot be
// compared against a binary or checked against the mode the node runs.
func TestANewFileRecordsItsNodeMode(t *testing.T) {
	f, err := seitoml.New("archive", "v6.7.0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	mode, err := parse(t, render(t, f)).Mode()
	if err != nil {
		t.Fatalf("a new file has no readable node mode: %v\nfile:\n%s", err, render(t, f))
	}
	if mode != "archive" {
		t.Errorf("the file records mode %q, want archive", mode)
	}
	if _, err := seitoml.New("", "v6.7.0"); err == nil {
		t.Error("a file was created with no mode. Every value written into it resolves for one, so " +
			"nothing could later tell an archive node's file from a validator's")
	}
}

// TestAnAbsentOrUnreadableNodeModeIsAnError keeps a comparison from guessing.
//
// Guessing picks one binary's idea of a default and silently measures an archive node's file against
// a validator's baselines, which is the mistake this key exists to make impossible.
func TestAnAbsentOrUnreadableNodeModeIsAnError(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"absent", "schema_version = 1\n"},
		{"not text", "schema_version = 1\nnode_mode = 3\n"},
		{"empty", "schema_version = 1\nnode_mode = \"\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parse(t, tc.body).Mode(); err == nil {
				t.Errorf("a %s node mode was accepted, so a reader would compare the file against "+
					"whichever defaults it happened to pick", tc.name)
			}
		})
	}
	if mode, err := parse(t, commented).Mode(); err != nil || mode != "validator" {
		t.Errorf("a well-formed mode read (%q, %v), want validator", mode, err)
	}
}

// TestTheNodeModeIsNotAConfigurationKey keeps file metadata out of the key space.
//
// It describes the file rather than configuring the node, so a check comparing written keys against
// the declared set would otherwise report it as a key no section owns, on every node, forever.
func TestTheNodeModeIsNotAConfigurationKey(t *testing.T) {
	values, err := parse(t, commented).Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}

	if _, present := values[seitoml.ModeKey]; present {
		t.Errorf("%s appears in the written key space: %v", seitoml.ModeKey, values)
	}
	if len(values) != 2 {
		t.Errorf("read %d keys, want the section's 2: %v", len(values), values)
	}
}

// TestAMigrationCarriesTheNodeModeForward holds the field across an upgrade.
//
// A migration that dropped it would leave a file nothing can compare, and the failure would appear
// at the next diff rather than at the upgrade that caused it.
func TestAMigrationCarriesTheNodeModeForward(t *testing.T) {
	f := parse(t, commented)

	if err := f.Set("giga_executor.enabled", false); err != nil {
		t.Fatalf("Set: %v", err)
	}

	mode, err := parse(t, render(t, f)).Mode()
	if err != nil || mode != "validator" {
		t.Errorf("editing the file lost its node mode: (%q, %v)", mode, err)
	}
}

// TestTheReleaseThatWroteTheFileIsRecorded holds the provenance the file carries.
//
// Nobody could otherwise tell which binary produced a file, which is the first thing worth knowing
// when its values look wrong and the first thing that says whether regenerating would change
// anything.
func TestTheReleaseThatWroteTheFileIsRecorded(t *testing.T) {
	f, err := seitoml.New("validator", "v6.7.0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	release, recorded := parse(t, render(t, f)).GeneratedBy()
	if !recorded || release != "v6.7.0" {
		t.Errorf("the file records (%q, %v), want v6.7.0\nfile:\n%s", release, recorded, render(t, f))
	}
}

// TestABuildWithNoReleaseOmitsTheKey holds the case every developer hits.
//
// The release comes from a linker flag the release build sets, so a binary built any other way knows
// none. Writing an empty string would put a key in an operator's file that says less than no key, and
// refusing would leave a development build unable to produce a file at all.
func TestABuildWithNoReleaseOmitsTheKey(t *testing.T) {
	f, err := seitoml.New("validator", "")
	if err != nil {
		t.Fatalf("New refused a build that carries no release: %v", err)
	}

	body := render(t, f)
	if strings.Contains(body, seitoml.GeneratedByKey) {
		t.Errorf("the file carries %s with nothing behind it:\n%s", seitoml.GeneratedByKey, body)
	}
	if _, recorded := parse(t, body).GeneratedBy(); recorded {
		t.Error("GeneratedBy reports a release for a file that records none")
	}
	// A file already on disk carrying an empty value reads the same way, since an empty release says
	// nothing and a caller told one is present would print it as though it meant something.
	onDisk := parse(t, "schema_version = 1\nnode_mode = \"validator\"\ngenerated_by = \"\"\n")
	if release, recorded := onDisk.GeneratedBy(); recorded {
		t.Errorf("a file recording an empty release reports (%q, %v), want it treated as absent",
			release, recorded)
	}
	// And the file is otherwise complete, or omitting the key would have cost something.
	if v, err := parse(t, body).Version(); err != nil || v != seitoml.SchemaVersion {
		t.Errorf("the file lost its schema version: (%d, %v)", v, err)
	}
	if mode, err := parse(t, body).Mode(); err != nil || mode != "validator" {
		t.Errorf("the file lost its node mode: (%q, %v)", mode, err)
	}
}

// TestTheReleaseKeyIsNotAConfigurationKey keeps provenance out of the key space.
//
// Only the key at the document's top level. A key of the same name inside a table is an ordinary
// setting called section.generated_by, and doctor should report it as one, so the exclusion is on the
// exact path rather than on the name.
func TestTheReleaseKeyIsNotAConfigurationKey(t *testing.T) {
	f := parse(t, commented)
	if err := f.SetGeneratedBy("v6.7.0"); err != nil {
		t.Fatalf("SetGeneratedBy: %v", err)
	}

	values, err := f.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if _, present := values[seitoml.GeneratedByKey]; present {
		t.Errorf("%s appears in the written key space: %v", seitoml.GeneratedByKey, values)
	}
	if len(values) != 2 {
		t.Errorf("read %d keys, want the section's 2: %v", len(values), values)
	}

	// The same name inside a table stays a configuration key, since it is one.
	inTable := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\ngenerated_by = \"x\"\n")
	nested, err := inTable.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if _, present := nested["probe.generated_by"]; !present {
		t.Errorf("probe.generated_by was excluded from the key space: %v. Only the top-level key is "+
			"provenance; inside a table it is a setting like any other and doctor should say so", nested)
	}
}

// TestNothingBehavesDifferentlyForTheReleaseKey is the constraint that makes the field safe.
//
// It is provenance, so no answer anywhere may depend on it. The moment something branches on it, a
// development build writing no release becomes a node that cannot read its own configuration, and a
// file hand-edited to a nonsense release becomes one nothing will touch.
//
// Held by driving every reader over the same file three ways: recording a release, recording none,
// and recording something no release ever was. Every answer has to match.
func TestNothingBehavesDifferentlyForTheReleaseKey(t *testing.T) {
	const body = "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nworkers = 4\n"

	answers := map[string][]string{}
	for _, release := range []string{"", "v6.7.0", "not-a-release-anyone-shipped"} {
		f := parse(t, body)
		if err := f.SetGeneratedBy(release); err != nil {
			t.Fatalf("SetGeneratedBy(%q): %v", release, err)
		}

		var got []string
		version, err := f.Version()
		got = append(got, fmt.Sprintf("version=%d err=%v", version, err))
		mode, err := f.Mode()
		got = append(got, fmt.Sprintf("mode=%q err=%v", mode, err))
		values, err := f.Values()
		got = append(got, fmt.Sprintf("values=%v err=%v", values, err))
		value, present, err := f.Get("probe.workers")
		got = append(got, fmt.Sprintf("get=%#v present=%v err=%v", value, present, err))

		// Save too, since a round trip through the disk is where a reader could pick the key up again.
		path := filepath.Join(t.TempDir(), "sei.toml")
		if err := f.Save(path); err != nil {
			t.Fatalf("Save: %v", err)
		}
		reread, err := seitoml.Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		rereadValues, err := reread.Values()
		got = append(got, fmt.Sprintf("reread=%v err=%v", rereadValues, err))

		answers[release] = got
	}

	base := answers["v6.7.0"]
	for release, got := range answers {
		for i := range got {
			if got[i] != base[i] {
				t.Errorf("with %s = %q a reader answered\n  %s\nand with a recorded release it answered\n"+
					"  %s\n\nThe field is provenance and nothing may depend on it: a development build "+
					"writes none, so anything branching on it makes such a build unable to read its own "+
					"configuration", seitoml.GeneratedByKey, release, got[i], base[i])
			}
		}
	}
}

// TestEveryValueShapeTomlAllowsReadsBack drives the value forms an operator's file can hold.
//
// The file is hand-written, and TOML gives an operator more ways to write a value than a generated
// file would ever use: two string quotings and their multi-line forms, an integer in hex or with
// separators, a date, an array with a comment inside it, an inline table. Each has to come back as the
// Go value a reader compares against a default, because a shape that decodes wrongly is a value an
// operator wrote and the node silently disagrees about.
func TestEveryValueShapeTomlAllowsReadsBack(t *testing.T) {
	f := parse(t, `schema_version = 1
node_mode = "validator"

[probe]
flag = true
basic = "a\ttab"
literal = 'C:\Users\node'
folded = """
first line
second line"""
verbatim = '''
kept \as \written'''
grouped = 1_000_000
hex = 0x1f
ratio = 2.5
stamped = 2026-08-18
peers = ["a", "b"]
commented = [
  # the first one is the seed
  "a",
  "b",
]
inline = { host = "h", port = 26657 }
nested = { outer = { inner = 3 } }
`)

	values, err := f.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	for key, want := range map[string]any{
		"probe.flag":               true,
		"probe.basic":              "a\ttab",
		"probe.literal":            `C:\Users\node`,
		"probe.folded":             "first line\nsecond line",
		"probe.verbatim":           `kept \as \written`,
		"probe.grouped":            int64(1000000),
		"probe.hex":                int64(31),
		"probe.ratio":              2.5,
		"probe.stamped":            "2026-08-18",
		"probe.inline.host":        "h",
		"probe.inline.port":        int64(26657),
		"probe.nested.outer.inner": int64(3),
	} {
		got, ok := values[key]
		if !ok {
			t.Errorf("%s is written in the file and Values left it out", key)
			continue
		}
		if got != want {
			t.Errorf("%s read back as %#v, want %#v", key, got, want)
		}
	}

	// Arrays compare element by element, and the commented one proves a comment between items is not
	// mistaken for an item.
	for key, want := range map[string][]any{
		"probe.peers":     {"a", "b"},
		"probe.commented": {"a", "b"},
	} {
		got, ok := values[key].([]any)
		if !ok {
			t.Errorf("%s read back as %#v, want a list", key, values[key])
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s read back as %#v, want %#v", key, got, want)
		}
	}

	// Get answers for one key the same way Values does for all of them, since a caller reading a single
	// key must not get a different decoding from one reading the file.
	for _, key := range []string{"probe.literal", "probe.folded", "probe.hex", "probe.inline.port"} {
		got, present, err := f.Get(key)
		if err != nil || !present {
			// An inline table's leaf is reachable through Values and not through Get, which walks the
			// document rather than the flattened space.
			if strings.Contains(key, "inline") {
				continue
			}
			t.Errorf("Get(%q) = (%#v, %v, %v)", key, got, present, err)
			continue
		}
		if got != values[key] {
			t.Errorf("Get(%q) read %#v and Values read %#v; one reader disagrees with the other",
				key, got, values[key])
		}
	}
}

// TestAValueTomlDoesNotRecognizeIsNamedNotGuessed covers what a hand-edited file can go wrong as.
//
// A bare word never reaches here, because the parser refuses one before any value is decoded, in a
// table and inside an array or an inline table alike. What does reach here is a value TOML accepts and
// this package cannot use: a number past int64, and an infinity, which TOML spells as a word and
// ParseFloat accepts.
//
// Each has to name the key and what is wrong with it. Read as a zero, the node would boot on a value
// nobody wrote; dropped, the operator's line would be silently ignored.
func TestAValueTomlDoesNotRecognizeIsNamedNotGuessed(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		key  string
		want string
	}{
		{"an integer past int64", "[probe]\nn = 99999999999999999999\n", "probe.n", "not an integer"},
		{"an infinity", "[probe]\nn = inf\n", "probe.n", "not a finite number"},
		{"a negative infinity", "[probe]\nn = -inf\n", "probe.n", "not a finite number"},
		{"a NaN", "[probe]\nn = nan\n", "probe.n", "not a finite number"},
		{"an infinity inside an array", "[probe]\nlist = [1.5, inf]\n", "probe.list", "not a finite number"},
		{"an infinity inside an inline table", "[probe]\nt = { a = inf }\n", "probe.t", "not a finite number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parse(t, tc.body)

			_, err := f.Values()
			if err == nil {
				t.Fatalf("Values accepted %s, so the node would run on a value nothing produced", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the error reads %q, which does not mention %q", err, tc.want)
			}

			// Get has to refuse the same value, or a caller reading one key would see what a caller
			// reading the whole file cannot.
			if _, _, err := f.Get(tc.key); err == nil {
				t.Errorf("Get(%q) accepted the value Values refused", tc.key)
			}
		})
	}
}

// TestParseRefusesAFileTomlCannotRead covers the boundary before any value is read.
//
// A truncated or corrupt file has to fail as a file rather than as an empty one, since an empty
// document reads as a node that chose nothing and resolves every key to a default.
func TestParseRefusesAFileTomlCannotRead(t *testing.T) {
	if _, err := seitoml.Parse(strings.NewReader("[unterminated\nkey = 1\n")); err == nil {
		t.Error("a malformed document parsed, and an empty one reads as a node that chose nothing")
	}
	if _, err := seitoml.Load(filepath.Join(t.TempDir(), "absent.toml")); err == nil {
		t.Error("loading a file that does not exist succeeded")
	}
	bad := filepath.Join(t.TempDir(), "sei.toml")
	if err := os.WriteFile(bad, []byte("[unterminated\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := seitoml.Load(bad)
	if err == nil {
		t.Fatal("loading a malformed file succeeded")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Errorf("the error reads %q and does not name the file, so an operator reading it cannot tell "+
			"which one to fix", err)
	}
}

// TestSetWritesIntoATableTheFileAlreadyHas covers the branch that does not create a heading.
//
// A file an operator wrote already has its sections, so most writes land in an existing table rather
// than making one. Appending a second heading for a table that is already there produces a file with
// the section twice, which is not what the operator wrote and not what a reader expects.
func TestSetWritesIntoATableTheFileAlreadyHas(t *testing.T) {
	f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nfirst = 1\n")
	if err := f.Set("probe.second", 2); err != nil {
		t.Fatalf("Set: %v", err)
	}

	out := render(t, f)
	if n := strings.Count(out, "[probe]"); n != 1 {
		t.Errorf("the file carries the [probe] heading %d times, want once:\n%s", n, out)
	}
	reread := parse(t, out)
	for key, want := range map[string]any{"probe.first": int64(1), "probe.second": int64(2)} {
		got, ok, err := reread.Get(key)
		if err != nil || !ok || got != want {
			t.Errorf("%s = (%#v, %v, %v), want %#v", key, got, ok, err, want)
		}
	}
}

// TestSetWritesATopLevelKeyIntoAFileThatStartsWithATable covers a document with no global section.
//
// A file whose first line is a heading has nothing above it, so writing the schema version or the node
// mode into one has to create that space rather than fail or land inside the first table. Landing
// inside it would make the key read as section.schema_version, which no reader asks for.
func TestSetWritesATopLevelKeyIntoAFileThatStartsWithATable(t *testing.T) {
	f := parse(t, "[probe]\nfirst = 1\n")
	if err := f.Set(seitoml.ModeKey, "archive"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	reread := parse(t, render(t, f))
	mode, err := reread.Mode()
	if err != nil {
		t.Fatalf("Mode after writing it into a file that had no top level: %v\n%s", err, render(t, f))
	}
	if mode != "archive" {
		t.Errorf("mode read back as %q, want archive", mode)
	}
	if _, ok, _ := reread.Get("probe." + seitoml.ModeKey); ok {
		t.Error("the key landed inside the first table, so it reads as one that section owns")
	}
}

// TestThePreambleIsReplacedRatherThanStacked holds what regenerating a file does to its header.
//
// The preamble explains the file to whoever opens it, and a generate or adopt run writes one. Stacking
// a new block on the last would grow the header on every run until the explanation is buried in copies
// of itself. An empty list removes it, which is how a caller drops a header it no longer wants.
func TestThePreambleIsReplacedRatherThanStacked(t *testing.T) {
	f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nfirst = 1\n")

	f.SetPreamble([]string{" written by the first run"})
	first := render(t, f)
	if !strings.Contains(first, "# written by the first run") {
		t.Fatalf("the preamble is not in the file:\n%s", first)
	}

	f.SetPreamble([]string{" written by the second run"})
	second := render(t, f)
	if strings.Contains(second, "first run") {
		t.Errorf("the second preamble stacked on the first, so a header grows on every run:\n%s", second)
	}
	if !strings.Contains(second, "# written by the second run") {
		t.Errorf("the second preamble is not in the file:\n%s", second)
	}

	// The keys are untouched throughout, since a header is not configuration.
	if got, ok, err := parse(t, second).Get("probe.first"); err != nil || !ok || got != int64(1) {
		t.Errorf("probe.first = (%#v, %v, %v) after two preambles, want 1", got, ok, err)
	}

	f.SetPreamble(nil)
	if bare := render(t, f); strings.Contains(bare, "second run") {
		t.Errorf("an empty preamble left the old one in place:\n%s", bare)
	}
}

// TestThePreambleGoesAboveEverythingInAFileThatStartsWithATable covers the no-global-section case.
func TestThePreambleGoesAboveEverythingInAFileThatStartsWithATable(t *testing.T) {
	f := parse(t, "[probe]\nfirst = 1\n")
	f.SetPreamble([]string{" a header"})

	out := render(t, f)
	if !strings.HasPrefix(strings.TrimSpace(out), "# a header") {
		t.Errorf("the preamble is not the first thing in the file:\n%s", out)
	}
}

// TestAMalformedKeyIsRefusedByEveryVerbThatTakesOne holds the four entry points to one answer.
//
// Set, Unset and Get each take a dotted key from a caller, and a key TOML cannot express has to be
// refused rather than written as something else. Held together because a verb that accepted one would
// put a key in the file that no other verb can address.
func TestAMalformedKeyIsRefusedByEveryVerbThatTakesOne(t *testing.T) {
	for _, key := range []string{"", "probe.", ".value", "probe..value"} {
		t.Run("key "+key, func(t *testing.T) {
			f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n")
			if err := f.Set(key, 1); err == nil {
				t.Errorf("Set(%q) was accepted", key)
			}
			if _, err := f.Unset(key); err == nil {
				t.Errorf("Unset(%q) was accepted", key)
			}
			if _, _, err := f.Get(key); err == nil {
				t.Errorf("Get(%q) was accepted", key)
			}
		})
	}
}

// TestGetAnswersAbsentForAKeyTheFileDoesNotCarry separates absence from failure.
//
// An absent key is ordinary: it means the operator chose nothing and the value resolves to a default. A
// caller told this as an error would treat every unset key as a broken file.
func TestGetAnswersAbsentForAKeyTheFileDoesNotCarry(t *testing.T) {
	f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nfirst = 1\n")
	for _, key := range []string{"probe.absent", "absent.key", "absent"} {
		got, present, err := f.Get(key)
		if err != nil {
			t.Errorf("Get(%q) failed with %v, and an unwritten key is ordinary", key, err)
		}
		if present || got != nil {
			t.Errorf("Get(%q) = (%#v, %v), want absent", key, got, present)
		}
	}
}

// TestAnInfinityCannotBeWritten holds the writer to the same rule as the reader.
//
// This file format has no form for an infinity or a NaN, so writing one produces a line no reader can
// load. Refusing names the value; the alternative writes it into an operator's file with nothing said.
func TestAnInfinityCannotBeWritten(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    float64
	}{
		{"positive infinity", math.Inf(1)},
		{"negative infinity", math.Inf(-1)},
		{"NaN", math.NaN()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := seitoml.New("validator", "v6.7.0")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			err = f.Set("probe.value", tc.v)
			if err == nil {
				t.Fatalf("%s was written, and no reader can load the line it produces", tc.name)
			}
			if !strings.Contains(err.Error(), "finite numbers") {
				t.Errorf("the refusal reads %q and does not say why", err)
			}
		})
	}
}

// TestAFileDescribingItselfWithANonValueIsRefusedPerReader covers the three keys about the file.
//
// schema_version, node_mode and generated_by are read before anything else, so a value this package
// cannot decode has to fail there rather than further in. The three differ in what failure means:
// version and mode are machinery a reader cannot proceed without, and the release is provenance whose
// absence is ordinary, so an undecodable one reads as absent rather than as an error.
func TestAFileDescribingItselfWithANonValueIsRefusedPerReader(t *testing.T) {
	t.Run("an undecodable schema version", func(t *testing.T) {
		_, err := parse(t, "schema_version = inf\n").Version()
		if err == nil {
			t.Fatal("an undecodable version was accepted, so a migration would run against a file whose " +
				"shape nobody established")
		}
		if !strings.Contains(err.Error(), seitoml.VersionKey) {
			t.Errorf("the error reads %q and does not name the key", err)
		}
	})

	t.Run("an undecodable node mode", func(t *testing.T) {
		_, err := parse(t, "node_mode = inf\n").Mode()
		if err == nil {
			t.Fatal("an undecodable mode was accepted, so an archive node's file would be compared " +
				"against a validator's defaults")
		}
		if !strings.Contains(err.Error(), seitoml.ModeKey) {
			t.Errorf("the error reads %q and does not name the key", err)
		}
	})

	t.Run("an undecodable release", func(t *testing.T) {
		release, ok := parse(t, "generated_by = inf\n").GeneratedBy()
		if ok || release != "" {
			t.Errorf("GeneratedBy = (%q, %v), want absent. The field is provenance, so a value nothing "+
				"can decode is the same as no value rather than a failure a caller has to handle",
				release, ok)
		}
	})

	t.Run("a node mode that is not a string", func(t *testing.T) {
		_, err := parse(t, "node_mode = 3\n").Mode()
		if err == nil || !strings.Contains(err.Error(), "want a mode name") {
			t.Errorf("Mode on a numeric mode returned %v, want a refusal naming what it wanted", err)
		}
	})

	t.Run("a schema version that is not an integer", func(t *testing.T) {
		_, err := parse(t, "schema_version = \"one\"\n").Version()
		if err == nil || !strings.Contains(err.Error(), "want an integer") {
			t.Errorf("Version on a string version returned %v, want a refusal naming what it wanted", err)
		}
	})
}

// TestSaveNamesThePathWhenItCannotWriteThere holds the failure an operator is most likely to hit.
//
// A configured directory that does not exist is an ordinary mistake, and the error has to name the path
// so the operator knows which one to create. Failing without it leaves them guessing which of a
// configured data directory, home directory or flag was wrong.
func TestSaveNamesThePathWhenItCannotWriteThere(t *testing.T) {
	f, err := seitoml.New("validator", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(t.TempDir(), "absent-directory", "sei.toml")

	err = f.Save(path)
	if err == nil {
		t.Fatal("saving into a directory that does not exist succeeded")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("the error reads %q and does not name the path, so an operator cannot tell which "+
			"directory to create", err)
	}
}

// TestSaveRefusesAPathThatIsADirectory covers the install step of the atomic write.
//
// A configured path pointing at a directory is an ordinary mistake, and the temporary file is written
// before anything notices. The error has to name the destination, and the directory it wrote beside has
// to be left clean, or the next save finds it littered with the leavings of this one.
func TestSaveRefusesAPathThatIsADirectory(t *testing.T) {
	f, err := seitoml.New("validator", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "sei.toml")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err = f.Save(target)
	if err == nil {
		t.Fatal("saving over a directory succeeded")
	}
	if !strings.Contains(err.Error(), target) {
		t.Errorf("the error reads %q and does not name the destination", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the directory holds %v after a failed save, want only the destination. A temporary "+
			"file left behind accumulates on every retry", names)
	}
}

// TestAListCarryingAValueThatCannotBeWrittenNamesTheElement covers writing a list back.
//
// Reading an array produces a list of any, which anything that reads a list and writes it again hands
// straight back. An element this package cannot render has to name its position, since a list of ten
// values with one bad element is otherwise a refusal an operator cannot act on.
func TestAListCarryingAValueThatCannotBeWrittenNamesTheElement(t *testing.T) {
	f, err := seitoml.New("validator", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = f.Set("probe.list", []any{"fine", struct{}{}})
	if err == nil {
		t.Fatal("a list carrying a value with no TOML form was written")
	}
	if !strings.Contains(err.Error(), "element 1") {
		t.Errorf("the refusal reads %q and does not say which element is at fault", err)
	}
}
