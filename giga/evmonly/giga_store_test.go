package evmonly

import (
	"context"
	"errors"
	"math/big"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

type recordingGigaStore struct {
	snapshot    gigatypes.StateView
	openCount   int
	commitErr   error
	commitBlock []int64
	commits     []*gigatypes.StateChangeSet
}

func (s *recordingGigaStore) CommitStateChanges(blockNum int64, changes *gigatypes.StateChangeSet) error {
	s.commitBlock = append(s.commitBlock, blockNum)
	s.commits = append(s.commits, changes)
	return s.commitErr
}

func (s *recordingGigaStore) OpenView() gigatypes.StateView {
	s.openCount++
	return s.snapshot
}

func (s *recordingGigaStore) RegisterHashListener(gigatypes.HashListener) (lthash.BlockHash, error) {
	return lthash.BlockHash{}, nil
}

func (s *recordingGigaStore) OpenViewAt(int64) (gigatypes.StateView, bool) {
	return nil, false
}

func (s *recordingGigaStore) Close() error { return nil }

type memoryGigaSnapshot struct {
	height     int64
	balances   map[common.Address]common.Hash
	nonces     map[common.Address]uint64
	code       map[common.Address][]byte
	storage    map[gigaStorageKey]common.Hash
	closeCount int
}

type gigaStorageKey struct {
	address common.Address
	key     common.Hash
}

func newMemoryGigaSnapshot(height int64) *memoryGigaSnapshot {
	return &memoryGigaSnapshot{
		height:   height,
		balances: map[common.Address]common.Hash{},
		nonces:   map[common.Address]uint64{},
		code:     map[common.Address][]byte{},
		storage:  map[gigaStorageKey]common.Hash{},
	}
}

func (s *memoryGigaSnapshot) AccountExists(address gigatypes.Address) bool {
	if s.balances[address] != (common.Hash{}) || s.nonces[address] != 0 || len(s.code[address]) != 0 {
		return true
	}
	for key := range s.storage {
		if key.address == address {
			return true
		}
	}
	return false
}

func (s *memoryGigaSnapshot) GetStorage(address gigatypes.Address, slot gigatypes.Hash) gigatypes.Hash {
	return s.storage[gigaStorageKey{address: address, key: slot}]
}

func (s *memoryGigaSnapshot) GetBalance(address gigatypes.Address) gigatypes.Hash {
	return s.balances[address]
}

func (s *memoryGigaSnapshot) GetNonce(address gigatypes.Address) uint64 {
	return s.nonces[address]
}

func (s *memoryGigaSnapshot) GetCodeSize(address gigatypes.Address) int {
	return len(s.code[address])
}

func (s *memoryGigaSnapshot) GetCodeHash(address gigatypes.Address) gigatypes.Hash {
	if !s.AccountExists(address) {
		return gigatypes.Hash{}
	}
	return crypto.Keccak256Hash(s.code[address])
}

func (s *memoryGigaSnapshot) GetCode(address gigatypes.Address) []byte {
	return s.code[address]
}

func (s *memoryGigaSnapshot) ReadAccount(address gigatypes.Address) (gigatypes.Account, bool) {
	if !s.AccountExists(address) {
		return gigatypes.Account{}, false
	}
	return gigatypes.Account{
		Balance:  s.balances[address],
		Nonce:    s.nonces[address],
		CodeHash: s.GetCodeHash(address),
	}, true
}

func (s *memoryGigaSnapshot) GetBlockHeight() int64 {
	return s.height
}

func (s *memoryGigaSnapshot) Get(string, []byte) ([]byte, bool) {
	return nil, false
}

func (s *memoryGigaSnapshot) GetRaw([]byte) ([]byte, bool) {
	panic("memory giga snapshot holds no physical rows")
}

func (s *memoryGigaSnapshot) BatchGetRaw([][]byte) map[string][]byte {
	panic("memory giga snapshot holds no physical rows")
}

func (s *memoryGigaSnapshot) Close() {
	s.closeCount++
}

func (s *memoryGigaSnapshot) setBalance(addr common.Address, balance *big.Int) {
	var encoded common.Hash
	balance.FillBytes(encoded[:])
	s.balances[addr] = encoded
}

func TestGigaSnapshotStateReader(t *testing.T) {
	addr := testAddress(0xa8)
	slot := common.HexToHash("0x01")
	value := common.HexToHash("0x02")
	code := []byte{0x60, 0x00}
	snapshot := newMemoryGigaSnapshot(3)
	snapshot.setBalance(addr, big.NewInt(123))
	snapshot.nonces[addr] = 9
	snapshot.code[addr] = code
	snapshot.storage[gigaStorageKey{
		address: addr,
		key:     slot,
	}] = value
	reader := gigaSnapshotStateReader{snapshot: snapshot}

	require.Equal(t, big.NewInt(123), reader.GetBalance(addr))
	require.Equal(t, uint64(9), reader.GetNonce(addr))
	require.Equal(t, value, reader.GetState(addr, slot))
	gotCode := reader.GetCode(addr)
	require.Equal(t, code, gotCode)
	gotCode[0] = 0xff
	require.Equal(t, byte(0x60), snapshot.code[addr][0])
}

func TestGigaSnapshotStateReaderMissingStateFallback(t *testing.T) {
	present := testAddress(0xb0)
	nonceOnly := testAddress(0xb1)
	absent := testAddress(0xb2)
	slot := common.HexToHash("0x01")

	snapshot := newMemoryGigaSnapshot(3)
	snapshot.setBalance(present, big.NewInt(5))
	snapshot.nonces[nonceOnly] = 2

	fallback := NewMemoryState()
	for _, addr := range []common.Address{present, nonceOnly, absent} {
		fallback.SetBalance(addr, big.NewInt(1000))
		fallback.SetNonce(addr, 77)
		fallback.SetCode(addr, []byte{0xfe})
		fallback.SetState(addr, slot, common.HexToHash("0x99"))
	}
	reader := gigaSnapshotStateReader{snapshot: snapshot, missingState: fallback}

	require.Equal(t, big.NewInt(5), reader.GetBalance(present))
	require.Equal(t, uint64(0), reader.GetNonce(present))
	require.Empty(t, reader.GetCode(present))
	require.Equal(t, common.Hash{}, reader.GetState(present, slot))

	require.Zero(t, reader.GetBalance(nonceOnly).Sign())
	require.Equal(t, uint64(2), reader.GetNonce(nonceOnly))

	require.Equal(t, big.NewInt(1000), reader.GetBalance(absent))
	require.Equal(t, uint64(77), reader.GetNonce(absent))
	require.Equal(t, []byte{0xfe}, reader.GetCode(absent))
	require.Equal(t, common.HexToHash("0x99"), reader.GetState(absent, slot))

	noFallback := gigaSnapshotStateReader{snapshot: snapshot}
	require.Zero(t, noFallback.GetBalance(absent).Sign())
	require.Equal(t, uint64(0), noFallback.GetNonce(absent))
	require.Empty(t, noFallback.GetCode(absent))
	require.Equal(t, common.Hash{}, noFallback.GetState(absent, slot))
}

func TestExecutorCommitsGigaStoreStateChanges(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &recordingGigaStore{snapshot: snapshot}
	wantMisc := []gigatypes.MiscChange{{Module: "encoded", Key: []byte("key"), Value: []byte("value")}}
	encodeCalls := 0
	encoder := func(BlockContext, *BlockResult) ([]gigatypes.MiscChange, error) {
		encodeCalls++
		return wantMisc, nil
	}

	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)
	blockCtx := blockContext(chainID)
	blockCtx.Number = 41
	executor := NewExecutor(
		Config{},
		withTestStores(store, NewMemoryReceiptStore()),
		WithBlockChangeSetEncoder(encoder),
	)
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockCtx,
		Txs:     [][]byte{rawTx},
	})

	require.NoError(t, err)
	require.Equal(t, 1, encodeCalls)
	require.Equal(t, 1, store.openCount)
	require.Equal(t, 1, snapshot.closeCount)
	require.Equal(t, []int64{41}, store.commitBlock)
	require.Len(t, store.commits, 1)
	require.True(t, slices.ContainsFunc(store.commits[0].Accounts, func(change AccountChange) bool {
		return change.Address == sender && change.Prior.Nonce == 0 && change.Account.Nonce == 1
	}))
	require.Equal(t, wantMisc, store.commits[0].Misc)
	require.Contains(t, result.ChangeSet.Accounts, AccountChange{
		Address: recipient,
		Account: AccountState{Balance: big.NewInt(7)},
		Prior:   AccountState{Balance: new(uint256.Int).ToBig()},
	})
	result.Release()
}

// Releasing a pooled block result resets its change slices; the pending commit, which the next block
// reads through an overlay, keeps its changes.
func TestPipelineCommitKeepsItsChangesWhenTheResultIsReleased(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xab)
	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(1_000_000_000_000_000_000))
	store := &recordingGigaStore{snapshot: snapshot}
	executor := NewExecutor(Config{BlockResultPoolSize: 1}, withTestStores(store, NewMemoryReceiptStore()))
	defer executor.Close()

	prepared, err := executor.PrepareBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)},
	})
	require.NoError(t, err)
	result, err := executor.ExecutePreparedBlock(t.Context(), prepared)
	require.NoError(t, err)
	pending := executor.pipelinePending()
	require.NotNil(t, pending)
	require.NotEmpty(t, pending.Accounts)
	accounts := slices.Clone(pending.Accounts)

	result.Release()

	require.Equal(t, accounts, pending.Accounts)
}

func TestExecutorGigaStoreSnapshotFeedsOCCExecution(t *testing.T) {
	chainID := big.NewInt(testChainID)
	snapshot := newMemoryGigaSnapshot(8)
	rawTxs := make([][]byte, 0, 2)
	recipients := make([]common.Address, 0, 2)
	for i := range 2 {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		recipient := testAddress(byte(0xaa + i))
		snapshot.setBalance(sender, big.NewInt(1_000_000_000))
		recipients = append(recipients, recipient)
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(
			t, key, chainID, 0, &recipient, big.NewInt(int64(i+1)), nil, 100_000, big.NewInt(0),
		))
	}

	store := &recordingGigaStore{snapshot: snapshot}
	executor := NewExecutor(
		Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2},
		withTestStores(store, NewMemoryReceiptStore()),
	)
	defer executor.Close()
	blockCtx := blockContext(chainID)
	blockCtx.Number = 9

	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockCtx,
		Txs:     rawTxs,
	})

	require.NoError(t, err)
	require.True(t, result.OCCStats.Attempted)
	require.Equal(t, 1, snapshot.closeCount)
	require.Len(t, store.commits, 1)
	for i, recipient := range recipients {
		accounts := store.commits[0].Accounts
		at := slices.IndexFunc(accounts, func(c AccountChange) bool { return c.Address == recipient })
		require.GreaterOrEqual(t, at, 0, "no account change for recipient %s", recipient)
		change := accounts[at]
		// Compared by value: big.Int's internal form differs between equal values.
		require.Zero(t, change.Account.Balance.Cmp(big.NewInt(int64(i+1))), "recipient %s balance", recipient)
		require.Zero(t, change.Prior.Balance.Sign(), "recipient %s prior balance", recipient)
		require.Zero(t, change.Account.Nonce)
		require.Empty(t, change.Account.Code)
	}
	result.Release()
}

func TestExecutorGigaStoreFailuresDoNotCommitPartialState(t *testing.T) {
	t.Run("missing stores", func(t *testing.T) {
		executor := NewExecutor(Config{})

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID))})

		require.ErrorIs(t, err, errMissingStateStore)
		require.Nil(t, result)
	})

	t.Run("missing state store", func(t *testing.T) {
		executor := NewExecutor(Config{}, WithReceiptStore(NewMemoryReceiptStore()))

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID))})

		require.ErrorIs(t, err, errMissingStateStore)
		require.Nil(t, result)
	})

	t.Run("no receipt store commits state without receipts", func(t *testing.T) {
		store := NewMemoryStore(NewMemoryState())
		executor := NewExecutor(Config{}, WithStore(store))

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID))})

		require.NoError(t, err)
		require.NotNil(t, result)
		result.Release()
	})

	t.Run("nil snapshot", func(t *testing.T) {
		store := &recordingGigaStore{}
		executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore()))

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID))})

		require.ErrorContains(t, err, "nil snapshot")
		require.Nil(t, result)
		require.Empty(t, store.commits)
	})

	t.Run("encoder error", func(t *testing.T) {
		snapshot := newMemoryGigaSnapshot(0)
		store := &recordingGigaStore{snapshot: snapshot}
		encodeErr := errors.New("encode failed")
		executor := NewExecutor(
			Config{BlockResultPoolSize: 1},
			withTestStores(store, NewMemoryReceiptStore()),
			WithBlockChangeSetEncoder(func(BlockContext, *BlockResult) ([]gigatypes.MiscChange, error) {
				return nil, encodeErr
			}),
		)

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID))})

		require.ErrorIs(t, err, encodeErr)
		require.Nil(t, result)
		require.Empty(t, store.commits)
		require.Equal(t, 1, snapshot.closeCount)
		require.Equal(t, BlockResultPoolStats{Capacity: 1, Available: 1}, executor.ResultPoolStats())
	})

	t.Run("execution error", func(t *testing.T) {
		chainID := big.NewInt(testChainID)
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		recipient := testAddress(0xac)
		rawTx := signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(1), nil, 100_000, big.NewInt(0))
		snapshot := newMemoryGigaSnapshot(0)
		store := &recordingGigaStore{snapshot: snapshot}
		encodeCalls := 0
		executor := NewExecutor(
			Config{MinGasPrice: big.NewInt(0)},
			withTestStores(store, NewMemoryReceiptStore()),
			WithBlockChangeSetEncoder(func(BlockContext, *BlockResult) ([]gigatypes.MiscChange, error) {
				encodeCalls++
				return nil, nil
			}),
		)

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{rawTx},
		})

		require.Error(t, err)
		require.Nil(t, result)
		require.Zero(t, encodeCalls)
		require.Empty(t, store.commits)
		require.Equal(t, 1, snapshot.closeCount)
	})

	t.Run("context canceled during encoding", func(t *testing.T) {
		snapshot := newMemoryGigaSnapshot(0)
		store := &recordingGigaStore{snapshot: snapshot}
		ctx, cancel := context.WithCancel(t.Context())
		executor := NewExecutor(
			Config{BlockResultPoolSize: 1},
			withTestStores(store, NewMemoryReceiptStore()),
			WithBlockChangeSetEncoder(func(BlockContext, *BlockResult) ([]gigatypes.MiscChange, error) {
				cancel()
				return nil, nil
			}),
		)

		result, err := executor.ExecuteBlock(ctx, BlockRequest{Context: blockContext(big.NewInt(testChainID))})

		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, result)
		require.Empty(t, store.commits)
		require.Equal(t, 1, snapshot.closeCount)
		require.Equal(t, BlockResultPoolStats{Capacity: 1, Available: 1}, executor.ResultPoolStats())
	})

	t.Run("commit error", func(t *testing.T) {
		chainID := big.NewInt(testChainID)
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		recipient := testAddress(0xad)
		initialBalance := big.NewInt(1_000_000_000)
		snapshot := newMemoryGigaSnapshot(0)
		snapshot.setBalance(sender, initialBalance)
		commitErr := errors.New("commit failed")
		store := &recordingGigaStore{snapshot: snapshot, commitErr: commitErr}
		receiptStore := NewMemoryReceiptStore()
		executor := NewExecutor(
			Config{BlockResultPoolSize: 1, MinGasPrice: big.NewInt(0)},
			withTestStores(store, receiptStore),
		)
		rawTx := signLegacyTxWithGasPrice(
			t, key, chainID, 0, &recipient, big.NewInt(7), nil, 100_000, big.NewInt(0),
		)

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{rawTx},
		})

		require.ErrorIs(t, err, commitErr)
		require.Nil(t, result)
		require.Len(t, store.commits, 1)
		require.Equal(t, 1, snapshot.closeCount)
		require.Equal(t, BlockResultPoolStats{Capacity: 1, Available: 1}, executor.ResultPoolStats())
		require.Equal(t, int64(blockContext(big.NewInt(testChainID)).Number), receiptStore.LatestVersion())
	})

	t.Run("block number overflow", func(t *testing.T) {
		snapshot := newMemoryGigaSnapshot(0)
		store := &recordingGigaStore{snapshot: snapshot}
		executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore()))
		blockCtx := blockContext(big.NewInt(testChainID))
		blockCtx.Number = maxGigaStoreBlockNumber + 1

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockCtx})

		require.ErrorContains(t, err, "exceeds int64")
		require.Nil(t, result)
		require.Zero(t, store.openCount)
		require.Empty(t, store.commits)
	})
}
