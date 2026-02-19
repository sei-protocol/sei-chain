package auth_test

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	abcitypes "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
)

func TestItCreatesModuleAccountOnInitBlock(t *testing.T) {
	app := app.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, sdk.Header{})

	app.InitChain(
		context.Background(), &abcitypes.RequestInitChain{
			AppStateBytes: []byte("{}"),
			ChainId:       "test-chain-id",
		},
	)

	acc := app.AccountKeeper.GetAccount(ctx, types.NewModuleAddress(types.FeeCollectorName))
	require.NotNil(t, acc)
}
