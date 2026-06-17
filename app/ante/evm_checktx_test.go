package ante

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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
