package seitoml_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/seitoml"
)

// rewritten applies a rewrite to a file parsed from body and returns what the file then holds.
func rewritten(t *testing.T, body string, rewrite func(*seitoml.File) error) (map[string]any, error) {
	t.Helper()
	f, err := seitoml.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse the fixture: %v", err)
	}
	if err := rewrite(f); err != nil {
		return nil, err
	}
	values, err := f.Values()
	if err != nil {
		t.Fatalf("read the result: %v", err)
	}
	return values, nil
}

const twoSettings = `schema_version = 1
node_mode = "full"

[state-commit]
sc-old-name = 42
sc-keep = 7
`

// TestARenameCarriesTheValueAndLeavesNothingBehind is what a key correction needs.
func TestARenameCarriesTheValueAndLeavesNothingBehind(t *testing.T) {
	got, err := rewritten(t, twoSettings,
		seitoml.RenameKey("state-commit.sc-old-name", "state-commit.sc-new-name"))
	if err != nil {
		t.Fatalf("the rename refused a file it should have moved: %v", err)
	}

	if v, ok := got["state-commit.sc-new-name"]; !ok || v != int64(42) {
		t.Errorf("the new key holds %#v (present=%v), want 42. A rename that does not carry the value "+
			"moves the node onto a default nobody chose", v, ok)
	}
	if _, ok := got["state-commit.sc-old-name"]; ok {
		t.Error("the old key is still written. A diagnostic then reports it as a key nothing recognises, " +
			"on every node that has been upgraded")
	}
	if v, ok := got["state-commit.sc-keep"]; !ok || v != int64(7) {
		t.Errorf("an unrelated key in the same section became %#v (present=%v), want 7", v, ok)
	}
}

// TestARenameLeavesAFileThatNeverWroteTheKeyAlone is the rule every rewrite here shares.
//
// Most files will not have written the setting a migration changes. Refusing those would refuse to upgrade
// the majority of a fleet in order to transform a minority.
func TestARenameLeavesAFileThatNeverWroteTheKeyAlone(t *testing.T) {
	body := "schema_version = 1\nnode_mode = \"full\"\n\n[state-commit]\nsc-keep = 7\n"
	got, err := rewritten(t, body, seitoml.RenameKey("state-commit.sc-old-name", "state-commit.sc-new-name"))
	if err != nil {
		t.Fatalf("the rename refused a file that never wrote the old key: %v.\n\nThat is most files, so "+
			"this would refuse to upgrade a fleet in order to transform part of one", err)
	}
	if _, ok := got["state-commit.sc-new-name"]; ok {
		t.Error("the new key was written for a file that never set the old one. The operator did not " +
			"choose that value, and writing it makes a default look like a decision")
	}
	if len(got) != 1 {
		t.Errorf("the file holds %v, want only the untouched key", got)
	}
}

// TestARenameRefusesAFileWritingBothSpellings is the case that cannot be resolved silently.
func TestARenameRefusesAFileWritingBothSpellings(t *testing.T) {
	body := "schema_version = 1\nnode_mode = \"full\"\n\n[state-commit]\nsc-old-name = 42\nsc-new-name = 9\n"
	_, err := rewritten(t, body, seitoml.RenameKey("state-commit.sc-old-name", "state-commit.sc-new-name"))
	if err == nil {
		t.Fatal("a file writing both spellings was accepted. One of the two values is the one the node " +
			"should run, nothing here can tell which, and keeping either discards a setting somebody chose")
	}
	for _, want := range []string{"sc-old-name", "sc-new-name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s, so an operator cannot tell which keys to look at: %v",
				want, err)
		}
	}
}

// TestARenameCarriesAListAsWrittenAsWellAsAScalar covers the shape that used to fail.
//
// A list read back is a list of untyped values, and the writer once took only a list of strings. So a
// rename of a list-valued key failed on a value this package had just produced.
func TestARenameCarriesAListAsWrittenAsWellAsAScalar(t *testing.T) {
	body := "schema_version = 1\nnode_mode = \"full\"\nold-events = [\"tx.height\", \"message.action\"]\n"
	got, err := rewritten(t, body, seitoml.RenameKey("old-events", "index-events"))
	if err != nil {
		t.Fatalf("the rename refused a list-valued key: %v", err)
	}
	want := []any{"tx.height", "message.action"}
	if !reflect.DeepEqual(got["index-events"], want) {
		t.Errorf("the list arrived as %#v, want %#v", got["index-events"], want)
	}
}

// TestValuesAreRewrittenOnlyWhereTheReplacementsCoverThem is the enumerated-setting case.
//
// The state-commit write mode is the live example: a file from an older release says "cosmos_only" for the
// routing a later one calls "memiavl_only", and the reader translates it on every start. Translating the
// file once is what lets the reader stop.
func TestValuesAreRewrittenOnlyWhereTheReplacementsCoverThem(t *testing.T) {
	replacements := map[string]string{"cosmos_only": "memiavl_only"}

	for _, c := range []struct {
		name    string
		written string
		want    any
	}{
		{"a spelling the release renamed", "cosmos_only", "memiavl_only"},
		{"the current spelling, which most files hold", "memiavl_only", "memiavl_only"},
		{"a spelling nothing recognises, left for a diagnostic to report", "sideways", "sideways"},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := "schema_version = 1\nnode_mode = \"full\"\n\n[state-commit]\nsc-write-mode = \"" +
				c.written + "\"\n"
			got, err := rewritten(t, body, seitoml.MapValues("state-commit.sc-write-mode", replacements))
			if err != nil {
				t.Fatalf("the rewrite refused %q: %v", c.written, err)
			}
			if got["state-commit.sc-write-mode"] != c.want {
				t.Errorf("%q became %#v, want %#v", c.written, got["state-commit.sc-write-mode"], c.want)
			}
		})
	}
}

// TestARewriteOfValuesLeavesAFileThatNeverWroteTheKeyAlone repeats the shared rule for the other rewrite.
func TestARewriteOfValuesLeavesAFileThatNeverWroteTheKeyAlone(t *testing.T) {
	body := "schema_version = 1\nnode_mode = \"full\"\n"
	got, err := rewritten(t, body,
		seitoml.MapValues("state-commit.sc-write-mode", map[string]string{"cosmos_only": "memiavl_only"}))
	if err != nil {
		t.Fatalf("the rewrite refused a file that never wrote the key: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("the file gained %v. A value nobody wrote is not a value to translate", got)
	}
}

// TestARewriteThatReplacesNothingIsRefused catches the rewrite an author meant to fill in.
func TestARewriteThatReplacesNothingIsRefused(t *testing.T) {
	if _, err := rewritten(t, "schema_version = 1\nnode_mode = \"full\"\n",
		seitoml.MapValues("state-commit.sc-write-mode", nil)); err == nil {
		t.Error("a rewrite with no replacements was accepted. It is a migration that does not migrate, " +
			"and the version it produces would claim a transformation nothing performed")
	}
}

// TestARenameLeavesTheRestOfTheFileAsTheOperatorWroteIt is why these edit a document rather than rewrite one.
//
// An operator's file carries their comments. A migration that reformatted it, or dropped the notes they left
// themselves, would make every upgrade a change they have to read in full to trust.
func TestARenameLeavesTheRestOfTheFileAsTheOperatorWroteIt(t *testing.T) {
	body := `schema_version = 1
node_mode = "full"

# Storage settings, reviewed 2026-03 with the platform team.
[state-commit]
# Raised after the incident in March. Do not lower without asking.
sc-keep = 7
sc-old-name = 42
`
	f, err := seitoml.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := seitoml.RenameKey("state-commit.sc-old-name", "state-commit.sc-new-name")(f); err != nil {
		t.Fatalf("rename: %v", err)
	}
	raw, err := f.Bytes()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, note := range []string{
		"# Storage settings, reviewed 2026-03 with the platform team.",
		"# Raised after the incident in March. Do not lower without asking.",
	} {
		if !strings.Contains(string(raw), note) {
			t.Errorf("the file no longer carries %q.\n\nAn upgrade that drops what an operator wrote to "+
				"themselves is one they have to re-read in full to trust:\n\n%s", note, raw)
		}
	}
}

// TestARenameMovesAKeyBetweenSectionsAndToTheRoot covers the shapes a correction may need.
//
// A key correction is not always within one section. The tags that need correcting put leaves outside the
// section their reader looks in, so moving between two sections and moving up to the root are both real.
func TestARenameMovesAKeyBetweenSectionsAndToTheRoot(t *testing.T) {
	body := "schema_version = 1\nnode_mode = \"full\"\n\n[state-commit]\nmisplaced = 5\nalso-misplaced = 6\n"
	f, err := seitoml.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := seitoml.RenameKey("state-commit.misplaced", "state-store.ss-misplaced")(f); err != nil {
		t.Fatalf("move between sections: %v", err)
	}
	if err := seitoml.RenameKey("state-commit.also-misplaced", "concurrency-workers")(f); err != nil {
		t.Fatalf("move to the root: %v", err)
	}

	raw, err := f.Bytes()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	reread, err := seitoml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("the moved file does not parse: %v\n\n%s", err, raw)
	}
	got, err := reread.Values()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	for key, want := range map[string]any{
		"state-store.ss-misplaced": int64(5),
		"concurrency-workers":      int64(6),
	} {
		if got[key] != want {
			t.Errorf("%s reads back as %#v, want %#v.\n\nA key moved to the root and written after a "+
				"table heading belongs to that table:\n\n%s", key, got[key], want, raw)
		}
	}
	if _, ok := got["state-commit.misplaced"]; ok {
		t.Error("the key is still in its old section")
	}
}

// TestAnUncoveredValueKeepsTheLineTheOperatorWrote is the stronger form of leaving a value alone.
//
// Writing a value back as itself resolves to the same setting, so nothing comparing values would notice. It
// still replaces the line, which normalises how the value was written: a string the operator quoted one way
// comes back quoted another. That turns a migration for one spelling into a reformat of every file that
// already held the current one, and an operator reviewing the upgrade sees lines change for no reason.
func TestAnUncoveredValueKeepsTheLineTheOperatorWrote(t *testing.T) {
	// Single quotes, which TOML accepts and this package does not render.
	const body = "schema_version = 1\nnode_mode = \"full\"\n\n[state-commit]\nsc-write-mode = 'memiavl_only'\n"

	f, err := seitoml.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rewrite := seitoml.MapValues("state-commit.sc-write-mode", map[string]string{"cosmos_only": "memiavl_only"})
	if err := rewrite(f); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	raw, err := f.Bytes()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !strings.Contains(string(raw), "sc-write-mode = 'memiavl_only'") {
		t.Errorf("the line became something other than what the operator wrote:\n\n%s\n\nThe replacements "+
			"do not cover this value, so nothing here should have rewritten the line", raw)
	}
}
