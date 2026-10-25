package gigareceipt_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/gigareceipt"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	autobahn "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmutils "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
)

var (
	startupAddr  = common.HexToAddress("0x5ea7")
	startupTopic = common.HexToHash("0xbeef")
)

// nonceChange is a one-key EVM state change for block, distinct per block.
func nonceChange(block uint64) []*proto.NamedChangeSet {
	key := append([]byte{0x0a, byte(block)}, make([]byte, 19)...)
	value := append(make([]byte, 7), byte(block))
	return []*proto.NamedChangeSet{{
		Name:      evm.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: key, Value: value}}},
	}}
}

// writeBlockLedger finalizes blocks [1, through] in the block store under one synthetic QC.
func writeBlockLedger(t *testing.T, manager *bootstrap.GigaStorageManager, through uint64) {
	t.Helper()
	rng := tmutils.TestRng()
	next := autobahn.GlobalBlockNumber(through + 1)
	qc := autobahn.GenFullCommitQCRange(rng, 1, next)
	blocks := make([]*autobahn.Block, 0, through)
	headers := make([]*autobahn.BlockHeader, 0, through)
	for n := autobahn.GlobalBlockNumber(1); n < next; n++ {
		block := autobahn.GenBlock(rng)
		blocks = append(blocks, block)
		headers = append(headers, block.Header())
	}
	qc = autobahn.NewFullCommitQC(qc.QC(), headers)
	require.NoError(t, manager.BlockStore().WriteQC(qc, blocks))
}

// executeBlocks commits state and receipts for blocks [from, through], as executing them would.
func executeBlocks(t *testing.T, manager *bootstrap.GigaStorageManager, ctx sdk.Context, from, through uint64) {
	t.Helper()
	for block := from; block <= through; block++ {
		height := int64(block) //nolint:gosec // small test heights
		require.NoError(t, manager.StateDB().CommitStateChanges(height, nonceChange(block)))
		require.NoError(t, manager.ReceiptDB().SetReceipts(ctx.WithBlockHeight(height),
			[]receipt.ReceiptRecord{litReceipt(block, 0, startupAddr, startupTopic)}))
	}
	require.Eventually(t, func() bool {
		return manager.ReceiptDB().LatestVersion() == int64(through) //nolint:gosec // small test heights
	}, 5*time.Second, time.Millisecond)
}

// requireEveryReceiptOnce checks that every block in [1, through] has block stats and is returned exactly
// once by GetReceipt, FilterLogs and IterateReceipts.
func requireEveryReceiptOnce(t *testing.T, store receipt.ReceiptStore, ctx sdk.Context, through uint64) {
	t.Helper()
	var want []uint64
	for block := uint64(1); block <= through; block++ {
		want = append(want, block)
		got, err := store.GetReceipt(ctx, litTxHash(block, 0))
		require.NoErrorf(t, err, "block %d", block)
		require.Equal(t, block, got.BlockNumber)
		_, err = store.GetBlockStats(ctx, block)
		require.NoErrorf(t, err, "block %d's stats", block)
	}

	logs, err := store.FilterLogs(ctx, 1, through,
		filters.FilterCriteria{Addresses: []common.Address{startupAddr}}, nil)
	require.NoError(t, err)
	logBlocks := make([]uint64, 0, len(logs))
	for _, lg := range logs {
		logBlocks = append(logBlocks, lg.BlockNumber)
	}
	require.Equal(t, want, logBlocks)

	it, err := store.IterateReceipts(1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()
	var walked []uint64
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		walked = append(walked, it.BlockNumber())
	}
	require.Equal(t, want, walked, "a walk must visit every block once, in order")
}

// After a crash leaves litt and the index at different heads, startup recovery rolls the node's state
// back to the lower head, and replaying the lost blocks stores every receipt exactly once.
func TestStartupRecoveryRollsBackToTheLowerReceiptHead(t *testing.T) {
	for _, tc := range []struct {
		name  string
		crash func(dir string, keep uint64) error
	}{
		{name: "index behind litt", crash: gigareceipt.LoseIndexAbove},
		{name: "litt behind the index", crash: gigareceipt.LoseLittAbove},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
			require.NoError(t, err)
			cfg.SSConfig.Enable = false
			storeKey := storetypes.NewKVStoreKey("evm")
			ctx := testutil.DefaultContext(storeKey, storetypes.NewTransientStoreKey("evm_transient"))

			manager, err := bootstrap.NewGigaStorageManager(t.Context(), cfg)
			require.NoError(t, err)
			writeBlockLedger(t, manager, 6)
			executeBlocks(t, manager, ctx, 1, 6)
			require.NoError(t, manager.Close())

			require.NoError(t, tc.crash(cfg.ReceiptDBConfig.DBDirectory, 4))

			manager, err = bootstrap.NewGigaStorageManager(t.Context(), cfg)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, manager.Close()) })

			blockHead, err := manager.BlockStore().GetLatestBlock()
			require.NoError(t, err)
			require.Equal(t, uint64(6), blockHead, "recovery never cuts the block store")
			require.Equal(t, int64(4), manager.ReceiptDB().LatestVersion())
			require.Equal(t, int64(4), manager.SC().Version(), "state must follow the receipt store back")

			executeBlocks(t, manager, ctx, 5, 6)
			requireEveryReceiptOnce(t, manager.ReceiptDB(), ctx, 6)
		})
	}
}
