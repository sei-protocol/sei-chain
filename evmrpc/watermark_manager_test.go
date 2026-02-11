package evmrpc

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	receipt "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	proto "github.com/sei-protocol/sei-chain/sei-db/proto"
	sstypes "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/bytes"
	"github.com/tendermint/tendermint/rpc/client/mock"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

func TestWatermarksAggregatesSources(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 10, EarliestBlockHeight: 2}},
	}
	stateStore := &fakeStateStore{latest: 8, earliest: 3}
	receiptStore := &fakeReceiptStore{latest: 9}
	wm := NewWatermarkManager(tmClient, nil, stateStore, receiptStore)

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
	wm := NewWatermarkManager(tmClient, func(int64) sdk.Context { return ctx }, nil, nil)

	blockEarliest, stateEarliest, latest, err := wm.Watermarks(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(5), blockEarliest)
	require.Equal(t, int64(5), stateEarliest)
	require.Equal(t, int64(12), latest)
}

func TestWatermarksNoSources(t *testing.T) {
	wm := NewWatermarkManager(nil, nil, nil, nil)
	_, _, _, err := wm.Watermarks(context.Background())
	require.ErrorIs(t, err, errNoHeightSource)
}

func TestResolveHeightGating(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 5, EarliestBlockHeight: 2}},
	}
	wm := NewWatermarkManager(tmClient, nil, nil, nil)

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
	wm := NewWatermarkManager(tmClient, nil, nil, nil)
	h := common.HexToHash("0x1")
	blockHeight, err := wm.ResolveHeight(context.Background(), rpc.BlockNumberOrHash{BlockHash: &h})
	require.NoError(t, err)
	require.Equal(t, int64(4), blockHeight)
}

func TestEnsureBlockHeightAvailableBounds(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 6, EarliestBlockHeight: 3}},
	}
	wm := NewWatermarkManager(tmClient, nil, nil, nil)

	require.NoError(t, wm.EnsureBlockHeightAvailable(context.Background(), 5))

	require.ErrorContains(t, wm.EnsureBlockHeightAvailable(context.Background(), 7), "not yet available")
	require.ErrorContains(t, wm.EnsureBlockHeightAvailable(context.Background(), 2), "has been pruned")
}

func TestLatestAndEarliestHeightHelpers(t *testing.T) {
	tmClient := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 22, EarliestBlockHeight: 11}},
	}
	wm := NewWatermarkManager(tmClient, nil, nil, nil)
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
	wm := NewWatermarkManager(tmClient, nil, stateStore, nil)

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
	wm := NewWatermarkManager(tmClient, nil, stateStore, nil)

	blockEarliest, stateEarliest, latest, err := wm.Watermarks(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(12), blockEarliest)
	require.Equal(t, int64(15), stateEarliest)
	require.Equal(t, int64(28), latest)
}

type fakeTMClient struct {
	mock.Client
	status         *coretypes.ResultStatus
	statusErr      error
	blockByHash    *coretypes.ResultBlock
	blockByHashErr error
	blocksByHeight map[int64]*coretypes.ResultBlock
	genesis        *coretypes.ResultGenesis
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
func (f *fakeStateStore) Iterator(string, int64, []byte, []byte) (sstypes.DBIterator, error) {
	return nil, nil
}
func (f *fakeStateStore) ReverseIterator(string, int64, []byte, []byte) (sstypes.DBIterator, error) {
	return nil, nil
}
func (f *fakeStateStore) RawIterate(string, func([]byte, []byte, int64) bool) (bool, error) {
	return false, nil
}
func (f *fakeStateStore) GetLatestVersion() int64                                      { return f.latest }
func (f *fakeStateStore) SetLatestVersion(version int64) error                         { return nil }
func (f *fakeStateStore) GetEarliestVersion() int64                                    { return f.earliest }
func (f *fakeStateStore) SetEarliestVersion(version int64, _ bool) error               { return nil }
func (f *fakeStateStore) GetLatestMigratedKey() ([]byte, error)                        { return nil, nil }
func (f *fakeStateStore) GetLatestMigratedModule() (string, error)                     { return "", nil }
func (f *fakeStateStore) ApplyChangesetSync(_ int64, _ []*proto.NamedChangeSet) error  { return nil }
func (f *fakeStateStore) ApplyChangesetAsync(_ int64, _ []*proto.NamedChangeSet) error { return nil }
func (f *fakeStateStore) Import(_ int64, _ <-chan sstypes.SnapshotNode) error          { return nil }
func (f *fakeStateStore) RawImport(_ <-chan sstypes.RawSnapshotNode) error             { return nil }
func (f *fakeStateStore) Prune(_ int64) error                                          { return nil }
func (f *fakeStateStore) Close() error                                                 { return nil }

type fakeReceiptStore struct {
	latest int64
}

func (f *fakeReceiptStore) LatestVersion() int64 {
	return f.latest
}

func (f *fakeReceiptStore) SetLatestVersion(version int64) error {
	f.latest = version
	return nil
}

func (f *fakeReceiptStore) SetEarliestVersion(_ int64) error { return nil }

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
