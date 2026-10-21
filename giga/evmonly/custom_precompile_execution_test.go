package evmonly

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var registeredPrecompileAddress = common.HexToAddress("0x0000000000000000000000000000000000001006")

// echoPrecompile writes its input's first byte into its own storage slot 0 and
// returns the caller address, pinning that a registered Contract actually runs
// (rather than failing closed) and that its writes are visible afterward.
type echoPrecompile struct{}

func (echoPrecompile) RequiredGas([]byte) uint64 { return 1_000 }

func (echoPrecompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	if len(input) > 0 {
		ctx.State.SetState(ctx.Address, common.Hash{}, common.BytesToHash(input[:1]))
	}
	return ctx.Caller.Bytes(), nil
}

func TestRegisteredCustomPrecompileRuns(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(behaviorSenderFunds))
	raw := signLegacyTxWithGasPrice(t, key, chainID, 0, &registeredPrecompileAddress, big.NewInt(0), []byte{0x2a}, 100_000, big.NewInt(1))

	registry := precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{
		registeredPrecompileAddress: echoPrecompile{},
	})
	result, err := NewExecutor(Config{
		MinGasPrice:       big.NewInt(0),
		CustomPrecompiles: registry,
	}, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{raw},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), result.Txs[0].Status)
	require.NoError(t, result.Txs[0].Err)
	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, common.BytesToHash([]byte{0x2a}), state.GetState(registeredPrecompileAddress, common.Hash{}))
}

// slotReader reads its storage slots 0 through input[0]-1, then slot 0 again.
type slotReader struct{}

func (slotReader) RequiredGas([]byte) uint64 { return 1_000 }

func (slotReader) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	for i := range input[0] {
		ctx.State.GetState(ctx.Address, common.BytesToHash([]byte{i}))
	}
	ctx.State.GetState(ctx.Address, common.Hash{})
	return nil, nil
}

func TestRegisteredCustomPrecompileMetersStorageReads(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	registry := precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{
		registeredPrecompileAddress: slotReader{},
	})
	run := func(slots byte, gasLimit uint64) *TxResult {
		state := NewMemoryState()
		state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(behaviorSenderFunds))
		raw := signLegacyTxWithGasPrice(t, key, chainID, 0, &registeredPrecompileAddress, big.NewInt(0), []byte{slots}, gasLimit, big.NewInt(1))
		result, err := NewExecutor(Config{
			MinGasPrice:       big.NewInt(0),
			CustomPrecompiles: registry,
		}, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{raw},
		})
		require.NoError(t, err)
		return &result.Txs[0]
	}
	base := params.TxGas + params.TxDataNonZeroGasEIP2028 + 1_000

	one := run(1, 100_000)
	require.Equal(t, uint64(1), one.Status)
	require.Equal(t, base+params.ColdSloadCostEIP2929+params.WarmStorageReadCostEIP2929, one.GasUsed, "one cold read, then warm")

	three := run(3, 100_000)
	require.Equal(t, uint64(1), three.Status)
	require.Equal(t, one.GasUsed+2*params.ColdSloadCostEIP2929, three.GasUsed, "each new slot is cold")

	short := run(1, base+params.ColdSloadCostEIP2929)
	require.Equal(t, uint64(0), short.Status, "the warm read is out of gas")
	require.Equal(t, base+params.ColdSloadCostEIP2929, short.GasUsed)
}

// gasRecorder reads storage slot 0 and records the gas Run started with.
type gasRecorder struct{ remaining *uint64 }

func (gasRecorder) RequiredGas([]byte) uint64 { return 1_000 }

func (g gasRecorder) Run(ctx *precompiles.Context, _ []byte) ([]byte, error) {
	*g.remaining = ctx.GasRemaining
	ctx.State.GetState(ctx.Address, common.Hash{})
	return nil, nil
}

func TestRegisteredCustomPrecompileReportsGas(t *testing.T) {
	var remaining uint64
	contract := registeredCustomPrecompile{address: registeredPrecompileAddress, contract: gasRecorder{&remaining}}
	stateDB := newNativeStateDB(NewMemoryState())
	evm := vm.NewEVM(buildBlockContext(blockContext(big.NewInt(testChainID))), stateDB, params.AllDevChainProtocolChanges, vm.Config{}, nil)
	stateDB.SetEVM(evm)
	type change struct{ from, to uint64 }
	var changes []change
	hooks := &tracing.Hooks{OnGasChange: func(from, to uint64, _ tracing.GasChangeReason) { changes = append(changes, change{from, to}) }}

	_, left, err := contract.RunAndCalculateGas(evm, common.Address{}, common.Address{}, nil, 10_000, nil, hooks, true, false)
	require.NoError(t, err)
	require.Equal(t, uint64(9_000), remaining, "Run sees the gas left after RequiredGas")
	require.Equal(t, 9_000-params.ColdSloadCostEIP2929, left)
	require.Equal(t, []change{{10_000, 9_000}, {9_000, left}}, changes, "tracers see the storage reads")
}

// counterPrecompile adds one to its storage slot 0 on every call.
type counterPrecompile struct{}

func (counterPrecompile) RequiredGas([]byte) uint64 { return 1_000 }

func (counterPrecompile) Run(ctx *precompiles.Context, _ []byte) ([]byte, error) {
	n := ctx.State.GetState(ctx.Address, common.Hash{}).Big()
	ctx.State.SetState(ctx.Address, common.Hash{}, common.BigToHash(n.Add(n, big.NewInt(1))))
	return nil, nil
}

func TestRegisteredCustomPrecompileOCCMatchesSequential(t *testing.T) {
	const senders = 8
	chainID := big.NewInt(testChainID)
	keys := make([]*ecdsa.PrivateKey, senders)
	var txs [][]byte
	for i := range keys {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		keys[i] = key
		txs = append(txs, signLegacyTxWithGasPrice(t, key, chainID, 0, &registeredPrecompileAddress, big.NewInt(0), nil, 100_000, big.NewInt(1)))
	}
	registry := precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{registeredPrecompileAddress: counterPrecompile{}})
	run := func(workers int) *BlockResult {
		state := NewMemoryState()
		for _, key := range keys {
			state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(behaviorSenderFunds))
		}
		result, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: workers, CustomPrecompiles: registry}, withTestState(state)).
			ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: txs})
		require.NoError(t, err)
		return result
	}
	sequential, occ := run(1), run(4)
	require.True(t, occ.OCCStats.Attempted)
	require.False(t, occ.OCCStats.Fallback, occ.OCCStats.FallbackReason)
	require.Positive(t, occ.OCCStats.RerunCount, "every call writes the same slot")
	require.Equal(t, sequential.GasUsed, occ.GasUsed)
	require.Equal(t, sequential.Txs, occ.Txs)
	require.Equal(t, sequential.Receipts, occ.Receipts)
	require.Equal(t, sequential.ChangeSet, occ.ChangeSet)
	state := NewMemoryState()
	state.ApplyChangeSet(occ.ChangeSet)
	require.Equal(t, common.BigToHash(big.NewInt(senders)), state.GetState(registeredPrecompileAddress, common.Hash{}))
}
