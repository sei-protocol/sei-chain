package wasmd_test

import (
	"encoding/binary"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/app"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestExecuteBatchAliasedCoinsAreMeteredBeforeJSONParsing(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithIsEVM(true)
	precompile, err := wasmd.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)

	executor := precompile.GetExecutor().(*wasmd.PrecompileExecutor)
	method, err := precompile.ABI.MethodById(executor.ExecuteBatchID)
	require.NoError(t, err)
	input := append([]byte{}, executor.ExecuteBatchID...)
	input = append(input, aliasedExecuteBatchArgs(4)...)
	_, err = method.Inputs.Unpack(input[4:])
	require.NoError(t, err)

	stateDB := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm := vm.EVM{StateDB: stateDB}
	_, remainingGas, err := precompile.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, input, pcommon.DefaultGasCost(input, false), nil, nil, false, false)
	require.Equal(t, vm.ErrExecutionReverted, err)
	require.Equal(t, uint64(0), remainingGas)
	require.EqualError(t, stateDB.GetPrecompileError(), "{wasmd coin JSON parse}")
}

func aliasedExecuteBatchArgs(elements uint64) []byte {
	tupleOffset := 32 * elements
	arrayPayload := make([]byte, 0, tupleOffset+194)
	for range elements {
		arrayPayload = append(arrayPayload, abiWord(tupleOffset)...)
	}
	arrayPayload = append(arrayPayload, abiWord(96)...)
	arrayPayload = append(arrayPayload, abiWord(128)...)
	arrayPayload = append(arrayPayload, abiWord(160)...)
	arrayPayload = append(arrayPayload, abiWord(0)...)
	arrayPayload = append(arrayPayload, abiWord(0)...)
	arrayPayload = append(arrayPayload, abiWord(2)...)
	arrayPayload = append(arrayPayload, []byte("[]")...)

	data := append(abiWord(32), abiWord(elements)...)
	return append(data, arrayPayload...)
}

func abiWord(value uint64) []byte {
	word := make([]byte, 32)
	binary.BigEndian.PutUint64(word[24:], value)
	return word
}
