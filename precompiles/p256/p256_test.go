package p256

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestPrecompile_verify(t *testing.T) {
	stateDB := &state.DBImpl{}
	stateDB.WithCtx(sdk.Context{})
	evm := &vm.EVM{StateDB: stateDB}
	p, err := NewPrecompile()
	require.Nil(t, err)
	method, err := p.MethodById(p.GetExecutor().(*PrecompileExecutor).VerifyID)
	require.Nil(t, err)
	hash, r, s, x, y := generateValidKeyAndSignature(t)

	tests := []struct {
		name           string // Added name field for each test
		hash           []byte
		r, s, x, y     *big.Int
		expectedOutput []byte
	}{
		{
			name:           "Verify returns 1 in 32 bytes format for valid signature",
			hash:           hash,
			r:              r,
			s:              s,
			x:              x,
			y:              y,
			expectedOutput: common.LeftPadBytes([]byte{1}, 32),
		},
		{
			name:           "Verify does not return any output data for invalid signature",
			hash:           hash,
			r:              big.NewInt(1),
			s:              s,
			x:              x,
			y:              y,
			expectedOutput: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inputData := make([]byte, 0, 32*5)
			inputData = append(inputData, common.LeftPadBytes(test.hash, 32)...)
			inputData = append(inputData, common.LeftPadBytes(test.r.Bytes(), 32)...)
			inputData = append(inputData, common.LeftPadBytes(test.s.Bytes(), 32)...)
			inputData = append(inputData, common.LeftPadBytes(test.x.Bytes(), 32)...)
			inputData = append(inputData, common.LeftPadBytes(test.y.Bytes(), 32)...)
			args, err := method.Inputs.Pack(inputData)
			require.Nil(t, err)
			input := append(p.GetExecutor().(*PrecompileExecutor).VerifyID, args...)
			res, err := p.Run(evm, common.Address{}, common.Address{}, input, nil, true, false, nil)
			require.Nil(t, err)
			if res != nil {
				output, err := method.Outputs.Unpack(res)
				require.Nil(t, err)
				require.Equal(t, test.expectedOutput, output[0].([]byte))
			} else {
				require.Equal(t, test.expectedOutput, res)
			}

		})
	}
}
