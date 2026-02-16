package bls

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestG1Add_IdentityPoints(t *testing.T) {
	p := NewPrecompile(AllOps()[0]) // G1ADD

	// Two G1 identity points (128 bytes each, all zeros) -> identity
	input := make([]byte, 256)
	result, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.NoError(t, err)
	require.Equal(t, 128, len(result))
	require.Equal(t, make([]byte, 128), result)
}

func TestG1Add_InvalidLength(t *testing.T) {
	p := NewPrecompile(AllOps()[0])

	// Invalid input length (not 256 bytes)
	input := make([]byte, 100)
	_, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.Error(t, err)
}

func TestG1MSM_IdentityPoint(t *testing.T) {
	p := NewPrecompile(AllOps()[1]) // G1MSM

	// G1 identity point (128 bytes) + scalar (32 bytes) = 160 bytes
	input := make([]byte, 160)
	result, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.NoError(t, err)
	require.Equal(t, 128, len(result))
	require.Equal(t, make([]byte, 128), result)
}

func TestG1MSM_InvalidLength(t *testing.T) {
	p := NewPrecompile(AllOps()[1])

	// Not a multiple of 160 bytes
	input := make([]byte, 100)
	_, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.Error(t, err)
}

func TestG2Add_IdentityPoints(t *testing.T) {
	p := NewPrecompile(AllOps()[2]) // G2ADD

	// Two G2 identity points (256 bytes each) -> identity
	input := make([]byte, 512)
	result, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.NoError(t, err)
	require.Equal(t, 256, len(result))
	require.Equal(t, make([]byte, 256), result)
}

func TestG2Add_InvalidLength(t *testing.T) {
	p := NewPrecompile(AllOps()[2])

	input := make([]byte, 100)
	_, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.Error(t, err)
}

func TestG2MSM_IdentityPoint(t *testing.T) {
	p := NewPrecompile(AllOps()[3]) // G2MSM

	// G2 identity point (256 bytes) + scalar (32 bytes) = 288 bytes
	input := make([]byte, 288)
	result, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.NoError(t, err)
	require.Equal(t, 256, len(result))
	require.Equal(t, make([]byte, 256), result)
}

func TestG2MSM_InvalidLength(t *testing.T) {
	p := NewPrecompile(AllOps()[3])

	input := make([]byte, 100)
	_, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.Error(t, err)
}

func TestPairing_IdentityPair(t *testing.T) {
	p := NewPrecompile(AllOps()[4]) // PAIRING

	// One pair: G1 identity (128 bytes) + G2 identity (256 bytes) = 384 bytes
	input := make([]byte, 384)
	result, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.NoError(t, err)
	require.Equal(t, 32, len(result))
	// Pairing check with identity points returns true (1)
	expected := make([]byte, 32)
	expected[31] = 1
	require.Equal(t, expected, result)
}

func TestPairing_EmptyInput(t *testing.T) {
	p := NewPrecompile(AllOps()[4])

	// Empty input is invalid (k must be >= 1)
	input := make([]byte, 0)
	_, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.Error(t, err)
}

func TestPairing_InvalidLength(t *testing.T) {
	p := NewPrecompile(AllOps()[4])

	// Not a multiple of 384 bytes
	input := make([]byte, 200)
	_, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.Error(t, err)
}

func TestMapG1_InvalidLength(t *testing.T) {
	p := NewPrecompile(AllOps()[5]) // MAP_FP_TO_G1

	// Invalid input length (not 64 bytes)
	input := make([]byte, 100)
	_, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.Error(t, err)
}

func TestMapG2_InvalidLength(t *testing.T) {
	p := NewPrecompile(AllOps()[6]) // MAP_FP2_TO_G2

	// Invalid input length (not 128 bytes)
	input := make([]byte, 100)
	_, err := p.Run(nil, common.Address{}, common.Address{}, input, big.NewInt(0), false, false, nil)
	require.Error(t, err)
}

func TestRequiredGas(t *testing.T) {
	ops := AllOps()
	tests := []struct {
		name      string
		op        OpInfo
		inputSize int
		expected  uint64
	}{
		{"G1ADD", ops[0], 256, 375},
		{"G1MSM_k1", ops[1], 160, 12000},
		{"G2ADD", ops[2], 512, 600},
		{"G2MSM_k1", ops[3], 288, 22500},
		{"PAIRING_k1", ops[4], 384, 70300},
		{"MAP_FP_TO_G1", ops[5], 64, 5500},
		{"MAP_FP2_TO_G2", ops[6], 128, 23800},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPrecompile(tt.op)
			gas := p.RequiredGas(make([]byte, tt.inputSize))
			require.Equal(t, tt.expected, gas)
		})
	}
}

func TestAddresses(t *testing.T) {
	ops := AllOps()
	expectedAddrs := []string{
		"0x000000000000000000000000000000000000000b",
		"0x000000000000000000000000000000000000000c",
		"0x000000000000000000000000000000000000000d",
		"0x000000000000000000000000000000000000000e",
		"0x000000000000000000000000000000000000000f",
		"0x0000000000000000000000000000000000000010",
		"0x0000000000000000000000000000000000000011",
	}

	require.Equal(t, 7, len(ops))
	for i, op := range ops {
		p := NewPrecompile(op)
		require.Equal(t, common.HexToAddress(expectedAddrs[i]), p.Address())
	}
}
