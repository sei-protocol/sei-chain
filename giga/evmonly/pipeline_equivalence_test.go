package evmonly

import (
	"crypto/ecdsa"
	"math/big"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// storeOrDestroyCode writes value to key when called without calldata, and self-destructs to
// beneficiary when called with any.
func storeOrDestroyCode(key, value common.Hash, beneficiary common.Address) []byte {
	const destroyAt = 0x48
	code := []byte{0x36, 0x60, destroyAt, 0x57, 0x7f} // CALLDATASIZE PUSH1 destroyAt JUMPI PUSH32
	code = append(code, value.Bytes()...)
	code = append(code, 0x7f) // PUSH32
	code = append(code, key.Bytes()...)
	code = append(code, 0x55, 0x00, 0x5b, 0x73) // SSTORE STOP JUMPDEST PUSH20
	code = append(code, beneficiary.Bytes()...)
	return append(code, 0xff) // SELFDESTRUCT
}

// pipelineScenario is a run of blocks in which every block depends on the state its predecessor
// produced: nonces advance each block, an account funded in one block spends in the next, and a
// contract's storage is written in one block and cleared by a self-destruct in a later one.
type pipelineScenario struct {
	chainID     *big.Int
	ring        []*ecdsa.PrivateKey
	fresh       *ecdsa.PrivateKey
	deployer    *ecdsa.PrivateKey
	contract    common.Address
	beneficiary common.Address
	slot        common.Hash
	slotValue   common.Hash
}

func newPipelineScenario(t *testing.T) *pipelineScenario {
	s := &pipelineScenario{
		chainID:     big.NewInt(testChainID),
		beneficiary: testAddress(0xbe),
		slot:        testHash(0x11),
		slotValue:   testHash(0x22),
	}
	for range 8 {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		s.ring = append(s.ring, key)
	}
	var err error
	s.fresh, err = crypto.GenerateKey()
	require.NoError(t, err)
	s.deployer, err = crypto.GenerateKey()
	require.NoError(t, err)
	s.contract = crypto.CreateAddress(crypto.PubkeyToAddress(s.deployer.PublicKey), 0)
	return s
}

func (s *pipelineScenario) genesis() *MemoryState {
	state := NewMemoryState()
	for _, key := range s.ring {
		state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(10*testFundedBalanceWei))
	}
	state.SetBalance(crypto.PubkeyToAddress(s.deployer.PublicKey), big.NewInt(2_000_000_000_000_000))
	return state
}

// block returns the transactions of block number, counted from 1.
func (s *pipelineScenario) block(t *testing.T, number uint64) [][]byte {
	txs := make([][]byte, 0, len(s.ring)+2)
	for i, key := range s.ring {
		nonce := number - 1
		if i == 0 && number > 1 {
			// The first ring sender also funds the fresh account in block 1.
			nonce = number
		}
		to := crypto.PubkeyToAddress(s.ring[(i+1)%len(s.ring)].PublicKey)
		txs = append(txs, signLegacyTx(t, key, s.chainID, nonce, &to, big.NewInt(int64(1000+i)), nil))
	}
	fresh := crypto.PubkeyToAddress(s.fresh.PublicKey)
	switch number {
	case 1:
		fund := big.NewInt(testFundedBalanceWei)
		txs = append(txs, signLegacyTx(t, s.ring[0], s.chainID, 1, &fresh, fund, nil))
		txs = append(txs, signLegacyTxWithGas(t, s.deployer, s.chainID, 0, nil, big.NewInt(0),
			initCode(storeOrDestroyCode(s.slot, s.slotValue, s.beneficiary)), 300_000))
	case 2:
		txs = append(txs, signLegacyTx(t, s.fresh, s.chainID, 0, &s.beneficiary, big.NewInt(7), nil))
		txs = append(txs, signLegacyTxWithGas(t, s.deployer, s.chainID, 1, &s.contract, big.NewInt(0), nil, 100_000))
	case 3:
		txs = append(txs, signLegacyTxWithGas(t, s.deployer, s.chainID, 2, &s.contract, big.NewInt(0), []byte{0x01}, 100_000))
	case 4:
		txs = append(txs, signLegacyTx(t, s.deployer, s.chainID, 3, &s.contract, big.NewInt(9), nil))
	}
	return txs
}

func (s *pipelineScenario) addresses() []common.Address {
	addrs := []common.Address{
		crypto.PubkeyToAddress(s.fresh.PublicKey),
		crypto.PubkeyToAddress(s.deployer.PublicKey),
		s.contract,
		s.beneficiary,
	}
	for _, key := range s.ring {
		addrs = append(addrs, crypto.PubkeyToAddress(key.PublicKey))
	}
	return addrs
}

// laggingStore holds each block's commit until the next block has opened its view, so that block
// can only see its predecessor's state through the pending overlay.
type laggingStore struct {
	*MemoryStore
	mu       sync.Mutex
	cond     *sync.Cond
	opened   int64
	released bool
}

func newLaggingStore(store *MemoryStore) *laggingStore {
	s := &laggingStore{MemoryStore: store}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *laggingStore) OpenView() gigatypes.StateView {
	s.mu.Lock()
	s.opened++
	s.cond.Broadcast()
	s.mu.Unlock()
	return s.MemoryStore.OpenView()
}

func (s *laggingStore) CommitStateChanges(blockNum int64, changesets []*proto.NamedChangeSet) error {
	s.mu.Lock()
	for s.opened <= blockNum && !s.released {
		s.cond.Wait()
	}
	s.mu.Unlock()
	return s.MemoryStore.CommitStateChanges(blockNum, changesets)
}

// release lets the last block's commit land without a following block.
func (s *laggingStore) release() {
	s.mu.Lock()
	s.released = true
	s.cond.Broadcast()
	s.mu.Unlock()
}

// Pipelined execution must produce exactly what synchronous execution does, block by block and in
// the store it leaves behind.
func TestPipelinedExecutionMatchesSynchronousExecution(t *testing.T) {
	scenario := newPipelineScenario(t)
	cfg := Config{OCCWorkers: 4, ChainConfig: legacySelfDestructChainConfig(scenario.chainID)}
	syncStore := NewMemoryStore(scenario.genesis())
	pipeStore := newLaggingStore(NewMemoryStore(scenario.genesis()))
	syncExecutor := NewExecutor(cfg, withTestStores(syncStore, NewMemoryReceiptStore(), syncStore.EncodeChangeSet))
	defer syncExecutor.Close()
	pipeExecutor := NewExecutor(cfg, withTestStores(pipeStore, NewMemoryReceiptStore(), pipeStore.EncodeChangeSet))
	defer pipeExecutor.Close()

	for number := uint64(1); number <= 4; number++ {
		txs := scenario.block(t, number)
		blockCtx := blockContext(scenario.chainID)
		blockCtx.Number = number
		req := BlockRequest{Context: blockCtx, Txs: txs}

		want, err := syncExecutor.ExecuteBlock(t.Context(), req)
		require.NoError(t, err)
		prepared, err := pipeExecutor.PrepareBlock(t.Context(), req)
		require.NoError(t, err)
		got, err := pipeExecutor.ExecutePreparedBlock(t.Context(), prepared)
		require.NoError(t, err)

		for i, tx := range want.Txs {
			require.Equalf(t, uint64(1), tx.Status, "block %d tx %d must succeed for the comparison to mean anything", number, i)
		}
		require.Equalf(t, want.Txs, got.Txs, "block %d transaction results", number)
		require.Equalf(t, want.Receipts, got.Receipts, "block %d receipts", number)
		require.Equalf(t, want.ChangeSet, got.ChangeSet, "block %d changeset", number)
		require.Equalf(t, want.GasUsed, got.GasUsed, "block %d gas", number)
		want.Release()
		got.Release()
	}
	pipeStore.release()
	require.NoError(t, pipeExecutor.AwaitCommits())

	syncView, pipeView := syncStore.OpenView(), pipeStore.OpenView()
	defer syncView.Close()
	defer pipeView.Close()
	require.Equal(t, syncView.GetBlockHeight(), pipeView.GetBlockHeight())
	for _, addr := range scenario.addresses() {
		require.Equalf(t, syncView.GetBalance(addr), pipeView.GetBalance(addr), "balance of %s", addr)
		require.Equalf(t, syncView.GetNonce(addr), pipeView.GetNonce(addr), "nonce of %s", addr)
		require.Equalf(t, syncView.GetCode(addr), pipeView.GetCode(addr), "code of %s", addr)
	}

	written, ok := pipeStore.OpenViewAt(2)
	require.True(t, ok)
	defer written.Close()
	require.Equal(t, scenario.slotValue, written.GetStorage(scenario.contract, scenario.slot),
		"block 2 must have written the slot, or the clear below proves nothing")
	require.Equal(t, common.Hash{}, pipeView.GetStorage(scenario.contract, scenario.slot),
		"the self-destruct in block 3 must leave no stale slot behind")
	require.Equal(t, syncView.GetStorage(scenario.contract, scenario.slot), pipeView.GetStorage(scenario.contract, scenario.slot))
}
