package evmonly

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var (
	firstEndBlocker  = common.HexToAddress("0x0000000000000000000000000000000000002102")
	secondEndBlocker = common.HexToAddress("0x0000000000000000000000000000000000002101")
	// Slots of an endBlockRecorder.
	callsSlot    = common.Hash{0xa0} // calls made to it
	observedSlot = common.Hash{0xb0} // callsSlot as the block's transactions left it
	blocksSlot   = common.Hash{0xb1} // blocks ended
	timeSlot     = common.Hash{0xb2} // the last block time
	errRefused   = errors.New("end block refused")
)

// endBlockRecorder counts calls to it and, at the end of each block, records
// what the block left behind.
type endBlockRecorder struct {
	address common.Address
	// reads, when set, is copied from that address's observedSlot instead.
	reads common.Address
	payee common.Address
	pay   *big.Int
	fail  bool
}

func (endBlockRecorder) RequiredGas([]byte) uint64 { return 1_000 }

func (r endBlockRecorder) Run(ctx *precompiles.Context, _ []byte) ([]byte, error) {
	calls := ctx.State.GetState(r.address, callsSlot).Big()
	ctx.State.SetState(r.address, callsSlot, common.BigToHash(calls.Add(calls, big.NewInt(1))))
	return nil, nil
}

func (r endBlockRecorder) EndBlock(block precompiles.BlockContext, state precompiles.State) error {
	if r.fail {
		return errRefused
	}
	if r.reads != (common.Address{}) {
		state.SetState(r.address, observedSlot, state.GetState(r.reads, observedSlot))
		return nil
	}
	blocks := state.GetState(r.address, blocksSlot).Big()
	state.SetState(r.address, blocksSlot, common.BigToHash(blocks.Add(blocks, big.NewInt(1))))
	state.SetState(r.address, observedSlot, state.GetState(r.address, callsSlot))
	state.SetState(r.address, timeSlot, common.BigToHash(new(big.Int).SetUint64(block.Time)))
	if r.pay != nil {
		state.AddBalance(r.payee, r.pay)
	}
	return nil
}

func recorderRegistry(recorders ...endBlockRecorder) precompiles.Registry {
	contracts := map[common.Address]precompiles.Contract{}
	order := make([]common.Address, len(recorders))
	for i, r := range recorders {
		contracts[r.address] = r
		order[i] = r.address
	}
	return precompiles.NewStaticRegistry(contracts, order...)
}

func recorderCall(t *testing.T, nonce uint64) ([]byte, common.Address) {
	t.Helper()
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	to := firstEndBlocker
	return signLegacyTxWithGasPrice(t, key, big.NewInt(testChainID), nonce, &to, big.NewInt(0), nil, 100_000, big.NewInt(1)),
		crypto.PubkeyToAddress(key.PublicKey)
}

func TestEndBlockerSeesTheBlockAndCommitsItsWrites(t *testing.T) {
	state := NewMemoryState()
	tx0, sender := recorderCall(t, 0)
	tx1, _ := recorderCall(t, 1)
	state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
	payee := common.HexToAddress("0x00000000000000000000000000000000000000ee")
	state.SetBalance(payee, big.NewInt(10))

	cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: recorderRegistry(
		endBlockRecorder{address: firstEndBlocker, payee: payee, pay: big.NewInt(5)},
	)}
	block := blockContext(big.NewInt(testChainID))
	block.Time = 77
	result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: block, Txs: [][]byte{tx0, tx1}})
	require.NoError(t, err)
	for _, tx := range result.Txs {
		require.Equal(t, uint64(1), tx.Status)
	}

	var calls *StorageChange
	for i, change := range result.ChangeSet.Storage {
		if change.Address == firstEndBlocker && change.Key == callsSlot {
			calls = &result.ChangeSet.Storage[i]
		}
	}
	require.NotNil(t, calls)
	var paid *BalanceChange
	for i, change := range result.ChangeSet.Balances {
		if change.Address == payee {
			paid = &result.ChangeSet.Balances[i]
		}
	}
	require.NotNil(t, paid)
	require.Equal(t, big.NewInt(10), paid.PriorBalance, "the prior is the balance before the block")

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(2), state.GetState(firstEndBlocker, observedSlot).Big())
	require.Equal(t, big.NewInt(1), state.GetState(firstEndBlocker, blocksSlot).Big())
	require.Equal(t, big.NewInt(77), state.GetState(firstEndBlocker, timeSlot).Big())
	require.Equal(t, big.NewInt(15), state.GetBalance(payee))
}

func TestEndBlockerRunsOnEmptyBlocks(t *testing.T) {
	state := NewMemoryState()
	executor := NewExecutor(Config{CustomPrecompiles: recorderRegistry(endBlockRecorder{address: firstEndBlocker})}, withTestState(state))
	for number := uint64(1); number <= 3; number++ {
		block := blockContext(big.NewInt(testChainID))
		block.Number = number
		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: block})
		require.NoError(t, err)
		state.ApplyChangeSet(result.ChangeSet)
		require.Equal(t, new(big.Int).SetUint64(number), state.GetState(firstEndBlocker, blocksSlot).Big())
	}
}

func TestEndBlockersRunInListedOrder(t *testing.T) {
	// secondEndBlocker sorts first by address but is listed second.
	state := NewMemoryState()
	tx, sender := recorderCall(t, 0)
	state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
	registry := recorderRegistry(
		endBlockRecorder{address: firstEndBlocker},
		endBlockRecorder{address: secondEndBlocker, reads: firstEndBlocker},
	)
	result, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: registry}, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(big.NewInt(testChainID)),
		Txs:     [][]byte{tx},
	})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(1), state.GetState(secondEndBlocker, observedSlot).Big())
}

func TestEndBlockerErrorFailsTheBlock(t *testing.T) {
	_, err := NewExecutor(Config{CustomPrecompiles: recorderRegistry(endBlockRecorder{address: firstEndBlocker, fail: true})},
		withTestState(NewMemoryState())).ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID))})
	require.ErrorIs(t, err, errRefused)
}

func TestEndBlockerSeesThePreviousBlockBeforeItsCommitLands(t *testing.T) {
	state := NewMemoryState()
	_, sender := recorderCall(t, 0)
	state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
	store := NewMemoryStore(state)
	executor := NewExecutor(
		Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: recorderRegistry(endBlockRecorder{address: firstEndBlocker})},
		withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet),
	)
	t.Cleanup(executor.Close)

	var last *BlockResult
	for number := uint64(1); number <= 3; number++ {
		tx, _ := recorderCall(t, number-1)
		last = executePipelinedBlock(t, executor, big.NewInt(testChainID), number, tx)
	}
	require.NoError(t, executor.AwaitCommits())
	want := map[common.Hash]int64{blocksSlot: 3, observedSlot: 3, callsSlot: 3}
	for _, change := range last.ChangeSet.Storage {
		if v, ok := want[change.Key]; ok && change.Address == firstEndBlocker {
			require.Equal(t, big.NewInt(v), change.Value.Big(), "slot %s", change.Key)
			delete(want, change.Key)
		}
	}
	require.Empty(t, want)
}

func TestCustomEndBlockersRejectsMisconfiguration(t *testing.T) {
	recorder := endBlockRecorder{address: firstEndBlocker}
	contracts := map[common.Address]precompiles.Contract{firstEndBlocker: recorder, secondEndBlocker: echoPrecompile{}}
	require.Len(t, customEndBlockers(precompiles.NewStaticRegistry(contracts, firstEndBlocker)), 1)
	require.Empty(t, customEndBlockers(nil))

	for name, registry := range map[string]precompiles.Registry{
		"unlisted":   precompiles.NewStaticRegistry(contracts),
		"not one":    precompiles.NewStaticRegistry(contracts, firstEndBlocker, secondEndBlocker),
		"twice":      precompiles.NewStaticRegistry(contracts, firstEndBlocker, firstEndBlocker),
		"unresolved": precompiles.NewStaticRegistry(contracts, firstEndBlocker, testAddress(0xee)),
	} {
		require.Panics(t, func() { customEndBlockers(registry) }, name)
	}
}

func TestMergeChangeSet(t *testing.T) {
	a, b, c := testAddress(0x0a), testAddress(0x0b), testAddress(0x0c)
	dst := StateChangeSet{
		Balances: []BalanceChange{{Address: a, Balance: big.NewInt(1), PriorBalance: big.NewInt(0)}, {Address: c, Balance: big.NewInt(3), PriorBalance: big.NewInt(9)}},
		Nonces:   []NonceChange{{Address: b, Nonce: 2, PriorNonce: 1}},
		Code:     []CodeChange{{Address: c, Code: []byte{0x01}}},
		Storage: []StorageChange{
			{Address: a, Key: testHash(1), Value: testHash(0x11), PriorValue: testHash(0x01)},
			{Address: b, Key: testHash(1), Value: testHash(0x21)},
		},
	}
	later := StateChangeSet{
		Balances: []BalanceChange{{Address: b, Balance: big.NewInt(20), PriorBalance: big.NewInt(2)}, {Address: c, Balance: big.NewInt(30), PriorBalance: big.NewInt(3)}},
		Nonces:   []NonceChange{{Address: a, Nonce: 1}, {Address: b, Nonce: 5, PriorNonce: 2}},
		Code:     []CodeChange{{Address: a, Code: []byte{0x02}}, {Address: c, Delete: true, PriorCode: []byte{0x01}}},
		Storage: []StorageChange{
			{Address: a, Key: testHash(0), Value: testHash(0x10)},
			{Address: a, Key: testHash(1), Delete: true, PriorValue: testHash(0x11)},
			{Address: c, Key: testHash(9), Value: testHash(0x39)},
		},
	}
	require.NoError(t, mergeChangeSet(&dst, later))
	require.Equal(t, StateChangeSet{
		Balances: []BalanceChange{
			{Address: a, Balance: big.NewInt(1), PriorBalance: big.NewInt(0)},
			{Address: b, Balance: big.NewInt(20), PriorBalance: big.NewInt(2)},
			{Address: c, Balance: big.NewInt(30), PriorBalance: big.NewInt(9)},
		},
		Nonces: []NonceChange{{Address: a, Nonce: 1}, {Address: b, Nonce: 5, PriorNonce: 1}},
		Code:   []CodeChange{{Address: a, Code: []byte{0x02}}, {Address: c, Delete: true}},
		Storage: []StorageChange{
			{Address: a, Key: testHash(0), Value: testHash(0x10)},
			{Address: a, Key: testHash(1), Delete: true, PriorValue: testHash(0x01)},
			{Address: b, Key: testHash(1), Value: testHash(0x21)},
			{Address: c, Key: testHash(9), Value: testHash(0x39)},
		},
	}, dst)

	require.ErrorIs(t, mergeChangeSet(&dst, StateChangeSet{StorageClears: []common.Address{a}}), errEndBlockClearedStorage)
}
