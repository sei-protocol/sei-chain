package seitoml_test

import (
	"errors"
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

// TestUnsetRemovesTheKeyRatherThanWritingItsDefault holds what unset means.
//
// An absent key resolves to the running binary's default. Writing the default value instead
// looks identical in the file but is a commitment that survives a release changing that default,
// which is the opposite of what the operator asked for.
func TestUnsetRemovesTheKeyRatherThanWritingItsDefault(t *testing.T) {
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
		t.Errorf("the key is still written after unset: %v. It would keep overriding the default the "+
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

// TestAShapeThisFileDoesNotCarryIsRefusedAtTheDoor drives the shapes Parse rejects.
//
// TOML permits more shapes than a node's configuration uses, and each of these was accepted and then
// lost or corrupted further in. Refusing at Parse is what keeps one answer per key: every later verb
// can then assume the document holds only shapes it can read and write back.
//
// Each case is a shape an operator could reasonably write, so each refusal has to name what is wrong
// and what to write instead.
func TestAShapeThisFileDoesNotCarryIsRefusedAtTheDoor(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			"an inline table",
			"[state-commit]\nflatkv = { enable = true, dir = \"/data\" }\n",
			"is an inline table",
		},
		{
			"an inline table inside an array",
			"[p]\npeers = [{ host = \"a\" }]\n",
			"is an inline table",
		},
		{
			"an array of tables",
			"[[peer]]\nhost = \"a\"\n\n[[peer]]\nhost = \"b\"\n",
			"is an array of tables",
		},
		{
			"a key written twice in one table",
			"[probe]\nn = 1\nn = 2\n",
			"is written more than once",
		},
		{
			"a repeated table heading",
			"[probe]\nn = 1\n\n[probe]\nn = 2\n",
			"appears more than once",
		},
		{
			"an upper-case key",
			"[probe]\nEnabled = true\n",
			"is not lower case",
		},
		{
			"an upper-case table heading",
			"[Probe]\nenabled = true\n",
			"is not lower case",
		},
		{
			"a quoted key carrying a dot",
			"[probe]\n\"a.b\" = 1\n",
			"is not a bare key",
		},
		{
			"a quoted key carrying a space",
			"[probe]\n\"a b\" = 1\n",
			"is not a bare key",
		},
		{
			"a quoted key carrying punctuation",
			"[probe]\n\"a#b\" = 1\n",
			"is not a bare key",
		},
		{
			"a quoted key carrying a plus",
			"[probe]\n\"a+b\" = 1\n",
			"is not a bare key",
		},
		{
			"an empty quoted key",
			"\"\" = 1\n",
			"empty segment",
		},
		{
			"a date",
			"[probe]\nstamped = 2026-08-18\n",
			"is a date or a time",
		},
		{
			"a time",
			"[probe]\nat = 07:32:00\n",
			"is a date or a time",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := seitoml.Parse(strings.NewReader(tc.body))
			if err == nil {
				t.Fatalf("%s parsed; every verb below Parse assumes it cannot appear", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal reads %q, which does not mention %q", err, tc.want)
			}
		})
	}
}

// TestTheShapesThisFileDoesCarryStillParse is the other half, so the refusals cannot pass by refusing
// everything.
//
// A table, a dotted key inside one, a lower-case hyphenated name and an array of scalars are what a
// node's configuration is written with, and each has to survive the check above.
func TestTheShapesThisFileDoesCarryStillParse(t *testing.T) {
	f := parse(t, `schema_version = 1
node_mode = "validator"

[state-commit]
sc-async-commit-buffer = 100

[state-commit.flatkv]
enable = true
dir = "/data"

[pruning]
memiavl.snapshot-interval = 100

[p2p]
persistent-peers = ["a", "b"]
`)

	values, err := f.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	for key, want := range map[string]any{
		"state-commit.sc-async-commit-buffer": int64(100),
		"state-commit.flatkv.enable":          true,
		"state-commit.flatkv.dir":             "/data",
		// A dotted key inside a table, which is a different shape from a nested heading and reads to the
		// same flattened key.
		"pruning.memiavl.snapshot-interval": int64(100),
	} {
		if values[key] != want {
			t.Errorf("%s read back as %#v, want %#v", key, values[key], want)
		}
	}
	if got, ok := values["p2p.persistent-peers"].([]any); !ok || len(got) != 2 {
		t.Errorf("the peer list read back as %#v, want two elements", values["p2p.persistent-peers"])
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

// TestAFailedSaveLeavesThePreviousFileExactlyAsItWas holds what a caller relies on after an error.
//
// A configuration file truncated by a crash mid-write is one the node cannot parse, so a save that
// cannot complete must leave the previous file byte for byte. Two ways to fail are driven, because
// they fail at different points and only one of them creates a temporary file to clean up.
//
// Neither reaches a rename that fails after the temporary file is written. That path needs the rename
// itself to fail with the destination writable, which no input to this package produces, so what holds
// it is the ordering in Save rather than a test.
func TestAFailedSaveLeavesThePreviousFileExactlyAsItWas(t *testing.T) {
	if os.Geteuid() == 0 {
		// A mode of 0500 does not stop uid 0, so the save would succeed and the failure would look
		// like a defect in Save rather than a test that cannot run as root.
		t.Skip("this drives failure through directory permissions, which do not apply to uid 0")
	}

	for _, tc := range []struct {
		name    string
		arrange func(t *testing.T, dir, path string)
	}{
		{
			"a directory the process cannot write",
			func(t *testing.T, dir, _ string) {
				if err := os.Chmod(dir, 0o500); err != nil {
					t.Fatalf("chmod: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
			},
		},
		{
			"a destination that is a symbolic link",
			func(t *testing.T, dir, path string) {
				target := filepath.Join(dir, "managed.toml")
				if err := os.WriteFile(target, []byte("schema_version = 1\n"), 0o600); err != nil {
					t.Fatalf("seed the target: %v", err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatalf("clear the seeded file: %v", err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatalf("link: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "sei.toml")
			if err := os.WriteFile(path, []byte(commented), 0o600); err != nil {
				t.Fatalf("seed the file: %v", err)
			}
			tc.arrange(t, dir, path)
			// Read after arranging, so this is what is on disk immediately before the save rather than
			// what the seed wrote. The symlink case deliberately points somewhere else.
			before, err := os.ReadFile(path) //nolint:gosec // a path this test created under t.TempDir
			if err != nil {
				t.Fatalf("read what is on disk before the save: %v", err)
			}

			f := parse(t, commented)
			if err := f.Set("giga_executor.occ_enabled", true); err != nil {
				t.Fatalf("Set: %v", err)
			}
			if err := f.Save(path); err == nil {
				t.Fatal("Save reported success, so a caller would believe the new configuration is on disk")
			}

			raw, err := os.ReadFile(path) //nolint:gosec // a path this test created under t.TempDir
			if err != nil {
				t.Fatalf("the previous file is unreadable after a failed save: %v", err)
			}
			if string(raw) != string(before) {
				t.Errorf("a failed save changed what is on disk. It now reads:\n%s\n\nThe node would "+
					"boot from something nobody wrote", raw)
			}
		})
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

// TestSaveKeepsAnExistingFilesPermissions holds that a save carries the mode it found.
//
// A configuration names the paths of a node's key files and its peers. An operator who narrowed the
// file deliberately would have that undone by a save, silently, and nothing about the change is
// visible in the file's contents. The reverse matters as much: a save is not the place to impose a
// mode, so a file an operator or an init step left wider stays as they left it.
//
// Both directions are driven, because a mode equal to newFileMode proves nothing. Asserting only that
// a 0600 file stays 0600 passes with the whole inheritance removed.
func TestSaveKeepsAnExistingFilesPermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o640, 0o644} {
		t.Run(fmt.Sprintf("%#o", mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sei.toml")
			if err := os.WriteFile(path, []byte(commented), mode); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatalf("chmod past the umask: %v", err)
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
			if got := info.Mode().Perm(); got != mode {
				t.Errorf("the file's mode moved from %#o to %#o. A save that changes access either "+
					"undoes a restriction an operator chose or imposes one they did not, and the "+
					"file's contents do not show it", mode, got)
			}
		})
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
// a validator's defaults, which is the mistake this key exists to make impossible.
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
escaped = """say \"hi\" here"""
folded_onto_one_line = """a\
   b"""
literal_backslash = """C:\\
next"""
coded = "a\u0062c"
grouped = 1_000_000
hex = 0x1f
ratio = 2.5
peers = ["a", "b"]
commented = [
  # the first one is the seed
  "a",
  "b",
]
`)

	values, err := f.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	for key, want := range map[string]any{
		"probe.flag":     true,
		"probe.basic":    "a\ttab",
		"probe.literal":  `C:\Users\node`,
		"probe.folded":   "first line\nsecond line",
		"probe.verbatim": `kept \as \written`,
		"probe.grouped":  int64(1000000),
		"probe.hex":      int64(31),
		"probe.ratio":    2.5,
		"probe.escaped":  `say "hi" here`,
		"probe.coded":    "abc",
		// A backslash ending a line folds the line break away; a doubled one is an escaped backslash
		// and keeps the break, so the two cannot be handled by the same rule.
		"probe.folded_onto_one_line": "ab",
		"probe.literal_backslash":    "C:\\\nnext",
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
	for _, key := range []string{"probe.literal", "probe.folded", "probe.verbatim", "probe.hex"} {
		got, present, err := f.Get(key)
		if err != nil || !present {
			t.Errorf("Get(%q) = (%#v, %v, %v)", key, got, present, err)
			continue
		}
		if got != values[key] {
			t.Errorf("Get(%q) read %#v and Values read %#v; one reader disagrees with the other",
				key, got, values[key])
		}
	}
}

// TestAValueTomlDoesNotRecognizeIsRefusedAtTheDoor covers what a hand-edited file can go wrong as.
//
// A bare word never reaches here, because the parser refuses one before any value is decoded, in a
// table and inside an array or an inline table alike. What does reach here is a value TOML accepts and
// this package cannot use: a number past int64, and an infinity, which TOML spells as a word and
// ParseFloat accepts.
//
// Each has to name the key and what is wrong with it. Read as a zero, the node would boot on a value
// nobody wrote; dropped, the operator's line would be silently ignored.
func TestAValueTomlDoesNotRecognizeIsRefusedAtTheDoor(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"an integer past int64", "[probe]\nn = 99999999999999999999\n", "value out of range"},
		{"an infinity", "[probe]\nn = inf\n", "has to be a finite number"},
		{"a negative infinity", "[probe]\nn = -inf\n", "has to be a finite number"},
		{"a NaN", "[probe]\nn = nan\n", "has to be a finite number"},
		{"an infinity inside an array", "[probe]\nlist = [1.5, inf]\n", "has to be a finite number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := seitoml.Parse(strings.NewReader(tc.body))
			if err == nil {
				t.Fatalf("%s parsed; a value no reader can use has to fail at the door rather than on "+
					"the first read of it", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal reads %q, which does not mention %q", err, tc.want)
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
	if !strings.HasPrefix(strings.TrimSpace(out), "# ---- generated by seid ----\n# a header") {
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
			f, err := seitoml.New("validator")
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

// TestAFileDescribingItselfWithANonValueIsRefused covers the two keys about the file.
//
// schema_version and node_mode are read before anything else, and both are machinery a reader cannot
// proceed without, so a value this package cannot decode has to fail there rather than further in. Each
// refusal names its key, because the two have different fixes.
func TestAFileDescribingItselfWithANonValueIsRefused(t *testing.T) {
	t.Run("a describing key holding a value no reader can use", func(t *testing.T) {
		// Refused at Parse along with every other value, so neither reader has to handle it.
		for _, body := range []string{"schema_version = inf\n", "node_mode = inf\n"} {
			if _, err := seitoml.Parse(strings.NewReader(body)); err == nil {
				t.Errorf("%q parsed, so a migration or a mode comparison would run against it",
					strings.TrimSpace(body))
			}
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
	f, err := seitoml.New("validator")
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
	f, err := seitoml.New("validator")
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
	f, err := seitoml.New("validator")
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

// TestAFileFromANewerReleaseIsRefused holds the guard the schema counter exists for.
//
// A release migrates the file forward on the node's own disk, so rolling the binary back does not roll
// the file back with it. Read anyway, the older binary applies only the keys it still recognises and
// boots on a configuration neither release produced, with nothing reporting it.
func TestAFileFromANewerReleaseIsRefused(t *testing.T) {
	ahead := fmt.Sprintf("schema_version = %d\nnode_mode = \"validator\"\n", seitoml.SchemaVersion+1)

	_, err := parse(t, ahead).Version()
	if err == nil {
		t.Fatal("a file from a newer release was read, so this binary would apply only the keys it " +
			"still recognises and boot on a configuration neither release produced")
	}
	for _, want := range []string{
		fmt.Sprint(seitoml.SchemaVersion + 1),
		fmt.Sprint(seitoml.SchemaVersion),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal reads %q and does not mention %q; an operator cannot tell how far "+
				"ahead the file is", err, want)
		}
	}

	// The current version and every version behind it still read, so the guard cannot pass by refusing
	// everything. Behind is what a migration exists to move forward.
	for v := 1; v <= seitoml.SchemaVersion; v++ {
		body := fmt.Sprintf("schema_version = %d\nnode_mode = \"validator\"\n", v)
		if got, err := parse(t, body).Version(); err != nil || got != v {
			t.Errorf("a file at version %d read as (%d, %v), want it accepted", v, got, err)
		}
	}
}

// TestAnUnflushedDirectoryEntryIsNotAFailedSave separates two outcomes a caller must not confuse.
//
// After the rename the new values are what the node reads. A directory entry that has not been flushed
// only leaves their survival of a power loss unproven, so reporting it the same way as a failed write
// tells an operator their change did not land when it did. The next thing they do is write it again or
// open an incident.
func TestAnUnflushedDirectoryEntryIsNotAFailedSave(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("this drives failure through directory permissions, which do not apply to uid 0")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "sei.toml")
	f, err := seitoml.New("validator")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := f.Set("probe.value", 7); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Write and traverse but not read, which is enough for the temporary file and the rename and not
	// enough to open the directory afterwards.
	if err := os.Chmod(dir, 0o300); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	saveErr := f.Save(path)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if saveErr != nil && !errors.Is(saveErr, seitoml.ErrNotDurable) {
		t.Fatalf("Save reported %v, which a caller reads as the file not being written", saveErr)
	}
	reread, err := seitoml.Load(path)
	if err != nil {
		t.Fatalf("the file is not readable after the save: %v", err)
	}
	got, present, err := reread.Get("probe.value")
	if err != nil || !present || got != int64(7) {
		t.Errorf("the value on disk is (%#v, %v, %v), want 7. The rename completed, so the new "+
			"configuration is what the node reads whatever the sync reported", got, present, err)
	}
}

// TestANameCannotBeAValueAndATableAtOnce covers the shape both entry points can produce.
//
// TOML gives a name to a value or to a table, never both. The editing parser accepts a file holding
// each under one name and a conforming decoder rejects it, so such a file parses and then every read of
// it fails. Set can produce it too, in both directions, and the file it writes re-parses cleanly, which
// makes it the worse of the two: nothing on the way in or out reports the damage.
func TestANameCannotBeAValueAndATableAtOnce(t *testing.T) {
	t.Run("a file already carrying both", func(t *testing.T) {
		_, err := seitoml.Parse(strings.NewReader(
			"[state-commit]\nflatkv = true\n\n[state-commit.flatkv]\nenable = false\n"))
		if err == nil {
			t.Fatal("a file naming one thing a value and a table parsed; every read of it then fails")
		}
		if !strings.Contains(err.Error(), "flatkv") {
			t.Errorf("the refusal reads %q and does not name the key at fault", err)
		}
	})

	t.Run("a table written under a name a value already has", func(t *testing.T) {
		f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[state-commit]\nflatkv = true\n")
		err := f.Set("state-commit.flatkv.enable", false)
		if err == nil {
			t.Fatal("Set wrote a table under a name a value already had, so Save would produce a file " +
				"no reader can load")
		}
		if !strings.Contains(err.Error(), "flatkv") {
			t.Errorf("the refusal reads %q and does not name the key at fault", err)
		}
		requireStillReadable(t, f)
	})

	t.Run("a value written under a name a table already has", func(t *testing.T) {
		f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[state-commit.flatkv]\nenable = false\n")
		err := f.Set("state-commit.flatkv", true)
		if err == nil {
			t.Fatal("Set wrote a value under a name a table already had")
		}
		requireStillReadable(t, f)
	})

	t.Run("a sibling that merely shares a prefix still writes", func(t *testing.T) {
		// A hyphen is this tree's word separator and sorts before a dot, so flatkv-mode is the sibling
		// that a string-ordered check would step over. flatkvx would not: x sorts after the dot, which is
		// the half of the comparison that cannot go wrong.
		f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[state-commit]\nflatkv = true\n")
		for _, key := range []string{"state-commit.flatkv-mode", "state-commit.flatkvx"} {
			if err := f.Set(key, false); err != nil {
				t.Errorf("%s shares only a prefix with state-commit.flatkv and was refused: %v", key, err)
			}
		}
		requireStillReadable(t, f)
	})

	t.Run("a conflict a hyphenated sibling sits between", func(t *testing.T) {
		// The shape a sorted comparison misses: flatkv-mode orders between flatkv and flatkv.enable.
		_, err := seitoml.Parse(strings.NewReader("schema_version = 1\nnode_mode = \"validator\"\n\n" +
			"[state-commit]\nflatkv = true\nflatkv-mode = \"sync\"\n\n[state-commit.flatkv]\nenable = false\n"))
		if err == nil {
			t.Fatal("a conflict separated by a hyphenated sibling parsed; the node's own decoder refuses it")
		}
	})

	t.Run("a table an ancestor's dotted key created", func(t *testing.T) {
		// The table exists without a heading of its own, so a heading for it would define it twice. The
		// key has to join the dotted name instead, and the result has to satisfy the node's decoder.
		f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[state-commit]\nflatkv.enable = true\n")
		if err := f.Set("state-commit.flatkv.dir", "/data"); err != nil {
			t.Fatalf("writing a sibling into an implicitly created table was refused: %v", err)
		}
		requireStillReadable(t, f)
		values, err := f.Values()
		if err != nil {
			t.Fatalf("Values: %v", err)
		}
		for key, want := range map[string]any{
			"state-commit.flatkv.enable": true,
			"state-commit.flatkv.dir":    "/data",
		} {
			if values[key] != want {
				t.Errorf("%s = %#v, want %#v", key, values[key], want)
			}
		}
	})

	t.Run("a table a top-level dotted key created", func(t *testing.T) {
		// The same shape as the headed case below it, except the table's ancestor is the file itself. The
		// global section carries no heading, so no prefix can find it, and a heading written for the table
		// would be the second definition the decoder refuses.
		f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\ngiga.enabled = true\n")
		if err := f.Set("giga.workers", 4); err != nil {
			t.Fatalf("writing a sibling under a top-level dotted table was refused: %v", err)
		}
		requireStillReadable(t, f)
		values, err := f.Values()
		if err != nil {
			t.Fatalf("Values: %v", err)
		}
		for key, want := range map[string]any{"giga.enabled": true, "giga.workers": int64(4)} {
			if values[key] != want {
				t.Errorf("%s = %#v, want %#v", key, values[key], want)
			}
		}
	})

	t.Run("a table two levels below a top-level dotted key", func(t *testing.T) {
		f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\na.b.c = 1\n")
		for _, key := range []string{"a.b.d", "a.e"} {
			if err := f.Set(key, 2); err != nil {
				t.Errorf("Set(%q) was refused: %v", key, err)
			}
		}
		requireStillReadable(t, f)
	})

	t.Run("a section nothing has created still gets a heading", func(t *testing.T) {
		// The other half: a table no key has named is new, and a heading is the form an operator expects
		// to read. Treating the global section as everything's ancestor would write dotted keys instead.
		f, err := seitoml.New("validator")
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		for _, key := range []string{"state-commit.enable", "p2p.laddr"} {
			if err := f.Set(key, "x"); err != nil {
				t.Fatalf("Set(%q): %v", key, err)
			}
		}
		out := render(t, f)
		for _, heading := range []string{"[state-commit]", "[p2p]"} {
			if !strings.Contains(out, heading) {
				t.Errorf("a brand new section lost its %s heading:\n%s", heading, out)
			}
		}
	})

	t.Run("a value named like a section holding nothing", func(t *testing.T) {
		// An empty section contributes no value, so a check over written values cannot see it.
		f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nn = 1\n")
		if _, err := f.Unset("probe.n"); err != nil {
			t.Fatalf("Unset: %v", err)
		}
		if err := f.Set("probe", 1); err == nil {
			t.Fatal("a value took the name of a section that still exists")
		}
		requireStillReadable(t, f)
	})
}

// requireStillReadable holds that a refused edit left the document readable.
//
// A refusal that half-applied would leave the file in the state the refusal exists to prevent.
func requireStillReadable(t *testing.T, f *seitoml.File) {
	t.Helper()
	if _, err := f.Values(); err != nil {
		t.Errorf("the document is unreadable after the edit: %v", err)
	}
	raw, err := f.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if _, err := seitoml.Parse(strings.NewReader(string(raw))); err != nil {
		t.Errorf("what the document renders to no longer parses: %v", err)
	}
}

// TestEveryVerbTakingAKeyAppliesOneRule holds Set, Unset and Get to the rule Parse applies.
//
// A key a verb accepts and Parse refuses is a key that can be written and then never read: the save
// succeeds, and the node cannot load its own configuration afterwards.
func TestEveryVerbTakingAKeyAppliesOneRule(t *testing.T) {
	// The first four are refused by a dot-or-space rule as well, so the last two are what hold the bare-key
	// rule these verbs now share with Parse.
	for _, key := range []string{"foo bar", "probe.a b", " leading", "trailing ", "probe.a#b", "probe.a+b"} {
		t.Run(fmt.Sprintf("key %q", key), func(t *testing.T) {
			f, err := seitoml.New("validator")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := f.Set(key, 1); err == nil {
				t.Errorf("Set(%q) was accepted, so the file it saves cannot be parsed again", key)
			}
			if _, err := f.Unset(key); err == nil {
				t.Errorf("Unset(%q) was accepted", key)
			}
			if _, _, err := f.Get(key); err == nil {
				t.Errorf("Get(%q) was accepted", key)
			}
		})
	}

	// A caller's upper case is folded rather than refused, since a key read lower-cased is the same key.
	f, err := seitoml.New("validator")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := f.Set("Probe.Enabled", true); err != nil {
		t.Fatalf("an upper-case key was refused rather than folded: %v", err)
	}
	if got, ok, err := f.Get("probe.enabled"); err != nil || !ok || got != true {
		t.Errorf("the folded key reads back as (%#v, %v, %v), want true", got, ok, err)
	}
}

// TestThePreambleIsReplacedAcrossASaveAndReload holds the property over the flow that actually runs.
//
// Regenerating is Load, SetPreamble, Save, and the same again on the next release, so the block this
// method must recognise is one that has been through the parser rather than one it just inserted. Held
// in memory only, the test cannot see a parser that reattaches a leading comment to whatever follows it,
// and the header would grow on every run.
func TestThePreambleIsReplacedAcrossASaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sei.toml")

	f, err := seitoml.New("validator")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := f.Set("probe.n", 1); err != nil {
		t.Fatalf("Set: %v", err)
	}
	f.SetPreamble([]string{" written by run one"})
	if err := f.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reread, err := seitoml.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	reread.SetPreamble([]string{" written by run two"})
	if err := reread.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(path) //nolint:gosec // a path this test created under t.TempDir
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "run one") {
		t.Errorf("the first preamble survived the second, so a header grows on every regenerate:\n%s", raw)
	}
	if !strings.Contains(string(raw), "# written by run two") {
		t.Errorf("the second preamble is not in the file:\n%s", raw)
	}
	// The values are untouched by either header, since a preamble is not configuration.
	final, err := seitoml.Load(path)
	if err != nil {
		t.Fatalf("Load after two preambles: %v", err)
	}
	if got, ok, err := final.Get("probe.n"); err != nil || !ok || got != int64(1) {
		t.Errorf("probe.n = (%#v, %v, %v) after two preambles, want 1", got, ok, err)
	}
}

// TestAPreambleLeavesAnOperatorsOwnTopCommentAlone covers the comment this method must not claim.
//
// SetPreamble replaces the block it put there before, and an operator may have written their own
// explanation at the top of the file. Removing that would be the comment loss this package exists to
// prevent, so it has to survive a header being written above it.
func TestAPreambleLeavesAnOperatorsOwnTopCommentAlone(t *testing.T) {
	// The blank line matters: the parser keeps a comment block standalone only when one follows it, and a
	// block attached to the next key is one SetPreamble never looks at. Without it this test passes
	// whatever the code does.
	f := parse(t, "# I wrote this and it explains the file\n# do not delete it\n\nschema_version = 1\n"+
		"node_mode = \"validator\"\n\n[probe]\nn = 1\n")

	f.SetPreamble([]string{" generated header"})

	out := render(t, f)
	for _, want := range []string{"I wrote this", "do not delete it", "# generated header"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is not in the file after a preamble was written:\n%s", want, out)
		}
	}
}

// TestAnUnsignedValueTooLargeToReadBackIsRefused holds the writer to what a reader can return.
//
// A TOML integer is signed and decodes into an int64, so a larger unsigned value renders as a line that
// reads back as an error. Accepted, it would make every later read of the file fail, including the two
// keys that describe the file itself.
func TestAnUnsignedValueTooLargeToReadBackIsRefused(t *testing.T) {
	f, err := seitoml.New("validator")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = f.Set("probe.n", uint64(math.MaxUint64))
	if err == nil {
		t.Fatal("a value past int64 was written; every later read of the file would then fail")
	}
	if !strings.Contains(err.Error(), "integers go") {
		t.Errorf("the refusal reads %q and does not say what the limit is", err)
	}

	// The largest value that does read back is still accepted, so the bound is not off by one.
	if err := f.Set("probe.big", uint64(math.MaxInt64)); err != nil {
		t.Fatalf("the largest readable unsigned value was refused: %v", err)
	}
	reread := parse(t, render(t, f))
	if got, ok, err := reread.Get("probe.big"); err != nil || !ok || got != int64(math.MaxInt64) {
		t.Errorf("probe.big = (%#v, %v, %v), want %d", got, ok, err, int64(math.MaxInt64))
	}
}

// TestASchemaVersionBelowTheFirstOneIsRefused closes the counter's lower end.
//
// Version documents that an absent or unreadable counter is an error rather than a zero, so an explicit
// zero must not return the very value that sentence rules out. A caller cannot tell that zero from the
// one it gets alongside an error.
func TestASchemaVersionBelowTheFirstOneIsRefused(t *testing.T) {
	for _, body := range []string{"schema_version = 0\n", "schema_version = -5\n"} {
		got, err := parse(t, body).Version()
		if err == nil {
			t.Errorf("%q read as version %d; a counter below the first schema names no shape",
				strings.TrimSpace(body), got)
		} else if !strings.Contains(err.Error(), "first schema") {
			t.Errorf("the refusal reads %q and does not say what the floor is", err)
		}
	}
}

// TestAValueThatIsNotTextIsRefused covers what the escaper would otherwise change silently.
//
// The escaper substitutes a replacement rune for a byte that is not valid UTF-8, so writing one stored a
// different value than the caller passed with nothing reporting it. A configuration file holds text, so
// the refusal is the honest answer.
func TestAValueThatIsNotTextIsRefused(t *testing.T) {
	f, err := seitoml.New("validator")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	invalid := string([]byte{'a', 0xff, 'b'})

	if err := f.Set("probe.v", invalid); err == nil {
		t.Fatal("a value that is not valid UTF-8 was written, and it reads back as something else")
	}
	if err := f.Set("probe.list", []string{"fine", invalid}); err == nil {
		t.Error("a list carrying one was written")
	}

	// Text that merely looks unusual still writes and survives, so the guard is not refusing breadth.
	for _, ok := range []string{"héllo", "日本語", "a\tb", `C:\sei`} {
		if err := f.Set("probe.v", ok); err != nil {
			t.Errorf("Set(%q) was refused: %v", ok, err)
			continue
		}
		if got, _, err := parse(t, render(t, f)).Get("probe.v"); err != nil || got != ok {
			t.Errorf("%q read back as (%#v, %v)", ok, got, err)
		}
	}
}

// TestAPreambleOnlyReplacesOneItWrote holds the block this method may claim.
//
// A comment block at the top of a file is an operator's explanation unless this method put it there, and
// it has no way to tell the two apart but a mark it writes and looks for. Deleting theirs would be the
// comment loss the package exists to prevent; leaving its own would grow a header on every regenerate.
func TestAPreambleOnlyReplacesOneItWrote(t *testing.T) {
	const operator = "# ops: do not raise flatkv without asking"
	f := parse(t, operator+"\n\nschema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nn = 1\n")

	f.SetPreamble([]string{" run one"})
	first := render(t, f)
	if !strings.Contains(first, "do not raise flatkv") {
		t.Fatalf("the operator's comment was deleted by the first preamble:\n%s", first)
	}

	// Through a parse, which is the state the next release's run sees.
	again := parse(t, first)
	again.SetPreamble([]string{" run two"})
	second := render(t, again)

	if strings.Contains(second, "run one") {
		t.Errorf("the first preamble survived the second, so a header grows on every run:\n%s", second)
	}
	if !strings.Contains(second, "run two") {
		t.Errorf("the second preamble is not in the file:\n%s", second)
	}
	if !strings.Contains(second, "do not raise flatkv") {
		t.Errorf("the operator's comment was deleted by the second preamble:\n%s", second)
	}
}

// TestAPreambleOwnsItsLinesRatherThanTheBlockTheySitIn holds what regenerating may touch.
//
// The mark ends the generated lines, and an operator writes wherever they like around them: above the
// header, or on a line the parser groups into the same comment item. Anchored at the first item, a
// header above which anything was written is never found and grows on every run; anchored at a block, an
// operator's line inside that block is deleted with it. Both are the failures the mark exists to prevent.
func TestAPreambleOwnsItsLinesRatherThanTheBlockTheySitIn(t *testing.T) {
	const note = "ops: we pin flatkv, see INC-4412"
	tail := "\nschema_version = 1\nnode_mode = \"validator\"\n"

	for _, tc := range []struct{ name, before string }{
		{
			"an operator block above the generated region",
			"# " + note + "\n\n" + generated("run one") + tail,
		},
		{
			"an operator line below the region, no blank line",
			generated("run one") + "# " + note + "\n" + tail,
		},
		{
			"an operator line above the region, no blank line",
			"# " + note + "\n" + generated("run one") + tail,
		},
		{
			"an operator line between two generated regions",
			generated("run one") + "# " + note + "\n" + generated("stale run") + tail,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parse(t, tc.before)
			f.SetPreamble([]string{" written by run two"})
			out := render(t, f)

			if strings.Contains(out, "run one") {
				t.Errorf("the previous header survived, so it grows on every regenerate:\n%s", out)
			}
			if !strings.Contains(out, note) {
				t.Errorf("the operator's comment was deleted:\n%s", out)
			}
			if n := strings.Count(out, "generated by seid"); n != 1 {
				t.Errorf("the file carries %d generated regions, want one:\n%s", n, out)
			}

			// And again through a parse, since a regenerate reads what the last one wrote.
			third := parse(t, out)
			third.SetPreamble([]string{" written by run three"})
			last := render(t, third)
			if strings.Contains(last, "run two") {
				t.Errorf("the second header survived the third:\n%s", last)
			}
			if !strings.Contains(last, note) {
				t.Errorf("the operator's comment was deleted on the third run:\n%s", last)
			}
		})
	}
}

// TestATableKeepsOneSpellingWhateverOrderItsKeysWereWritten holds insert's choice between the two forms.
//
// A table nothing has created is new and gets a heading, which is the form an operator reads. A table an
// ancestor's dotted key already created has no heading and cannot be given one, so its keys join that
// dotted name. Deciding by whether an ancestor section merely exists would spell the same table either
// way depending on which key was set first.
func TestATableKeepsOneSpellingWhateverOrderItsKeysWereWritten(t *testing.T) {
	t.Run("a new table under an existing section gets a heading", func(t *testing.T) {
		f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[state-commit]\nbuffer = 100\n")
		if err := f.Set("state-commit.flatkv.dir", "/data"); err != nil {
			t.Fatalf("Set: %v", err)
		}
		out := render(t, f)
		if !strings.Contains(out, "[state-commit.flatkv]") {
			t.Errorf("a table nothing had created did not get a heading:\n%s", out)
		}
		requireStillReadable(t, f)
	})

	t.Run("a table a dotted key created joins that name", func(t *testing.T) {
		f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[state-commit]\nflatkv.enable = true\n")
		if err := f.Set("state-commit.flatkv.dir", "/data"); err != nil {
			t.Fatalf("Set: %v", err)
		}
		out := render(t, f)
		if strings.Contains(out, "[state-commit.flatkv]") {
			t.Errorf("a table the document already created was given a second definition:\n%s", out)
		}
		requireStillReadable(t, f)
	})
}

// TestLandedSeparatesAnInstalledFileFromAFailedWrite gives the outcome check a correct spelling.
//
// Save reports one outcome through its error that is not a failure, so the plain err != nil check reads a
// landed save as a failed one. Landed is that check written once, so a caller cannot get it wrong by
// writing the idiom every other Go call wants.
func TestLandedSeparatesAnInstalledFileFromAFailedWrite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		landed bool
	}{
		{"a save that completed", nil, true},
		{"a save whose directory entry is not flushed", fmt.Errorf("x: %w", seitoml.ErrNotDurable), true},
		{"a save that could not write", errors.New("permission denied"), false},
	} {
		if got := seitoml.Landed(tc.err); got != tc.landed {
			t.Errorf("%s: Landed = %v, want %v", tc.name, got, tc.landed)
		}
	}
}

// TestReadingTwiceDoesNotDecodeTwice holds the cache an edit invalidates.
//
// A read renders the document and decodes it, so a caller walking every declared key would pay that per
// key, and building a file would be quadratic in its size because every edit checks the result. The
// values a read returns still have to follow the document, which is what makes the invalidation the part
// worth testing rather than the caching.
func TestReadingTwiceDoesNotDecodeTwice(t *testing.T) {
	f := parse(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nn = 1\n")

	first, err := f.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if first["probe.n"] != int64(1) {
		t.Fatalf("probe.n = %#v, want 1", first["probe.n"])
	}

	// Every edit has to be visible to the next read, or a cache is a correctness bug rather than a saving.
	for _, step := range []struct {
		name string
		edit func() error
		key  string
		want any
	}{
		{"Set replacing a value", func() error { return f.Set("probe.n", 2) }, "probe.n", int64(2)},
		{"Set adding a key", func() error { return f.Set("probe.m", 3) }, "probe.m", int64(3)},
		{"Unset removing one", func() error { _, err := f.Unset("probe.m"); return err }, "probe.m", nil},
	} {
		if err := step.edit(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		got, err := f.Values()
		if err != nil {
			t.Fatalf("%s: Values: %v", step.name, err)
		}
		if got[step.key] != step.want {
			t.Errorf("after %s, %s = %#v, want %#v", step.name, step.key, got[step.key], step.want)
		}
	}

	// A preamble changes the document too, and must not strand a decode of the version before it.
	f.SetPreamble([]string{" header"})
	if _, err := f.Values(); err != nil {
		t.Errorf("Values after a preamble: %v", err)
	}
}

// generated renders a preamble region the way SetPreamble writes one, so a fixture can start from a file
// a previous run produced.
func generated(header string) string {
	return "# ---- generated by seid ----\n# " + header +
		"\n# ---- end generated; your notes are safe below ----\n"
}

// TestAnOperatorWritingInsideTheGeneratedRegionIsTold pins the one line the region does claim.
//
// The delimiters say where the generated lines start and stop, so a note written between them is inside
// the part a regenerate replaces. That is the contract the file states in words, and it is worth a test
// because the alternative reading, that nothing a person typed may ever be replaced, would make the
// region unreplaceable.
func TestAnOperatorWritingInsideTheGeneratedRegionIsTold(t *testing.T) {
	inside := "# ---- generated by seid ----\n# written by run one\n# a note typed inside the region\n" +
		"# ---- end generated; your notes are safe below ----\nschema_version = 1\nnode_mode = \"v\"\n"

	f := parse(t, inside)
	f.SetPreamble([]string{" written by run two"})
	out := render(t, f)

	if strings.Contains(out, "typed inside the region") {
		t.Errorf("a line inside the generated region survived a regenerate, so the region cannot be "+
			"replaced at all:\n%s", out)
	}
	if !strings.Contains(out, "run two") {
		t.Errorf("the new header is missing:\n%s", out)
	}
}

// TestAnUnpairedDelimiterLeavesTheLinesAlone covers a file hand-edited into a shape with one delimiter.
//
// A region is the text between two delimiters, so one on its own bounds nothing. Guessing where it ends
// would delete an operator's lines on the strength of a marker they may have typed themselves, and
// leaving them is the answer that cannot lose their writing.
func TestAnUnpairedDelimiterLeavesTheLinesAlone(t *testing.T) {
	for _, tc := range []struct{ name, before string }{
		{
			"a begin with no end",
			"# ---- generated by seid ----\n# a note under a stray delimiter\n\nschema_version = 1\nnode_mode = \"v\"\n",
		},
		{
			"an end with no begin",
			"# a note above a stray delimiter\n# ---- end generated; your notes are safe below ----\n\nschema_version = 1\nnode_mode = \"v\"\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parse(t, tc.before)
			f.SetPreamble([]string{" written by this run"})

			out := render(t, f)
			if !strings.Contains(out, "stray delimiter") {
				t.Errorf("the operator's line was removed on the strength of one delimiter:\n%s", out)
			}
			if !strings.Contains(out, "written by this run") {
				t.Errorf("the new header is missing:\n%s", out)
			}
		})
	}
}
