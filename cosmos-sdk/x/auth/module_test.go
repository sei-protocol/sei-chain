package auth_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	abcitypes "github.com/sei-protocol/sei-chain/tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/simapp"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/x/auth/types"
)

func TestItCreatesModuleAccountOnInitBlock(t *testing.T) {
	app := simapp.Setup(false)
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
