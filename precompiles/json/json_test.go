package json_test

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/json"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestExtractAsBytes(t *testing.T) {
	stateDB := &state.DBImpl{}
	stateDB.WithCtx(sdk.Context{})
	evm := &vm.EVM{StateDB: stateDB}
	p, err := json.NewPrecompile()
	require.Nil(t, err)
	method, err := p.MethodById(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesID)
	require.Nil(t, err)
	for _, test := range []struct {
		body           []byte
		expectedOutput []byte
	}{
		{
			[]byte("{\"key\":1}"),
			[]byte("1"),
		}, {
			[]byte("{\"key\":\"1\"}"),
			[]byte("1"),
		}, {
			[]byte("{\"key\":[1,2,3]}"),
			[]byte("[1,2,3]"),
		}, {
			[]byte("{\"key\":{\"nested\":1}}"),
			[]byte("{\"nested\":1}"),
		},
	} {
		args, err := method.Inputs.Pack(test.body, "key")
		require.Nil(t, err)
		input := append(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesID, args...)
		res, err := p.Run(evm, common.Address{}, common.Address{}, input, nil, true)
		require.Nil(t, err)
		output, err := method.Outputs.Unpack(res)
		require.Nil(t, err)
		require.Equal(t, 1, len(output))
		require.Equal(t, output[0].([]byte), test.expectedOutput)
	}
}

func TestExtractAsBytesList(t *testing.T) {
	stateDB := &state.DBImpl{}
	stateDB.WithCtx(sdk.Context{})
	evm := &vm.EVM{StateDB: stateDB}
	p, err := json.NewPrecompile()
	require.Nil(t, err)
	method, err := p.MethodById(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesListID)
	require.Nil(t, err)
	for _, test := range []struct {
		body           []byte
		expectedOutput [][]byte
	}{
		{
			[]byte("{\"key\":[],\"key2\":1}"),
			[][]byte{},
		}, {
			[]byte("{\"key\":[1,2,3],\"key2\":1}"),
			[][]byte{[]byte("1"), []byte("2"), []byte("3")},
		}, {
			[]byte("{\"key\":[\"1\", \"2\"],\"key2\":1}"),
			[][]byte{[]byte("\"1\""), []byte("\"2\"")},
		}, {
			[]byte("{\"key\":[{\"nested\":1}, {\"nested\":2}],\"key2\":1}"),
			[][]byte{[]byte("{\"nested\":1}"), []byte("{\"nested\":2}")},
		},
	} {
		args, err := method.Inputs.Pack(test.body, "key")
		require.Nil(t, err)
		input := append(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesListID, args...)
		res, err := p.Run(evm, common.Address{}, common.Address{}, input, nil, true)
		require.Nil(t, err)
		output, err := method.Outputs.Unpack(res)
		require.Nil(t, err)
		require.Equal(t, 1, len(output))
		require.Equal(t, output[0].([][]byte), test.expectedOutput)
	}
}

func TestExtractAsUint256(t *testing.T) {
	stateDB := &state.DBImpl{}
	stateDB.WithCtx(sdk.Context{})
	evm := &vm.EVM{StateDB: stateDB}
	p, err := json.NewPrecompile()
	require.Nil(t, err)
	method, err := p.MethodById(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsUint256ID)
	require.Nil(t, err)
	n := new(big.Int)
	n.SetString("12345678901234567890", 10)
	for _, test := range []struct {
		body           []byte
		expectedOutput *big.Int
	}{
		{
			[]byte("{\"key\":\"12345678901234567890\"}"),
			n,
		}, {
			[]byte("{\"key\":\"0\"}"),
			big.NewInt(0),
		},
	} {
		args, err := method.Inputs.Pack(test.body, "key")
		require.Nil(t, err)
		input := append(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsUint256ID, args...)
		res, err := p.Run(evm, common.Address{}, common.Address{}, input, nil, true)
		require.Nil(t, err)
		output, err := method.Outputs.Unpack(res)
		require.Nil(t, err)
		require.Equal(t, 1, len(output))
		require.Equal(t, 0, output[0].(*big.Int).Cmp(test.expectedOutput))
	}
}
