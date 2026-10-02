package evmrpc_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	tmutils "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

type blockingPointReadStore struct {
	sdk.KVStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingPointReadStore) Get(key []byte) []byte {
	s.wait()
	return s.KVStore.Get(key)
}

func (s *blockingPointReadStore) VersionExists(version int64) bool {
	s.wait()
	return s.KVStore.VersionExists(version)
}

func (s *blockingPointReadStore) wait() {
	s.once.Do(func() { close(s.entered) })
	<-s.release
}

type pointReadMultiStore struct {
	sdk.MultiStore
	target sdk.StoreKey
	store  sdk.KVStore
}

func (m pointReadMultiStore) GetKVStore(key sdk.StoreKey) sdk.KVStore {
	if key.Name() == m.target.Name() {
		return m.store
	}
	return m.MultiStore.GetKVStore(key)
}

func TestStateReadsReturnDeadlineExceededAfterPointRead(t *testing.T) {
	tests := []struct {
		name     string
		storeKey sdk.StoreKey
		read     func(context.Context, *evmrpc.StateAPI) error
	}{
		{
			name:     "balance",
			storeKey: EVMKeeper.GetStoreKey(),
			read: func(ctx context.Context, api *evmrpc.StateAPI) error {
				_, err := api.GetBalance(ctx, common.HexToAddress("0x1234567890123456789012345678901234567890"), rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber))
				return err
			},
		},
		{
			name:     "code",
			storeKey: EVMKeeper.GetStoreKey(),
			read: func(ctx context.Context, api *evmrpc.StateAPI) error {
				_, err := api.GetCode(ctx, common.Address{}, rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber))
				return err
			},
		},
		{
			name:     "storage",
			storeKey: EVMKeeper.GetStoreKey(),
			read: func(ctx context.Context, api *evmrpc.StateAPI) error {
				_, err := api.GetStorageAt(ctx, common.Address{}, common.Hash{}.Hex(), rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber))
				return err
			},
		},
		{
			name:     "nonce",
			storeKey: EVMKeeper.GetStoreKey(),
			read: func(ctx context.Context, api *evmrpc.StateAPI) error {
				_, err := api.GetNonce(ctx, common.Address{})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const stateHeight int64 = 1
			entered := make(chan struct{})
			release := make(chan struct{})
			evmStore := &blockingPointReadStore{
				KVStore: Ctx.MultiStore().GetKVStore(tt.storeKey),
				entered: entered,
				release: release,
			}
			multiStore := pointReadMultiStore{
				MultiStore: Ctx.MultiStore(),
				target:     tt.storeKey,
				store:      evmStore,
			}
			ctxProvider := func(height int64) sdk.Context {
				if height == evmrpc.LatestCtxHeight {
					height = stateHeight
				}
				return Ctx.WithBlockHeight(height).WithMultiStore(multiStore)
			}
			watermarks := evmrpc.NewWatermarkManager(&MockClient{}, ctxProvider, nil, nil)
			api := evmrpc.NewStateAPI(&MockClient{}, EVMKeeper, ctxProvider, evmrpc.ConnectionTypeHTTP, watermarks)

			requestCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- tt.read(requestCtx, api) }()

			select {
			case <-entered:
			case err := <-result:
				t.Fatalf("handler returned before its store read started: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("store read did not start")
			}
			<-requestCtx.Done()
			close(release)
			require.ErrorIs(t, <-result, context.DeadlineExceeded)
		})
	}
}

func TestTransactionCountReturnsDeadlineExceededAfterPointRead(t *testing.T) {
	const stateHeight int64 = 1
	entered := make(chan struct{})
	release := make(chan struct{})
	store := &blockingPointReadStore{
		KVStore: Ctx.MultiStore().GetKVStore(EVMKeeper.GetStoreKey()),
		entered: entered,
		release: release,
	}
	multiStore := pointReadMultiStore{
		MultiStore: Ctx.MultiStore(),
		target:     EVMKeeper.GetStoreKey(),
		store:      store,
	}
	ctxProvider := func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			height = stateHeight
		}
		return Ctx.WithBlockHeight(height).WithMultiStore(multiStore)
	}
	watermarks := evmrpc.NewWatermarkManager(&MockClient{}, ctxProvider, nil, nil)
	api := evmrpc.NewTransactionAPI(
		&MockClient{}, EVMKeeper, ctxProvider, func(int64) client.TxConfig { return TxConfig },
		"", evmrpc.ConnectionTypeHTTP, tmutils.None[time.Duration](), watermarks, evmrpc.NewBlockCache(1), &sync.Mutex{},
	)

	requestCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := api.GetTransactionCount(requestCtx, common.Address{}, rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber))
		result <- err
	}()

	requireReadStarted(t, entered)
	<-requestCtx.Done()
	close(release)
	require.ErrorIs(t, <-result, context.DeadlineExceeded)
}

type blockingReceiptStore struct {
	receipt.ReceiptStore
	entered chan struct{}
	release chan struct{}
	ctxErr  chan error
}

type blockAfterFirstReceiptStore struct {
	receipt.ReceiptStore
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (s *blockAfterFirstReceiptStore) GetReceipt(ctx sdk.Context, hash common.Hash) (*evmtypes.Receipt, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	if call > 1 {
		if call == 2 {
			close(s.entered)
		}
		<-s.release
	}
	return s.ReceiptStore.GetReceipt(ctx, hash)
}

func (s *blockingReceiptStore) GetReceipt(ctx sdk.Context, _ common.Hash) (*evmtypes.Receipt, error) {
	close(s.entered)
	select {
	case <-ctx.Context().Done():
	case <-s.release:
	}
	s.ctxErr <- ctx.Context().Err()
	return &evmtypes.Receipt{BlockNumber: MockHeight8, Status: 1}, nil
}

func TestReceiptHandlersReturnDeadlineExceededDuringRead(t *testing.T) {
	tests := []struct {
		name string
		read func(context.Context, *evmrpc.TransactionAPI) error
	}{
		{
			name: "transaction receipt",
			read: func(ctx context.Context, api *evmrpc.TransactionAPI) error {
				_, err := api.GetTransactionReceipt(ctx, common.Hash{})
				return err
			},
		},
		{
			name: "transaction error",
			read: func(ctx context.Context, api *evmrpc.TransactionAPI) error {
				_, err := api.GetTransactionErrorByHash(ctx, common.Hash{})
				return err
			},
		},
		{
			name: "VM error",
			read: func(ctx context.Context, api *evmrpc.TransactionAPI) error {
				_, err := api.GetVMError(ctx, common.Hash{})
				return err
			},
		},
		{
			name: "transaction by hash",
			read: func(ctx context.Context, api *evmrpc.TransactionAPI) error {
				_, err := api.GetTransactionByHash(ctx, common.Hash{})
				return err
			},
		},
		{
			name: "transaction by block number and index",
			read: func(ctx context.Context, api *evmrpc.TransactionAPI) error {
				_, err := api.GetTransactionByBlockNumberAndIndex(ctx, rpc.BlockNumber(MockHeight8), 0)
				return err
			},
		},
		{
			name: "transaction by block hash and index",
			read: func(ctx context.Context, api *evmrpc.TransactionAPI) error {
				_, err := api.GetTransactionByBlockHashAndIndex(ctx, common.Hash{31: 1}, 0)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalStore := EVMKeeper.ReceiptStore()
			store := &blockingReceiptStore{
				ReceiptStore: originalStore,
				entered:      make(chan struct{}),
				release:      make(chan struct{}),
				ctxErr:       make(chan error, 1),
			}
			EVMKeeper.SetReceiptStoreForTesting(store)
			defer EVMKeeper.SetReceiptStoreForTesting(originalStore)

			ctxProvider := func(int64) sdk.Context { return Ctx }
			watermarks := evmrpc.NewWatermarkManager(&MockClient{}, ctxProvider, nil, store)
			api := evmrpc.NewTransactionAPI(
				&MockClient{}, EVMKeeper, ctxProvider, func(int64) client.TxConfig { return TxConfig },
				"", evmrpc.ConnectionTypeHTTP, tmutils.None[time.Duration](), watermarks, evmrpc.NewBlockCache(1), &sync.Mutex{},
			)

			requestCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- tt.read(requestCtx, api) }()

			requireReadStarted(t, store.entered)
			<-requestCtx.Done()
			close(store.release)
			require.ErrorIs(t, <-result, context.DeadlineExceeded)
			require.True(t, errors.Is(<-store.ctxErr, context.DeadlineExceeded), "receipt store did not receive the request deadline")
		})
	}
}

func TestTransactionReceiptReturnsDeadlineExceededDuringNormalizationRead(t *testing.T) {
	originalStore := EVMKeeper.ReceiptStore()
	store := &blockAfterFirstReceiptStore{
		ReceiptStore: originalStore,
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	EVMKeeper.SetReceiptStoreForTesting(store)
	defer EVMKeeper.SetReceiptStoreForTesting(originalStore)

	ctxProvider := func(int64) sdk.Context { return Ctx }
	watermarks := evmrpc.NewWatermarkManager(&MockClient{}, ctxProvider, nil, store)
	api := evmrpc.NewTransactionAPI(
		&MockClient{}, EVMKeeper, ctxProvider, func(int64) client.TxConfig { return TxConfig },
		"", evmrpc.ConnectionTypeHTTP, tmutils.None[time.Duration](), watermarks, evmrpc.NewBlockCache(1), &sync.Mutex{},
	)

	requestCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := api.GetTransactionReceipt(requestCtx, tx1.Hash())
		result <- err
	}()

	requireReadStarted(t, store.entered)
	<-requestCtx.Done()
	close(store.release)
	require.ErrorIs(t, <-result, context.DeadlineExceeded)
}

func TestGetReceiptWithRetryPropagatesDeadlineIntoRead(t *testing.T) {
	originalStore := EVMKeeper.ReceiptStore()
	store := &blockingReceiptStore{
		ReceiptStore: originalStore,
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
		ctxErr:       make(chan error, 1),
	}
	EVMKeeper.SetReceiptStoreForTesting(store)
	defer EVMKeeper.SetReceiptStoreForTesting(originalStore)

	requestCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := EVMKeeper.GetReceiptWithRetry(requestCtx, Ctx, common.Hash{}, 1)
		result <- err
	}()

	requireReadStarted(t, store.entered)
	<-requestCtx.Done()
	close(store.release)
	require.ErrorIs(t, <-result, context.DeadlineExceeded)
	require.ErrorIs(t, <-store.ctxErr, context.DeadlineExceeded)
}

func requireReadStarted(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("store read did not start")
	}
}
