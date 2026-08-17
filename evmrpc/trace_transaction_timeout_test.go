package evmrpc

import (
	"context"
	"errors"
	"io"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/tracersutils"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/stretchr/testify/require"
)

// TestTraceTransactionTimeoutReleasesSemaphore sends debug_traceTransaction
// through a StateAtTransaction that blocks in an SS-shaped skip loop. The
// first call returns the deadline, a concurrent call is rejected as busy, and
// the semaphore slot is free afterwards.
func TestTraceTransactionTimeoutReleasesSemaphore(t *testing.T) {
	t.Parallel()

	store := newSlowSkipStore()
	backend := newSlowIterTraceBackend(store)
	api := &DebugAPI{
		tracersAPI:         tracers.NewAPI(backend),
		keeper:             &keeper.Keeper{},
		ctxProvider:        func(int64) sdk.Context { return sdk.Context{} },
		traceCallSemaphore: make(chan struct{}, 1),
		traceTimeout:       50 * time.Millisecond,
	}

	firstErr := make(chan error, 1)
	go func() {
		_, err := api.TraceTransaction(context.Background(), backend.tx.Hash(), nil)
		firstErr <- err
	}()

	select {
	case <-store.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("StateAtTransaction did not enter the skip loop")
	}

	_, busyErr := api.TraceTransaction(context.Background(), backend.tx.Hash(), nil)
	require.ErrorIs(t, busyErr, errTraceConcurrencyLimit)

	select {
	case err := <-firstErr:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(2 * time.Second):
		t.Fatal("timed-out trace did not return")
	}

	select {
	case api.traceCallSemaphore <- struct{}{}:
		<-api.traceCallSemaphore
	default:
		t.Fatal("expected the timed-out trace to release the semaphore")
	}

	_, err := api.TraceTransaction(context.Background(), backend.tx.Hash(), nil)
	require.NotErrorIs(t, err, errTraceConcurrencyLimit)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

type slowSkipStore struct {
	entered chan struct{}
	once    sync.Once
}

func newSlowSkipStore() *slowSkipStore {
	return &slowSkipStore{entered: make(chan struct{})}
}

var (
	_ sdk.KVStore                = (*slowSkipStore)(nil)
	_ storetypes.ContextIterator = (*slowSkipStore)(nil)
)

func (s *slowSkipStore) skipUntilCancelled(ctx context.Context) {
	s.once.Do(func() { close(s.entered) })
	for {
		if err := ctx.Err(); err != nil {
			panic(err)
		}
		time.Sleep(time.Millisecond)
	}
}

func (s *slowSkipStore) IteratorWithContext(ctx context.Context, _, _ []byte) storetypes.Iterator {
	s.skipUntilCancelled(ctx)
	return &emptyTraceIter{}
}

func (s *slowSkipStore) ReverseIteratorWithContext(ctx context.Context, _, _ []byte) storetypes.Iterator {
	s.skipUntilCancelled(ctx)
	return &emptyTraceIter{}
}

func (s *slowSkipStore) Iterator(_, _ []byte) storetypes.Iterator {
	panic("Iterator called without context; expected IteratorWithContext")
}

func (s *slowSkipStore) ReverseIterator(_, _ []byte) storetypes.Iterator {
	panic("ReverseIterator called without context; expected ReverseIteratorWithContext")
}

func (s *slowSkipStore) GetStoreType() sdk.StoreType { return sdk.StoreTypeDB }
func (s *slowSkipStore) CacheWrap(sdk.StoreKey) sdk.CacheWrap {
	panic("not implemented")
}
func (s *slowSkipStore) CacheWrapWithTrace(sdk.StoreKey, io.Writer, sdk.TraceContext) sdk.CacheWrap {
	panic("not implemented")
}
func (s *slowSkipStore) Get([]byte) []byte               { return nil }
func (s *slowSkipStore) Has([]byte) bool                 { return false }
func (s *slowSkipStore) Set(_, _ []byte)                 {}
func (s *slowSkipStore) Delete([]byte)                   {}
func (s *slowSkipStore) GetWorkingHash() ([]byte, error) { return nil, nil }
func (s *slowSkipStore) VersionExists(int64) bool        { return true }
func (s *slowSkipStore) DeleteAll(_, _ []byte) error     { return nil }
func (s *slowSkipStore) GetAllKeyStrsInRange(_, _ []byte) []string {
	return nil
}

type emptyTraceIter struct{}

func (emptyTraceIter) Domain() ([]byte, []byte) { return nil, nil }
func (emptyTraceIter) Valid() bool              { return false }
func (emptyTraceIter) Next()                    {}
func (emptyTraceIter) Key() []byte              { return nil }
func (emptyTraceIter) Value() []byte            { return nil }
func (emptyTraceIter) Error() error             { return nil }
func (emptyTraceIter) Close() error             { return nil }

type oneStoreMS struct {
	fakeMultiStore
	kv sdk.KVStore
}

func (m *oneStoreMS) GetKVStore(sdk.StoreKey) sdk.KVStore { return m.kv }

type slowIterTraceBackend struct {
	block *ethtypes.Block
	tx    *ethtypes.Transaction
	store *slowSkipStore
}

var _ tracers.Backend = (*slowIterTraceBackend)(nil)

func newSlowIterTraceBackend(store *slowSkipStore) *slowIterTraceBackend {
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &common.Address{},
	})
	header := &ethtypes.Header{
		Number:     big.NewInt(8),
		Time:       1,
		Difficulty: big.NewInt(0),
	}
	block := ethtypes.NewBlock(header, &ethtypes.Body{Transactions: ethtypes.Transactions{tx}}, nil, trie.NewStackTrie(nil))
	return &slowIterTraceBackend{block: block, tx: tx, store: store}
}

func (b *slowIterTraceBackend) HeaderByHash(context.Context, common.Hash) (*ethtypes.Header, error) {
	return b.block.Header(), nil
}

func (b *slowIterTraceBackend) HeaderByNumber(context.Context, rpc.BlockNumber) (*ethtypes.Header, error) {
	return b.block.Header(), nil
}

func (b *slowIterTraceBackend) BlockByHash(context.Context, common.Hash) (*ethtypes.Block, []tracersutils.TraceBlockMetadata, error) {
	return b.block, nil, nil
}

func (b *slowIterTraceBackend) BlockByNumber(context.Context, rpc.BlockNumber) (*ethtypes.Block, []tracersutils.TraceBlockMetadata, error) {
	return b.block, nil, nil
}

func (b *slowIterTraceBackend) GetTransaction(context.Context, common.Hash) (bool, *ethtypes.Transaction, common.Hash, uint64, uint64, error) {
	return true, b.tx, b.block.Hash(), b.block.NumberU64(), 0, nil
}

func (b *slowIterTraceBackend) RPCGasCap() uint64 { return 0 }

func (b *slowIterTraceBackend) ChainConfig() *params.ChainConfig { return params.TestChainConfig }

func (b *slowIterTraceBackend) ChainConfigAtHeight(int64) *params.ChainConfig {
	return params.TestChainConfig
}

func (b *slowIterTraceBackend) Engine() consensus.Engine { return nil }

func (b *slowIterTraceBackend) ChainDb() ethdb.Database { return nil }

func (b *slowIterTraceBackend) StateAtBlock(context.Context, *ethtypes.Block, uint64, vm.StateDB, bool, bool) (vm.StateDB, tracers.StateReleaseFunc, error) {
	return nil, func() {}, errors.New("unused")
}

func (b *slowIterTraceBackend) GetCustomPrecompiles(int64) map[common.Address]vm.PrecompiledContract {
	return nil
}

func (b *slowIterTraceBackend) PrepareTx(vm.StateDB, *ethtypes.Transaction) error { return nil }

func (b *slowIterTraceBackend) GetBlockContext(context.Context, *ethtypes.Block, vm.StateDB, export.ChainContextBackend) (vm.BlockContext, error) {
	return vm.BlockContext{}, errors.New("unused")
}

func (b *slowIterTraceBackend) StateAtTransaction(ctx context.Context, _ *ethtypes.Block, _ int, _ uint64) (*ethtypes.Transaction, vm.BlockContext, vm.StateDB, tracers.StateReleaseFunc, error) {
	key := sdk.NewKVStoreKey("evm")
	sdkCtx := sdk.NewContext(&oneStoreMS{kv: b.store}, tmproto.Header{}, false).WithContext(ctx)
	iter := sdkCtx.KVStore(key).Iterator(nil, nil)
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
	}
	if err := ctx.Err(); err != nil {
		return nil, vm.BlockContext{}, nil, func() {}, err
	}
	return nil, vm.BlockContext{}, nil, func() {}, errors.New("slow iteration returned without deadline")
}
