package ante

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	channeltypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
	coretypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
)

type testTx struct {
	msgs []sdk.Msg
}

func (tx testTx) GetMsgs() []sdk.Msg  { return tx.msgs }
func (testTx) ValidateBasic() error   { return nil }
func (testTx) GetGasEstimate() uint64 { return 0 }

func TestAnteDecorator(t *testing.T) {
	decorator := NewAnteDecorator()

	t.Run("rejects IBC messages", func(t *testing.T) {
		nextCalled := false
		_, err := decorator.AnteHandle(sdk.Context{}, testTx{msgs: []sdk.Msg{
			&channeltypes.MsgChannelCloseInit{},
		}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextCalled = true
			return ctx, nil
		})

		require.ErrorIs(t, err, coretypes.ErrIBCDeprecated)
		require.False(t, nextCalled)
	})

	t.Run("passes non-IBC messages", func(t *testing.T) {
		nextCalled := false
		_, err := decorator.AnteHandle(sdk.Context{}, testTx{msgs: []sdk.Msg{
			&banktypes.MsgSend{},
		}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextCalled = true
			return ctx, nil
		})

		require.NoError(t, err)
		require.True(t, nextCalled)
	})
}
