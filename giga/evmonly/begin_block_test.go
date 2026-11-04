package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var (
	beginBlockerAddress = common.HexToAddress("0x0000000000000000000000000000000000002103")
	// Slots of a beginBlockRecorder.
	begunSlot   = common.Hash{0xc0} // blocks begun
	sawSlot     = common.Hash{0xc1} // begunSlot as the block's transactions saw it
	sharedSlot  = common.Hash{0xc2} // written by BeginBlock, then by each call
	sharedStart = common.Hash{0x05}
	sharedCall  = common.Hash{0x07}
)

// beginBlockRecorder counts the blocks it begins; each call records the count
// it sees.
type beginBlockRecorder struct {
	fail bool
}

func (beginBlockRecorder) RequiredGas([]byte) uint64 { return 1_000 }

func (beginBlockRecorder) Run(ctx *precompiles.Context, _ []byte) ([]byte, error) {
	ctx.State.SetState(beginBlockerAddress, sawSlot, ctx.State.GetState(beginBlockerAddress, begunSlot))
	ctx.State.SetState(beginBlockerAddress, sharedSlot, sharedCall)
	return nil, nil
}

func (r beginBlockRecorder) BeginBlock(_ precompiles.BlockContext, state precompiles.State) ([]error, error) {
	if r.fail {
		return nil, errRefused
	}
	begun := state.GetState(beginBlockerAddress, begunSlot).Big()
	state.SetState(beginBlockerAddress, begunSlot, common.BigToHash(begun.Add(begun, big.NewInt(1))))
	state.SetState(beginBlockerAddress, sharedSlot, sharedStart)
	return nil, nil
}

func beginBlockRegistry(r beginBlockRecorder) precompiles.Registry {
	return precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{beginBlockerAddress: r}).
		WithBeginBlockers(beginBlockerAddress)
}

func beginBlockCall(t *testing.T) ([]byte, common.Address) {
	t.Helper()
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	to := beginBlockerAddress
	return signLegacyTxWithGasPrice(t, key, big.NewInt(testChainID), 0, &to, big.NewInt(0), nil, 100_000, big.NewInt(1)),
		crypto.PubkeyToAddress(key.PublicKey)
}

func storageChange(t *testing.T, changes StateChangeSet, addr common.Address, key common.Hash) StorageChange {
	t.Helper()
	for _, change := range changes.Storage {
		if change.Address == addr && change.Key == key {
			return change
		}
	}
	require.FailNow(t, "no storage change", "%s %s", addr, key)
	return StorageChange{}
}

func TestBeginBlockerRunsBeforeTheBlocksTransactions(t *testing.T) {
	state := NewMemoryState()
	tx, sender := beginBlockCall(t)
	state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
	cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: beginBlockRegistry(beginBlockRecorder{})}

	result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID)), Txs: [][]byte{tx}})
	require.NoError(t, err)
	require.Equal(t, uint64(1), result.Txs[0].Status)

	require.Equal(t, common.BigToHash(big.NewInt(1)), storageChange(t, result.ChangeSet, beginBlockerAddress, sawSlot).Value,
		"the transaction sees the begin blocker's write")
	shared := storageChange(t, result.ChangeSet, beginBlockerAddress, sharedSlot)
	require.Equal(t, sharedCall, shared.Value, "the transaction's write is the slot's value after the block")
	require.Equal(t, common.Hash{}, shared.PriorValue, "the prior is the value before the begin blocker")

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(1), state.GetState(beginBlockerAddress, begunSlot).Big())
}

func TestBeginBlockerWritesLandInABlockWithNoTransactions(t *testing.T) {
	state := NewMemoryState()
	cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: beginBlockRegistry(beginBlockRecorder{})}

	result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID))})
	require.NoError(t, err)
	require.Equal(t, sharedStart, storageChange(t, result.ChangeSet, beginBlockerAddress, sharedSlot).Value)
	require.Equal(t, common.BigToHash(big.NewInt(1)), storageChange(t, result.ChangeSet, beginBlockerAddress, begunSlot).Value)
}

func TestBeginBlockerErrorFailsTheBlock(t *testing.T) {
	state := NewMemoryState()
	tx, sender := beginBlockCall(t)
	state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
	cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: beginBlockRegistry(beginBlockRecorder{fail: true})}

	_, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID)), Txs: [][]byte{tx}})
	require.ErrorIs(t, err, errRefused)
	require.Equal(t, common.Hash{}, state.GetState(beginBlockerAddress, sawSlot), "no transaction ran")
	require.Equal(t, uint64(0), state.GetNonce(sender))
}

func TestCustomBeginBlockersPanicsOnAMislistedBeginBlocker(t *testing.T) {
	contracts := map[common.Address]precompiles.Contract{
		beginBlockerAddress: beginBlockRecorder{},
		firstEndBlocker:     endBlockRecorder{address: firstEndBlocker},
	}
	base := precompiles.NewStaticRegistry(contracts, firstEndBlocker)
	require.Len(t, customBeginBlockers(base.WithBeginBlockers(beginBlockerAddress)), 1)
	require.Empty(t, customBeginBlockers(nil))
	for name, registry := range map[string]precompiles.Registry{
		"unlisted":   base,
		"not one":    base.WithBeginBlockers(beginBlockerAddress, firstEndBlocker),
		"twice":      base.WithBeginBlockers(beginBlockerAddress, beginBlockerAddress),
		"unresolved": base.WithBeginBlockers(beginBlockerAddress, testAddress(0xee)),
	} {
		require.Panics(t, func() { customBeginBlockers(registry) }, name)
	}
}

func TestMergeEarlierChangeSet(t *testing.T) {
	a, b, c := testAddress(0x0a), testAddress(0x0b), testAddress(0x0c)
	dst := StateChangeSet{
		Accounts: []AccountChange{
			{Address: b, Account: AccountState{Balance: big.NewInt(20)}, Prior: AccountState{Balance: big.NewInt(2)}},
		},
		Storage: []StorageChange{
			{Address: a, Key: testHash(1), Value: testHash(0x12), PriorValue: testHash(0x11)},
			{Address: c, Key: testHash(1), Value: testHash(0x31)},
		},
	}
	earlier := StateChangeSet{
		Accounts: []AccountChange{
			{Address: a, Account: AccountState{Balance: big.NewInt(1)}},
			{Address: b, Account: AccountState{Balance: big.NewInt(2)}, Prior: AccountState{Balance: big.NewInt(0)}},
		},
		Storage: []StorageChange{
			{Address: a, Key: testHash(1), Value: testHash(0x11), PriorValue: testHash(0x10)},
			{Address: b, Key: testHash(1), Value: testHash(0x21)},
		},
	}
	mergeEarlierChangeSet(&dst, earlier)
	require.Equal(t, StateChangeSet{
		Accounts: []AccountChange{
			{Address: a, Account: AccountState{Balance: big.NewInt(1)}},
			{Address: b, Account: AccountState{Balance: big.NewInt(20)}, Prior: AccountState{Balance: big.NewInt(0)}},
		},
		Storage: []StorageChange{
			{Address: a, Key: testHash(1), Value: testHash(0x12), PriorValue: testHash(0x10)},
			{Address: b, Key: testHash(1), Value: testHash(0x21)},
			{Address: c, Key: testHash(1), Value: testHash(0x31)},
		},
	}, dst)
}
