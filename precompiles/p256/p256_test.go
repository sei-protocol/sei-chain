package p256_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/p256"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

// TestPrecompile_verifyOutOfGasPropagates guards the framework invariant that an
// executor exhausting its gas mid-execution propagates the out-of-gas panic
// (failing the tx) rather than having it caught and downgraded to a reverted
// call. The fixed verify charge is levied outside verify's crypto recover, so a
// call funded for the decode but not the verify cost must panic out, not revert.
func TestPrecompile_verifyOutOfGasPropagates(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	p, err := p256.NewPrecompile(testkeeper.EVMTestApp.GetPrecompileKeepers())
	require.Nil(t, err)
	exec := p.GetExecutor().(*p256.PrecompileExecutor)
	method, err := p.MethodById(exec.VerifyID)
	require.Nil(t, err)
	hash, r, s, x, y := generateValidKeyAndSignature(t)
	inputData := make([]byte, 0, 32*5)
	inputData = append(inputData, common.LeftPadBytes(hash, 32)...)
	inputData = append(inputData, common.LeftPadBytes(r.Bytes(), 32)...)
	inputData = append(inputData, common.LeftPadBytes(s.Bytes(), 32)...)
	inputData = append(inputData, common.LeftPadBytes(x.Bytes(), 32)...)
	inputData = append(inputData, common.LeftPadBytes(y.Bytes(), 32)...)
	packed, err := method.Inputs.Pack(inputData)
	require.Nil(t, err)
	input := append(exec.VerifyID, packed...)

	stateDB := state.NewDBImpl(ctx, k, false)
	evm := &vm.EVM{StateDB: stateDB}
	// Enough gas to cover the calldata decode charge but not the fixed verify
	// cost (P256VerifyGas = 3450), so the verify-cost out-of-gas must propagate.
	require.PanicsWithValue(t, sdk.ErrorOutOfGas{Descriptor: "p256Verify"}, func() {
		_, _, _ = p.RunAndCalculateGas(evm, common.Address{}, common.Address{}, input, 4000, nil, nil, true, false)
	})
}

// generateValidKeyAndSignature mirrors the helper in verifier_test.go; that one
// lives in the white-box (package p256) test, which cannot import testkeeper
// (import cycle), so the dynamic-path test below re-declares it here.
func generateValidKeyAndSignature(t *testing.T) (hash []byte, r, s, x, y *big.Int) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	x = privateKey.PublicKey.X
	y = privateKey.PublicKey.Y
	hash = []byte("test message")
	r, s, err = ecdsa.Sign(rand.Reader, privateKey, hash)
	require.NoError(t, err)
	return hash, r, s, x, y
}

func TestPrecompile_verify(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	p, err := p256.NewPrecompile(testkeeper.EVMTestApp.GetPrecompileKeepers())
	require.Nil(t, err)
	exec := p.GetExecutor().(*p256.PrecompileExecutor)
	method, err := p.MethodById(exec.VerifyID)
	require.Nil(t, err)
	hash, r, s, x, y := generateValidKeyAndSignature(t)

	tests := []struct {
		name           string
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
			stateDB := state.NewDBImpl(ctx, k, false)
			evm := &vm.EVM{StateDB: stateDB}
			inputData := make([]byte, 0, 32*5)
			inputData = append(inputData, common.LeftPadBytes(test.hash, 32)...)
			inputData = append(inputData, common.LeftPadBytes(test.r.Bytes(), 32)...)
			inputData = append(inputData, common.LeftPadBytes(test.s.Bytes(), 32)...)
			inputData = append(inputData, common.LeftPadBytes(test.x.Bytes(), 32)...)
			inputData = append(inputData, common.LeftPadBytes(test.y.Bytes(), 32)...)
			args, err := method.Inputs.Pack(inputData)
			require.Nil(t, err)
			input := append(exec.VerifyID, args...)
			res, _, err := p.RunAndCalculateGas(evm, common.Address{}, common.Address{}, input, math.MaxUint64, nil, nil, true, false)
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
