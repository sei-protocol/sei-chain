package mint_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abcitypes "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/simapp"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/mint"
	"github.com/sei-protocol/sei-chain/x/mint/types"
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

func TestNewProposalHandler(t *testing.T) {
	app := app.Setup(false, false, false)

	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	app.MintKeeper.SetParams(ctx, types.DefaultParams())
	app.MintKeeper.SetMinter(ctx, types.DefaultInitialMinter())

	handler := mint.NewProposalHandler(app.MintKeeper)

	newMinter := types.NewMinter(
		"2023-10-05",
		"2023-11-22",
		"usei",
		12345,
	)
	updateMinterProposal := &types.UpdateMinterProposal{
		Title:       "Test Title",
		Description: "Test Description",
		Minter:      &newMinter,
	}
	err := handler(ctx, updateMinterProposal)
	require.NoError(t, err)
	updatedMinter := app.MintKeeper.GetMinter(ctx)
	require.Equal(t, newMinter, updatedMinter)

	invalidMinter := types.NewMinter(
		"2023-11-22",
		"2023-10-05",
		"test",
		12345,
	)
	invalidProposal := &types.UpdateMinterProposal{
		Title:       "Invalid Minter",
		Description: "Invalid Minter",
		Minter:      &invalidMinter,
	}
	err = handler(ctx, invalidProposal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "end date must be after start")
}
