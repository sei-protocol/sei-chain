package bootstrap

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

func evmNonceKey(addr byte) []byte {
	return append([]byte{0x0a}, append([]byte{addr}, make([]byte, 19)...)...)
}

func evmNonce(value byte) []byte {
	return append(make([]byte, 7), value)
}

func evmBlock(addr, nonce byte) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{{
		Name: evm.EVMStoreKey,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{{Key: evmNonceKey(addr), Value: evmNonce(nonce)}},
		},
	}}
}

func commitBlocks(t *testing.T, manager *GigaStorageManager, through byte) {
	t.Helper()
	for block := byte(1); block <= through; block++ {
		require.NoError(t, manager.StateDB().CommitStateChanges(int64(block), evmBlock(block, block)))
	}
}

func writeWALOnly(t *testing.T, wal statewal.StateWAL, block uint64, changesets []*proto.NamedChangeSet) {
	t.Helper()
	require.NoError(t, wal.Write(block, changesets))
	require.NoError(t, wal.SignalEndOfBlock())
	require.NoError(t, wal.Flush())
}

func applySSThrough(t *testing.T, manager *GigaStorageManager, through byte) {
	t.Helper()
	for block := byte(1); block <= through; block++ {
		require.NoError(t, manager.SS().ApplyChangesetSync(int64(block), evmBlock(block, block)))
	}
}

func snapshotSSAt(t *testing.T, manager *GigaStorageManager, height int64) {
	t.Helper()
	manager.SS().SetCheckpointScheduler(controller.NewCheckpointScheduler(config.CheckpointConfig{BlockInterval: 1}))
	manager.SS().ScheduleSnapshot(height)
	require.Eventually(t, func() bool { return manager.SS().Snapshots().Newest() >= height },
		10*time.Second, 10*time.Millisecond, "the snapshot a rollback restores from must be published")
}

func requireWALTail(t *testing.T, manager *GigaStorageManager, want uint64) {
	t.Helper()
	stored, _, last, err := manager.StateWAL().GetStoredRange()
	require.NoError(t, err)
	if want == 0 {
		require.False(t, stored)
		return
	}
	require.True(t, stored)
	require.Equal(t, want, last)
}

func TestFindTargetRecoveryHeightIsZeroWithoutABlockLedger(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)
	require.NoError(t, manager.ReceiptDB().SetLatestVersion(3))
	require.NoError(t, manager.StateWAL().Close())
	manager.stateWAL = nil

	got, err := manager.findTargetRecoveryHeight()
	require.NoError(t, err)
	require.Equal(t, int64(0), got)
}

func TestTruncateStateWALDropsBlocksAboveTheTarget(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 5)
	require.NoError(t, manager.StateWAL().Close())
	manager.stateWAL = nil

	require.NoError(t, manager.truncateStateWAL(3))

	require.NoError(t, manager.openStateWal())
	requireWALTail(t, manager, 3)
	require.NoError(t, manager.StateWAL().Write(4, evmBlock(4, 4)))
}

func TestRecoverSCReplaysAMissedWALBlock(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 2)
	writeWALOnly(t, manager.StateWAL(), 3, evmBlock(3, 3))
	require.Equal(t, int64(2), manager.SC().Version())

	require.NoError(t, manager.recoverSC(3))

	require.Equal(t, int64(3), manager.SC().Version())
}

func TestRecoverSCRollsBackToTheTarget(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)

	require.NoError(t, manager.recoverSC(2))

	require.Equal(t, int64(2), manager.SC().Version())
}

func TestRecoverSSReplaysEVMChangesets(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 2)
	require.Zero(t, manager.SS().GetLatestVersion())

	require.NoError(t, manager.recoverSS(2))

	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
	value, err := manager.SS().Get(evm.EVMStoreKey, 2, evmNonceKey(2))
	require.NoError(t, err)
	require.Equal(t, evmNonce(2), value)
}

func TestRecoverSSRollsBackToTheTarget(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)
	applySSThrough(t, manager, 1)
	snapshotSSAt(t, manager, 1)
	applySSThrough(t, manager, 3)

	require.NoError(t, manager.recoverSS(2))

	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
	above, err := manager.SS().Get(evm.EVMStoreKey, 3, evmNonceKey(3))
	require.NoError(t, err)
	require.Nil(t, above)
}

func TestRecoverReceiptRewindsTheHead(t *testing.T) {
	manager, _ := openManager(t, nil)
	require.NoError(t, manager.ReceiptDB().SetLatestVersion(5))

	require.NoError(t, manager.recoverReceipt(3))

	require.Equal(t, int64(3), manager.ReceiptDB().LatestVersion())
}

func TestOpenDBWithoutRecoveryOnAFreshHome(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.Zero(t, manager.SC().Version())
	require.Zero(t, manager.SS().GetLatestVersion())
	require.Zero(t, manager.ReceiptDB().LatestVersion())
}
