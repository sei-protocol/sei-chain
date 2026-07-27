package types_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// genesis.json is the one configuration file the node never writes and always reads,
// and its reader mutates what it read. ValidateAndComplete fills in a zero
// genesis_time with time.Now(), so a genesis document that omits the field is not
// reproducible: two nodes reading the same bytes get different chains.
//
// That is why the differential harness compares resolved views rather than file bytes,
// and why "the same genesis" is not a statement about a file.

// FuzzGenesisDocFromJSONTimeCompletion pins the completion rule: a zero genesis_time is
// replaced, a non-zero one is preserved verbatim.
//
// The replacement is the sharp part. It happens inside a function named for validation,
// so a caller that reads genesis to inspect it also silently stamps it, and nothing in
// the returned document says the time was invented.
func FuzzGenesisDocFromJSONTimeCompletion(f *testing.F) {
	// The completion branch needs a time that is actually zero, which is year 1 Jan 1 UTC
	// rather than the Unix epoch. Without this seed the target only ever exercised the
	// preservation half.
	f.Add(int64(-62135596800)) // the zero time: completed with the current time
	f.Add(int64(0))            // the Unix epoch, which is not the zero time: preserved
	f.Add(int64(1))
	f.Add(int64(1700000000))
	f.Add(int64(-1))

	f.Fuzz(func(t *testing.T, unixSeconds int64) {
		// Keep to times the JSON round-trip can express.
		if unixSeconds < -62135596800 || unixSeconds > 253402300799 {
			return
		}
		genesisTime := time.Unix(unixSeconds, 0).UTC()

		doc := map[string]any{
			"chain_id":     "sei-test",
			"genesis_time": genesisTime.Format(time.RFC3339Nano),
			"validators":   []any{},
			"app_hash":     "",
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}

		before := time.Now()
		got, err := types.GenesisDocFromJSON(raw)
		after := time.Now()
		// Every document this target builds is well-formed for the reader: a valid
		// chain_id, a time already clamped to the representable range, empty validators
		// and app_hash. So a rejection is a regression rather than a legitimate outcome,
		// and returning early on one would let a reader that rejected everything pass.
		if err != nil {
			t.Fatalf("the fixture is a well-formed genesis document and must be accepted, got %v", err)
		}

		if genesisTime.IsZero() {
			if got.GenesisTime.Before(before) || got.GenesisTime.After(after) {
				t.Fatalf("a zero genesis_time must be completed with the current time, got %v",
					got.GenesisTime)
			}
			return
		}
		if !got.GenesisTime.Equal(genesisTime) {
			t.Fatalf("genesis_time = %v was rewritten to %v; a non-zero time must be preserved",
				genesisTime, got.GenesisTime)
		}
	})
}

// TestGenesisDocWithoutTimeIsNotReproducible pins the consequence directly: the same
// bytes, read twice, yield two different chains.
//
// This is the reason generated-file comparison is not a valid equivalence check between
// two config managers, and the reason a genesis without an explicit time is a latent
// chain-identity bug rather than a formatting preference.
func TestGenesisDocWithoutTimeIsNotReproducible(t *testing.T) {
	raw := []byte(`{"chain_id":"sei-test","validators":[],"app_hash":""}`)

	// Bracketing each read rather than sleeping between them: the repo's conventions ask
	// tests not to sleep, and the interval assertion is stronger anyway because it does
	// not lean on clock resolution to make the two values differ.
	beforeFirst := time.Now()
	first, err := types.GenesisDocFromJSON(raw)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	afterFirst := time.Now()

	second, err := types.GenesisDocFromJSON(raw)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	afterSecond := time.Now()

	if first.GenesisTime.Before(beforeFirst) || first.GenesisTime.After(afterFirst) {
		t.Fatalf("the first read's completed time %v falls outside the interval it ran in",
			first.GenesisTime)
	}
	if second.GenesisTime.Before(beforeFirst) || second.GenesisTime.After(afterSecond) {
		t.Fatalf("the second read's completed time %v falls outside the interval it ran in",
			second.GenesisTime)
	}

	if first.GenesisTime.Equal(second.GenesisTime) {
		t.Fatalf("two reads produced the same genesis_time (%v). A deterministic completion is a "+
			"real improvement, and it changes whether generated genesis bytes are comparable "+
			"between nodes, so it gets recorded here rather than skipped past", first.GenesisTime)
	}
	if first.GenesisTime.IsZero() {
		t.Fatal("an absent genesis_time must be completed, not left zero")
	}
}

// TestGenesisDocFromFileRequiresTheFile pins the file-level failure: an absent
// genesis.json is an error naming the path, not an empty document. The node's
// unconditional read of this file on every start is why deleting it after a sync bricks
// startup.
func TestGenesisDocFromFileRequiresTheFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "genesis.json")
	if _, err := types.GenesisDocFromFile(missing); err == nil {
		t.Fatal("an absent genesis.json must be an error, not an empty document")
	}
}

// TestGenesisDocFromFileRejectsMalformedJSON pins that a corrupt file fails at read
// rather than yielding a partially-populated document a caller might use.
func TestGenesisDocFromFileRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "genesis.json")
	if err := os.WriteFile(path, []byte(`{"chain_id":`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := types.GenesisDocFromFile(path); err == nil {
		t.Fatal("a truncated genesis.json must be rejected")
	}
}

// TestGenesisDocRequiresAChainID pins the one field the reader will not invent. Unlike
// genesis_time, an absent chain_id is refused — so the completion behavior is
// per-field rather than a general policy.
func TestGenesisDocRequiresAChainID(t *testing.T) {
	if _, err := types.GenesisDocFromJSON([]byte(`{"validators":[],"app_hash":""}`)); err == nil {
		t.Fatal("an absent chain_id must be refused; only genesis_time is completed")
	}
}
