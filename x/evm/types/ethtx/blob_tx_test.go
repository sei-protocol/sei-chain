package ethtx

import (
	"math"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func mockBlobTransaction(value *uint256.Int) *ethtypes.Transaction {
	inner := &ethtypes.BlobTx{
		ChainID:    uint256.NewInt(1),
		GasTipCap:  uint256.NewInt(50),
		GasFeeCap:  uint256.NewInt(100),
		Gas:        1000,
		To:         common.Address{'a'},
		Value:      value,
		Data:       []byte{'b'},
		AccessList: mockAccessList(),
		BlobFeeCap: uint256.NewInt(30),
		BlobHashes: []common.Hash{{'c', 'd'}},
		Sidecar: &ethtypes.BlobTxSidecar{
			Blobs:       []kzg4844.Blob{{'e', 'f'}},
			Commitments: []kzg4844.Commitment{{'g', 'h'}},
			Proofs:      []kzg4844.Proof{{'i', 'j'}},
		},
		V: uint256.NewInt(3),
		R: uint256.NewInt(5),
		S: uint256.NewInt(7),
	}
	return ethtypes.NewTx(inner)
}

func TestBlobTransaction(t *testing.T) {
	ethTx := mockBlobTransaction(uint256.NewInt(20))
	tx, err := NewBlobTx(ethTx)
	require.Nil(t, err)
	require.Nil(t, tx.Validate())

	require.Equal(t, uint8(ethtypes.BlobTxType), tx.TxType())
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
	require.Equal(t, new(big.Int).Add(fee(tx.GasFeeCap.BigInt(), tx.GasLimit), fee(tx.BlobFeeCap.BigInt(), tx.BlobGas())), tx.Fee())
	require.Equal(t, cost(new(big.Int).Add(fee(tx.GasFeeCap.BigInt(), tx.GasLimit), fee(tx.BlobFeeCap.BigInt(), tx.BlobGas())), tx.Amount.BigInt()), tx.Cost())
	baseFee := big.NewInt(2)
	require.Equal(t, EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt()), tx.EffectiveGasPrice(baseFee))
	require.Equal(t, new(big.Int).Add(fee(EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt()), tx.GasLimit), fee(tx.BlobFeeCap.BigInt(), tx.BlobGas())), tx.EffectiveFee(baseFee))
	require.Equal(t, cost(new(big.Int).Add(fee(EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt()), tx.GasLimit), fee(tx.BlobFeeCap.BigInt(), tx.BlobGas())), tx.Amount.BigInt()), tx.EffectiveCost(baseFee))

	require.Equal(t, big.NewInt(30), tx.GetBlobFeeCap())
	require.Equal(t, []common.Hash{{'c', 'd'}}, tx.GetBlobHashes())
}

func TestValidateBlobTransaction(t *testing.T) {
	ethTx := mockBlobTransaction(uint256.NewInt(20))
	tx, err := NewBlobTx(ethTx)
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
