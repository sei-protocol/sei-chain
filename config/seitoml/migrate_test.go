package seitoml_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/seitoml"
)

// The chain shipped today is empty, so the machinery is driven by a chain built here. Every rule
// below is asserted twice over: against that chain, and against the real one where the rule can be
// stated without knowing what the real one contains.

// renameChain is a two-step chain used to drive the machinery.
//
// Two steps rather than one, because a single-step chain cannot tell an implementation that runs
// every pending migration apart from one that runs only the first.
func renameChain() []seitoml.Migration {
	return []seitoml.Migration{
		{
			To:      2,
			Summary: "move probe.workers to probe.worker_count",
			Fixture: "schema_version = 1\n\n[probe]\nworkers = 4\n",
			Apply: func(f *seitoml.File) error {
				v, ok, err := f.Get("probe.workers")
				if err != nil || !ok {
					return err
				}
				if _, err := f.Unset("probe.workers"); err != nil {
					return err
				}
				return f.Set("probe.worker_count", v)
			},
		},
		{
			To:      3,
			Summary: "add probe.timeout",
			Fixture: "schema_version = 2\n\n[probe]\nworker_count = 4\n",
			Apply: func(f *seitoml.File) error {
				return f.Set("probe.timeout", "30s")
			},
		},
	}
}

// write puts a body on disk and returns its path.
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sei.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // a path this test created under t.TempDir
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(raw)
}

// TestUpgradeRunsEveryPendingStepInOrder holds what upgrade promises.
//
// Driven from two versions behind, so an implementation that ran only the first pending step, or
// only the last, fails. The steps are reported in order because that is what an operator reads to
// understand what happened to their file.
func TestUpgradeRunsEveryPendingStepInOrder(t *testing.T) {
	path := write(t, "schema_version = 1\n\n[probe]\nworkers = 4\n")

	steps, err := seitoml.Upgrade(path, renameChain(), false)
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}

	if len(steps) != 2 {
		t.Fatalf("ran %d steps from two versions behind, want 2: %+v. A single-step run leaves the "+
			"file at an intermediate version and reports success", len(steps), steps)
	}
	if steps[0].To != 2 || steps[1].To != 3 {
		t.Errorf("the steps reached versions %d then %d, want 2 then 3", steps[0].To, steps[1].To)
	}

	file, err := seitoml.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if v, err := file.Version(); err != nil || v != 3 {
		t.Errorf("the file is at version %d (%v) after upgrading, want 3", v, err)
	}
	values, err := file.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if _, stale := values["probe.workers"]; stale {
		t.Errorf("the old key is still written: %v", values)
	}
	if values["probe.worker_count"] != int64(4) {
		t.Errorf("the renamed key holds %#v, want the 4 the operator wrote. A migration that lost "+
			"the value would silently re-baseline the setting", values["probe.worker_count"])
	}
	if values["probe.timeout"] != "30s" {
		t.Errorf("the second step did not run: %v", values)
	}
}

// TestUpgradeReportsWhichKeysEachStepMoved is what an operator reads before trusting the result.
//
// A step reported with no keys is indistinguishable from a step that did nothing, so the diff is
// what makes the report worth printing.
func TestUpgradeReportsWhichKeysEachStepMoved(t *testing.T) {
	path := write(t, "schema_version = 1\n\n[probe]\nworkers = 4\n")

	steps, err := seitoml.Upgrade(path, renameChain(), false)
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}

	want := [][]string{
		{"probe.worker_count", "probe.workers"},
		{"probe.timeout"},
	}
	for i, step := range steps {
		if strings.Join(step.Changed, ",") != strings.Join(want[i], ",") {
			t.Errorf("step to version %d reports %v, want %v. A step whose report is empty cannot be "+
				"told apart from one that did nothing", step.To, step.Changed, want[i])
		}
		if step.Summary == "" {
			t.Errorf("step to version %d has no summary", step.To)
		}
	}
}

// TestADryRunPreviewsExactlyWhatARunWouldDo is what makes a preview worth trusting.
//
// A preview that reported something other than the real run would be worse than no preview, since
// an operator would approve one change and get another.
func TestADryRunPreviewsExactlyWhatARunWouldDo(t *testing.T) {
	const body = "schema_version = 1\n\n[probe]\nworkers = 4\n"
	preview := write(t, body)
	real := write(t, body)

	previewed, err := seitoml.Upgrade(preview, renameChain(), true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	applied, err := seitoml.Upgrade(real, renameChain(), false)
	if err != nil {
		t.Fatalf("real run: %v", err)
	}

	if fmt.Sprint(previewed) != fmt.Sprint(applied) {
		t.Errorf("a dry run reported\n  %+v\nand the real run did\n  %+v", previewed, applied)
	}
	if got := read(t, preview); got != body {
		t.Errorf("a dry run wrote to the file. It now reads:\n%s", got)
	}
}

// TestUpgradeAdvancesTheFileOneVersionPerWrite is why the save is inside the loop.
//
// A crash part way through a chain must leave a file at a version some migration produced, not one
// whose contents belong to no version. Held by failing the second step and reading what is on disk.
func TestUpgradeAdvancesTheFileOneVersionPerWrite(t *testing.T) {
	path := write(t, "schema_version = 1\n\n[probe]\nworkers = 4\n")
	chain := renameChain()
	chain[1].Apply = func(*seitoml.File) error {
		return fmt.Errorf("this step fails")
	}

	steps, err := seitoml.Upgrade(path, chain, false)
	if err == nil {
		t.Fatal("Upgrade reported success though its second step failed")
	}

	if len(steps) != 1 {
		t.Errorf("reported %d completed steps, want the 1 that succeeded: %+v", len(steps), steps)
	}
	file, err := seitoml.Load(path)
	if err != nil {
		t.Fatalf("the file is unreadable after a failed upgrade: %v", err)
	}
	v, err := file.Version()
	if err != nil {
		t.Fatalf("the file has no readable version after a failed upgrade: %v", err)
	}
	if v != 2 {
		t.Errorf("the file is at version %d after the first step succeeded and the second failed, "+
			"want 2. A file left at a version no migration produced cannot be carried forward", v)
	}
	// And the completed step's work is on disk, so re-running resumes rather than repeats.
	values, _ := file.Values()
	if values["probe.worker_count"] != int64(4) {
		t.Errorf("the first step's result is not on disk: %v", values)
	}
}

// TestUpgradeRefusesAFileFromANewerBinary keeps a downgrade from reading as success.
//
// The file's keys were written against a schema this binary does not have. Reporting it current
// would boot a node against a file it cannot fully read, and nothing would say so.
func TestUpgradeRefusesAFileFromANewerBinary(t *testing.T) {
	path := write(t, "schema_version = 9\n\n[probe]\nworkers = 4\n")

	if _, err := seitoml.Upgrade(path, renameChain(), false); err == nil {
		t.Error("a file from a newer binary was accepted as current. Its keys were written against a " +
			"schema this binary does not have, and treating it as finished hides that")
	}
	if _, err := seitoml.Pending(9, renameChain()); err == nil {
		t.Error("Pending accepted a version above the chain's own")
	}
}

// TestAFileAlreadyCurrentNeedsNoSteps holds the ordinary case.
//
// Without it, the refusals above would hold for an upgrade that refused everything.
func TestAFileAlreadyCurrentNeedsNoSteps(t *testing.T) {
	path := write(t, "schema_version = 3\n\n[probe]\nworker_count = 4\n")
	before := read(t, path)

	steps, err := seitoml.Upgrade(path, renameChain(), false)
	if err != nil {
		t.Fatalf("Upgrade on a current file: %v", err)
	}

	if len(steps) != 0 {
		t.Errorf("a current file ran %d steps: %+v", len(steps), steps)
	}
	if got := read(t, path); got != before {
		t.Errorf("a current file was rewritten:\n%s", got)
	}
}

// TestUpgradeStartsFromTheVersionTheFileRecords holds that the file's own version is what decides.
//
// Without it, the tests above would pass for an upgrade that always ran the whole chain, which on a
// file already partly migrated would apply a step twice.
func TestUpgradeStartsFromTheVersionTheFileRecords(t *testing.T) {
	path := write(t, "schema_version = 2\n\n[probe]\nworker_count = 4\n")

	steps, err := seitoml.Upgrade(path, renameChain(), false)
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}

	if len(steps) != 1 || steps[0].To != 3 {
		t.Errorf("a file at version 2 ran %+v, want only the step to version 3. Running the whole "+
			"chain would apply an already-applied step a second time", steps)
	}
}

// TestAMigrationCannotClaimAVersionItsContentsDoNotMatch keeps the version write out of Apply.
//
// A migration that set the version itself could raise it without transforming anything, and the
// file would then claim a shape it does not have with nothing able to detect it.
func TestAMigrationCannotClaimAVersionItsContentsDoNotMatch(t *testing.T) {
	path := write(t, "schema_version = 1\n\n[probe]\nworkers = 4\n")
	chain := []seitoml.Migration{{
		To:      2,
		Summary: "a migration that tries to set the version itself",
		Fixture: "schema_version = 1\n",
		Apply: func(f *seitoml.File) error {
			// Even if a migration writes a version, the caller's write is what lands.
			return f.Set(seitoml.VersionKey, 7)
		},
	}}

	if _, err := seitoml.Upgrade(path, chain, false); err != nil {
		t.Fatalf("Upgrade: %v", err)
	}

	file, err := seitoml.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if v, _ := file.Version(); v != 2 {
		t.Errorf("the file records version %d, want the 2 the chain produced. A migration that can "+
			"set the version itself can raise it without transforming anything", v)
	}
}

// TestAnIllFormedChainIsRefused holds the shape a chain built by appending must have.
//
// A gap leaves a file at a version nothing can move forward. A repeat means two migrations claim
// one version, so the order between them decides the result. Both are programming errors, and
// finding them at the first upgrade on a real node is far too late.
func TestAnIllFormedChainIsRefused(t *testing.T) {
	ok := func(to int) seitoml.Migration {
		return seitoml.Migration{
			To: to, Summary: "s", Fixture: "schema_version = 1\n",
			Apply: func(*seitoml.File) error { return nil },
		}
	}
	for _, tc := range []struct {
		name  string
		chain []seitoml.Migration
	}{
		{"starts above 2", []seitoml.Migration{ok(3)}},
		{"a gap", []seitoml.Migration{ok(2), ok(4)}},
		{"a repeat", []seitoml.Migration{ok(2), ok(2)}},
		{"out of order", []seitoml.Migration{ok(3), ok(2)}},
		{"no summary", []seitoml.Migration{{To: 2, Fixture: "x", Apply: func(*seitoml.File) error { return nil }}}},
		{"no fixture", []seitoml.Migration{{To: 2, Summary: "s", Apply: func(*seitoml.File) error { return nil }}}},
		{"no apply", []seitoml.Migration{{To: 2, Summary: "s", Fixture: "x"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := seitoml.ValidateChain(tc.chain); err == nil {
				t.Errorf("a chain that %s was accepted", tc.name)
			}
		})
	}
	if err := seitoml.ValidateChain(renameChain()); err != nil {
		t.Errorf("a well-formed chain was refused: %v", err)
	}
}

// TestTheShippedChainIsWellFormedAndMatchesTheSchemaVersion is the guard on the real chain.
//
// The version this binary writes has to be the version the chain produces. If they can drift, a
// file is written claiming a version no migration path reaches, and every later upgrade either
// skips a step or refuses the file.
func TestTheShippedChainIsWellFormedAndMatchesTheSchemaVersion(t *testing.T) {
	chain := seitoml.Migrations()

	if err := seitoml.ValidateChain(chain); err != nil {
		t.Errorf("the shipped chain is not well formed: %v", err)
	}
	if got := seitoml.CurrentVersion(); got != seitoml.SchemaVersion {
		t.Errorf("the chain produces version %d and this binary writes %d. Adding a migration "+
			"without raising SchemaVersion writes files claiming a version no path reaches",
			got, seitoml.SchemaVersion)
	}
	if seitoml.SchemaVersion != len(chain)+1 {
		t.Errorf("SchemaVersion is %d with %d migrations. The first version needs no migration, so "+
			"the two are one apart", seitoml.SchemaVersion, len(chain))
	}
}

// TestEveryShippedMigrationMatchesItsRecordedResult is what makes a shipped migration frozen.
//
// Every node running a given release must transform its configuration the same way. Editing a
// migration that has already shipped would leave two nodes agreeing on their schema version and
// disagreeing on their contents, and no ordinary test would notice.
//
// So each migration's result on its own fixture is hashed and recorded here. Editing what a
// migration does moves its hash, and the only honest way to satisfy this again is to append a new
// migration instead. A reviewer sees which shipped step changed rather than being asked to spot an
// edit inside a function body.
//
// The table is empty because no migration has shipped. The first entry is added in the same change
// as the first migration.
func TestEveryShippedMigrationMatchesItsRecordedResult(t *testing.T) {
	recorded := map[int]string{}

	for _, m := range seitoml.Migrations() {
		t.Run(fmt.Sprintf("to-v%d", m.To), func(t *testing.T) {
			got, err := fingerprintMigration(m)
			if err != nil {
				t.Fatalf("applying the migration to its own fixture failed: %v", err)
			}
			want, ok := recorded[m.To]
			if !ok {
				t.Fatalf("the migration to version %d has no recorded result. Add\n\n\t%d: %q,\n\n"+
					"to the table in this test, in the same change that adds the migration, so a later "+
					"edit to it cannot pass unnoticed", m.To, m.To, got)
			}
			if got != want {
				t.Errorf("the migration to version %d no longer produces its recorded result "+
					"(%s, recorded %s).\n\nThis migration has shipped. Nodes that already ran it hold "+
					"the old result, so changing it now means two nodes agree on their schema version "+
					"and disagree on their contents. Append a new migration instead.",
					m.To, got[:12], want[:12])
			}
		})
	}

	// The table may not name a migration the chain does not have, or a removed migration would
	// leave a row that silently passes forever.
	for to := range recorded {
		if to > seitoml.CurrentVersion() {
			t.Errorf("the table records version %d, which the chain does not produce. A row for a "+
				"migration that was removed passes forever and hides the removal", to)
		}
	}
}

// TestTheFingerprintMovesWhenAMigrationChanges is what keeps the record above meaningful.
//
// An empty chain makes that test vacuous: it would pass against a fingerprint that returned a
// constant. This drives the same function with a migration whose behaviour is altered, and requires
// the answer to move.
func TestTheFingerprintMovesWhenAMigrationChanges(t *testing.T) {
	chain := renameChain()

	base, err := fingerprintMigration(chain[0])
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	same, err := fingerprintMigration(renameChain()[0])
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if base != same {
		t.Errorf("the same migration fingerprinted differently, %s then %s, so the record would fail "+
			"on every unrelated commit", base[:12], same[:12])
	}

	altered := chain[0]
	altered.Apply = func(f *seitoml.File) error {
		return f.Set("probe.worker_count", 99) // a different result for the same fixture
	}
	moved, err := fingerprintMigration(altered)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if moved == base {
		t.Errorf("changing what a migration does left its fingerprint at %s, so editing a shipped "+
			"migration would pass unnoticed", base[:12])
	}
}

// fingerprintMigration applies a migration to its own fixture and hashes the result.
//
// The rendered file is what is hashed, not the function, because what must not change is the
// transformation an operator's file undergoes.
func fingerprintMigration(m seitoml.Migration) (string, error) {
	file, err := seitoml.Parse(strings.NewReader(m.Fixture))
	if err != nil {
		return "", fmt.Errorf("its fixture does not parse: %w", err)
	}
	if m.Apply == nil {
		return "", fmt.Errorf("it has no Apply")
	}
	if err := m.Apply(file); err != nil {
		return "", err
	}
	raw, err := file.Bytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
