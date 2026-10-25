package evmonly

// Pins evmonly custom precompile calls failing only the calling frame (plain error, not vm.AbortError).

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var behaviorCustomPrecompile = common.HexToAddress("0x0000000000000000000000000000000000001001")

func TestEVMOnlyCustomPrecompileCallOpcodes_Behavior(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	successSlot := testHash(0x62)
	caller := testAddress(0xca)

	tests := []struct {
		name        string
		op          vm.OpCode
		wantGasUsed uint64
	}{
		{name: "CALL", op: vm.CALL, wantGasUsed: 88_859},
		{name: "CALLCODE", op: vm.CALLCODE, wantGasUsed: 88_859},
		{name: "DELEGATECALL", op: vm.DELEGATECALL, wantGasUsed: 88_856},
		{name: "STATICCALL", op: vm.STATICCALL, wantGasUsed: 88_856},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := NewMemoryState()
			state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
			state.SetCode(caller, callOpcodeRuntime(byte(tc.op), behaviorCustomPrecompile, successSlot))
			raw := signLegacyTxWithGasPrice(t, key, chainID, 0, &caller, big.NewInt(0), nil, 200_000, big.NewInt(1))

			result, err := NewExecutor(Config{
				MinGasPrice:       big.NewInt(0),
				CustomPrecompiles: staticPrecompileRegistry{addr: behaviorCustomPrecompile},
			}, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
				Context: blockContext(chainID),
				Txs:     [][]byte{raw},
			})
			require.NoError(t, err)
			state.ApplyChangeSet(result.ChangeSet)

			// Outer tx succeeds; the sub-call fails and consumes its forwarded gas.
			require.Equal(t, uint64(1), result.Txs[0].Status)
			require.NoError(t, result.Txs[0].Err)
			require.Equal(t, common.Hash{}, state.GetState(caller, successSlot))
			require.Equal(t, uint64(1), state.GetNonce(sender))
			require.Equal(t, tc.wantGasUsed, result.Txs[0].GasUsed)
		})
	}

	t.Run("direct tx", func(t *testing.T) {
		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
		raw := signLegacyTxWithGasPrice(t, key, chainID, 0, &behaviorCustomPrecompile, big.NewInt(0), []byte{0x01, 0x02}, 100_000, big.NewInt(1))

		result, err := NewExecutor(Config{
			MinGasPrice:       big.NewInt(0),
			CustomPrecompiles: staticPrecompileRegistry{addr: behaviorCustomPrecompile},
		}, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{raw},
		})
		require.NoError(t, err)
		require.Equal(t, uint64(0), result.Txs[0].Status)
		require.ErrorIs(t, result.Txs[0].Err, precompiles.ErrCustomPrecompilesOpen)
		require.Equal(t, uint64(100_000), result.Txs[0].GasUsed)
	})
}

func TestEVMOnlyEstimateGasCustomPrecompile_Behavior(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	successSlot := testHash(0x62)
	caller := testAddress(0xca)
	cfg := Config{
		MinGasPrice:       big.NewInt(0),
		CustomPrecompiles: staticPrecompileRegistry{addr: behaviorCustomPrecompile},
	}

	t.Run("contract calling custom precompile", func(t *testing.T) {
		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
		state.SetCode(caller, callOpcodeRuntime(byte(vm.CALL), behaviorCustomPrecompile, successSlot))
		store := NewMemoryStore(state)
		executor := NewExecutor(cfg, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

		msg := callMessage(sender, &caller)
		msg.GasLimit = 0
		estimate, revert, err := executor.EstimateGas(t.Context(), blockContext(chainID), msg, 0)
		require.NoError(t, err)
		require.Empty(t, revert)
		// Outer frame succeeds; the precompile sub-call still fails.
		require.Equal(t, uint64(89_794), estimate)

		// The estimate suffices for real execution.
		raw := signLegacyTxWithGasPrice(t, key, chainID, 0, &caller, big.NewInt(0), nil, estimate, big.NewInt(1))
		result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{raw},
		})
		require.NoError(t, err)
		require.Equal(t, uint64(1), result.Txs[0].Status)
		require.Equal(t, uint64(88_859), result.Txs[0].GasUsed)
	})

	t.Run("direct call to custom precompile", func(t *testing.T) {
		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
		store := NewMemoryStore(state)
		executor := NewExecutor(cfg, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

		msg := callMessage(sender, &behaviorCustomPrecompile)
		msg.GasLimit = 0
		estimate, revert, err := executor.EstimateGas(t.Context(), blockContext(chainID), msg, 0)
		// Placeholder error is returned verbatim, not "gas required exceeds allowance".
		require.ErrorIs(t, err, precompiles.ErrCustomPrecompilesOpen)
		require.Equal(t, "evm-only custom precompiles are not implemented", err.Error())
		require.Equal(t, uint64(0), estimate)
		require.Empty(t, revert)
	})

	// EstimateGas does not recover state-reader panics.
	t.Run("panic from state reader mid-estimate propagates", func(t *testing.T) {
		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
		store := NewMemoryStore(state)
		executor := NewExecutor(cfg,
			withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet),
			WithMissingAccountState(panickingStateReader{}))

		target := testAddress(0x7e)
		msg := callMessage(sender, &target)
		require.PanicsWithValue(t, "behavior: state read panic", func() {
			_, _, _ = executor.EstimateGas(t.Context(), blockContext(chainID), msg, 0)
		})
	})
}

type panickingStateReader struct{}

func (panickingStateReader) GetBalance(common.Address) *big.Int { panic("behavior: state read panic") }
func (panickingStateReader) GetNonce(common.Address) uint64     { panic("behavior: state read panic") }
func (panickingStateReader) GetCode(common.Address) []byte      { panic("behavior: state read panic") }
func (panickingStateReader) GetState(common.Address, common.Hash) common.Hash {
	panic("behavior: state read panic")
}
