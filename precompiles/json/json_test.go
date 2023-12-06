package json_test

import (
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
	method, err := p.MethodById(p.ExtractAsBytesID)
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
			[]byte("\"1\""),
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
		input := append(p.ExtractAsBytesID, args...)
		res, err := p.Run(evm, common.Address{}, input)
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
	method, err := p.MethodById(p.ExtractAsBytesListID)
	require.Nil(t, err)
	for _, test := range []struct {
		body           []byte
		expectedOutput [][]byte
	}{
		{
			[]byte("{\"key\":[]}"),
			[][]byte{},
		}, {
			[]byte("{\"key\":[1,2,3]}"),
			[][]byte{[]byte("1"), []byte("2"), []byte("3")},
		}, {
			[]byte("{\"key\":[\"1\", \"2\"]}"),
			[][]byte{[]byte("\"1\""), []byte("\"2\"")},
		}, {
			[]byte("{\"key\":[{\"nested\":1}, {\"nested\":2}]}"),
			[][]byte{[]byte("{\"nested\":1}"), []byte("{\"nested\":2}")},
		},
	} {
		args, err := method.Inputs.Pack(test.body, "key")
		require.Nil(t, err)
		input := append(p.ExtractAsBytesListID, args...)
		res, err := p.Run(evm, common.Address{}, input)
		require.Nil(t, err)
		output, err := method.Outputs.Unpack(res)
		require.Nil(t, err)
		require.Equal(t, 1, len(output))
		require.Equal(t, output[0].([][]byte), test.expectedOutput)
	}
}
