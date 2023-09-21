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

func mockDynamicFeeTransaction(value *big.Int) *ethtypes.Transaction {
	inner := &ethtypes.DynamicFeeTx{
		ChainID:    big.NewInt(1),
		GasTipCap:  big.NewInt(50),
		GasFeeCap:  big.NewInt(100),
		Gas:        1000,
		To:         &common.Address{'a'},
		Value:      value,
		Data:       []byte{'b'},
		AccessList: mockAccessList(),
		V:          big.NewInt(3),
		R:          big.NewInt(5),
		S:          big.NewInt(7),
	}
	return ethtypes.NewTx(inner)
}

func TestDynamicFeeTransaction(t *testing.T) {
	ethTx := mockDynamicFeeTransaction(big.NewInt(20))
	tx, err := NewDynamicFeeTx(ethTx)
	require.Nil(t, err)
	require.Nil(t, tx.Validate())

	require.Equal(t, uint8(ethtypes.DynamicFeeTxType), tx.TxType())
	copy := tx.Copy()
	require.Equal(t, tx, copy)
	cached := tx.ChainID
	tx.ChainID = nil
	require.Nil(t, tx.GetChainID())
	tx.ChainID = cached
	require.Equal(t, cached.BigInt(), tx.GetChainID())
	al := tx.Accesses
	tx.Accesses = nil
	require.Nil(t, tx.GetAccessList())
	tx.Accesses = al
	require.Equal(t, *al.ToEthAccessList(), tx.GetAccessList())
	require.Equal(t, common.CopyBytes(tx.Data), tx.GetData())
	require.Equal(t, tx.GasLimit, tx.GetGas())
	gfc := tx.GasFeeCap
	tx.GasFeeCap = nil
	require.Nil(t, tx.GetGasPrice())
	tx.GasFeeCap = gfc
	require.Equal(t, gfc.BigInt(), tx.GetGasPrice())
	gtc := tx.GasTipCap
	tx.GasTipCap = nil
	require.Nil(t, tx.GetGasTipCap())
	tx.GasTipCap = gtc
	require.Equal(t, gtc.BigInt(), tx.GetGasTipCap())
	require.Equal(t, gfc.BigInt(), tx.GetGasFeeCap())
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
	v, s, r := tx.GetRawSignatureValues()
	V, S, R := ethTx.RawSignatureValues()
	require.Equal(t, *v, *V)
	require.Equal(t, *s, *S)
	require.Equal(t, *r, *R)
	tx.SetSignatureValues(cached.BigInt(), v, s, r)
	require.Equal(t, fee(tx.GasFeeCap.BigInt(), tx.GasLimit), tx.Fee())
	require.Equal(t, cost(fee(tx.GasFeeCap.BigInt(), tx.GasLimit), tx.Amount.BigInt()), tx.Cost())
	baseFee := big.NewInt(2)
	require.Equal(t, EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt()), tx.EffectiveGasPrice(baseFee))
	require.Equal(t, fee(EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt()), tx.GasLimit), tx.EffectiveFee(baseFee))
	require.Equal(t, cost(fee(EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt()), tx.GasLimit), tx.Amount.BigInt()), tx.EffectiveCost(baseFee))
}

func TestInvalidDynamicFeeTransaction(t *testing.T) {
	maxInt64 := big.NewInt(math.MaxInt64)
	overflowed := &big.Int{}
	// (2^64)^5 > 2^256
	ethTx := mockDynamicFeeTransaction(overflowed.Exp(maxInt64, big.NewInt(5), nil))
	_, err := NewDynamicFeeTx(ethTx)
	require.NotNil(t, err)
}

func TestValidateDynamicFeeTransaction(t *testing.T) {
	ethTx := mockDynamicFeeTransaction(big.NewInt(20))
	tx, err := NewDynamicFeeTx(ethTx)
	require.Nil(t, err)
	gtc := tx.GasTipCap
	tx.GasTipCap = nil
	require.NotNil(t, tx.Validate())
	ngtc := gtc.Neg()
	tx.GasTipCap = &ngtc
	require.NotNil(t, tx.Validate())
	tx.GasTipCap = gtc
	gfc := tx.GasFeeCap
	tx.GasFeeCap = nil
	require.NotNil(t, tx.Validate())
	ngfc := gfc.Neg()
	tx.GasFeeCap = &ngfc
	require.NotNil(t, tx.Validate())
	sgfc := tx.GasTipCap.Sub(sdk.OneInt())
	tx.GasFeeCap = &sgfc
	require.NotNil(t, tx.Validate())
	tx.GasFeeCap = gfc
	amt := tx.Amount
	namt := amt.Neg()
	tx.Amount = &namt
	require.NotNil(t, tx.Validate())
	tx.Amount = amt
	overflowed := &big.Int{}
	sdkOverflowed := sdk.NewIntFromBigInt(overflowed.Exp(big.NewInt(math.MaxInt64), big.NewInt(4), nil))
	tx.GasFeeCap = &sdkOverflowed
	require.NotNil(t, tx.Validate())
	tx.GasFeeCap = gfc
	to := tx.To
	tx.To = "xyz"
	require.NotNil(t, tx.Validate())
	tx.To = to
	chainID := tx.ChainID
	tx.ChainID = nil
	require.NotNil(t, tx.Validate())
	tx.ChainID = chainID
}
