package evmrpc

import (
	"context"
	"encoding/hex"
	"net/url"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/mock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

const highBlockHashHex = "0xfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeed"

type heightTestClient struct {
	mock.Client
	highHash  bytes.HexBytes
	highBlock *coretypes.ResultBlock
	earliest  int64
	latest    int64
}

func (*heightTestClient) EvmNextPendingNonce(common.Address) uint64 {
	return 0
}

func (*heightTestClient) EvmTxByHash(common.Hash) (tmtypes.Tx, bool) {
	return nil, false
}

func (*heightTestClient) EvmProxy(common.Address) utils.Option[*url.URL] {
	return utils.None[*url.URL]()
}

func newHeightTestClient(highHeight, earliest, latest int64) *heightTestClient {
	return &heightTestClient{
		Client:   mock.Client{},
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

func testCtxProvider(int64) sdk.Context { return sdk.Context{} }

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
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, nil)
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
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, nil)
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
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, nil)
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
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, nil)
	api := NewBlockAPI(client, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, watermarks, nil, nil)
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

func TestGetBlockByNumberExcludeTraceFailGenesis(t *testing.T) {
	t.Parallel()

	api := NewSeiBlockAPI(nil, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, nil, nil, nil)
	block, err := api.GetBlockByNumberExcludeTraceFail(context.Background(), 0, false)
	require.NoError(t, err)
	require.NotNil(t, block)
	require.Equal(t, genesisBlockHashHex, block["hash"])
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
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, nil)
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

func TestStateAPIGetProofUnavailableHeight(t *testing.T) {
	t.Parallel()

	earliest := int64(2)
	latest := int64(80)
	highHeight := latest + 4
	client := newHeightTestClient(highHeight, earliest, latest)
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, nil)
	api := NewStateAPI(client, nil, testCtxProvider, ConnectionTypeHTTP, watermarks)

	blockParam := rpc.BlockNumberOrHashWithHash(common.HexToHash(highBlockHashHex), true)
	_, err := api.GetProof(context.Background(), common.Address{}, []string{}, blockParam)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requested height")
}
