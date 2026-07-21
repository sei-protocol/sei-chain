package receipt_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// The backends under comparison, all sharing the exact log filter semantics
// so they run the same test suite: littdb (LittDB bodies + per-block blooms),
// littidx (LittDB bodies + exact per-tag pebble index), pebblev3 (inline
// pebble values + blooms), and pebbleidx (inline pebble values + per-tag
// index).
var receiptBackends = []string{"littdb", "littidx", "pebblev3", "pebbleidx"}

func setupReceiptBackend(t *testing.T, backend, dir string) (receipt.ReceiptStore, sdk.Context) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = backend
	cfg.DBDirectory = dir
	cfg.KeepRecent = 0
	store, err := receipt.NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	return store, ctx
}

func backendTxHash(block uint64, txIndex uint32) common.Hash {
	var h common.Hash
	copy(h[:], fmt.Sprintf("tx-%d-%d", block, txIndex))
	return h
}

// makeBackendReceipt builds a receipt with one log carrying the given
// address and topics.
func makeBackendReceipt(block uint64, txIndex uint32, addr common.Address, topics ...common.Hash) receipt.ReceiptRecord {
	topicHex := make([]string, 0, len(topics))
	for _, topic := range topics {
		topicHex = append(topicHex, topic.Hex())
	}
	txHash := backendTxHash(block, txIndex)
	return receipt.ReceiptRecord{
		TxHash: txHash,
		Receipt: &types.Receipt{
			TxHashHex:        txHash.Hex(),
			BlockNumber:      block,
			TransactionIndex: txIndex,
			GasUsed:          21000,
			Logs: []*types.Log{
				{
					Address: addr.Hex(),
					Topics:  topicHex,
					Data:    []byte{0xde, 0xad},
					Index:   0,
				},
			},
		},
	}
}

func writeBackendBlock(t *testing.T, store receipt.ReceiptStore, ctx sdk.Context, block uint64, records ...receipt.ReceiptRecord) {
	t.Helper()
	blockCtx := ctx.WithBlockHeight(int64(block)) //nolint:gosec // test block heights are small
	require.NoError(t, store.SetReceipts(blockCtx, records))
}

func TestReceiptBackendReadWrite(t *testing.T) {
	for _, backend := range receiptBackends {
		t.Run(backend, func(t *testing.T) {
			store, ctx := setupReceiptBackend(t, backend, t.TempDir())
			defer func() { _ = store.Close() }()

			addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
			addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
			topicA := common.HexToHash("0xaaaa")
			topicB := common.HexToHash("0xbbbb")

			writeBackendBlock(t, store, ctx, 1,
				makeBackendReceipt(1, 0, addr1, topicA),
				makeBackendReceipt(1, 1, addr2, topicB),
			)
			writeBackendBlock(t, store, ctx, 2,
				makeBackendReceipt(2, 0, addr1, topicA, topicB),
			)

			require.Equal(t, int64(2), store.LatestVersion())

			for _, expected := range []struct {
				block   uint64
				txIndex uint32
			}{{1, 0}, {1, 1}, {2, 0}} {
				rcpt, err := store.GetReceiptFromStore(ctx, backendTxHash(expected.block, expected.txIndex))
				require.NoError(t, err)
				require.Equal(t, expected.block, rcpt.BlockNumber)
				require.Equal(t, expected.txIndex, rcpt.TransactionIndex)
			}

			// Misses are cheap negatives, not scans or errors beyond ErrNotFound.
			_, err := store.GetReceiptFromStore(ctx, common.HexToHash("0xdeadbeef"))
			require.ErrorIs(t, err, receipt.ErrNotFound)
		})
	}
}

func TestReceiptBackendReopen(t *testing.T) {
	for _, backend := range receiptBackends {
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			store, ctx := setupReceiptBackend(t, backend, dir)

			addr := common.HexToAddress("0x3333333333333333333333333333333333333333")
			topic := common.HexToHash("0xcccc")
			for block := uint64(1); block <= 5; block++ {
				writeBackendBlock(t, store, ctx, block, makeBackendReceipt(block, 0, addr, topic))
			}
			require.NoError(t, store.Close())

			// Reopen: metadata and data must survive; the cache is cold so all
			// reads exercise the persistent backend.
			store, ctx = setupReceiptBackend(t, backend, dir)
			defer func() { _ = store.Close() }()

			require.Equal(t, int64(5), store.LatestVersion())
			for block := uint64(1); block <= 5; block++ {
				rcpt, err := store.GetReceiptFromStore(ctx, backendTxHash(block, 0))
				require.NoError(t, err)
				require.Equal(t, block, rcpt.BlockNumber)
			}

			logs, err := store.FilterLogs(ctx, 1, 5, filters.FilterCriteria{
				Addresses: []common.Address{addr},
			})
			require.NoError(t, err)
			require.Len(t, logs, 5)

			// Appends after reopen continue cleanly (littdb skips re-puts of
			// blocks it already has, new blocks land normally).
			writeBackendBlock(t, store, ctx, 6, makeBackendReceipt(6, 0, addr, topic))
			rcpt, err := store.GetReceiptFromStore(ctx, backendTxHash(6, 0))
			require.NoError(t, err)
			require.Equal(t, uint64(6), rcpt.BlockNumber)
		})
	}
}

func TestReceiptBackendFilterLogsSemantics(t *testing.T) {
	for _, backend := range receiptBackends {
		t.Run(backend, func(t *testing.T) {
			store, ctx := setupReceiptBackend(t, backend, t.TempDir())
			defer func() { _ = store.Close() }()

			addr1 := common.HexToAddress("0x4444444444444444444444444444444444444444")
			addr2 := common.HexToAddress("0x5555555555555555555555555555555555555555")
			transfer := common.HexToHash("0x1111aa")
			approve := common.HexToHash("0x2222bb")
			alice := common.HexToHash("0x3333cc")
			bob := common.HexToHash("0x4444dd")

			writeBackendBlock(t, store, ctx, 1,
				makeBackendReceipt(1, 0, addr1, transfer, alice),
				makeBackendReceipt(1, 1, addr1, transfer, bob),
			)
			writeBackendBlock(t, store, ctx, 2,
				makeBackendReceipt(2, 0, addr2, approve, alice),
			)
			writeBackendBlock(t, store, ctx, 3,
				makeBackendReceipt(3, 0, addr1, approve, bob),
			)

			// Address OR: either address matches.
			logs, err := store.FilterLogs(ctx, 1, 3, filters.FilterCriteria{
				Addresses: []common.Address{addr1, addr2},
			})
			require.NoError(t, err)
			require.Len(t, logs, 4)

			// AND across topic positions: transfer at position 0 AND alice at position 1.
			logs, err = store.FilterLogs(ctx, 1, 3, filters.FilterCriteria{
				Topics: [][]common.Hash{{transfer}, {alice}},
			})
			require.NoError(t, err)
			require.Len(t, logs, 1)
			require.Equal(t, uint64(1), logs[0].BlockNumber)
			require.Equal(t, uint(0), logs[0].TxIndex)

			// OR within a topic position: alice or bob at position 1.
			logs, err = store.FilterLogs(ctx, 1, 3, filters.FilterCriteria{
				Topics: [][]common.Hash{{transfer}, {alice, bob}},
			})
			require.NoError(t, err)
			require.Len(t, logs, 2)

			// Wildcard position 0 (empty list), bob at position 1.
			logs, err = store.FilterLogs(ctx, 1, 3, filters.FilterCriteria{
				Topics: [][]common.Hash{{}, {bob}},
			})
			require.NoError(t, err)
			require.Len(t, logs, 2)

			// Range bounds respected.
			logs, err = store.FilterLogs(ctx, 2, 2, filters.FilterCriteria{})
			require.NoError(t, err)
			require.Len(t, logs, 1)
			require.Equal(t, uint64(2), logs[0].BlockNumber)
		})
	}
}

func TestReceiptBackendPrune(t *testing.T) {
	for _, backend := range receiptBackends {
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			store, ctx := setupReceiptBackend(t, backend, dir)
			defer func() { _ = store.Close() }()

			addr := common.HexToAddress("0x6666666666666666666666666666666666666666")
			topic := common.HexToHash("0xeeee")
			for block := uint64(1); block <= 10; block++ {
				writeBackendBlock(t, store, ctx, block, makeBackendReceipt(block, 0, addr, topic))
			}

			require.NoError(t, receipt.PruneReceiptBackend(store, 6))

			// Reopen so reads bypass the in-memory tip cache (pruning targets
			// the persistent backend; the cache ages out on rotation).
			require.NoError(t, store.Close())
			store, ctx = setupReceiptBackend(t, backend, dir)

			// Pruned blocks are invisible: pebblev3 deletes them eagerly via
			// DeleteRange; littdb enforces the retention floor at read time
			// while its TTL GC reclaims bytes lazily.
			for block := uint64(1); block <= 5; block++ {
				_, err := store.GetReceiptFromStore(ctx, backendTxHash(block, 0))
				require.ErrorIs(t, err, receipt.ErrNotFound, "block %d should be pruned", block)
			}
			logs, err := store.FilterLogs(ctx, 1, 5, filters.FilterCriteria{Addresses: []common.Address{addr}})
			require.NoError(t, err)
			require.Empty(t, logs)

			// Retained blocks unaffected.
			for block := uint64(6); block <= 10; block++ {
				rcpt, err := store.GetReceiptFromStore(ctx, backendTxHash(block, 0))
				require.NoError(t, err)
				require.Equal(t, block, rcpt.BlockNumber)
			}
			logs, err = store.FilterLogs(ctx, 1, 10, filters.FilterCriteria{Addresses: []common.Address{addr}})
			require.NoError(t, err)
			require.Len(t, logs, 5)
		})
	}
}

func TestReceiptBackendEmptyBlockAdvancesVersion(t *testing.T) {
	for _, backend := range receiptBackends {
		t.Run(backend, func(t *testing.T) {
			store, ctx := setupReceiptBackend(t, backend, t.TempDir())
			defer func() { _ = store.Close() }()

			writeBackendBlock(t, store, ctx, 1, makeBackendReceipt(1, 0, common.HexToAddress("0x77"), common.HexToHash("0x88")))
			// A block with no EVM receipts must still advance the version.
			writeBackendBlock(t, store, ctx, 2)
			require.Equal(t, int64(2), store.LatestVersion())

			logs, err := store.FilterLogs(ctx, 1, 2, filters.FilterCriteria{})
			require.NoError(t, err)
			require.Len(t, logs, 1)
		})
	}
}

func TestReceiptBackendMultiLogReceipts(t *testing.T) {
	for _, backend := range receiptBackends {
		t.Run(backend, func(t *testing.T) {
			store, ctx := setupReceiptBackend(t, backend, t.TempDir())
			defer func() { _ = store.Close() }()

			addr := common.HexToAddress("0x9999999999999999999999999999999999999999")
			topic := common.HexToHash("0x1234")
			txHash := backendTxHash(7, 0)
			rec := receipt.ReceiptRecord{
				TxHash: txHash,
				Receipt: &types.Receipt{
					TxHashHex:        txHash.Hex(),
					BlockNumber:      7,
					TransactionIndex: 0,
					Logs: []*types.Log{
						{Address: addr.Hex(), Topics: []string{topic.Hex()}, Index: 0},
						{Address: addr.Hex(), Topics: []string{topic.Hex()}, Index: 1},
						{Address: addr.Hex(), Topics: []string{topic.Hex()}, Index: 2},
					},
				},
			}
			txHash2 := backendTxHash(7, 1)
			rec2 := receipt.ReceiptRecord{
				TxHash: txHash2,
				Receipt: &types.Receipt{
					TxHashHex:        txHash2.Hex(),
					BlockNumber:      7,
					TransactionIndex: 1,
					Logs: []*types.Log{
						{Address: addr.Hex(), Topics: []string{topic.Hex()}, Index: 0},
					},
				},
			}
			writeBackendBlock(t, store, ctx, 7, rec, rec2)

			logs, err := store.FilterLogs(ctx, 7, 7, filters.FilterCriteria{Addresses: []common.Address{addr}})
			require.NoError(t, err)
			require.Len(t, logs, 4)
			// Log indexes must be block-scoped: 0,1,2 for tx 0 and 3 for tx 1.
			indexes := make([]uint, 0, 4)
			for _, lg := range logs {
				indexes = append(indexes, lg.Index)
			}
			require.ElementsMatch(t, []uint{0, 1, 2, 3}, indexes)
		})
	}
}

// TestReceiptBackendPartialBlockRewrite covers the legacy-migration write
// pattern: MigrateLegacyReceiptsBatch flushes historical blocks in
// tx-hash-ordered subsets, so the same block reaches SetReceipts across
// multiple calls. Later subsets must merge with — not overwrite or be
// dropped by — earlier ones, including the stored bloom.
func TestReceiptBackendPartialBlockRewrite(t *testing.T) {
	for _, backend := range receiptBackends {
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			store, ctx := setupReceiptBackend(t, backend, dir)
			defer func() { _ = store.Close() }()

			addrA := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			addrB := common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
			topicA := common.HexToHash("0xa1a1")
			topicB := common.HexToHash("0xb2b2")

			// Two SetReceipts calls for the same historical block with
			// disjoint receipt subsets, written from a later chain height.
			writeBackendBlock(t, store, ctx.WithBlockHeight(100), 5, makeBackendReceipt(5, 7, addrA, topicA))
			writeBackendBlock(t, store, ctx.WithBlockHeight(101), 5, makeBackendReceipt(5, 3, addrB, topicB))

			// Reopen so reads hit the persistent backend, not the tip cache.
			require.NoError(t, store.Close())
			store, ctx = setupReceiptBackend(t, backend, dir)

			rcpt, err := store.GetReceiptFromStore(ctx, backendTxHash(5, 7))
			require.NoError(t, err)
			require.Equal(t, uint32(7), rcpt.TransactionIndex)
			require.Equal(t, backendTxHash(5, 7).Hex(), rcpt.TxHashHex)

			rcpt, err = store.GetReceiptFromStore(ctx, backendTxHash(5, 3))
			require.NoError(t, err)
			require.Equal(t, uint32(3), rcpt.TransactionIndex)
			require.Equal(t, backendTxHash(5, 3).Hex(), rcpt.TxHashHex)

			// The bloom must cover both subsets (merged, not replaced).
			logs, err := store.FilterLogs(ctx, 5, 5, filters.FilterCriteria{Addresses: []common.Address{addrA}})
			require.NoError(t, err)
			require.Len(t, logs, 1)
			logs, err = store.FilterLogs(ctx, 5, 5, filters.FilterCriteria{Addresses: []common.Address{addrB}})
			require.NoError(t, err)
			require.Len(t, logs, 1)
			logs, err = store.FilterLogs(ctx, 5, 5, filters.FilterCriteria{Addresses: []common.Address{addrA, addrB}})
			require.NoError(t, err)
			require.Len(t, logs, 2)
		})
	}
}

// TestReceiptBackendWarmupAfterReopen guards the cached wrapper's coverage
// window: after a restart the wrapper treats the current chunk
// ([floor(latest/500)*500, latest]) as fully cached once the first write
// lands, so the backend must warm the cache with those blocks or in-window
// FilterLogs queries silently drop pre-restart logs.
func TestReceiptBackendWarmupAfterReopen(t *testing.T) {
	for _, backend := range receiptBackends {
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			store, ctx := setupReceiptBackend(t, backend, dir)
			defer func() { _ = store.Close() }()

			addr := common.HexToAddress("0xcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd")
			topic := common.HexToHash("0x5678")
			for block := uint64(1); block <= 5; block++ {
				writeBackendBlock(t, store, ctx, block, makeBackendReceipt(block, 0, addr, topic))
			}
			require.NoError(t, store.Close())

			store, ctx = setupReceiptBackend(t, backend, dir)
			// First post-restart write activates the cache coverage window
			// [0, 6]; the query below is then answered cache-only.
			writeBackendBlock(t, store, ctx, 6, makeBackendReceipt(6, 0, addr, topic))

			logs, err := store.FilterLogs(ctx, 1, 6, filters.FilterCriteria{Addresses: []common.Address{addr}})
			require.NoError(t, err)
			require.Len(t, logs, 6, "pre-restart blocks must be warmed into the cache")
		})
	}
}

// TestReceiptBackendDuplicateTxHashInBlock: synthetic benchmark workloads can
// repeat a tx hash within a block; the write must not fail (littdb retries
// its Put without the duplicate secondary key).
func TestReceiptBackendDuplicateTxHashInBlock(t *testing.T) {
	for _, backend := range receiptBackends {
		t.Run(backend, func(t *testing.T) {
			store, ctx := setupReceiptBackend(t, backend, t.TempDir())
			defer func() { _ = store.Close() }()

			addr := common.HexToAddress("0xefef")
			topic := common.HexToHash("0x4242")
			dup1 := makeBackendReceipt(9, 0, addr, topic)
			dup2 := makeBackendReceipt(9, 1, addr, topic)
			dup2.TxHash = dup1.TxHash // same hash, different tx index
			dup2.Receipt.TxHashHex = dup1.Receipt.TxHashHex

			writeBackendBlock(t, store, ctx, 9, dup1, dup2, makeBackendReceipt(9, 2, addr, topic))

			rcpt, err := store.GetReceiptFromStore(ctx, dup1.TxHash)
			require.NoError(t, err)
			require.Equal(t, dup1.TxHash.Hex(), rcpt.TxHashHex)
			rcpt, err = store.GetReceiptFromStore(ctx, backendTxHash(9, 2))
			require.NoError(t, err)
			require.Equal(t, uint32(2), rcpt.TransactionIndex)
		})
	}
}

func TestReceiptBackendName(t *testing.T) {
	for _, backend := range receiptBackends {
		t.Run(backend, func(t *testing.T) {
			store, _ := setupReceiptBackend(t, backend, t.TempDir())
			defer func() { _ = store.Close() }()
			require.Equal(t, backend, receipt.BackendTypeName(store))
		})
	}
}

func TestBlockBloomNoFalseNegatives(t *testing.T) {
	bloom := make([]byte, receipt.BlockBloomSizeBytesForTest)
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	topic := common.HexToHash("0xfeedface")
	receipt.BloomAddForTest(bloom, addr[:])
	receipt.BloomAddForTest(bloom, topic[:])

	require.True(t, receipt.BloomMayContainForTest(bloom, addr[:]))
	require.True(t, receipt.BloomMayContainForTest(bloom, topic[:]))

	require.True(t, receipt.BloomMatchesCriteriaForTest(bloom, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	}))
	// AND semantics: a topic position that was never added must reject.
	require.False(t, receipt.BloomMatchesCriteriaForTest(bloom, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}, {common.HexToHash("0x0badc0de")}},
	}))
	// Empty criteria always matches (wildcard query).
	require.True(t, receipt.BloomMatchesCriteriaForTest(bloom, filters.FilterCriteria{}))
}

// TestReceiptBackendSplitDirectories: the litt backends can spread bodies
// across multiple paths and root the keymap and log index on their own
// drives. Data written with the split layout must survive a close/reopen
// (keymap discovery must find the overridden location).
func TestReceiptBackendSplitDirectories(t *testing.T) {
	for _, backend := range []string{"littdb", "littidx"} {
		t.Run(backend, func(t *testing.T) {
			storeKey := storetypes.NewKVStoreKey("evm")
			tkey := storetypes.NewTransientStoreKey("evm_transient")
			ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)
			cfg := dbconfig.DefaultReceiptStoreConfig()
			cfg.Backend = backend
			cfg.DBDirectory = t.TempDir()
			cfg.LittPaths = []string{t.TempDir(), t.TempDir()}
			cfg.LittKeymapDirectory = t.TempDir()
			cfg.LogIndexDirectory = filepath.Join(t.TempDir(), "log-index")
			cfg.KeepRecent = 0

			store, err := receipt.NewReceiptStore(cfg, storeKey)
			require.NoError(t, err)

			addr := common.HexToAddress("0xabcd")
			topic := common.HexToHash("0x1234")
			for block := uint64(1); block <= 5; block++ {
				writeBackendBlock(t, store, ctx, block, makeBackendReceipt(block, 0, addr, topic))
			}
			require.NoError(t, store.Close())

			// The overridden directories are the ones holding data.
			keymapDir, err := os.ReadDir(cfg.LittKeymapDirectory)
			require.NoError(t, err)
			require.NotEmpty(t, keymapDir)
			logIdxDir, err := os.ReadDir(cfg.LogIndexDirectory)
			require.NoError(t, err)
			require.NotEmpty(t, logIdxDir)

			store, err = receipt.NewReceiptStore(cfg, storeKey)
			require.NoError(t, err)
			defer func() { _ = store.Close() }()
			for block := uint64(1); block <= 5; block++ {
				rcpt, err := store.GetReceiptFromStore(ctx, backendTxHash(block, 0))
				require.NoError(t, err)
				require.Equal(t, block, rcpt.BlockNumber)
			}
			logs, err := store.FilterLogs(ctx, 1, 5, filters.FilterCriteria{
				Addresses: []common.Address{addr},
			})
			require.NoError(t, err)
			require.Len(t, logs, 5)
		})
	}
}
