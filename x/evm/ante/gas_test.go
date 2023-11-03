package ante_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestGasLimitDecorator(t *testing.T) {
	k, _, ctx := testkeeper.MockEVMKeeper()
	a := ante.NewGasLimitDecorator(k)
	ctx, err := a.AnteHandle(ctx, nil, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)
	ctx = types.SetContextTxData(ctx, &ethtx.LegacyTx{GasLimit: 100})
	ctx, err = a.AnteHandle(ctx, nil, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	require.Equal(t, 100, int(ctx.GasMeter().Limit()))
}
