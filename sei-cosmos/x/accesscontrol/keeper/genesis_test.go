package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestKeeper_InitAndExportGenesis(t *testing.T) {
	a := app.Setup(t, false, false, false)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})
	addresses := app.AddTestAddrsIncremental(a, ctx, 2, sdk.NewInt(30000000))

	testGenesis := types.GenesisState{
		Params: types.DefaultParams(),
		MessageDependencyMapping: []accesscontrol.MessageDependencyMapping{
			types.SynchronousMessageDependencyMapping("Test"),
		},
		WasmDependencyMappings: []accesscontrol.WasmDependencyMapping{
			types.SynchronousWasmDependencyMapping(addresses[0].String()),
		},
	}

	a.AccessControlKeeper.InitGenesis(ctx, testGenesis)

	exportedGenesis := a.AccessControlKeeper.ExportGenesis(ctx)
	require.Equal(t, len(testGenesis.MessageDependencyMapping), len(exportedGenesis.MessageDependencyMapping))
	require.Equal(t, len(testGenesis.WasmDependencyMappings), len(exportedGenesis.WasmDependencyMappings))
	require.Equal(t, &testGenesis, exportedGenesis)
}

func TestKeeper_InitGenesis_EmptyGenesis(t *testing.T) {
	a := app.Setup(t, false, false, false)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})
	testGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	a.AccessControlKeeper.InitGenesis(ctx, testGenesis)
	exportedGenesis := a.AccessControlKeeper.ExportGenesis(ctx)
	require.Equal(t, 0, len(exportedGenesis.MessageDependencyMapping))
	require.Equal(t, 0, len(exportedGenesis.WasmDependencyMappings))
}

func TestKeeper_InitGenesis_MultipleDependencies(t *testing.T) {
	a := app.Setup(t, false, false, false)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})
	addresses := app.AddTestAddrsIncremental(a, ctx, 3, sdk.NewInt(30000000))

	testGenesis := types.GenesisState{
		Params: types.DefaultParams(),
		MessageDependencyMapping: []accesscontrol.MessageDependencyMapping{
			types.SynchronousMessageDependencyMapping("Test1"),
			types.SynchronousMessageDependencyMapping("Test2"),
		},
		WasmDependencyMappings: []accesscontrol.WasmDependencyMapping{
			types.SynchronousWasmDependencyMapping(addresses[0].String()),
			types.SynchronousWasmDependencyMapping(addresses[1].String()),
		},
	}
	a.AccessControlKeeper.InitGenesis(ctx, testGenesis)
	exportedGenesis := a.AccessControlKeeper.ExportGenesis(ctx)
	require.Equal(t, 2, len(exportedGenesis.MessageDependencyMapping))
	require.Equal(t, 2, len(exportedGenesis.WasmDependencyMappings))
}

func TestKeeper_InitGenesis_InvalidDependencies(t *testing.T) {
	a := app.Setup(t, false, false, false)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})

	invalidAccessOp := types.SynchronousMessageDependencyMapping("Test1")
	invalidAccessOp.AccessOps[0].IdentifierTemplate = ""
	invalidAccessOp.AccessOps = []accesscontrol.AccessOperation{
		invalidAccessOp.AccessOps[0],
	}

	invalidMessageGenesis := types.GenesisState{
		Params: types.DefaultParams(),
		MessageDependencyMapping: []accesscontrol.MessageDependencyMapping{
			invalidAccessOp,
		},
	}

	require.Panics(t, func() {
		a.AccessControlKeeper.InitGenesis(ctx, invalidMessageGenesis)
	})

	invalidWasmGenesis := types.GenesisState{
		Params: types.DefaultParams(),
		WasmDependencyMappings: []accesscontrol.WasmDependencyMapping{
			types.SynchronousWasmDependencyMapping("Test"),
		},
	}
	require.Panics(t, func() {
		a.AccessControlKeeper.InitGenesis(ctx, invalidWasmGenesis)
	})

}
