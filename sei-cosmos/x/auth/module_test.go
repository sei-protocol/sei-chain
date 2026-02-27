package auth_test

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	abcitypes "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

func TestItCreatesModuleAccountOnInitBlock(t *testing.T) {
	app := app.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	app.InitChain(
		context.Background(), &abcitypes.RequestInitChain{
			AppStateBytes: []byte("{}"),
			ChainId:       "test-chain-id",
		},
	)

	acc := app.AccountKeeper.GetAccount(ctx, types.NewModuleAddress(types.FeeCollectorName))
	require.NotNil(t, acc)
}
