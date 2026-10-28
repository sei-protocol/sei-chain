package evmonly

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
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
	// failures are returned from EndBlock as work it could not carry out.
	failures []error
}

func (endBlockRecorder) RequiredGas([]byte) uint64 { return 1_000 }

func (r endBlockRecorder) Run(ctx *precompiles.Context, _ []byte) ([]byte, error) {
	calls := ctx.State.GetState(r.address, callsSlot).Big()
	ctx.State.SetState(r.address, callsSlot, common.BigToHash(calls.Add(calls, big.NewInt(1))))
	return nil, nil
}

func (r endBlockRecorder) EndBlock(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	if r.fail {
		return nil, errRefused
	}
	if r.reads != (common.Address{}) {
		state.SetState(r.address, observedSlot, state.GetState(r.reads, observedSlot))
		return r.failures, nil
	}
	blocks := state.GetState(r.address, blocksSlot).Big()
	state.SetState(r.address, blocksSlot, common.BigToHash(blocks.Add(blocks, big.NewInt(1))))
	state.SetState(r.address, observedSlot, state.GetState(r.address, callsSlot))
	state.SetState(r.address, timeSlot, common.BigToHash(new(big.Int).SetUint64(block.UnixNano)))
	if r.pay != nil {
		state.AddBalance(r.payee, r.pay)
	}
	return r.failures, nil
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
	var paid *AccountChange
	for i, change := range result.ChangeSet.Accounts {
		if change.Address == payee {
			paid = &result.ChangeSet.Accounts[i]
		}
	}
	require.NotNil(t, paid)
	require.Equal(t, big.NewInt(10), paid.Prior.Balance, "the prior is the balance before the block")

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(2), state.GetState(firstEndBlocker, observedSlot).Big())
	require.Equal(t, big.NewInt(1), state.GetState(firstEndBlocker, blocksSlot).Big())
	require.Equal(t, big.NewInt(77_000_000_000), state.GetState(firstEndBlocker, timeSlot).Big(), "the block time in nanoseconds")
	require.Equal(t, big.NewInt(15), state.GetBalance(payee))
}

// storageWriter writes to another account's storage, from calls and at the end
// of each block.
type storageWriter struct {
	target common.Address
}

func (storageWriter) RequiredGas([]byte) uint64 { return 1_000 }

func (w storageWriter) Run(ctx *precompiles.Context, _ []byte) ([]byte, error) {
	ctx.State.SetState(w.target, callsSlot, common.Hash{1})
	return nil, nil
}

func (w storageWriter) EndBlock(_ precompiles.BlockContext, state precompiles.State) ([]error, error) {
	state.SetState(w.target, blocksSlot, common.Hash{1})
	return nil, nil
}

// refusingReader reads its own callsSlot, then refuses the call.
type refusingReader struct {
	address common.Address
}

func (refusingReader) RequiredGas([]byte) uint64 { return 1_000 }

func (r refusingReader) Run(ctx *precompiles.Context, _ []byte) ([]byte, error) {
	ctx.State.GetState(r.address, callsSlot)
	return nil, errRefused
}

func TestPrecompileWriteGivesAnEmptyAccountARow(t *testing.T) {
	state := NewMemoryState()
	tx, sender := recorderCall(t, 0)
	state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
	target := common.HexToAddress("0x0000000000000000000000000000000000002103")
	registry := precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{
		firstEndBlocker: storageWriter{target: target},
	}, firstEndBlocker)

	result, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: registry}, withTestState(state)).
		ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID)), Txs: [][]byte{tx}})
	require.NoError(t, err)
	require.Equal(t, uint64(1), result.Txs[0].Status)

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, uint64(1), state.GetNonce(target))
	require.Equal(t, common.Hash{1}, state.GetState(target, callsSlot))
	require.Equal(t, common.Hash{1}, state.GetState(target, blocksSlot))
}

func TestFailedCallKeepsStorageAPrecompileWrote(t *testing.T) {
	state := NewMemoryState()
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(behaviorSenderFunds))
	target := common.HexToAddress("0x0000000000000000000000000000000000002103")
	registry := precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{
		firstEndBlocker: storageWriter{target: target},
		target:          refusingReader{address: target},
	}, firstEndBlocker)

	block := blockContext(big.NewInt(testChainID))
	for nonce, to := range []common.Address{firstEndBlocker, target} {
		tx := signLegacyTxWithGasPrice(t, key, big.NewInt(testChainID), uint64(nonce), &to, big.NewInt(0), nil, 100_000, big.NewInt(1))
		block.Number = uint64(nonce) + 1
		result, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: registry}, withTestState(state)).
			ExecuteBlock(t.Context(), BlockRequest{Context: block, Txs: [][]byte{tx}})
		require.NoError(t, err)
		state.ApplyChangeSet(result.ChangeSet)
	}
	require.Equal(t, common.Hash{1}, state.GetState(target, callsSlot))
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

func TestEndBlockerFailuresDoNotFailTheBlock(t *testing.T) {
	state := NewMemoryState()
	recorder := endBlockRecorder{address: firstEndBlocker, failures: []error{errRefused}}
	executor := NewExecutor(Config{CustomPrecompiles: recorderRegistry(recorder)}, withTestState(state))
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID))})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(1), state.GetState(firstEndBlocker, blocksSlot).Big(), "the end blocker's writes are kept")
}

func TestTruncateUTF8(t *testing.T) {
	require.Equal(t, "short", truncateUTF8("short", 10))
	require.Equal(t, "abc", truncateUTF8("abcdef", 3))
	require.Equal(t, "a", truncateUTF8("aé", 2), "a two-byte rune is not split")
	require.Equal(t, "aé", truncateUTF8("aé", 3))
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

// fundedState answers for every address absent from the store with a balance
// and nothing else, as the EVM-only application's fallback does.
type fundedState struct{}

func (fundedState) GetBalance(common.Address) *big.Int               { return big.NewInt(behaviorSenderFunds) }
func (fundedState) GetNonce(common.Address) uint64                   { return 0 }
func (fundedState) GetCode(common.Address) []byte                    { return nil }
func (fundedState) GetState(common.Address, common.Hash) common.Hash { return common.Hash{} }

// flatKVStateDB serves a flatkv store as the executor's store, with no history.
type flatKVStateDB struct{ *flatkv.CommitStore }

func (flatKVStateDB) OpenViewAt(int64) (gigatypes.StateView, bool) { return nil, false }

func TestEndBlockerStorageReadsBackFromFlatKV(t *testing.T) {
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = t.TempDir()
	store, err := openFlatKVTestStore(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	executor := NewExecutor(Config{CustomPrecompiles: recorderRegistry(endBlockRecorder{address: firstEndBlocker})},
		withTestStores(flatKVStateDB{store}, NewMemoryReceiptStore(), NewFlatKVChangeSetEncoder()), WithMissingAccountState(fundedState{}))
	t.Cleanup(executor.Close)

	block := blockContext(big.NewInt(testChainID))
	for number := uint64(1); number <= 2; number++ {
		block.Number = number
		_, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: block})
		require.NoError(t, err)
	}

	view := store.OpenView()
	defer view.Close()
	require.Equal(t, common.BigToHash(big.NewInt(2)), view.GetStorage(firstEndBlocker, blocksSlot))
	require.True(t, view.AccountExists(firstEndBlocker))
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
		Accounts: []AccountChange{
			{Address: a, Account: AccountState{Balance: big.NewInt(1)}, Prior: AccountState{Balance: big.NewInt(0)}},
			{Address: c, Account: AccountState{Balance: big.NewInt(3), Code: []byte{0x01}}, Prior: AccountState{Balance: big.NewInt(9)}},
		},
		Storage: []StorageChange{
			{Address: a, Key: testHash(1), Value: testHash(0x11), PriorValue: testHash(0x01)},
			{Address: b, Key: testHash(1), Value: testHash(0x21)},
		},
	}
	later := StateChangeSet{
		Accounts: []AccountChange{
			{Address: b, Account: AccountState{Balance: big.NewInt(20), Nonce: 5}, Prior: AccountState{Balance: big.NewInt(2), Nonce: 2}},
			{Address: c, Account: AccountState{Balance: big.NewInt(30), Nonce: 1}, Prior: AccountState{Balance: big.NewInt(3), Code: []byte{0x01}}},
		},
		Storage: []StorageChange{
			{Address: a, Key: testHash(0), Value: testHash(0x10)},
			{Address: a, Key: testHash(1), Delete: true, PriorValue: testHash(0x11)},
			{Address: c, Key: testHash(9), Value: testHash(0x39)},
		},
	}
	mergeChangeSet(&dst, later)
	require.Equal(t, StateChangeSet{
		Accounts: []AccountChange{
			{Address: a, Account: AccountState{Balance: big.NewInt(1)}, Prior: AccountState{Balance: big.NewInt(0)}},
			{Address: b, Account: AccountState{Balance: big.NewInt(20), Nonce: 5}, Prior: AccountState{Balance: big.NewInt(2), Nonce: 2}},
			{Address: c, Account: AccountState{Balance: big.NewInt(30), Nonce: 1}, Prior: AccountState{Balance: big.NewInt(9)}},
		},
		Storage: []StorageChange{
			{Address: a, Key: testHash(0), Value: testHash(0x10)},
			{Address: a, Key: testHash(1), Delete: true, PriorValue: testHash(0x01)},
			{Address: b, Key: testHash(1), Value: testHash(0x21)},
			{Address: c, Key: testHash(9), Value: testHash(0x39)},
		},
	}, dst)
}

// panicking panics from both Run and EndBlock.
type panicking struct{}

func (panicking) RequiredGas([]byte) uint64 { return 0 }
func (panicking) Run(*precompiles.Context, []byte) ([]byte, error) {
	panic("boom")
}
func (panicking) EndBlock(precompiles.BlockContext, precompiles.State) ([]error, error) {
	panic("boom")
}

func TestPrecompilePanicsBecomeErrors(t *testing.T) {
	_, err := runContract(panicking{}, &precompiles.Context{}, nil)
	require.ErrorIs(t, err, errPrecompilePanic)
	require.ErrorContains(t, err, "boom")
	_, err = runEndBlock(panicking{}, precompiles.BlockContext{}, nil)
	require.ErrorIs(t, err, errPrecompilePanic)
}
