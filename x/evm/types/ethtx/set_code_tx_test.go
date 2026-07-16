package ethtx

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

func mockSetCodeTx(authList AuthList) *SetCodeTx {
	chainID := sdk.NewInt(1)
	gasTipCap := sdk.NewInt(50)
	gasFeeCap := sdk.NewInt(100)
	amount := sdk.NewInt(20)
	ethAccessList := mockAccessList()

	return &SetCodeTx{
		ChainID:   &chainID,
		Nonce:     1,
		GasTipCap: &gasTipCap,
		GasFeeCap: &gasFeeCap,
		GasLimit:  1000,
		To:        common.Address{'a'}.Hex(),
		Amount:    &amount,
		Data:      []byte{'b'},
		Accesses:  NewAccessList(&ethAccessList),
		AuthList:  authList,
		V:         []byte{3},
		R:         []byte{5},
		S:         []byte{7},
	}
}

func mockAuthList() AuthList {
	chainID := sdk.NewInt(1)
	return AuthList{
		{
			ChainID: &chainID,
			Address: common.Address{'c'}.Hex(),
			Nonce:   1,
			V:       []byte{0},
			R:       []byte{5},
			S:       []byte{7},
		},
	}
}

func TestSetCodeTransaction(t *testing.T) {
	tx := mockSetCodeTx(mockAuthList())
	require.NoError(t, tx.Validate())
	require.Equal(t, uint8(ethtypes.SetCodeTxType), tx.TxType())
	require.Equal(t, tx, tx.Copy())
	require.Equal(t, tx.GetAuthList(), ethtypes.NewTx(tx.AsEthereumData()).SetCodeAuthorizations())
	require.Nil(t, tx.GetBlobFeeCap())
	require.Nil(t, tx.GetBlobHashes())

	baseFee := big.NewInt(2)
	require.Equal(t, EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt()), tx.EffectiveGasPrice(baseFee))
	require.Equal(t, fee(EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt()), tx.GasLimit), tx.EffectiveFee(baseFee))
	require.Equal(t, cost(fee(EffectiveGasPrice(baseFee, tx.GasFeeCap.BigInt(), tx.GasTipCap.BigInt()), tx.GasLimit), tx.Amount.BigInt()), tx.EffectiveCost(baseFee))
}

func TestValidateSetCodeTransactionRejectsEmptyAuthList(t *testing.T) {
	tx := mockSetCodeTx(nil)
	require.ErrorContains(t, tx.Validate(), "auth list cannot be empty")

	tx.AuthList = AuthList{}
	require.ErrorContains(t, tx.Validate(), "auth list cannot be empty")

	tx.AuthList = mockAuthList()
	require.NoError(t, tx.Validate())
}
