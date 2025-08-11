package distribution_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	abcitypes "github.com/sei-protocol/sei-chain/tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/simapp"
	authtypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/auth/types"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/x/distribution/types"
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

	acc := app.AccountKeeper.GetAccount(ctx, authtypes.NewModuleAddress(types.ModuleName))
	require.NotNil(t, acc)
}
