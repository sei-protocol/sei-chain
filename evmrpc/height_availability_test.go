package evmrpc

import (
	"context"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

const highBlockHashHex = "0xfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeed"

type heightTestClient struct {
	client.Client
	highHash  bytes.HexBytes
	highBlock *coretypes.ResultBlock
	earliest  int64
	latest    int64
}

func (*heightTestClient) EvmNextPendingNonce(common.Address) uint64 {
	return 0
}

func (*heightTestClient) GenesisInitialHeight() int64 {
	return 1
}

func (*heightTestClient) EvmTxByHash(common.Hash) (tmtypes.Tx, bool) {
	return nil, false
}

func (*heightTestClient) EvmProxy(common.Address) utils.Option[*rpc.Client] {
	return utils.None[*rpc.Client]()
}

func newHeightTestClient(highHeight, earliest, latest int64) *heightTestClient {
	return &heightTestClient{
		highHash: bytes.HexBytes(mustDecodeHex(highBlockHashHex[2:])),
		highBlock: &coretypes.ResultBlock{
			Block: &tmtypes.Block{
				Header: tmtypes.Header{Height: highHeight},
			},
		},
		earliest: earliest,
		latest:   latest,
	}
}

func (c *heightTestClient) BlockByHash(ctx context.Context, hash bytes.HexBytes) (*coretypes.ResultBlock, error) {
	if hash.String() == c.highHash.String() {
		return c.highBlock, nil
	}
	return &coretypes.ResultBlock{
		Block: &tmtypes.Block{Header: tmtypes.Header{Height: c.latest}},
	}, nil
}

func (c *heightTestClient) Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error) {
	h := c.latest
	if height != nil {
		h = *height
	}
	return &coretypes.ResultBlock{Block: &tmtypes.Block{Header: tmtypes.Header{Height: h}}}, nil
}

func (c *heightTestClient) BlockResults(context.Context, *int64) (*coretypes.ResultBlockResults, error) {
	return &coretypes.ResultBlockResults{}, nil
}

func (c *heightTestClient) Status(context.Context) (*coretypes.ResultStatus, error) {
	return &coretypes.ResultStatus{
		SyncInfo: coretypes.SyncInfo{
			LatestBlockHeight:   c.latest,
			EarliestBlockHeight: c.earliest,
		},
	}, nil
}

func (c *heightTestClient) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	return &coretypes.ResultGenesis{
		Genesis: &tmtypes.GenesisDoc{
			InitialHeight: c.earliest,
		},
	}, nil
}

// blockNotFoundTestClient returns ResultBlock{Block: nil} for a specific hash to simulate Tendermint "block not found".
type blockNotFoundTestClient struct {
	*heightTestClient
	notFoundHash bytes.HexBytes
}

func (c *blockNotFoundTestClient) BlockByHash(ctx context.Context, hash bytes.HexBytes) (*coretypes.ResultBlock, error) {
	if hash.String() == c.notFoundHash.String() {
		return &coretypes.ResultBlock{Block: nil}, nil
	}
	return c.heightTestClient.BlockByHash(ctx, hash)
}

func mustDecodeHex(h string) []byte {
	bz, err := hex.DecodeString(h)
	if err != nil {
		panic(err)
	}
	return bz
}

func testTxConfigProvider(int64) client.TxConfig { return nil }

func testCtxProvider(h int64) sdk.Context {
	if h == LatestCtxHeight {
		return sdk.Context{}.WithBlockHeight(1 << 60)
	}
	return sdk.Context{}.WithBlockHeight(h)
}

func newHeightTestWatermarks(client client.LocalClient, latest int64) *WatermarkManager {
	return NewWatermarkManager(client, testCtxProvider, nil, &fakeReceiptStore{latest: latest})
}

// newTestKeeperWithReceiptStore returns a keeper carrying nothing but a receipt store, which the
// endpoints that read a block's transactions or receipts refuse to run without.
func newTestKeeperWithReceiptStore() *keeper.Keeper {
	k := &keeper.Keeper{}
	k.SetReceiptStoreForTesting(&fakeReceiptStore{})
	return k
}

// GetBlockByHash for a block whose height sits above safe latest must return
// JSON null per the Ethereum JSON-RPC spec (the block doesn't exist from the
// caller's perspective), matching get-block-by-empty-hash.iox / get-block-by-
// notfound-hash.iox semantics.
func TestBlockAPIAboveWatermarkReturnsNull(t *testing.T) {
	t.Parallel()

	earliest := int64(1)
	latest := int64(100)
	highHeight := latest + 5
	client := newHeightTestClient(highHeight, earliest, latest)
	watermarks := newHeightTestWatermarks(client, latest)
	api := NewBlockAPI(client, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, watermarks, nil, nil)

	result, err := api.GetBlockByHash(context.Background(), common.HexToHash(highBlockHashHex), false)
	require.NoError(t, err)
	require.Nil(t, result)
}

// TestGetBlockByHashNotFoundReturnsNull verifies Ethereum-compatible behavior: empty or non-existent block hash
// returns (nil, nil) so RPC responds with result: null, not an error (see get-block-by-empty-hash.iox, get-block-by-notfound-hash.iox).
func TestGetBlockByHashNotFoundReturnsNull(t *testing.T) {
	t.Parallel()

	earliest := int64(1)
	latest := int64(100)
	base := newHeightTestClient(latest+5, earliest, latest)
	notFoundHashHex := "0x00000000000000000000000000000000000000000000000000000000deadbeef"
	client := &blockNotFoundTestClient{
		heightTestClient: base,
		notFoundHash:     bytes.HexBytes(mustDecodeHex(notFoundHashHex[2:])),
	}
	watermarks := newHeightTestWatermarks(client, latest)
	api := NewBlockAPI(client, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, watermarks, nil, nil)
	ctx := context.Background()

	// Empty hash: short-circuit, result null
	result, err := api.GetBlockByHash(ctx, common.Hash{}, false)
	require.NoError(t, err)
	require.Nil(t, result)

	// Non-existent hash (client returns Block: nil): result null
	result, err = api.GetBlockByHash(ctx, common.HexToHash(notFoundHashHex), false)
	require.NoError(t, err)
	require.Nil(t, result)
}

// TestGetBlockReceiptsNotFoundReturnsNull verifies Ethereum-compatible behavior: empty or non-existent block hash
// returns (nil, nil) so RPC responds with result: null (see get-block-receipts-empty.iox, get-block-receipts-not-found.iox).
func TestGetBlockReceiptsNotFoundReturnsNull(t *testing.T) {
	t.Parallel()

	earliest := int64(1)
	latest := int64(100)
	base := newHeightTestClient(latest+5, earliest, latest)
	notFoundHashHex := "0x00000000000000000000000000000000000000000000000000000000deadbeef"
	client := &blockNotFoundTestClient{
		heightTestClient: base,
		notFoundHash:     bytes.HexBytes(mustDecodeHex(notFoundHashHex[2:])),
	}
	watermarks := newHeightTestWatermarks(client, latest)
	api := NewBlockAPI(client, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, watermarks, nil, nil)
	ctx := context.Background()

	// Empty hash: short-circuit, result null
	receipts, err := api.GetBlockReceipts(ctx, rpc.BlockNumberOrHashWithHash(common.Hash{}, true))
	require.NoError(t, err)
	require.Nil(t, receipts)

	// Non-existent hash (client returns Block: nil): result null
	receipts, err = api.GetBlockReceipts(ctx, rpc.BlockNumberOrHashWithHash(common.HexToHash(notFoundHashHex), true))
	require.NoError(t, err)
	require.Nil(t, receipts)
}

// TestBlockAPILatestTagResolves verifies that block endpoints accepting
// "latest"/"safe"/"finalized"/"pending" tags resolve to the safe-latest height
// rather than returning JSON null. The tag arrives as numberPtr=nil from
// getBlockNumber; the by-number helper must route it through wm.LatestHeight.
//
// GetBlockByNumber and getTransactionByBlockNumberAndIndex use the identical
// (getBlockNumber → blockByNumberOrNullForJSONRPC → if block == nil) pattern
// but require a real keeper for downstream encoding so they're not exercised
// directly here.
func TestBlockAPILatestTagResolves(t *testing.T) {
	t.Parallel()

	earliest := int64(1)
	latest := int64(100)
	client := newHeightTestClient(latest+5, earliest, latest)
	watermarks := newHeightTestWatermarks(client, latest)
	api := NewBlockAPI(client, newTestKeeperWithReceiptStore(), testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, watermarks, nil, nil)
	ctx := context.Background()

	tags := []rpc.BlockNumber{
		rpc.LatestBlockNumber,
		rpc.SafeBlockNumber,
		rpc.FinalizedBlockNumber,
		rpc.PendingBlockNumber,
	}
	for _, tag := range tags {
		receipts, err := api.GetBlockReceipts(ctx, rpc.BlockNumberOrHashWithNumber(tag))
		require.NoError(t, err)
		require.NotNil(t, receipts, "GetBlockReceipts tag %v must resolve, not null", tag)

		count, err := api.GetBlockTransactionCountByNumber(ctx, tag)
		require.NoError(t, err)
		require.NotNil(t, count, "GetBlockTransactionCountByNumber tag %v must resolve, not null", tag)
	}
}

// TestGetBlockTransactionCountByHashGenesis verifies that the genesis block hash returned by
// eth_getBlockByNumber("0x0") is accepted by eth_getBlockTransactionCountByHash (consistency).
func TestGetBlockTransactionCountByHashGenesis(t *testing.T) {
	t.Parallel()

	api := NewBlockAPI(nil, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, nil, nil, nil)
	count, err := api.GetBlockTransactionCountByHash(context.Background(), genesisBlockHash)
	require.NoError(t, err)
	require.NotNil(t, count)
	require.Equal(t, hexutil.Uint(0), *count)
}

func TestGetBlockTransactionCountByNumberGenesis(t *testing.T) {
	t.Parallel()

	api := NewBlockAPI(nil, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, nil, nil, nil)
	count, err := api.GetBlockTransactionCountByNumber(context.Background(), 0)
	require.NoError(t, err)
	require.NotNil(t, count)
	require.Equal(t, hexutil.Uint(0), *count)
}

func TestGetBlockReceiptsGenesis(t *testing.T) {
	t.Parallel()

	api := NewBlockAPI(nil, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, nil, nil, nil)
	receipts, err := api.GetBlockReceipts(context.Background(), rpc.BlockNumberOrHashWithHash(genesisBlockHash, true))
	require.NoError(t, err)
	require.NotNil(t, receipts)
	require.Empty(t, receipts)
}

func TestGetBlockReceiptsGenesisByNumber(t *testing.T) {
	t.Parallel()

	api := NewBlockAPI(nil, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, nil, nil, nil)
	n := rpc.BlockNumber(0)
	receipts, err := api.GetBlockReceipts(context.Background(), rpc.BlockNumberOrHashWithNumber(n))
	require.NoError(t, err)
	require.NotNil(t, receipts)
	require.Empty(t, receipts)
}

func TestGetBlockNumberByNrOrHashGenesis(t *testing.T) {
	t.Parallel()

	height, err := GetBlockNumberByNrOrHash(
		context.Background(),
		nil,
		nil,
		rpc.BlockNumberOrHashWithHash(genesisBlockHash, true),
	)
	require.NoError(t, err)
	require.NotNil(t, height)
	require.Equal(t, int64(0), *height)
}

func TestLogFetcherSkipsUnavailableCachedBlock(t *testing.T) {
	t.Parallel()

	earliest := int64(1)
	latest := int64(90)
	highHeight := latest + 3
	client := newHeightTestClient(highHeight, earliest, latest)
	watermarks := newHeightTestWatermarks(client, latest)
	cache := NewBlockCache(2)
	cache.Add(highHeight, &BlockCacheEntry{
		Block:    client.highBlock,
		Receipts: make(map[common.Hash]*evmtypes.Receipt),
	})
	fetcher := &LogFetcher{
		tmClient:                 client,
		k:                        nil,
		txConfigProvider:         testTxConfigProvider,
		ctxProvider:              testCtxProvider,
		filterConfig:             &FilterConfig{maxLog: DefaultMaxLogLimit, maxBlock: DefaultMaxBlockRange},
		includeSyntheticReceipts: false,
		dbReadSemaphore:          make(chan struct{}, 1),
		globalBlockCache:         cache,
		globalLogSlicePool:       NewLogSlicePool(),
		watermarks:               watermarks,
	}

	resCh := make(chan *coretypes.ResultBlock, 1)
	errCh := make(chan error, 1)
	fetcher.processBatch(context.Background(), highHeight, highHeight, filters.FilterCriteria{}, nil, resCh, errCh)

	select {
	case <-resCh:
		t.Fatalf("expected no block to be emitted for height %d", highHeight)
	default:
	}

	select {
	case err := <-errCh:
		t.Fatalf("unexpected error: %v", err)
	default:
	}
}

func TestGetBlockTransactionCountByNumberReceiptsPruned(t *testing.T) {
	t.Parallel()

	client := newHeightTestClient(100, 1, 200)
	rs := &fakeReceiptStore{latest: 200, earliest: 150}
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, rs)
	api := NewBlockAPI(client, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, watermarks, nil, nil)

	_, err := api.GetBlockTransactionCountByNumber(context.Background(), rpc.BlockNumber(100))
	require.Error(t, err)
	require.Contains(t, err.Error(), "receipts have been pruned")
}

func TestGetBlockTransactionCountByHashReceiptsPruned(t *testing.T) {
	t.Parallel()

	client := newHeightTestClient(100, 1, 200)
	rs := &fakeReceiptStore{latest: 200, earliest: 150}
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, rs)
	api := NewBlockAPI(client, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, watermarks, nil, nil)

	_, err := api.GetBlockTransactionCountByHash(context.Background(), common.HexToHash(highBlockHashHex))
	require.Error(t, err)
	require.Contains(t, err.Error(), "receipts have been pruned")
}

func TestGetBlockByNumberReceiptsPruned(t *testing.T) {
	t.Parallel()

	client := newHeightTestClient(100, 1, 200)
	rs := &fakeReceiptStore{latest: 200, earliest: 150}
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, rs)
	api := NewBlockAPI(client, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, watermarks, nil, nil)

	_, err := api.GetBlockByNumber(context.Background(), rpc.BlockNumber(100), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "receipts have been pruned")
}

func TestGetBlockByHashReceiptsPruned(t *testing.T) {
	t.Parallel()

	client := newHeightTestClient(100, 1, 200)
	rs := &fakeReceiptStore{latest: 200, earliest: 150}
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, rs)
	api := NewBlockAPI(client, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, watermarks, nil, nil)

	_, err := api.GetBlockByHash(context.Background(), common.HexToHash(highBlockHashHex), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "receipts have been pruned")
}

func TestGetBlockReceiptsReceiptsPruned(t *testing.T) {
	t.Parallel()

	client := newHeightTestClient(100, 1, 200)
	rs := &fakeReceiptStore{latest: 200, earliest: 150}
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, rs)
	api := NewBlockAPI(client, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, watermarks, nil, nil)

	_, err := api.GetBlockReceipts(context.Background(), rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(100)))
	require.Error(t, err)
	require.Contains(t, err.Error(), "receipts have been pruned")
}

// receiptFloorTestHeights sets up a node whose blocks reach back to 1 while its receipts start at
// 150, the shape a state-synced or snapshot-restored node has.
const (
	receiptFloorTestBlockEarliest   = int64(1)
	receiptFloorTestReceiptEarliest = int64(150)
	receiptFloorTestLatest          = int64(200)
)

func newReceiptFloorTestWatermarks(client client.LocalClient) *WatermarkManager {
	rs := &fakeReceiptStore{latest: receiptFloorTestLatest, earliest: receiptFloorTestReceiptEarliest}
	return NewWatermarkManager(client, testCtxProvider, nil, rs)
}

func TestEarliestReceiptHeight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		blockEarliest int64
		receiptStore  *fakeReceiptStore
		want          int64
	}{
		{
			name:          "receipt floor above block floor",
			blockEarliest: 1,
			receiptStore:  &fakeReceiptStore{latest: 200, earliest: 150},
			want:          150,
		},
		{
			name:          "never-pruned receipt store",
			blockEarliest: 1,
			receiptStore:  &fakeReceiptStore{latest: 200, earliest: 0},
			want:          1,
		},
		{
			name:          "receipt floor below block floor",
			blockEarliest: 50,
			receiptStore:  &fakeReceiptStore{latest: 200, earliest: 10},
			want:          50,
		},
		{
			name:          "no receipt store",
			blockEarliest: 50,
			receiptStore:  nil,
			want:          50,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newHeightTestClient(100, tt.blockEarliest, 200)
			var wm *WatermarkManager
			if tt.receiptStore == nil {
				wm = NewWatermarkManager(client, testCtxProvider, nil, nil)
			} else {
				wm = NewWatermarkManager(client, testCtxProvider, nil, tt.receiptStore)
			}
			got, err := wm.EarliestReceiptHeight(context.Background())
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEnsureReceiptHeightAvailableWrapsSentinel(t *testing.T) {
	t.Parallel()

	wm := newReceiptFloorTestWatermarks(newHeightTestClient(100, receiptFloorTestBlockEarliest, receiptFloorTestLatest))

	err := wm.EnsureReceiptHeightAvailable(100)
	require.ErrorIs(t, err, ErrReceiptsPruned)
	require.EqualError(t, err, "requested height 100 receipts have been pruned; earliest available is 150")

	require.NoError(t, wm.EnsureReceiptHeightAvailable(receiptFloorTestReceiptEarliest))
}

func TestGetTransactionByBlockAndIndexReceiptsPruned(t *testing.T) {
	t.Parallel()

	client := newHeightTestClient(100, receiptFloorTestBlockEarliest, receiptFloorTestLatest)
	api := NewTransactionAPI(client, nil, testCtxProvider, testTxConfigProvider, "", ConnectionTypeHTTP,
		utils.None[time.Duration](), newReceiptFloorTestWatermarks(client), nil, nil)
	ctx := context.Background()

	_, err := api.GetTransactionByBlockNumberAndIndex(ctx, rpc.BlockNumber(100), 0)
	require.ErrorIs(t, err, ErrReceiptsPruned)

	_, err = api.GetTransactionByBlockHashAndIndex(ctx, common.HexToHash(highBlockHashHex), 0)
	require.ErrorIs(t, err, ErrReceiptsPruned)

	// At the receipt floor the guard passes; the nil keeper then fails the receipt read instead.
	_, err = api.GetTransactionByBlockNumberAndIndex(ctx, rpc.BlockNumber(receiptFloorTestReceiptEarliest), 0)
	require.ErrorIs(t, err, receipt.ErrNotConfigured)
}

func TestDebugTraceBlockGuardsReceiptFloor(t *testing.T) {
	t.Parallel()

	latestCtx := sdk.Context{}.WithBlockHeight(receiptFloorTestLatest)
	base := newHeightTestClient(100, receiptFloorTestBlockEarliest, receiptFloorTestLatest)
	notFoundHashHex := "0x00000000000000000000000000000000000000000000000000000000deadbeef"
	client := &blockNotFoundTestClient{
		heightTestClient: base,
		notFoundHash:     bytes.HexBytes(mustDecodeHex(notFoundHashHex[2:])),
	}
	api := &DebugAPI{
		tmClient:         client,
		ctxProvider:      func(int64) sdk.Context { return latestCtx },
		connectionType:   ConnectionTypeHTTP,
		maxBlockLookback: -1,
		backend:          &Backend{watermarks: newReceiptFloorTestWatermarks(client)},
	}
	ctx := context.Background()

	require.ErrorIs(t, api.guardDebugTraceBlockByNumber(ctx, rpc.BlockNumber(100)), ErrReceiptsPruned)
	require.NoError(t, api.guardDebugTraceBlockByNumber(ctx, rpc.BlockNumber(receiptFloorTestReceiptEarliest)))

	require.ErrorIs(t, api.guardDebugTraceBlockByHash(ctx, common.HexToHash(highBlockHashHex)), ErrReceiptsPruned)
	require.NoError(t, api.guardDebugTraceBlockByHash(ctx, common.HexToHash(notFoundHashHex)),
		"an unknown hash passes so the tracer reports it")

	// debug_traceCall needs state, not receipts, and keeps working below the receipt floor.
	require.NoError(t, api.guardHistoricalDebugTraceByNumberOrHash(ctx, "debug_traceCall",
		rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(100))))
	require.NoError(t, api.guardHistoricalDebugTraceByNumberOrHash(ctx, "debug_traceCall",
		rpc.BlockNumberOrHashWithHash(common.HexToHash(highBlockHashHex), false)))
}

func TestLogFetcherRespectsReceiptFloor(t *testing.T) {
	t.Parallel()

	notFoundHashHex := "0x00000000000000000000000000000000000000000000000000000000deadbeef"
	newFetcher := func(highHeight int64) *LogFetcher {
		client := &blockNotFoundTestClient{
			heightTestClient: newHeightTestClient(highHeight, receiptFloorTestBlockEarliest, receiptFloorTestLatest),
			notFoundHash:     bytes.HexBytes(mustDecodeHex(notFoundHashHex[2:])),
		}
		return &LogFetcher{
			tmClient:           client,
			txConfigProvider:   testTxConfigProvider,
			ctxProvider:        testCtxProvider,
			filterConfig:       &FilterConfig{maxLog: DefaultMaxLogLimit, maxBlock: DefaultMaxBlockRange},
			dbReadSemaphore:    make(chan struct{}, 1),
			globalBlockCache:   NewBlockCache(2),
			globalLogSlicePool: NewLogSlicePool(),
			watermarks:         newReceiptFloorTestWatermarks(client),
		}
	}
	ctx := context.Background()

	t.Run("range floor is the receipt floor", func(t *testing.T) {
		earliest, err := newFetcher(100).earliestHeight(ctx)
		require.NoError(t, err)
		require.Equal(t, receiptFloorTestReceiptEarliest, earliest)
	})

	t.Run("range below the receipt floor errors", func(t *testing.T) {
		crit := filters.FilterCriteria{FromBlock: big.NewInt(100), ToBlock: big.NewInt(120)}
		_, _, err := newFetcher(100).fetchBlocksByCrit(ctx, crit, 0, nil)
		require.ErrorContains(t, err, "requested fromBlock 100 is before earliest available block 150")
	})

	t.Run("block hash below the receipt floor errors", func(t *testing.T) {
		hash := common.HexToHash(highBlockHashHex)
		_, _, err := newFetcher(100).fetchBlocksByCrit(ctx, filters.FilterCriteria{BlockHash: &hash}, 0, nil)
		require.ErrorIs(t, err, ErrReceiptsPruned)
	})

	t.Run("block hash at the receipt floor is served", func(t *testing.T) {
		hash := common.HexToHash(highBlockHashHex)
		blocks, _, err := newFetcher(receiptFloorTestReceiptEarliest).fetchBlocksByCrit(ctx, filters.FilterCriteria{BlockHash: &hash}, 0, nil)
		require.NoError(t, err)
		block := <-blocks
		require.Equal(t, receiptFloorTestReceiptEarliest, block.Block.Height)
	})

	t.Run("unknown block hash returns no blocks", func(t *testing.T) {
		hash := common.HexToHash(notFoundHashHex)
		blocks, _, err := newFetcher(100).fetchBlocksByCrit(ctx, filters.FilterCriteria{BlockHash: &hash}, 0, nil)
		require.NoError(t, err)
		_, open := <-blocks
		require.False(t, open)
	})
}

func TestFeeHistoryFloorIsReceiptFloor(t *testing.T) {
	t.Parallel()

	client := newHeightTestClient(100, receiptFloorTestBlockEarliest, receiptFloorTestLatest)
	api := &InfoAPI{tmClient: client, watermarks: newReceiptFloorTestWatermarks(client)}

	earliest, err := api.earliestHeight(context.Background())
	require.NoError(t, err)
	require.Equal(t, receiptFloorTestReceiptEarliest, earliest)
}

func TestCategorizeErrorCountsPrunedAsBlockNotFound(t *testing.T) {
	t.Parallel()

	wm := newReceiptFloorTestWatermarks(newHeightTestClient(100, receiptFloorTestBlockEarliest, receiptFloorTestLatest))
	m := &WorkerPoolMetrics{}
	m.categorizeError(wm.EnsureReceiptHeightAvailable(100))
	m.categorizeError(ensureWithinWatermarks(1, 10, 20))
	require.Equal(t, int64(2), m.ErrBlockNotFound.Load())
	require.Equal(t, int64(0), m.ErrOther.Load())
}
