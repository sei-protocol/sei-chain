package ante_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/ante"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestGasLimitDecorator(t *testing.T) {
	k := &testkeeper.EVMTestApp.GigaEvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	a := ante.NewGasDecorator(k)
	limitMsg, _ := types.NewMsgEVMTransaction(&ethtx.LegacyTx{GasLimit: 100})
	ctx, err := a.AnteHandle(ctx, &mockTx{msgs: []sdk.Msg{limitMsg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	require.Equal(t, 100, int(ctx.GasMeter().Limit()))
}
