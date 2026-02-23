package evmrpc

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
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

func mustDecodeHex(h string) []byte {
	bz, err := hex.DecodeString(h)
	if err != nil {
		panic(err)
	}
	return bz
}

func testTxConfigProvider(int64) client.TxConfig { return nil }

func testCtxProvider(int64) sdk.Context { return sdk.Context{} }

func TestBlockAPIEnsureHeightUnavailable(t *testing.T) {
	t.Parallel()

	earliest := int64(1)
	latest := int64(100)
	highHeight := latest + 5
	client := newHeightTestClient(highHeight, earliest, latest)
	watermarks := NewWatermarkManager(client, testCtxProvider, nil, nil)
	api := NewBlockAPI(client, nil, testCtxProvider, testTxConfigProvider, ConnectionTypeHTTP, watermarks, nil, nil)

	_, err := api.GetBlockByHash(context.Background(), common.HexToHash(highBlockHashHex), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requested height")
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
