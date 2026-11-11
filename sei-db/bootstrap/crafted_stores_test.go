package bootstrap

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/bud"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/gigareceipt"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	autobahn "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

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

func evmNonceKey(addr byte) []byte {
	return append([]byte{0x0a}, append([]byte{addr}, make([]byte, 19)...)...)
}

func evmNonce(value byte) []byte {
	return append(make([]byte, 7), value)
}

// evmBlock returns a block that sets the nonce of the account evmNonceKey(addr) names to nonce.
func evmBlock(addr byte, nonce byte) *gigatypes.StateChangeSet {
	return &gigatypes.StateChangeSet{Accounts: []gigatypes.AccountChange{{
		Address: gigatypes.Address{addr},
		Account: gigatypes.AccountState{Nonce: uint64(nonce)},
	}}}
}

// testBlockHash is the block hash execution would report for height. It is a function of the height
// alone, so a block re-executed after a restart reports the same hash the vault already stored.
func testBlockHash(height uint64) [32]byte {
	var blockHash [32]byte
	binary.BigEndian.PutUint64(blockHash[:8], height)
	return blockHash
}

// writeBlockLedger finalizes blocks 1 through through in cfg's block store.
func writeBlockLedger(t *testing.T, cfg *config.GigaStorageConfig, through uint64) {
	t.Helper()
	blockStore, err := blockstore.New(cfg.BlockDBConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, blockStore.Close()) }()
	rng := utils.TestRng()
	for index := autobahn.RoadIndex(1); index <= autobahn.RoadIndex(through); index++ {
		block := autobahn.GenBlock(rng)
		qc := autobahn.GenFullCommitQCAt(rng, index, 1)
		qc = autobahn.NewFullCommitQC(qc.QC(), []*autobahn.BlockHeader{block.Header()})
		require.NoError(t, blockStore.WriteBlock(autobahn.NewRoadBlock(qc, []*autobahn.Block{block})))
	}
}

// openStandaloneStateDB opens cfg's state WAL, live state DB and historical state DB outside any storage manager,
// with a BUD hasher that discards what it is given.
func openStandaloneStateDB(t *testing.T, cfg *config.GigaStorageConfig) *giga.StateDB {
	t.Helper()
	stateDB, err := giga.NewStateDB(
		t.Context(), cfg.FlatKVConfig, cfg.SSConfig, cfg.CheckpointConfig, bud.NewNoOpBUDHasher())
	require.NoError(t, err)
	return stateDB
}

// commitStateBlocks commits evmBlock(block, block) to stateDB for blocks from through through.
func commitStateBlocks(t *testing.T, stateDB *giga.StateDB, from byte, through byte) {
	t.Helper()
	for block := from; block <= through; block++ {
		require.NoError(t, stateDB.CommitStateChanges(int64(block), evmBlock(block, block)))
	}
}

// writeState commits blocks 1 through through to cfg's state WAL, live state DB and historical state DB.
func writeState(t *testing.T, cfg *config.GigaStorageConfig, through byte) {
	t.Helper()
	stateDB := openStandaloneStateDB(t, cfg)
	commitStateBlocks(t, stateDB, 1, through)
	require.NoError(t, stateDB.Close())
}

// snapshotLiveStateDBAt commits block height so the live state DB snapshots it, leaving a snapshot a later
// rollback can rewind to. A BlockInterval of 1 makes every offered version a boundary, and the snapshot is written
// off the commit path, so it has to be waited for.
//
// The blocks committed before this are flushed through the writer first. A block is offered to the
// writer on commit but asked about on the writer's goroutine, against whichever schedule is installed
// when it gets there, so one still queued when the every-block schedule goes in would be snapshotted
// too.
func snapshotLiveStateDBAt(t *testing.T, stateDB *giga.StateDB, height byte) {
	t.Helper()
	require.NoError(t, stateDB.SC().FlushSnapshots())
	stateDB.SC().SetCheckpointScheduler(controller.NewCheckpointScheduler(
		config.CheckpointConfig{BlockInterval: 1}))
	require.NoError(t, stateDB.CommitStateChanges(int64(height), evmBlock(height, height)))
	require.NoError(t, stateDB.SC().FlushSnapshots())
}

// stopSnapshotting gives stateDB's live state DB a schedule that takes no snapshot in any test.
func stopSnapshotting(stateDB *giga.StateDB) {
	stateDB.SC().SetCheckpointScheduler(controller.NewCheckpointScheduler(
		config.CheckpointConfig{BlockInterval: 1_000_000}))
}

// liveStateDBSnapshotDir returns where the live state DB keeps its snapshot of version under dataDir.
func liveStateDBSnapshotDir(dataDir string, version int64) string {
	return filepath.Join(dataDir, fmt.Sprintf("snapshot-%020d", version))
}

// writeWALOnly writes block to the state WAL alone, converting changes as though no key they write existed
// before the block.
func writeWALOnly(t *testing.T, wal statewal.StateWAL, block uint64, changes *gigatypes.StateChangeSet) {
	t.Helper()
	//nolint:gosec // small test heights
	mutations, err := flatkv.MutationsFromStateChangeSet(changes, block, emptyStateView{height: int64(block) - 1},
		flatkvconfig.DefaultConfig())
	require.NoError(t, err)
	require.NoError(t, wal.Write(block, mutations))
	require.NoError(t, wal.Flush())
}

// replaceWALWithBlocks empties cfg's state WAL and writes blocks from through through, so the WAL's first
// block can sit above 1. The state DB must be closed.
func replaceWALWithBlocks(t *testing.T, cfg *config.GigaStorageConfig, from byte, through byte) {
	t.Helper()
	truncateWAL(t, cfg, 0)
	wal, err := flatkv.OpenStateWAL(cfg.FlatKVConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, wal.Close()) }()
	for block := from; block <= through; block++ {
		writeWALOnly(t, wal, uint64(block), evmBlock(block, block))
	}
}

// truncateWAL drops every block above last from cfg's state WAL, as losing its unflushed tail does. The state DB
// must be closed.
func truncateWAL(t *testing.T, cfg *config.GigaStorageConfig, last uint64) {
	t.Helper()
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), last))
}

// writeReceipts writes one receipt per block into cfg's receipt store, for blocks 1 through through.
//
// A rollback refuses a store whose index promises receipts its bodies do not have, so a test that rolls
// receipts back has to write bodies rather than only stamp a head.
func writeReceipts(t *testing.T, cfg *config.GigaStorageConfig, through uint64) {
	t.Helper()
	receiptDB, err := gigareceipt.NewReceiptStore(cfg.ReceiptDBConfig, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, receiptDB.Close()) }()
	storeKey := storetypes.NewKVStoreKey("evm")
	ctx := testutil.DefaultContext(storeKey, storetypes.NewTransientStoreKey("evm_transient"))
	for block := uint64(1); block <= through; block++ {
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		records := []receipt.ReceiptRecord{{
			TxHash: txHash,
			Receipt: &evmtypes.Receipt{
				TxHashHex:   txHash.Hex(),
				BlockNumber: block,
				GasUsed:     21000,
			},
		}}
		//nolint:gosec // small test heights
		require.NoError(t, receiptDB.SetReceipts(ctx.WithBlockHeight(int64(block)), records))
	}
}

// canonicalStateHashes returns the live state DB's state hash after each of blocks 1 through through, as committing
// evmBlock(block, block) for each of them leaves it. Index i holds the hash of block i+1.
func canonicalStateHashes(t *testing.T, through byte) [][32]byte {
	t.Helper()
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)
	disableSS(cfg)
	stateDB := openStandaloneStateDB(t, cfg)
	hashes := make([][32]byte, 0, through)
	_, err = stateDB.RegisterHashListener(func(_ context.Context, _ int64, hash *lthash.BlockHash) error {
		hashes = append(hashes, hash.Global.Checksum())
		return nil
	})
	require.NoError(t, err)
	commitStateBlocks(t, stateDB, 1, through)
	require.NoError(t, stateDB.SC().FlushHashes())
	require.NoError(t, stateDB.Close())
	return hashes
}

// writeAppHashes stores app hashes for blocks 1 through through in cfg's app hash vault, each carrying the state
// hash committing evmBlock(block, block) for every block up to it leaves.
func writeAppHashes(t *testing.T, cfg *config.GigaStorageConfig, through byte) {
	t.Helper()
	appendAppHashes(t, cfg, canonicalStateHashes(t, through))
}

// appendAppHashes stores an app hash for each of blocks 1 through len(stateHashes) in cfg's app hash vault. Block
// i+1's record carries stateHashes[i]; every other hash it holds is a function of its height alone.
func appendAppHashes(t *testing.T, cfg *config.GigaStorageConfig, stateHashes [][32]byte) {
	t.Helper()
	appHashConfig := cfg.AppHashBuilderConfig
	hashVault, err := vault.NewStandardHashVault(
		appHashConfig.HashVaultConfig, appHashConfig.ChainID, appHashConfig.GigaActivationHeight)
	require.NoError(t, err)
	defer func() { require.NoError(t, hashVault.Close()) }()
	records := make([]*apphash.AppHashData, 0, len(stateHashes))
	var previous [32]byte
	for i, stateHash := range stateHashes {
		height := uint64(i) + 1 //nolint:gosec // small test heights
		record := apphash.NewAppHashData(appHashConfig.ChainID, height, testBlockHash(height), stateHash,
			apphash.BUDHash{}, apphash.ReceiptHash{}, previous)
		records = append(records, record)
		previous = record.AppHash()
	}
	require.NoError(t, hashVault.Append(records))
}

// writeNode leaves every store of cfg at block through, as a node that stopped cleanly there does.
func writeNode(t *testing.T, cfg *config.GigaStorageConfig, through byte) {
	t.Helper()
	writeBlockLedger(t, cfg, uint64(through))
	writeState(t, cfg, through)
	if cfg.ReceiptDBConfig.Enable {
		writeReceipts(t, cfg, uint64(through))
	}
	writeAppHashes(t, cfg, through)
}
