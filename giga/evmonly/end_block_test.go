package evmonly

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var (
	endBlockerAddr      = common.HexToAddress("0x0000000000000000000000000000000000002101")
	endBlockerLaterAddr = common.HexToAddress("0x0000000000000000000000000000000000002102")
	// Slots of endBlockerAddr written at the end of each block.
	endBlockBlocksSlot   = common.Hash{0xb0} // blocks ended so far
	endBlockObservedSlot = common.Hash{0xb1} // counterSharedSlot as the block's transactions left it
	endBlockTimeSlot     = common.Hash{0xb2} // the block time
	errEndBlockRefused   = errors.New("end block refused")
)

// recordingEndBlocker is counterPrecompile plus an end-of-block step that
// records what the block's transactions left behind.
type recordingEndBlocker struct {
	counterPrecompile
	// reads, when set, is the address whose endBlockObservedSlot is copied into this contract's.
	reads    common.Address
	payee    common.Address
	pay      *big.Int
	override *common.Hash
	fail     bool
}

func (c recordingEndBlocker) EndBlock(block precompiles.BlockContext, state precompiles.State) error {
	self := endBlockerAddr
	if c.reads != (common.Address{}) {
		self = endBlockerLaterAddr
		state.SetState(self, endBlockObservedSlot, state.GetState(c.reads, endBlockObservedSlot))
		return nil
	}
	if c.fail {
		return errEndBlockRefused
	}
	blocks := new(big.Int).Add(state.GetState(self, endBlockBlocksSlot).Big(), big.NewInt(1))
	state.SetState(self, endBlockBlocksSlot, common.BigToHash(blocks))
	state.SetState(self, endBlockObservedSlot, state.GetState(self, counterSharedSlot))
	state.SetState(self, endBlockTimeSlot, common.BigToHash(new(big.Int).SetUint64(block.Time)))
	if c.override != nil {
		state.SetState(self, counterSharedSlot, *c.override)
	}
	if c.pay != nil {
		state.AddBalance(c.payee, c.pay)
	}
	return nil
}

// endBlockRegistry serves its contracts in the order given, which need not be address order.
type endBlockRegistry map[common.Address]precompiles.Contract

func (r endBlockRegistry) Get(addr common.Address) (precompiles.Contract, bool) {
	contract, ok := r[addr]
	return contract, ok
}

func (r endBlockRegistry) Addresses() []common.Address {
	addrs := make([]common.Address, 0, len(r))
	for addr := range r {
		addrs = append(addrs, addr)
	}
	return addrs
}

func endBlockerTx(t *testing.T, caller counterCaller, input byte) []byte {
	t.Helper()
	to := endBlockerAddr
	return signLegacyTxWithGasPrice(t, caller.key, big.NewInt(testChainID), 0, &to, big.NewInt(0), []byte{input}, 100_000, big.NewInt(0))
}

func endBlockerSlot(state *MemoryState, slot common.Hash) *big.Int {
	return state.GetState(endBlockerAddr, slot).Big()
}

func TestEndBlockerSeesTheBlockAndCommitsItsWrites(t *testing.T) {
	const txCount = 16
	states := map[bool]*MemoryState{false: NewMemoryState(), true: NewMemoryState()}
	callers := newCounterCallers(t, txCount, states[false], states[true])
	rawTxs := make([][]byte, txCount)
	for i, caller := range callers {
		rawTxs[i] = endBlockerTx(t, caller, counterShared)
	}
	results := map[bool]*BlockResult{}
	for _, occ := range []bool{false, true} {
		t.Run(map[bool]string{false: "sequential", true: "occ"}[occ], func(t *testing.T) {
			state := states[occ]
			cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: endBlockRegistry{endBlockerAddr: recordingEndBlocker{}}}
			if occ {
				cfg.OCCWorkers = 4
			}
			block := blockContext(big.NewInt(testChainID))
			block.Time = 77
			result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: block, Txs: rawTxs})
			require.NoError(t, err)
			requireOCCRan(t, cfg, result)
			for _, tx := range result.Txs {
				require.Equal(t, ethtypes.ReceiptStatusSuccessful, tx.Status)
			}
			requireSortedUniqueChangeSet(t, result.ChangeSet)
			results[occ] = result

			state.ApplyChangeSet(result.ChangeSet)
			require.Equal(t, big.NewInt(txCount), endBlockerSlot(state, counterSharedSlot))
			require.Equal(t, big.NewInt(txCount), endBlockerSlot(state, endBlockObservedSlot))
			require.Equal(t, big.NewInt(1), endBlockerSlot(state, endBlockBlocksSlot))
			require.Equal(t, big.NewInt(77), endBlockerSlot(state, endBlockTimeSlot))
			require.Equal(t, uint64(1), state.GetNonce(endBlockerAddr))
		})
	}
	require.Equal(t, results[false].ChangeSet, results[true].ChangeSet)
}

func contractStorage(changes StateChangeSet, addr common.Address) []StorageChange {
	var own []StorageChange
	for _, change := range changes.Storage {
		if change.Address == addr {
			own = append(own, change)
		}
	}
	return own
}

func TestEndBlockerRunsOnEmptyBlocks(t *testing.T) {
	state := NewMemoryState()
	executor := NewExecutor(Config{CustomPrecompiles: endBlockRegistry{endBlockerAddr: recordingEndBlocker{}}}, withTestState(state))
	for number := uint64(1); number <= 3; number++ {
		block := blockContext(big.NewInt(testChainID))
		block.Number = number
		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: block})
		require.NoError(t, err)
		require.Empty(t, result.Txs)
		state.ApplyChangeSet(result.ChangeSet)
		require.Equal(t, new(big.Int).SetUint64(number), endBlockerSlot(state, endBlockBlocksSlot))
	}
}

func TestEndBlockerWithoutEndBlockersLeavesTheBlockAlone(t *testing.T) {
	state := NewMemoryState()
	result, err := NewExecutor(Config{CustomPrecompiles: counterRegistry{}}, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(big.NewInt(testChainID)),
	})
	require.NoError(t, err)
	require.True(t, result.ChangeSet.isEmpty())
}

func TestEndBlockerWritesReplaceTheBlocksWrites(t *testing.T) {
	for _, occ := range []bool{false, true} {
		t.Run(map[bool]string{false: "sequential", true: "occ"}[occ], func(t *testing.T) {
			const txCount = 4
			state := NewMemoryState()
			callers := newCounterCallers(t, txCount, state)
			rawTxs := make([][]byte, txCount)
			for i, caller := range callers {
				rawTxs[i] = endBlockerTx(t, caller, counterShared)
			}
			override := common.BigToHash(big.NewInt(1_000))
			payee := callers[1].addr
			cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: endBlockRegistry{endBlockerAddr: recordingEndBlocker{
				override: &override,
				payee:    payee,
				pay:      big.NewInt(5),
			}}}
			if occ {
				cfg.OCCWorkers = 4
			}
			result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
				Context: blockContext(big.NewInt(testChainID)),
				Txs:     rawTxs,
			})
			require.NoError(t, err)
			requireOCCRan(t, cfg, result)
			requireSortedUniqueChangeSet(t, result.ChangeSet)

			var sharedWrites int
			for _, change := range result.ChangeSet.Storage {
				if change.Address == endBlockerAddr && change.Key == counterSharedSlot {
					sharedWrites++
					require.Equal(t, override, change.Value)
				}
			}
			require.Equal(t, 1, sharedWrites)

			state.ApplyChangeSet(result.ChangeSet)
			require.Equal(t, big.NewInt(txCount), endBlockerSlot(state, endBlockObservedSlot))
			require.Equal(t, big.NewInt(1_000), endBlockerSlot(state, counterSharedSlot))
			require.Equal(t, new(big.Int).Add(state.GetBalance(callers[0].addr), big.NewInt(5)), state.GetBalance(payee))
		})
	}
}

func requireSortedUniqueChangeSet(t *testing.T, changes StateChangeSet) {
	t.Helper()
	requireStrictlyIncreasing(t, changes.Balances, func(c BalanceChange) []byte { return c.Address[:] })
	requireStrictlyIncreasing(t, changes.Nonces, func(c NonceChange) []byte { return c.Address[:] })
	requireStrictlyIncreasing(t, changes.Code, func(c CodeChange) []byte { return c.Address[:] })
	requireStrictlyIncreasing(t, changes.Storage, storageChangeSortKey)
}

func requireStrictlyIncreasing[T any](t *testing.T, list []T, key func(T) []byte) {
	t.Helper()
	for i := 1; i < len(list); i++ {
		require.Negative(t, compareBytes(key(list[i-1]), key(list[i])), "entry %d is not after entry %d", i, i-1)
	}
}

func compareBytes(a, b []byte) int {
	return new(big.Int).SetBytes(a).Cmp(new(big.Int).SetBytes(b))
}

func TestEndBlockerErrorFailsTheBlock(t *testing.T) {
	for _, occ := range []bool{false, true} {
		t.Run(map[bool]string{false: "sequential", true: "occ"}[occ], func(t *testing.T) {
			state := NewMemoryState()
			callers := newCounterCallers(t, 2, state)
			cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: endBlockRegistry{endBlockerAddr: recordingEndBlocker{fail: true}}}
			if occ {
				cfg.OCCWorkers = 4
			}
			_, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
				Context: blockContext(big.NewInt(testChainID)),
				Txs:     [][]byte{endBlockerTx(t, callers[0], counterOwn), endBlockerTx(t, callers[1], counterOwn)},
			})
			require.ErrorIs(t, err, errEndBlockRefused)
		})
	}
}

func TestEndBlockersRunInAddressOrder(t *testing.T) {
	state := NewMemoryState()
	caller := newCounterCallers(t, 1, state)[0]
	registry := endBlockRegistry{
		endBlockerLaterAddr: recordingEndBlocker{reads: endBlockerAddr},
		endBlockerAddr:      recordingEndBlocker{},
	}
	for range 8 {
		executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: registry}, withTestState(state))
		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
			Context: blockContext(big.NewInt(testChainID)),
			Txs:     [][]byte{endBlockerTx(t, caller, counterShared)},
		})
		require.NoError(t, err)
		// Each iteration runs against the untouched state, so the later contract always sees 1.
		next := NewMemoryState()
		next.ApplyChangeSet(result.ChangeSet)
		require.Equal(t, big.NewInt(1), next.GetState(endBlockerLaterAddr, endBlockObservedSlot).Big())
		require.Equal(t, uint64(1), next.GetNonce(endBlockerLaterAddr))
	}
}

func TestEndBlockerStorageSurvivesLaterCalls(t *testing.T) {
	state := NewMemoryState()
	caller := newCounterCallers(t, 1, state)[0]
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: endBlockRegistry{endBlockerAddr: recordingEndBlocker{}}}, withTestState(state))

	first := blockContext(big.NewInt(testChainID))
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: first})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, uint64(1), state.GetNonce(endBlockerAddr))

	second := blockContext(big.NewInt(testChainID))
	second.Number = 2
	result, err = executor.ExecuteBlock(t.Context(), BlockRequest{Context: second, Txs: [][]byte{endBlockerTx(t, caller, counterOwn)}})
	require.NoError(t, err)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(2), endBlockerSlot(state, endBlockBlocksSlot))
	require.Equal(t, big.NewInt(1), endBlockerSlot(state, common.BytesToHash(caller.addr.Bytes())))
}

func TestEndBlockerSeesThePreviousBlockBeforeItsCommitLands(t *testing.T) {
	state := NewMemoryState()
	callers := newCounterCallers(t, 3, state)
	store := NewMemoryStore(state)
	executor := NewExecutor(
		Config{MinGasPrice: big.NewInt(0), OCCWorkers: 4, CustomPrecompiles: endBlockRegistry{endBlockerAddr: recordingEndBlocker{}}},
		withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet),
	)
	t.Cleanup(executor.Close)

	var last *BlockResult
	for number := uint64(1); number <= 3; number++ {
		last = executePipelinedBlock(t, executor, big.NewInt(testChainID), number,
			endBlockerTx(t, callers[number-1], counterShared))
	}
	require.NoError(t, executor.AwaitCommits())
	want := map[common.Hash]common.Hash{
		endBlockBlocksSlot:   common.BigToHash(big.NewInt(3)),
		endBlockObservedSlot: common.BigToHash(big.NewInt(3)),
		counterSharedSlot:    common.BigToHash(big.NewInt(3)),
	}
	for _, change := range contractStorage(last.ChangeSet, endBlockerAddr) {
		if value, ok := want[change.Key]; ok {
			require.Equal(t, value, change.Value, "slot %s", change.Key)
			delete(want, change.Key)
		}
	}
	require.Empty(t, want)
}

func TestEndBlockerAtABuiltinAddressIsSkipped(t *testing.T) {
	sha256Addr := common.BytesToAddress([]byte{0x02})
	registry := endBlockRegistry{sha256Addr: recordingEndBlocker{}}
	require.Empty(t, customEndBlockers(registry))

	result, err := NewExecutor(Config{CustomPrecompiles: registry}, withTestState(NewMemoryState())).ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(big.NewInt(testChainID)),
	})
	require.NoError(t, err)
	require.True(t, result.ChangeSet.isEmpty())
}

func TestCustomEndBlockersSkipsContractsWithoutEndBlock(t *testing.T) {
	registry := endBlockRegistry{
		endBlockerLaterAddr: counterPrecompile{},
		endBlockerAddr:      recordingEndBlocker{},
		testAddress(0xee):   nil,
	}
	blockers := customEndBlockers(registry)
	require.Len(t, blockers, 1)
	require.Equal(t, endBlockerAddr, blockers[0].address)
	require.Empty(t, customEndBlockers(nil))
}

func TestMergeChangeSet(t *testing.T) {
	a, b, c := testAddress(0x0a), testAddress(0x0b), testAddress(0x0c)
	dst := StateChangeSet{
		Balances: []BalanceChange{{Address: a, Balance: big.NewInt(1)}, {Address: c, Balance: big.NewInt(3)}},
		Nonces:   []NonceChange{{Address: b, Nonce: 2}},
		Code:     []CodeChange{{Address: c, Code: []byte{0x01}}},
		Storage: []StorageChange{
			{Address: a, Key: testHash(1), Value: testHash(0x11)},
			{Address: b, Key: testHash(1), Value: testHash(0x21)},
		},
	}
	later := StateChangeSet{
		Balances: []BalanceChange{{Address: b, Balance: big.NewInt(20)}, {Address: c, Balance: big.NewInt(30)}},
		Nonces:   []NonceChange{{Address: a, Nonce: 1}, {Address: b, Nonce: 5}},
		Code:     []CodeChange{{Address: a, Code: []byte{0x02}}, {Address: c, Delete: true}},
		Storage: []StorageChange{
			{Address: a, Key: testHash(0), Value: testHash(0x10)},
			{Address: a, Key: testHash(1), Delete: true},
			{Address: c, Key: testHash(9), Value: testHash(0x39)},
		},
	}
	require.NoError(t, mergeChangeSet(&dst, later))
	require.Equal(t, StateChangeSet{
		Balances: []BalanceChange{{Address: a, Balance: big.NewInt(1)}, {Address: b, Balance: big.NewInt(20)}, {Address: c, Balance: big.NewInt(30)}},
		Nonces:   []NonceChange{{Address: a, Nonce: 1}, {Address: b, Nonce: 5}},
		Code:     []CodeChange{{Address: a, Code: []byte{0x02}}, {Address: c, Delete: true}},
		Storage: []StorageChange{
			{Address: a, Key: testHash(0), Value: testHash(0x10)},
			{Address: a, Key: testHash(1), Delete: true},
			{Address: b, Key: testHash(1), Value: testHash(0x21)},
			{Address: c, Key: testHash(9), Value: testHash(0x39)},
		},
	}, dst)

	require.ErrorIs(t, mergeChangeSet(&dst, StateChangeSet{StorageClears: []common.Address{a}}), errEndBlockClearedStorage)
}
