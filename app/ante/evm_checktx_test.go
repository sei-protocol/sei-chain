package ante

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

type evmStatelessCheckTx struct {
	msgs []sdk.Msg
}

func (tx evmStatelessCheckTx) GetMsgs() []sdk.Msg {
	return tx.msgs
}

func (tx evmStatelessCheckTx) ValidateBasic() error {
	return nil
}

func (tx evmStatelessCheckTx) GetGasEstimate() uint64 {
	return 0
}

func TestEvmStatelessChecksRejectsEmptySetCodeAuthList(t *testing.T) {
	chainID := sdk.NewInt(1)
	gasTipCap := sdk.NewInt(50)
	gasFeeCap := sdk.NewInt(100)
	amount := sdk.NewInt(20)
	setCodeTx := &ethtx.SetCodeTx{
		ChainID:   &chainID,
		Nonce:     1,
		GasTipCap: &gasTipCap,
		GasFeeCap: &gasFeeCap,
		GasLimit:  1000,
		To:        common.Address{'a'}.Hex(),
		Amount:    &amount,
		AuthList:  ethtx.AuthList{},
		V:         []byte{3},
		R:         []byte{5},
		S:         []byte{7},
	}
	msg, err := evmtypes.NewMsgEVMTransaction(setCodeTx)
	require.NoError(t, err)

	err = EvmStatelessChecks(sdk.Context{}, evmStatelessCheckTx{msgs: []sdk.Msg{msg}}, big.NewInt(1))
	require.ErrorContains(t, err, "auth list cannot be empty")
}

func legacyTransferForStatelessChecks(t *testing.T, gas uint64, data []byte) evmStatelessCheckTx {
	t.Helper()
	gasPrice := sdk.NewInt(1)
	amount := sdk.NewInt(0)
	legacyTx := &ethtx.LegacyTx{
		GasPrice: &gasPrice,
		GasLimit: gas,
		To:       common.Address{'a'}.Hex(),
		Amount:   &amount,
		Data:     data,
		V:        []byte{27},
		R:        []byte{5},
		S:        []byte{7},
	}
	msg, err := evmtypes.NewMsgEVMTransaction(legacyTx)
	require.NoError(t, err)
	return evmStatelessCheckTx{msgs: []sdk.Msg{msg}}
}

func TestEvmStatelessChecksIntrinsicGasTooLow(t *testing.T) {
	tx := legacyTransferForStatelessChecks(t, 1000, nil)
	err := EvmStatelessChecks(sdk.Context{}.WithIsCheckTx(true), tx, big.NewInt(1))
	require.ErrorIs(t, err, core.ErrIntrinsicGas)
	require.Equal(t, "intrinsic gas too low: gas 1000, minimum needed 21000", err.Error())

	err = EvmStatelessChecks(sdk.Context{}, tx, big.NewInt(1))
	require.Equal(t, core.ErrIntrinsicGas, err)
}

func TestEvmStatelessChecksFloorDataGasCheckTxOnly(t *testing.T) {
	data := bytes.Repeat([]byte{1}, 1000)
	tx := legacyTransferForStatelessChecks(t, 40000, data)

	err := EvmStatelessChecks(sdk.Context{}.WithIsCheckTx(true), tx, big.NewInt(1))
	require.ErrorIs(t, err, core.ErrFloorDataGas)
	require.Equal(t, "insufficient gas for floor data gas cost: gas 40000, minimum needed 61000", err.Error())

	err = EvmStatelessChecks(sdk.Context{}.WithIsReCheckTx(true), tx, big.NewInt(1))
	require.ErrorIs(t, err, core.ErrFloorDataGas)

	err = EvmStatelessChecks(sdk.Context{}, tx, big.NewInt(1))
	require.NoError(t, err)

	atFloor := legacyTransferForStatelessChecks(t, 61000, data)
	err = EvmStatelessChecks(sdk.Context{}.WithIsCheckTx(true), atFloor, big.NewInt(1))
	require.NoError(t, err)
}
