package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestMsgClaim(t *testing.T) {
	sender := sdk.AccAddress("acc_________________")
	claimer := common.HexToAddress("0x0123456789abcdef012345abcdef12345678")
	msg := types.NewMsgClaim(sender, claimer)
	require.Equal(t, "evm", msg.Route())
	require.Equal(t, "evm_claim", msg.Type())
	require.Len(t, msg.GetSigners(), 1)
	msg.Sender = "bad"
	require.Error(t, msg.ValidateBasic())
	require.Panics(t, func() { msg.GetSigners() })
	msg.Sender = sender.String()
	require.NotEmpty(t, msg.GetSignBytes())
	require.NoError(t, msg.ValidateBasic())
}
