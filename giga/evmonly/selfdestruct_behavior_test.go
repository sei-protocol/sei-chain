package evmonly

// Pins nativeStateDB SELFDESTRUCT semantics against the geth reference executor.

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// Anvil account 0.
const behaviorAnvilKey0 = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

const behaviorSenderFunds = 1_000_000_000_000

type sdBehaviorRun struct {
	native *MemoryState
	result *BlockResult
	geth   *gethReferenceResult
}

// runSelfDestructBehavior runs one tx on evmonly and the geth reference executor.
func runSelfDestructBehavior(t *testing.T, state *MemoryState, rawTx []byte) sdBehaviorRun {
	t.Helper()
	cfg := Config{MinGasPrice: big.NewInt(0)}
	ctx := blockContext(big.NewInt(testChainID))
	gethResult, err := executeGethReferenceBlock(t, state, cfg, ctx, [][]byte{rawTx})
	require.NoError(t, err)
	result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
		Context: ctx,
		Txs:     [][]byte{rawTx},
	})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	return sdBehaviorRun{native: state, result: result, geth: gethResult}
}

func storeThenSelfDestruct(slot, value common.Hash, beneficiary []byte) []byte {
	code := appendPush32(nil, value)
	code = appendPush32(code, slot)
	code = append(code, byte(vm.SSTORE))
	return append(code, beneficiary...)
}

func push20SelfDestruct(addr common.Address) []byte {
	return append(appendPush20(nil, addr), byte(vm.SELFDESTRUCT))
}

func TestEVMOnlySelfDestructSemantics_Behavior(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	beneficiary := testAddress(0xbe)
	slot := testHash(0x01)
	value := testHash(0x42)
	created := crypto.CreateAddress(sender, 0)

	t.Run("same-tx create+selfdestruct clears code storage balance and nonce", func(t *testing.T) {
		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
		raw := signLegacyTxWithGasPrice(t, key, chainID, 0, nil, big.NewInt(1000),
			storeThenSelfDestruct(slot, value, push20SelfDestruct(beneficiary)), 300_000, big.NewInt(1))

		run := runSelfDestructBehavior(t, state, raw)

		requireExecutionParity(t, run.result, run.geth)
		requireAddressParity(t, state, run.geth.state, created, slot)
		requireAddressParity(t, state, run.geth.state, beneficiary)
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, run.result.Txs[0].Status)
		require.Equal(t, uint64(108_167), run.result.Txs[0].GasUsed)
		require.Empty(t, state.GetCode(created))
		require.Equal(t, common.Hash{}, state.GetState(created, slot))
		require.Equal(t, big.NewInt(0), state.GetBalance(created))
		require.Equal(t, uint64(0), state.GetNonce(created))
		require.Equal(t, big.NewInt(1000), state.GetBalance(beneficiary))
		require.Contains(t, run.result.ChangeSet.StorageClears, created)
	})

	t.Run("pre-existing contract only moves balance", func(t *testing.T) {
		contract := testAddress(0xc5)
		runtime := push20SelfDestruct(beneficiary)
		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
		state.SetBalance(contract, big.NewInt(500))
		state.SetNonce(contract, 1)
		state.SetCode(contract, runtime)
		state.SetState(contract, slot, value)
		raw := signLegacyTxWithGasPrice(t, key, chainID, 0, &contract, big.NewInt(0), nil, 100_000, big.NewInt(1))

		run := runSelfDestructBehavior(t, state, raw)

		requireExecutionParity(t, run.result, run.geth)
		requireAddressParity(t, state, run.geth.state, contract, slot)
		requireAddressParity(t, state, run.geth.state, beneficiary)
		require.Equal(t, uint64(53_603), run.result.Txs[0].GasUsed)
		require.Equal(t, runtime, state.GetCode(contract))
		require.Equal(t, value, state.GetState(contract, slot))
		require.Equal(t, uint64(1), state.GetNonce(contract))
		require.Equal(t, big.NewInt(0), state.GetBalance(contract))
		require.Equal(t, big.NewInt(500), state.GetBalance(beneficiary))
		require.Empty(t, run.result.ChangeSet.StorageClears)
	})

	t.Run("same-tx created contract destructing to itself burns its balance", func(t *testing.T) {
		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
		raw := signLegacyTxWithGasPrice(t, key, chainID, 0, nil, big.NewInt(1000),
			storeThenSelfDestruct(slot, value, []byte{byte(vm.ADDRESS), byte(vm.SELFDESTRUCT)}), 300_000, big.NewInt(1))

		run := runSelfDestructBehavior(t, state, raw)

		requireExecutionParity(t, run.result, run.geth)
		requireAddressParity(t, state, run.geth.state, created, slot)
		requireAddressParity(t, state, run.geth.state, sender)
		gasUsed := run.result.Txs[0].GasUsed
		require.Equal(t, uint64(80_474), gasUsed)
		require.Equal(t, big.NewInt(0), state.GetBalance(created))
		require.Empty(t, state.GetCode(created))
		require.Equal(t, uint64(0), state.GetNonce(created))
		// Sender paid value + gas; the 1000 wei is burned.
		require.Equal(t, big.NewInt(behaviorSenderFunds-1000-int64(gasUsed)), state.GetBalance(sender)) //nolint:gosec
		require.Equal(t, big.NewInt(int64(gasUsed)), state.GetBalance(blockContext(chainID).Coinbase))  //nolint:gosec
	})

	t.Run("prefunded address then created and destroyed forwards prefund plus endowment", func(t *testing.T) {
		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
		state.SetBalance(created, big.NewInt(777))
		raw := signLegacyTxWithGasPrice(t, key, chainID, 0, nil, big.NewInt(1000),
			storeThenSelfDestruct(slot, value, push20SelfDestruct(beneficiary)), 300_000, big.NewInt(1))

		run := runSelfDestructBehavior(t, state, raw)

		requireExecutionParity(t, run.result, run.geth)
		requireAddressParity(t, state, run.geth.state, created, slot)
		requireAddressParity(t, state, run.geth.state, beneficiary)
		require.Equal(t, uint64(108_167), run.result.Txs[0].GasUsed)
		require.Equal(t, big.NewInt(1777), state.GetBalance(beneficiary))
		require.Equal(t, big.NewInt(0), state.GetBalance(created))
		require.Empty(t, state.GetCode(created))
		require.Equal(t, uint64(0), state.GetNonce(created))
	})

	t.Run("value sent to a contract after it selfdestructed in the same tx", func(t *testing.T) {
		factory := testAddress(0xfa)
		childInit := push20SelfDestruct(beneficiary)
		code := appendPush1(nil, byte(len(childInit)))
		offsetPos := len(code) + 1
		code = appendPush1(code, 0)
		code = appendPush1(code, 0)
		code = append(code, byte(vm.CODECOPY))
		code = appendPush1(code, byte(len(childInit)))
		code = appendPush1(code, 0)
		code = appendPush1(code, 0)
		code = append(code, byte(vm.CREATE))
		code = appendPush1(code, 0)
		code = appendPush1(code, 0)
		code = appendPush1(code, 0)
		code = appendPush1(code, 0)
		code = appendPush1(code, 5)
		code = append(code, byte(vm.DUP6))
		code = appendPush2(code, 0xffff)
		code = append(code, byte(vm.CALL), byte(vm.STOP))
		code[offsetPos] = byte(len(code))
		code = append(code, childInit...)
		child := crypto.CreateAddress(factory, 1)

		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
		state.SetBalance(factory, big.NewInt(100))
		state.SetNonce(factory, 1)
		state.SetCode(factory, code)
		raw := signLegacyTxWithGasPrice(t, key, chainID, 0, &factory, big.NewInt(0), nil, 500_000, big.NewInt(1))

		run := runSelfDestructBehavior(t, state, raw)

		requireExecutionParity(t, run.result, run.geth)
		requireAddressParity(t, state, run.geth.state, factory)
		requireAddressParity(t, state, run.geth.state, beneficiary)
		require.Equal(t, uint64(67_453), run.result.Txs[0].GasUsed)
		require.Equal(t, big.NewInt(95), state.GetBalance(factory))
		require.Empty(t, state.GetCode(child))
		require.Equal(t, uint64(0), state.GetNonce(child))
		// geth burns the 5 wei received after SELFDESTRUCT.
		require.Equal(t, "0", run.geth.state.GetBalance(child).String())
		// DIVERGENCE: evmonly keeps value sent after same-tx selfdestruct; geth burns it.
		require.Equal(t, big.NewInt(5), state.GetBalance(child))
	})
}
