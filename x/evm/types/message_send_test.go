package types_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestMessageSendValidate(t *testing.T) {
	fromAddr, err := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	require.Nil(t, err)
	msg := types.NewMsgSend(fromAddr, common.HexToAddress("to"), sdk.Coins{sdk.Coin{
		Denom:  "sei",
		Amount: sdk.NewInt(1),
	}})
	require.Nil(t, msg.ValidateBasic())

	// No coins
	msg = types.NewMsgSend(fromAddr, common.HexToAddress("to"), sdk.Coins{})
	require.Error(t, msg.ValidateBasic())

	// Negative coins
	msg = types.NewMsgSend(fromAddr, common.HexToAddress("to"), sdk.Coins{sdk.Coin{
		Denom:  "sei",
		Amount: sdk.NewInt(-1),
	}})
	require.Error(t, msg.ValidateBasic())
}
