package evm

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEvmTxEffectiveGasTip(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tx      ethtypes.TxData
		baseFee int64
		want    uint64
	}{
		{
			name: "zero base fee",
			tx:   &ethtypes.DynamicFeeTx{GasFeeCap: big.NewInt(100), GasTipCap: big.NewInt(3)},
			want: 3,
		},
		{
			name:    "tip cap limits tip",
			tx:      &ethtypes.DynamicFeeTx{GasFeeCap: big.NewInt(100), GasTipCap: big.NewInt(3)},
			baseFee: 10, want: 3,
		},
		{
			name:    "fee cap limits tip",
			tx:      &ethtypes.DynamicFeeTx{GasFeeCap: big.NewInt(12), GasTipCap: big.NewInt(3)},
			baseFee: 10, want: 2,
		},
		{
			name:    "zero tip",
			tx:      &ethtypes.DynamicFeeTx{GasFeeCap: big.NewInt(10), GasTipCap: big.NewInt(3)},
			baseFee: 10, want: 0,
		},
		{
			name:    "legacy gas price",
			tx:      &ethtypes.LegacyTx{GasPrice: big.NewInt(100)},
			baseFee: 10, want: 90,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := TestEvmTx(ethtypes.NewTx(tc.tx), common.Address{})
			baseFee := big.NewInt(tc.baseFee)
			tip, ok := tx.EffectiveGasTip(baseFee)
			require.True(t, ok)
			require.Equal(t, *uint256.NewInt(tc.want), tip)
			require.Equal(t, tc.baseFee, baseFee.Int64())
		})
	}
}

func TestEvmTxEffectiveGasTipMaxUint256(t *testing.T) {
	maxFee := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	tx := TestEvmTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		GasFeeCap: maxFee, GasTipCap: maxFee,
	}), common.Address{})
	tip, ok := tx.EffectiveGasTip(new(big.Int))
	require.True(t, ok)
	require.Equal(t, *new(uint256.Int).SetAllOne(), tip)
}

func TestEvmTxEffectiveGasTipInvalid(t *testing.T) {
	maxFee := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	overflow := new(big.Int).Add(maxFee, big.NewInt(1))
	for _, tc := range []struct {
		name    string
		feeCap  *big.Int
		tipCap  *big.Int
		baseFee *big.Int
	}{
		{name: "negative effective tip", feeCap: big.NewInt(9), tipCap: big.NewInt(3), baseFee: big.NewInt(10)},
		{name: "oversized base fee", feeCap: maxFee, tipCap: big.NewInt(3), baseFee: overflow},
		{name: "negative base fee", feeCap: maxFee, tipCap: big.NewInt(3), baseFee: big.NewInt(-1)},
		{name: "oversized fee cap", feeCap: overflow, tipCap: big.NewInt(3), baseFee: new(big.Int)},
		{name: "oversized tip cap", feeCap: maxFee, tipCap: overflow, baseFee: new(big.Int)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := TestEvmTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
				GasFeeCap: tc.feeCap, GasTipCap: tc.tipCap,
			}), common.Address{})
			tip, ok := tx.EffectiveGasTip(tc.baseFee)
			require.False(t, ok)
			require.Equal(t, uint256.Int{}, tip)
		})
	}
}

func TestEvmTxEffectiveGasTipNilBaseFee(t *testing.T) {
	tx := TestEvmTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		GasFeeCap: big.NewInt(12), GasTipCap: big.NewInt(3),
	}), common.Address{})
	tip, ok := tx.EffectiveGasTip(nil)
	require.True(t, ok)
	require.Equal(t, *uint256.NewInt(3), tip)
}
