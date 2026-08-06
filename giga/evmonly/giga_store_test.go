package evmonly

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigastore "github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
)

type recordingGigaStore struct {
	snapshot    gigastore.StateSnapshot
	openCount   int
	commitErr   error
	commitBlock []int64
	commits     [][]*proto.NamedChangeSet
}

func (s *recordingGigaStore) CommitStateChanges(blockNum int64, changeset []*proto.NamedChangeSet) error {
	s.commitBlock = append(s.commitBlock, blockNum)
	s.commits = append(s.commits, changeset)
	return s.commitErr
}

func (s *recordingGigaStore) OpenSnapshot() gigastore.StateSnapshot {
	s.openCount++
	return s.snapshot
}

func (s *recordingGigaStore) OpenSnapshotAt(int64) (gigastore.StateSnapshot, bool) {
	return nil, false
}

type memoryGigaSnapshot struct {
	height     int64
	balances   map[gigastore.Address]gigastore.Hash
	nonces     map[gigastore.Address]uint64
	code       map[gigastore.Address][]byte
	storage    map[gigaStorageKey]gigastore.Hash
	closeCount int
}

type gigaStorageKey struct {
	address gigastore.Address
	key     gigastore.Hash
}

func newMemoryGigaSnapshot(height int64) *memoryGigaSnapshot {
	return &memoryGigaSnapshot{
		height:   height,
		balances: map[gigastore.Address]gigastore.Hash{},
		nonces:   map[gigastore.Address]uint64{},
		code:     map[gigastore.Address][]byte{},
		storage:  map[gigaStorageKey]gigastore.Hash{},
	}
}

func (s *memoryGigaSnapshot) AccountExists(addr gigastore.Address) bool {
	if s.balances[addr] != (gigastore.Hash{}) || s.nonces[addr] != 0 || len(s.code[addr]) != 0 {
		return true
	}
	for key := range s.storage {
		if key.address == addr {
			return true
		}
	}
	return false
}

func (s *memoryGigaSnapshot) GetStorage(addr gigastore.Address, key gigastore.Hash) gigastore.Hash {
	return s.storage[gigaStorageKey{address: addr, key: key}]
}

func (s *memoryGigaSnapshot) GetBalance(addr gigastore.Address) gigastore.Hash {
	return s.balances[addr]
}

func (s *memoryGigaSnapshot) GetNonce(addr gigastore.Address) uint64 {
	return s.nonces[addr]
}

func (s *memoryGigaSnapshot) GetCodeSize(addr gigastore.Address) int {
	return len(s.code[addr])
}

func (s *memoryGigaSnapshot) GetCodeHash(addr gigastore.Address) gigastore.Hash {
	if !s.AccountExists(addr) {
		return gigastore.Hash{}
	}
	return gigastore.Hash(crypto.Keccak256Hash(s.code[addr]))
}

func (s *memoryGigaSnapshot) GetCode(addr gigastore.Address) []byte {
	return s.code[addr]
}

func (s *memoryGigaSnapshot) GetBlockHeight() int64 {
	return s.height
}

func (s *memoryGigaSnapshot) Get([]byte) ([]byte, bool) {
	return nil, false
}

func (s *memoryGigaSnapshot) Close() {
	s.closeCount++
}

func (s *memoryGigaSnapshot) setBalance(addr common.Address, balance *big.Int) {
	var encoded gigastore.Hash
	balance.FillBytes(encoded[:])
	s.balances[gigastore.Address(addr)] = encoded
}

func TestGigaSnapshotStateReader(t *testing.T) {
	addr := testAddress(0xa8)
	slot := common.HexToHash("0x01")
	value := common.HexToHash("0x02")
	code := []byte{0x60, 0x00}
	snapshot := newMemoryGigaSnapshot(3)
	snapshot.setBalance(addr, big.NewInt(123))
	snapshot.nonces[gigastore.Address(addr)] = 9
	snapshot.code[gigastore.Address(addr)] = code
	snapshot.storage[gigaStorageKey{
		address: gigastore.Address(addr),
		key:     gigastore.Hash(slot),
	}] = gigastore.Hash(value)
	reader := gigaSnapshotStateReader{snapshot: snapshot}

	require.Equal(t, big.NewInt(123), reader.GetBalance(addr))
	require.Equal(t, uint64(9), reader.GetNonce(addr))
	require.Equal(t, value, reader.GetState(addr, slot))
	gotCode := reader.GetCode(addr)
	require.Equal(t, code, gotCode)
	gotCode[0] = 0xff
	require.Equal(t, byte(0x60), snapshot.code[gigastore.Address(addr)][0])
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
	wantChangesets := []*proto.NamedChangeSet{{
		Name: "encoded",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{
			Key:   []byte("key"),
			Value: []byte("value"),
		}}},
	}}
	encodeCalls := 0
	encoder := func(changes StateChangeSet) ([]*proto.NamedChangeSet, error) {
		encodeCalls++
		require.NotEmpty(t, changes.Balances)
		require.Contains(t, changes.Nonces, NonceChange{Address: sender, Nonce: 1})
		return wantChangesets, nil
	}

	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)
	blockCtx := blockContext(chainID)
	blockCtx.Number = 41
	executor := NewExecutor(Config{}, WithGigaStore(store, encoder))
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockCtx,
		Txs:     [][]byte{rawTx},
	})

	require.NoError(t, err)
	require.Equal(t, 1, encodeCalls)
	require.Equal(t, 1, store.openCount)
	require.Equal(t, 1, snapshot.closeCount)
	require.Equal(t, []int64{41}, store.commitBlock)
	require.Equal(t, [][]*proto.NamedChangeSet{wantChangesets}, store.commits)
	require.Contains(t, result.ChangeSet.Balances, BalanceChange{Address: recipient, Balance: big.NewInt(7)})
	result.Release()
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
	encoder := func(changes StateChangeSet) ([]*proto.NamedChangeSet, error) {
		for i, recipient := range recipients {
			require.Contains(t, changes.Balances, BalanceChange{
				Address: recipient,
				Balance: big.NewInt(int64(i + 1)),
			})
		}
		return []*proto.NamedChangeSet{}, nil
	}
	executor := NewExecutor(
		Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2},
		WithGigaStore(store, encoder),
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
	result.Release()
}

func TestExecutorGigaStoreFailuresDoNotCommitPartialState(t *testing.T) {
	t.Run("missing encoder", func(t *testing.T) {
		snapshot := newMemoryGigaSnapshot(0)
		store := &recordingGigaStore{snapshot: snapshot}
		executor := NewExecutor(Config{}, WithGigaStore(store, nil))

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID))})

		require.ErrorIs(t, err, errMissingNamedChangeSetEncoder)
		require.Nil(t, result)
		require.Zero(t, store.openCount)
		require.Empty(t, store.commits)
	})

	t.Run("nil snapshot", func(t *testing.T) {
		store := &recordingGigaStore{}
		executor := NewExecutor(Config{}, WithGigaStore(store, func(StateChangeSet) ([]*proto.NamedChangeSet, error) {
			return nil, nil
		}))

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID))})

		require.ErrorContains(t, err, "nil snapshot")
		require.Nil(t, result)
		require.Empty(t, store.commits)
	})

	t.Run("encoder error", func(t *testing.T) {
		snapshot := newMemoryGigaSnapshot(0)
		store := &recordingGigaStore{snapshot: snapshot}
		encodeErr := errors.New("encode failed")
		executor := NewExecutor(Config{BlockResultPoolSize: 1}, WithGigaStore(store, func(StateChangeSet) ([]*proto.NamedChangeSet, error) {
			return nil, encodeErr
		}))

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
		executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, WithGigaStore(store, func(StateChangeSet) ([]*proto.NamedChangeSet, error) {
			encodeCalls++
			return nil, nil
		}))

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
		executor := NewExecutor(Config{BlockResultPoolSize: 1}, WithGigaStore(store, func(StateChangeSet) ([]*proto.NamedChangeSet, error) {
			cancel()
			return []*proto.NamedChangeSet{}, nil
		}))

		result, err := executor.ExecuteBlock(ctx, BlockRequest{Context: blockContext(big.NewInt(testChainID))})

		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, result)
		require.Empty(t, store.commits)
		require.Equal(t, 1, snapshot.closeCount)
		require.Equal(t, BlockResultPoolStats{Capacity: 1, Available: 1}, executor.ResultPoolStats())
	})

	t.Run("commit error", func(t *testing.T) {
		snapshot := newMemoryGigaSnapshot(0)
		commitErr := errors.New("commit failed")
		store := &recordingGigaStore{snapshot: snapshot, commitErr: commitErr}
		executor := NewExecutor(Config{BlockResultPoolSize: 1}, WithGigaStore(store, func(StateChangeSet) ([]*proto.NamedChangeSet, error) {
			return []*proto.NamedChangeSet{}, nil
		}))

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID))})

		require.ErrorIs(t, err, commitErr)
		require.Nil(t, result)
		require.Len(t, store.commits, 1)
		require.Equal(t, 1, snapshot.closeCount)
		require.Equal(t, BlockResultPoolStats{Capacity: 1, Available: 1}, executor.ResultPoolStats())
	})

	t.Run("block number overflow", func(t *testing.T) {
		snapshot := newMemoryGigaSnapshot(0)
		store := &recordingGigaStore{snapshot: snapshot}
		executor := NewExecutor(Config{}, WithGigaStore(store, func(StateChangeSet) ([]*proto.NamedChangeSet, error) {
			return nil, nil
		}))
		blockCtx := blockContext(big.NewInt(testChainID))
		blockCtx.Number = maxGigaStoreBlockNumber + 1

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockCtx})

		require.ErrorContains(t, err, "exceeds int64")
		require.Nil(t, result)
		require.Zero(t, store.openCount)
		require.Empty(t, store.commits)
	})
}
