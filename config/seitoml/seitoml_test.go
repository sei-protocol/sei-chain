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
			"carries a dot or a space",
		},
		{
			"a quoted key carrying a space",
			"[probe]\n\"a b\" = 1\n",
			"carries a dot or a space",
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
stamped = 2026-08-18
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
		"probe.stamped":  "2026-08-18",
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

// TestAFileWrittenOnWindowsReadsTheSameValues covers the line ending an editor leaves behind.
//
// An operator editing on Windows produces a file whose lines end with a carriage return. A multi-line
// string's value begins after the delimiter's own newline, and matching only the Unix form leaves the
// carriage return inside the value, so it differs from the default it matches and a diff reports a
// change nobody can see.
func TestAFileWrittenOnWindowsReadsTheSameValues(t *testing.T) {
	const unix = "schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nfolded = \"\"\"\nfirst\nsecond\"\"\"\nflag = true\n"
	windows := strings.ReplaceAll(unix, "\n", "\r\n")

	want, err := parse(t, unix).Values()
	if err != nil {
		t.Fatalf("the Unix file: %v", err)
	}
	got, err := parse(t, windows).Values()
	if err != nil {
		t.Fatalf("the Windows file: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the same file read\n  %#v\nwith carriage returns and\n  %#v\nwithout. A value that "+
			"differs by line ending differs from the default it matches", got, want)
	}
}
