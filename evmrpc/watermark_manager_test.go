package evmrpc

import (
	"context"
	"errors"
	"io"
	"net/url"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	dbm "github.com/tendermint/tm-db"
)

func TestWatermarksAggregatesSources(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 10, EarliestBlockHeight: 2}},
	}
	stateStore := &fakeStateStore{latest: 8, earliest: 3}
	wm := newTestWatermarkManager(tmClient, 12, stateStore, 9)

	blockEarliest, stateEarliest, latest, err := wm.Watermarks(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), blockEarliest)
	require.Equal(t, int64(3), stateEarliest)
	require.Equal(t, int64(8), latest)
}

func TestWatermarksIncludesCtxProviderHeight(t *testing.T) {
	multi := &fakeMultiStore{earliest: 0, latest: 0}
	ctx := sdk.Context{}.
		WithBlockHeight(12).
		WithMultiStore(multi)
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 15, EarliestBlockHeight: 5}},
	}
	wm := NewWatermarkManager(tmClient, func(int64) sdk.Context { return ctx }, nil, &fakeReceiptStore{latest: 14})

	blockEarliest, stateEarliest, latest, err := wm.Watermarks(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(5), blockEarliest)
	require.Equal(t, int64(12), stateEarliest)
	require.Equal(t, int64(12), latest)
}

func TestWatermarksPropagatesHeightSourceError(t *testing.T) {
	wm := NewWatermarkManager(&fakeTMClient{statusErr: errNoHeightSource}, watermarkTestCtxProvider(0), nil, &fakeReceiptStore{})
	_, _, _, err := wm.Watermarks(context.Background())
	require.ErrorIs(t, err, errNoHeightSource)
}

func TestResolveHeightGating(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 5, EarliestBlockHeight: 2}},
	}
	stateStore := &fakeStateStore{latest: 5, earliest: 2}
	wm := newTestWatermarkManager(tmClient, 5, stateStore, 5)

	tooHigh := rpc.BlockNumber(6)
	_, err := wm.ResolveHeight(context.Background(), rpc.BlockNumberOrHash{BlockNumber: &tooHigh})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not yet available")

	within := rpc.BlockNumber(4)
	height, err := wm.ResolveHeight(context.Background(), rpc.BlockNumberOrHash{BlockNumber: &within})
	require.NoError(t, err)
	require.Equal(t, int64(4), height)
}

func TestResolveHeightByHash(t *testing.T) {
	tmClient := &fakeTMClient{
		status:      &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 5, EarliestBlockHeight: 1}},
		blockByHash: makeBlockResult(4),
	}
	stateStore := &fakeStateStore{latest: 5, earliest: 1}
	wm := newTestWatermarkManager(tmClient, 5, stateStore, 5)
	h := common.HexToHash("0x1")
	blockHeight, err := wm.ResolveHeight(context.Background(), rpc.BlockNumberOrHash{BlockHash: &h})
	require.NoError(t, err)
	require.Equal(t, int64(4), blockHeight)
}

func TestEnsureBlockHeightAvailableBounds(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 6, EarliestBlockHeight: 3}},
	}
	wm := newTestWatermarkManager(tmClient, 6, nil, 6)

	require.NoError(t, wm.EnsureBlockHeightAvailable(context.Background(), 5))

	require.ErrorContains(t, wm.EnsureBlockHeightAvailable(context.Background(), 7), "not yet available")
	require.ErrorContains(t, wm.EnsureBlockHeightAvailable(context.Background(), 2), "has been pruned")
}

func TestEnsureReceiptHeightAvailable(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 200, EarliestBlockHeight: 1}},
	}

	t.Run("receipt store with no pruning allows any height", func(t *testing.T) {
		rs := &fakeReceiptStore{latest: 200, earliest: 0}
		wm := NewWatermarkManager(tmClient, watermarkTestCtxProvider(200), nil, rs)
		require.NoError(t, wm.EnsureReceiptHeightAvailable(5))
	})

	t.Run("pruned receipt height returns error", func(t *testing.T) {
		rs := &fakeReceiptStore{latest: 200, earliest: 150}
		wm := NewWatermarkManager(tmClient, watermarkTestCtxProvider(200), nil, rs)
		require.ErrorContains(t, wm.EnsureReceiptHeightAvailable(100), "receipts have been pruned")
		require.ErrorContains(t, wm.EnsureReceiptHeightAvailable(149), "receipts have been pruned")
	})

	t.Run("height within receipt retention succeeds", func(t *testing.T) {
		rs := &fakeReceiptStore{latest: 200, earliest: 150}
		wm := NewWatermarkManager(tmClient, watermarkTestCtxProvider(200), nil, rs)
		require.NoError(t, wm.EnsureReceiptHeightAvailable(150))
		require.NoError(t, wm.EnsureReceiptHeightAvailable(175))
	})
}

func TestLatestAndEarliestHeightHelpers(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 22, EarliestBlockHeight: 11}},
	}
	stateStore := &fakeStateStore{latest: 22, earliest: 11}
	wm := newTestWatermarkManager(tmClient, 22, stateStore, 22)
	earliest, err := wm.EarliestHeight(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(11), earliest)
	earliestState, err := wm.EarliestStateHeight(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(11), earliestState)
	latest, err := wm.LatestHeight(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(22), latest)
}

func TestResolveHeightUsesStateEarliest(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 20, EarliestBlockHeight: 5}},
	}
	stateStore := &fakeStateStore{latest: 18, earliest: 10}
	wm := newTestWatermarkManager(tmClient, 20, stateStore, 20)

	belowState := rpc.BlockNumber(9)
	_, err := wm.ResolveHeight(context.Background(), rpc.BlockNumberOrHash{BlockNumber: &belowState})
	require.Error(t, err)
	require.Contains(t, err.Error(), "has been pruned")

	within := rpc.BlockNumber(12)
	resolved, err := wm.ResolveHeight(context.Background(), rpc.BlockNumberOrHash{BlockNumber: &within})
	require.NoError(t, err)
	require.Equal(t, int64(12), resolved)
}

func TestStateWatermarksCanLagBlocks(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 30, EarliestBlockHeight: 12}},
	}
	stateStore := &fakeStateStore{latest: 28, earliest: 15}
	wm := newTestWatermarkManager(tmClient, 30, stateStore, 29)

	blockEarliest, stateEarliest, latest, err := wm.Watermarks(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(12), blockEarliest)
	require.Equal(t, int64(15), stateEarliest)
	require.Equal(t, int64(28), latest)
}

// TestResolveEarliestFloorsAtBlockEarliestOnFreshNode reproduces SEI-10383. On a
// fresh, never-pruned SS-enabled node GetEarliestVersion() returns 0. Without a
// floor, `earliest` resolves to height 0, which CreateQueryContext coerces to
// lastBlockHeight (the tip), so eth_getTransactionCount/getCode/... at `earliest`
// return current state instead of genesis. stateEarliest must floor at
// blockEarliest so `earliest` resolves to genesis.
func TestResolveEarliestFloorsAtBlockEarliestOnFreshNode(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 6, EarliestBlockHeight: 1}},
	}
	// Fresh node: state store enabled, but the earliest version was never
	// written (nothing pruned, no state-sync), so GetEarliestVersion() == 0.
	stateStore := &fakeStateStore{latest: 6, earliest: 0}
	wm := newTestWatermarkManager(tmClient, 6, stateStore, 6)

	blockEarliest, stateEarliest, latest, err := wm.Watermarks(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), blockEarliest)
	require.Equal(t, int64(1), stateEarliest, "fresh node: stateEarliest must floor at blockEarliest, not 0")
	require.Equal(t, int64(6), latest)

	// The `earliest` tag must resolve to genesis (blockEarliest=1), never 0.
	earliest := rpc.EarliestBlockNumber
	resolved, err := wm.ResolveHeight(context.Background(), rpc.BlockNumberOrHash{BlockNumber: &earliest})
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved, "earliest must resolve to blockEarliest, not 0 (SEI-10383)")
}

func newTestWatermarkManager(tmClient client.LocalClient, ctxHeight int64, stateStore types.StateStore, receiptLatest int64) *WatermarkManager {
	return NewWatermarkManager(tmClient, watermarkTestCtxProvider(ctxHeight), stateStore, &fakeReceiptStore{latest: receiptLatest})
}

func watermarkTestCtxProvider(height int64) func(int64) sdk.Context {
	return func(int64) sdk.Context {
		return sdk.Context{}.
			WithBlockHeight(height).
			WithMultiStore(&fakeMultiStore{latest: height})
	}
}

type fakeTMClient struct {
	client.Client
	status         *coretypes.ResultStatus
	statusErr      error
	blockByHash    *coretypes.ResultBlock
	blockByHashErr error
	blocksByHeight map[int64]*coretypes.ResultBlock
	genesis        *coretypes.ResultGenesis
}

func (*fakeTMClient) EvmNextPendingNonce(common.Address) uint64 {
	return 0
}

func (*fakeTMClient) EvmTxByHash(common.Hash) (tmtypes.Tx, bool) {
	return nil, false
}

func (*fakeTMClient) EvmProxy(common.Address) utils.Option[*url.URL] {
	return utils.None[*url.URL]()
}

func (f *fakeTMClient) Status(context.Context) (*coretypes.ResultStatus, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	if f.status != nil {
		return f.status, nil
	}
	return nil, errors.New("status not set")
}

func (f *fakeTMClient) BlockByHash(_ context.Context, _ bytes.HexBytes) (*coretypes.ResultBlock, error) {
	if f.blockByHashErr != nil {
		return nil, f.blockByHashErr
	}
	if f.blockByHash != nil {
		return f.blockByHash, nil
	}
	return nil, errors.New("block not found")
}

func (f *fakeTMClient) Block(_ context.Context, height *int64) (*coretypes.ResultBlock, error) {
	if f.blocksByHeight == nil {
		if f.blockByHash != nil {
			return f.blockByHash, nil
		}
		return nil, errors.New("block not found")
	}
	h := int64(-1)
	if height != nil {
		h = *height
	}
	if res, ok := f.blocksByHeight[h]; ok {
		return res, nil
	}
	return nil, errors.New("block not found")
}

func (f *fakeTMClient) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	if f.genesis != nil {
		return f.genesis, nil
	}
	return &coretypes.ResultGenesis{Genesis: &tmtypes.GenesisDoc{InitialHeight: 1}}, nil
}

type fakeStateStore struct {
	latest   int64
	earliest int64
}

func (f *fakeStateStore) Get(string, int64, []byte) ([]byte, error) { return nil, nil }
func (f *fakeStateStore) Has(string, int64, []byte) (bool, error)   { return false, nil }
func (f *fakeStateStore) Iterator(string, int64, []byte, []byte) (dbm.Iterator, error) {
	return nil, nil
}
func (f *fakeStateStore) ReverseIterator(string, int64, []byte, []byte) (dbm.Iterator, error) {
	return nil, nil
}
func (f *fakeStateStore) RawIterate(string, func([]byte, []byte, int64) bool) (bool, error) {
	return false, nil
}
func (f *fakeStateStore) GetLatestVersion() int64                                      { return f.latest }
func (f *fakeStateStore) SetLatestVersion(version int64) error                         { return nil }
func (f *fakeStateStore) GetEarliestVersion() int64                                    { return f.earliest }
func (f *fakeStateStore) SetEarliestVersion(version int64, _ bool) error               { return nil }
func (f *fakeStateStore) ApplyChangesetSync(_ int64, _ []*proto.NamedChangeSet) error  { return nil }
func (f *fakeStateStore) ApplyChangesetAsync(_ int64, _ []*proto.NamedChangeSet) error { return nil }
func (f *fakeStateStore) Import(_ int64, _ <-chan types.SnapshotNode) error            { return nil }
func (f *fakeStateStore) Prune(_ int64) error                                          { return nil }
func (f *fakeStateStore) Close() error                                                 { return nil }

type fakeReceiptStore struct {
	latest   int64
	earliest int64
}

func (f *fakeReceiptStore) LatestVersion() int64 {
	return f.latest
}

func (f *fakeReceiptStore) EarliestVersion() int64 {
	return f.earliest
}

func (f *fakeReceiptStore) SetLatestVersion(version int64) error {
	f.latest = version
	return nil
}

func (f *fakeReceiptStore) SetEarliestVersion(version int64) error {
	f.earliest = version
	return nil
}

func (f *fakeReceiptStore) GetReceipt(sdk.Context, common.Hash) (*evmtypes.Receipt, error) {
	return nil, errors.New("not found")
}

func (f *fakeReceiptStore) GetReceiptFromStore(sdk.Context, common.Hash) (*evmtypes.Receipt, error) {
	return nil, errors.New("not found")
}

func (f *fakeReceiptStore) SetReceipts(sdk.Context, []receipt.ReceiptRecord) error {
	return nil
}

func (f *fakeReceiptStore) FilterLogs(sdk.Context, uint64, uint64, filters.FilterCriteria) ([]*ethtypes.Log, error) {
	return nil, receipt.ErrRangeQueryNotSupported
}

func (f *fakeReceiptStore) Close() error { return nil }

type fakeMultiStore struct {
	earliest int64
	latest   int64
}

func (f *fakeMultiStore) GetStoreType() storetypes.StoreType                 { return storetypes.StoreTypeMulti }
func (f *fakeMultiStore) CacheWrap(storetypes.StoreKey) storetypes.CacheWrap { return nil }
func (f *fakeMultiStore) CacheWrapWithTrace(storetypes.StoreKey, io.Writer, storetypes.TraceContext) storetypes.CacheWrap {
	return nil
}
func (f *fakeMultiStore) CacheMultiStore() storetypes.CacheMultiStore { return nil }
func (f *fakeMultiStore) CacheMultiStoreWithVersion(int64) (storetypes.CacheMultiStore, error) {
	return nil, nil
}
func (f *fakeMultiStore) CacheMultiStoreForExport(int64) (storetypes.CacheMultiStore, error) {
	return nil, nil
}
func (f *fakeMultiStore) GetStore(storetypes.StoreKey) storetypes.Store                   { return nil }
func (f *fakeMultiStore) GetKVStore(storetypes.StoreKey) storetypes.KVStore               { return nil }
func (f *fakeMultiStore) GetEarliestVersion() int64                                       { return f.earliest }
func (f *fakeMultiStore) TracingEnabled() bool                                            { return false }
func (f *fakeMultiStore) SetTracer(io.Writer) storetypes.MultiStore                       { return f }
func (f *fakeMultiStore) SetTracingContext(storetypes.TraceContext) storetypes.MultiStore { return f }
func (f *fakeMultiStore) GetWorkingHash() ([]byte, error)                                 { return nil, nil }
func (f *fakeMultiStore) GetEvents() []abci.Event                                         { return nil }
func (f *fakeMultiStore) ResetEvents()                                                    {}
func (f *fakeMultiStore) SetKVStores(func(storetypes.StoreKey, storetypes.KVStore) storetypes.CacheWrap) storetypes.MultiStore {
	return f
}
func (f *fakeMultiStore) StoreKeys() []storetypes.StoreKey { return nil }
func (f *fakeMultiStore) LastCommitID() storetypes.CommitID {
	return storetypes.CommitID{Version: f.latest}
}

func makeBlockResult(height int64) *coretypes.ResultBlock {
	return &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{},
		Block: &tmtypes.Block{
			Header: tmtypes.Header{Height: height},
		},
	}
}

// blockByNumberOrNullForJSONRPC: above-watermark height returns (nil, nil);
// in-range height returns the block; non-watermark errors propagate.
func TestBlockByNumberOrNullForJSONRPC(t *testing.T) {
	t.Parallel()

	stat := &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 100, EarliestBlockHeight: 1}}

	t.Run("above watermark returns (nil, nil)", func(t *testing.T) {
		c := &fakeTMClient{status: stat, blocksByHeight: map[int64]*coretypes.ResultBlock{150: makeBlockResult(150)}}
		wm := newTestWatermarkManager(c, 100, nil, 100)
		h := int64(150) // above latest=100
		block, err := blockByNumberOrNullForJSONRPC(context.Background(), c, wm, &h, 0)
		require.NoError(t, err)
		require.Nil(t, block)
	})

	t.Run("in-range height returns block", func(t *testing.T) {
		c := &fakeTMClient{status: stat, blocksByHeight: map[int64]*coretypes.ResultBlock{50: makeBlockResult(50)}}
		wm := newTestWatermarkManager(c, 100, nil, 100)
		h := int64(50)
		block, err := blockByNumberOrNullForJSONRPC(context.Background(), c, wm, &h, 0)
		require.NoError(t, err)
		require.NotNil(t, block)
		require.Equal(t, int64(50), block.Block.Height)
	})

	t.Run("non-watermark error propagates", func(t *testing.T) {
		// Watermark itself fails (TM status error) — error is errNoHeightSource,
		// which must NOT be silently converted to null.
		c := &fakeTMClient{statusErr: errNoHeightSource}
		wm := newTestWatermarkManager(c, 100, nil, 100)
		h := int64(50)
		_, err := blockByNumberOrNullForJSONRPC(context.Background(), c, wm, &h, 0)
		require.Error(t, err)
		require.False(t, errors.Is(err, ErrBlockHeightNotYetAvailable))
	})
}

// blockByHashOrNullForJSONRPC: above-watermark AND unknown-hash both return
// (nil, nil); in-range hash returns the block; other errors propagate.
func TestBlockByHashOrNullForJSONRPC(t *testing.T) {
	t.Parallel()

	stat := &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 100, EarliestBlockHeight: 1}}

	t.Run("above watermark returns (nil, nil)", func(t *testing.T) {
		c := &fakeTMClient{status: stat, blockByHash: makeBlockResult(150)} // height above latest=100
		wm := newTestWatermarkManager(c, 100, nil, 100)
		block, err := blockByHashOrNullForJSONRPC(context.Background(), c, wm, []byte{0xaa}, 0)
		require.NoError(t, err)
		require.Nil(t, block)
	})

	t.Run("unknown hash (Block: nil) returns (nil, nil)", func(t *testing.T) {
		// blockByHashWithRetry wraps Block:nil as ErrBlockNotFoundByHash;
		// the helper must catch that sentinel too.
		c := &fakeTMClient{status: stat, blockByHash: &coretypes.ResultBlock{Block: nil}}
		wm := newTestWatermarkManager(c, 100, nil, 100)
		block, err := blockByHashOrNullForJSONRPC(context.Background(), c, wm, []byte{0xbb}, 0)
		require.NoError(t, err)
		require.Nil(t, block)
	})

	t.Run("in-range hash returns block", func(t *testing.T) {
		c := &fakeTMClient{status: stat, blockByHash: makeBlockResult(50)}
		wm := newTestWatermarkManager(c, 100, nil, 100)
		block, err := blockByHashOrNullForJSONRPC(context.Background(), c, wm, []byte{0xcc}, 0)
		require.NoError(t, err)
		require.NotNil(t, block)
		require.Equal(t, int64(50), block.Block.Height)
	})

	t.Run("transport error propagates", func(t *testing.T) {
		// A non-sentinel error from the TM client (e.g. RPC transport
		// failure) must NOT be silently swallowed into null.
		c := &fakeTMClient{status: stat, blockByHashErr: io.ErrUnexpectedEOF}
		wm := newTestWatermarkManager(c, 100, nil, 100)
		_, err := blockByHashOrNullForJSONRPC(context.Background(), c, wm, []byte{0xdd}, 0)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})
}
