package keeper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
)

func TestDeprecatedMessages(t *testing.T) {
	response, err := DeprecatedMsgServer{}.Transfer(context.Background(), &types.MsgTransfer{})

	require.Nil(t, response)
	require.ErrorIs(t, err, types.ErrTransferDeprecated)
}
