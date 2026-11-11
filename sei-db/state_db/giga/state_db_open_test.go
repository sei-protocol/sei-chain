package giga

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// commitNonceBlocks commits blocks from through through to db, each setting a new account's nonce.
func commitNonceBlocks(t *testing.T, db *StateDB, from int64, through int64) {
	t.Helper()
	for block := from; block <= through; block++ {
		require.NoError(t, db.CommitStateChanges(block, nonceChanges(common.Address{byte(block)}, 0, 1)))
	}
}

// testCheckpointConfig is the snapshot schedule the stores in these tests open and roll back on.
var testCheckpointConfig = config.CheckpointConfig{BlockInterval: 1_000}

var _ gigatypes.StateView = emptyStateView{}

// emptyStateView is a StateView holding no keys. Only the raw reads are supported.
type emptyStateView struct {
	// Left nil, so every read other than the raw reads panics.
	gigatypes.StateView

	// The block height the view reports.
	height int64
}

func (v emptyStateView) GetBlockHeight() int64 { return v.height }

func (v emptyStateView) BatchGetRaw([][]byte) map[string][]byte { return map[string][]byte{} }

func (v emptyStateView) Close() {}

// writeWALOnly writes block to db's state WAL alone, setting addr's nonce as though addr did not exist before the
// block.
func writeWALOnly(t *testing.T, db *StateDB, block uint64, addr common.Address, nonce uint64) {
	t.Helper()
	//nolint:gosec // small test heights
	prior := emptyStateView{height: int64(block) - 1}
	mutations, err := flatkv.MutationsFromStateChangeSet(nonceChanges(addr, 0, nonce), block, prior,
		flatkvconfig.DefaultConfig())
	require.NoError(t, err)
	require.NoError(t, db.WAL().Write(block, mutations))
	require.NoError(t, db.WAL().Flush())
}

// truncateWAL drops every block above last from home's state WAL, as losing its unflushed tail does. The StateDB
// must be closed.
func truncateWAL(t *testing.T, home undoTestHome, last uint64) {
	t.Helper()
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(home.flatkv.DataDir), last))
}

// An open refuses a live state DB behind the WAL's head rather than replaying it there, which is
// flatkv.RollBackLiveStateDB()'s job.
func TestOpenRefusesALiveStateDBBehindTheWALHead(t *testing.T) {
	home := newUndoTestHome(t)
	db := home.open(t)
	commitNonceBlocks(t, db, 1, 2)
	writeWALOnly(t, db, 3, common.Address{3}, 1)
	require.NoError(t, db.Close())

	_, err := NewStateDB(t.Context(), home.flatkv, home.ss, testCheckpointConfig, newDiscardingBUDHasher(t))

	require.ErrorContains(t, err, "the live state DB is at 2 but the state WAL ends at 3")
}

// An open refuses a live state DB above the WAL's head rather than discarding the state above it.
func TestOpenRefusesALiveStateDBAboveTheWALHead(t *testing.T) {
	home := newUndoTestHome(t)
	db := home.open(t)
	commitNonceBlocks(t, db, 1, 3)
	require.NoError(t, db.Close())
	truncateWAL(t, home, 2)

	_, err := NewStateDB(t.Context(), home.flatkv, home.ss, testCheckpointConfig, newDiscardingBUDHasher(t))

	require.ErrorContains(t, err, "the live state DB is at 3 but the state WAL ends at 2")
}
