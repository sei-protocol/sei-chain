package seitoml_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/seitoml"
)

// The shipped chain, exercised the way an operator reaches it: a file they wrote, upgraded in place.
//
// The tests above cover the machinery against chains built for them. These cover the chain this binary
// actually carries, which is the only one that runs on a node.

// upgradeInPlace writes body to a temporary file, runs the shipped chain over it, and returns the result.
func upgradeInPlace(t *testing.T, body string) (string, []seitoml.Step) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sei.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}
	steps, err := seitoml.Upgrade(path, seitoml.Migrations(), false, "seid v6.6.0")
	if err != nil {
		t.Fatalf("the shipped chain refused a file an operator could be holding: %v", err)
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- a path this test just wrote
	if err != nil {
		t.Fatalf("read the upgraded file: %v", err)
	}
	return string(raw), steps
}

// TestTheShippedChainMovesAFileAnOperatorWrote is the end of the path this machinery exists for.
func TestTheShippedChainMovesAFileAnOperatorWrote(t *testing.T) {
	const body = `schema_version = 1
node_mode = "full"
generated_by = "seid v6.4.0"

[state-commit]
# Pinned to the old routing during the March migration. Ask before changing.
sc-write-mode = "cosmos_only"
sc-keep-recent = 1
`
	got, steps := upgradeInPlace(t, body)

	if len(steps) != 1 {
		t.Fatalf("the upgrade ran %d step(s), want 1: %+v", len(steps), steps)
	}
	if !strings.Contains(got, `sc-write-mode = "memiavl_only"`) {
		t.Errorf("the write mode was not translated:\n\n%s", got)
	}
	if strings.Contains(got, "cosmos_only") {
		t.Errorf("the old spelling is still in the file:\n\n%s", got)
	}
	if !strings.Contains(got, "schema_version = 2") {
		t.Errorf("the file does not claim the version the chain produces:\n\n%s", got)
	}
	if !strings.Contains(got, `generated_by = "seid v6.6.0"`) {
		t.Errorf("the upgrade did not record the release that moved the file:\n\n%s", got)
	}
	// The operator's note survives, because a migration edits the document rather than rewriting it.
	if !strings.Contains(got, "# Pinned to the old routing during the March migration.") {
		t.Errorf("the operator's comment was dropped:\n\n%s", got)
	}
	if !strings.Contains(got, "sc-keep-recent = 1") {
		t.Errorf("an unrelated setting was lost:\n\n%s", got)
	}
	if len(steps[0].Changed) != 1 || steps[0].Changed[0] != "state-commit.sc-write-mode" {
		t.Errorf("the step reports %v changed, want only the write mode. An operator reading the summary "+
			"has to be told exactly what moved", steps[0].Changed)
	}
}

// TestTheShippedChainLeavesAFileHoldingTheCurrentSpelling is most of a fleet.
//
// A file written by any recent release already says memiavl_only. The upgrade still moves it to the new
// version, and must not touch the value on the way.
func TestTheShippedChainLeavesAFileHoldingTheCurrentSpelling(t *testing.T) {
	const body = "schema_version = 1\nnode_mode = \"full\"\n\n[state-commit]\n" +
		"sc-write-mode = \"memiavl_only\"\n"
	got, steps := upgradeInPlace(t, body)

	if !strings.Contains(got, `sc-write-mode = "memiavl_only"`) {
		t.Errorf("the current spelling did not survive:\n\n%s", got)
	}
	if !strings.Contains(got, "schema_version = 2") {
		t.Errorf("the file was not moved to the new version:\n\n%s", got)
	}
	if len(steps) != 1 || len(steps[0].Changed) != 0 {
		t.Errorf("the step reports %v changed, want nothing. The value was already right, so telling an "+
			"operator it moved would be false", steps)
	}
}

// TestTheShippedChainLeavesAFileThatNeverWroteTheKey is the rest of a fleet.
func TestTheShippedChainLeavesAFileThatNeverWroteTheKey(t *testing.T) {
	got, steps := upgradeInPlace(t, "schema_version = 1\nnode_mode = \"validator\"\n")

	if strings.Contains(got, "sc-write-mode") {
		t.Errorf("the upgrade wrote a key the operator never had:\n\n%s", got)
	}
	if !strings.Contains(got, "schema_version = 2") {
		t.Errorf("the file was not moved to the new version:\n\n%s", got)
	}
	if len(steps) != 1 || len(steps[0].Changed) != 0 {
		t.Errorf("the step reports %v changed, want nothing", steps)
	}
}

// TestRunningTheShippedChainTwiceChangesNothingTheSecondTime is what makes an upgrade safe to retry.
func TestRunningTheShippedChainTwiceChangesNothingTheSecondTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sei.toml")
	body := "schema_version = 1\nnode_mode = \"full\"\n\n[state-commit]\nsc-write-mode = \"cosmos_only\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := seitoml.Upgrade(path, seitoml.Migrations(), false, "seid v6.6.0"); err != nil {
		t.Fatalf("first upgrade: %v", err)
	}
	first, err := os.ReadFile(path) // #nosec G304 -- a path this test just wrote
	if err != nil {
		t.Fatal(err)
	}

	steps, err := seitoml.Upgrade(path, seitoml.Migrations(), false, "seid v6.6.0")
	if err != nil {
		t.Fatalf("second upgrade: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("the second run performed %d step(s), want none. A file already at the current version "+
			"has nothing left to transform", len(steps))
	}
	second, err := os.ReadFile(path) // #nosec G304 -- a path this test just wrote
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("running the upgrade again rewrote the file.\n\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestAPreviewOfTheShippedChainWritesNothing is what makes the preview worth trusting.
func TestAPreviewOfTheShippedChainWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sei.toml")
	body := "schema_version = 1\nnode_mode = \"full\"\n\n[state-commit]\nsc-write-mode = \"cosmos_only\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	steps, err := seitoml.Upgrade(path, seitoml.Migrations(), true, "seid v6.6.0")
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(steps) != 1 || len(steps[0].Changed) != 1 {
		t.Errorf("the preview reported %+v, want the one step a real run performs", steps)
	}

	after, err := os.ReadFile(path) // #nosec G304 -- a path this test just wrote
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != body {
		t.Errorf("the preview changed the file:\n\n%s", after)
	}
}
