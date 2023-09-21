package ethtx

import (
	"math"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func mockLegacyTransaction(value *big.Int) *ethtypes.Transaction {
	inner := &ethtypes.LegacyTx{
		GasPrice: big.NewInt(100),
		Gas:      1000,
		To:       &common.Address{'a'},
		Value:    value,
		Data:     []byte{'b'},
		V:        big.NewInt(3),
		R:        big.NewInt(5),
		S:        big.NewInt(7),
	}
	return ethtypes.NewTx(inner)
}

func TestLegacyTransaction(t *testing.T) {
	ethTx := mockLegacyTransaction(big.NewInt(20))
	tx, err := NewLegacyTx(ethTx)
	require.Nil(t, err)
	require.Nil(t, tx.Validate())

	require.Equal(t, uint8(ethtypes.LegacyTxType), tx.TxType())
	copy := tx.Copy()
	require.Equal(t, tx, copy)
	v, r, s := tx.GetRawSignatureValues()
	chainID28 := big.NewInt(28)
	tx.SetSignatureValues(nil, chainID28, r, s)
	require.Equal(t, big.NewInt(0), tx.GetChainID())
	chainID37 := big.NewInt(37)
	tx.SetSignatureValues(nil, chainID37, r, s)
	require.Equal(t, big.NewInt(1), tx.GetChainID())
	chainIDLarge := new(big.Int).Add(big.NewInt(math.MaxInt64), big.NewInt(35))
	tx.SetSignatureValues(nil, chainIDLarge, r, s)
	require.Equal(t, new(big.Int).Div(big.NewInt(math.MaxInt64), big.NewInt(2)), tx.GetChainID())
	tx.SetSignatureValues(nil, v, r, s)
	require.Equal(t, common.CopyBytes(tx.Data), tx.GetData())
	require.Equal(t, tx.GasLimit, tx.GetGas())
	gp := tx.GasPrice
	tx.GasPrice = nil
	require.Nil(t, tx.GetGasPrice())
	tx.GasPrice = gp
	require.Equal(t, gp.BigInt(), tx.GetGasPrice())
	require.Equal(t, gp.BigInt(), tx.GetGasTipCap())
	require.Equal(t, gp.BigInt(), tx.GetGasFeeCap())
	amt := tx.Amount
	tx.Amount = nil
	require.Nil(t, tx.GetValue())
	tx.Amount = amt
	require.Equal(t, amt.BigInt(), tx.GetValue())
	require.Equal(t, tx.Nonce, tx.GetNonce())
	to := tx.To
	tx.To = ""
	require.Nil(t, tx.GetTo())
	tx.To = to
	require.Equal(t, common.HexToAddress(to), *tx.GetTo())
	require.Equal(t, ethTx.Hash(), ethtypes.NewTx(tx.AsEthereumData()).Hash())
	V, R, S := ethTx.RawSignatureValues()
	require.Equal(t, *v, *V)
	require.Equal(t, *s, *S)
	require.Equal(t, *r, *R)
	tx.SetSignatureValues(nil, v, s, r)
	require.Equal(t, fee(tx.GasPrice.BigInt(), tx.GasLimit), tx.Fee())
	require.Equal(t, cost(fee(tx.GasPrice.BigInt(), tx.GasLimit), tx.Amount.BigInt()), tx.Cost())
	require.Equal(t, tx.GasPrice.BigInt(), tx.EffectiveGasPrice(nil))
	require.Equal(t, fee(tx.GasPrice.BigInt(), tx.GasLimit), tx.EffectiveFee(nil))
	require.Equal(t, cost(fee(tx.GasPrice.BigInt(), tx.GasLimit), tx.Amount.BigInt()), tx.EffectiveCost(nil))
}

func TestInvalidLegacyTransaction(t *testing.T) {
	maxInt64 := big.NewInt(math.MaxInt64)
	overflowed := &big.Int{}
	// (2^64)^5 > 2^256
	ethTx := mockLegacyTransaction(overflowed.Exp(maxInt64, big.NewInt(5), nil))
	_, err := NewLegacyTx(ethTx)
	require.NotNil(t, err)
}

func TestValidateLegacyTransaction(t *testing.T) {
	ethTx := mockLegacyTransaction(big.NewInt(20))
	tx, err := NewLegacyTx(ethTx)
	require.Nil(t, err)
	gp := tx.GasPrice
	tx.GasPrice = nil
	require.NotNil(t, tx.Validate())
	ngp := gp.Neg()
	tx.GasPrice = &ngp
	require.NotNil(t, tx.Validate())
	tx.GasPrice = gp
	amt := tx.Amount
	namt := amt.Neg()
	tx.Amount = &namt
	require.NotNil(t, tx.Validate())
	tx.Amount = amt
	overflowed := &big.Int{}
	sdkOverflowed := sdk.NewIntFromBigInt(overflowed.Exp(big.NewInt(math.MaxInt64), big.NewInt(4), nil))
	tx.GasPrice = &sdkOverflowed
	require.NotNil(t, tx.Validate())
	tx.GasPrice = gp
	to := tx.To
	tx.To = "xyz"
	require.NotNil(t, tx.Validate())
	tx.To = to
}
