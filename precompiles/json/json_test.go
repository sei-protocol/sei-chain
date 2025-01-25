package json_test

import (
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"math/big"
	"strings"
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
		},
	} {
		args, err := method.Inputs.Pack(test.body, "key")
		require.Nil(t, err)
		input := append(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesID, args...)
		res, err := p.Run(evm, common.Address{}, common.Address{}, input, nil, true, false)
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
		res, err := p.Run(evm, common.Address{}, common.Address{}, input, nil, true, false)
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
		res, err := p.Run(evm, common.Address{}, common.Address{}, input, nil, true, false)
		require.Nil(t, err)
		output, err := method.Outputs.Unpack(res)
		require.Nil(t, err)
		require.Equal(t, 1, len(output))
		require.Equal(t, 0, output[0].(*big.Int).Cmp(test.expectedOutput))
	}
}

func TestPrecompileExecutor_extractAsBytesFromArray(t *testing.T) {
	stateDB := &state.DBImpl{}
	stateDB.WithCtx(sdk.Context{})
	evm := &vm.EVM{StateDB: stateDB}

	type args struct {
		in0    sdk.Context
		method *abi.Method
		args   []interface{}
		value  *big.Int
	}

	type input struct {
		body       []byte
		indexArray uint16
	}
	tests := []struct {
		name       string
		input      input
		args       args
		want       []byte
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "extracts string el from array",
			input: input{
				body:       []byte("[\"1\", \"2\"]"),
				indexArray: 1,
			},
			want: []byte("2"),
		},
		{
			name: "extracts number el from array",
			input: input{
				body:       []byte("[1, \"2\"]"),
				indexArray: 0,
			},
			want: []byte("1"),
		},
		{
			name: "extracts nested array el from array",
			input: input{
				body:       []byte("[1, \"2\", [1,2]]"),
				indexArray: 2,
			},
			want: []byte("[1,2]"),
		},
		{
			name: "extracts nested object el from array",
			input: input{
				body:       []byte("[1, \"2\", {\"key\":1}]"),
				indexArray: 2,
			},
			want: []byte("{\"key\":1}"),
		},
		{
			name: "extracts bool el from array",
			input: input{
				body:       []byte("[true, \"2\"]"),
				indexArray: 0,
			},
			want: []byte("true"),
		},
		{
			name: "extracts null el from array",
			input: input{
				body:       []byte("[null, \"2\"]"),
				indexArray: 0,
			},
			want: []byte("null"),
		},
		{
			name: "extracts empty array el from array",
			input: input{
				body:       []byte("[[], \"2\"]"),
				indexArray: 0,
			},
			want: []byte("[]"),
		},
		{
			name: "extracts empty object el from array",
			input: input{
				body: []byte("[{}, \"2\"]"),
			},
			want: []byte("{}"),
		},
		{
			name: "extracts empty string el from array",
			input: input{
				body: []byte("[\"\", \"2\"]"),
			},
			want: []byte(""),
		},
		{
			name: "returns error if indexArray is out of bounds",
			input: input{
				body:       []byte("[\"1\", \"2\"]"),
				indexArray: 2,
			},
			wantErr:    true,
			wantErrMsg: "index 2 is out of bounds",
		},
		{
			name: "returns error if indexArray is out of bounds for empty array",
			input: input{
				body:       []byte("[]"),
				indexArray: 0,
			},
			wantErr:    true,
			wantErrMsg: "index 0 is out of bounds",
		},
		{
			name: "returns error if value is passed",
			args: args{
				value: big.NewInt(1),
			},
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "extracts last element from 2^16 el size array",
			input: input{
				body:       generateLargeArray(65536),
				indexArray: 65535,
			},
			want: []byte("65535"),
		},
		{
			name: "should error out if size of array is greater than 65536", // 65536 as we can have 2^16 elements in array 0 to 65535
			input: input{
				body:       generateLargeArray(65537),
				indexArray: 65535,
			},
			wantErr:    true,
			wantErrMsg: "input array is larger than 2^16",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := json.NewPrecompile()
			require.Nil(t, err)
			method, err := p.MethodById(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesFromArrayID)
			inputArgs, err := method.Inputs.Pack(tt.input.body, tt.input.indexArray)
			require.Nil(t, err)
			in := append(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesFromArrayID, inputArgs...)
			res, err := p.Run(evm, common.Address{}, common.Address{}, in, tt.args.value, true, false)

			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.wantErrMsg, string(res))
				return
			} else {
				output, err := method.Outputs.Unpack(res)
				require.NoError(t, err)
				require.Equal(t, tt.want, output[0].([]byte))
			}
		})
	}
}

func generateLargeArray(size int) []byte {
	array := make([]string, size)
	for i := 0; i < size; i++ {
		array[i] = fmt.Sprintf("\"%d\"", i)
	}
	return []byte(fmt.Sprintf("[%s]", strings.Join(array, ",")))
}

func TestExtractElementFromNestedArray(t *testing.T) {
	stateDB := &state.DBImpl{}
	stateDB.WithCtx(sdk.Context{})
	evm := &vm.EVM{StateDB: stateDB}
	p, err := json.NewPrecompile()
	require.NoError(t, err)
	methodExtractAsBytes, err := p.MethodById(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesID)
	methodExtractAsBytesList, err := p.MethodById(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesListID)
	methodExtractAsBytesFromArray, err := p.MethodById(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesFromArrayID)
	require.NoError(t, err)

	body := []byte("{\"data\":{\"exchange_rates\": [[1727762390,\"1.033207463698531844\"],[1727545459,\"1.032887092691178063\"]],\"apr\": \"0.000101145698240442\"}}")

	args, err := methodExtractAsBytes.Inputs.Pack(body, "data")
	input := append(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesID, args...)
	res, err := p.Run(evm, common.Address{}, common.Address{}, input, nil, true, false)
	require.NoError(t, err)
	data, err := methodExtractAsBytes.Outputs.Unpack(res)
	require.NoError(t, err)

	args, err = methodExtractAsBytesList.Inputs.Pack(data[0].([]byte), "exchange_rates")
	input2 := append(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesListID, args...)
	res, err = p.Run(evm, common.Address{}, common.Address{}, input2, nil, true, false)
	require.NoError(t, err)
	exchangeRates, err := methodExtractAsBytesList.Outputs.Unpack(res)
	require.NoError(t, err)

	array := exchangeRates[0].([][]byte)[0] // [1727762390,"1.033207463698531844"]

	require.NoError(t, err)
	inputArgs, err := methodExtractAsBytesFromArray.Inputs.Pack(array, uint16(1))
	require.NoError(t, err)
	in := append(p.GetExecutor().(*json.PrecompileExecutor).ExtractAsBytesFromArrayID, inputArgs...)
	res, err = p.Run(evm, common.Address{}, common.Address{}, in, nil, true, false)
	require.NoError(t, err)
	output, err := methodExtractAsBytesFromArray.Outputs.Unpack(res)
	require.NoError(t, err)
	require.Equal(t, []byte("1.033207463698531844"), output[0].([]byte))
}
