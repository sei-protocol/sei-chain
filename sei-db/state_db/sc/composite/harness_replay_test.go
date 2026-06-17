package composite

// FlatKV archive-validation harness (Arm A) — replayer.
// Drives a corpus through the *real* composite/migration machinery: writes the boundary state in
// MemiavlOnly, flips to MigrateEVM at the corpus's fixed schedule, then replays one batch per block.
// The same changesets feed a storeOracle so the existing verifyOracle battery checks read-routing,
// and the oracle's final fold is cross-checked against corpus-gen's independent expected_state.
// Test-only; tracks PLT-680.

import (
	"encoding/hex"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/stretchr/testify/require"
)

// boundaryChangeSet wraps the corpus boundary (state at H) as an EVM-store changeset.
// Returns nil when the corpus declares no boundary state.
func (c *harnessCorpus) boundaryChangeSet() (*proto.NamedChangeSet, error) {
	if len(c.Boundary) == 0 {
		return nil, nil
	}
	blk := harnessBlock{}
	blk.NamedChangeSet.Name = keys.EVMStoreKey
	blk.NamedChangeSet.Pairs = c.Boundary
	return blk.toNamedChangeSet()
}

// replayCorpus runs the corpus through real migration machinery and returns the live store plus the
// oracle that mirrors every applied changeset. The caller owns Close. The store is left mid- or
// post-migration depending on whether the schedule drained every key within len(Blocks) commits.
func replayCorpus(t *testing.T, c *harnessCorpus) (*CompositeCommitStore, *storeOracle) {
	t.Helper()
	dir := t.TempDir()
	oracle := newStoreOracle()

	// Pre-migration boundary state, written in MemiavlOnly (the predecessor mode).
	memCfg := config.DefaultStateCommitConfig()
	memCfg.WriteMode = config.MemiavlOnly
	memCfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cs, err := NewCompositeCommitStore(t.Context(), dir, memCfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	boundary, err := c.boundaryChangeSet()
	require.NoError(t, err)
	if boundary != nil {
		ncs := []*proto.NamedChangeSet{boundary}
		require.NoError(t, cs.ApplyChangeSets(ncs))
		oracle.apply(ncs)
		_, err = cs.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs.Close())

	// Flip to MigrateEVM and replay the corpus at its declared schedule: exactly one
	// ApplyChangeSets+Commit per block so the migration boundary advances once per height.
	cs = reopenInMigrateEVM(t, dir, c.Manifest.Schedule.KeysToMigratePerBlock)
	for _, blk := range c.Blocks {
		named, err := blk.toNamedChangeSet()
		require.NoError(t, err)
		ncs := []*proto.NamedChangeSet{named}
		require.NoError(t, cs.ApplyChangeSets(ncs))
		oracle.apply(ncs)
		_, err = cs.Commit()
		require.NoError(t, err)
	}
	return cs, oracle
}

// requireOracleMatchesExpected cross-checks the replay's logical fold against corpus-gen's
// independent expected_state (the v0 TRUTH). Two implementations (Go reader+oracle, Python
// generator) agreeing on the live EVM state catches a reader bug or a generator bug.
//
// Caveat: this assumes the corpus removes keys via Delete (the oracle's contract). A corpus that
// encodes a net-zero SSTORE as a zero VALUE (the zero-prune convention) needs the migration's
// prune pass before this holds — that is the surface-#2 increment, not exercised here.
func requireOracleMatchesExpected(t *testing.T, oracle *storeOracle, c *harnessCorpus) {
	t.Helper()
	live := oracle.stores[keys.EVMStoreKey]
	require.Len(t, live, len(c.Assertions.ExpectedState),
		"replayed live-key count must match corpus-gen expected_state")
	for hexKey, hexVal := range c.Assertions.ExpectedState {
		key, err := hex.DecodeString(hexKey)
		require.NoError(t, err)
		want, err := hex.DecodeString(hexVal)
		require.NoError(t, err)
		gotVal, ok := live[string(key)]
		require.Truef(t, ok, "expected_state key %s absent from replayed fold", hexKey)
		require.Equalf(t, want, gotVal, "expected_state value mismatch for key %s", hexKey)
	}
}

// TestHarness_ReplayWindowStraddling is the M2 smoke driver: replay a risk-shaped corpus through
// real migration machinery and render the truth + consistency axes. window_straddling is the
// merged-iterator / moving-boundary surface (HLD §2.6) with no zero-prune or delete edges, so the
// store, the oracle, and corpus-gen's expected_state agree without a prune pass.
func TestHarness_ReplayWindowStraddling(t *testing.T) {
	c, err := loadHarnessCorpus("testdata/flatkv-corpus/window_straddling-1")
	require.NoError(t, err)
	require.Equal(t, "window_straddling", c.Manifest.Scenario)

	cs, oracle := replayCorpus(t, c)
	defer func() { _ = cs.Close() }()

	// TRUTH (routing): every composite read matches the fold of the same changesets.
	verifyOracle(t, cs, oracle)
	// TRUTH (independent): the fold equals corpus-gen's v0 expected_state.
	requireOracleMatchesExpected(t, oracle, c)
	// CONSISTENCY: on-disk flatkv content matches its own committed lattice root.
	require.NoError(t, flatkv.VerifyLtHash(cs.flatKV))
}
