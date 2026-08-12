package seitoml_test

import (
	"os"
	"path/filepath"
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
		{"float", 1.5, 1.5},
		{"string", "hello", "hello"},
		{"string with a quote", `say "hi"`, `say "hi"`},
		{"string with a backslash", `C:\sei\data`, `C:\sei\data`},
		{"empty string", "", ""},
		{"duration", 90 * time.Second, "1m30s"},
		{"string list", []string{"a", "b"}, []any{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := seitoml.New("validator")
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
	f, err := seitoml.New("validator")
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
	f, err := seitoml.New("validator")
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
	f, err := seitoml.New("validator")
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
	f, err := seitoml.New("validator")
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
	f, err := seitoml.New("archive")
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
	if _, err := seitoml.New(""); err == nil {
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
