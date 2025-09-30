package migrations_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/migrations"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestV1ToV2(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 2, sdk.NewInt(30000000))
	wasmContractAddress1 := wasmContractAddresses[0]
	wasmContractAddress2 := wasmContractAddresses[1]

	// populate legacy mappings
	store := ctx.KVStore(app.AccessControlKeeper.GetStoreKey())
	legacyMapping1 := acltypes.LegacyWasmDependencyMapping{
		AccessOps: []acltypes.LegacyAccessOperationWithSelector{
			{
				Operation: &acltypes.AccessOperation{
					AccessType:         acltypes.AccessType_READ,
					ResourceType:       acltypes.ResourceType_KV,
					IdentifierTemplate: "*",
				},
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
				Selector:     "",
			},
			{
				Operation: &acltypes.AccessOperation{
					AccessType:         acltypes.AccessType_WRITE,
					ResourceType:       acltypes.ResourceType_KV_AUTH,
					IdentifierTemplate: "acct%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ,
				Selector:     ".send.to",
			},
		},
		ContractAddress: wasmContractAddress1.String(),
		ResetReason:     "",
	}
	legacyMapping2 := acltypes.LegacyWasmDependencyMapping{
		AccessOps: []acltypes.LegacyAccessOperationWithSelector{
			{
				Operation: &acltypes.AccessOperation{
					AccessType:         acltypes.AccessType_COMMIT,
					ResourceType:       acltypes.ResourceType_ANY,
					IdentifierTemplate: "*",
				},
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
				Selector:     "",
			},
		},
		ContractAddress: wasmContractAddress2.String(),
		ResetReason:     "bad",
	}
	legacyBz1, _ := legacyMapping1.Marshal()
	legacyBz2, _ := legacyMapping2.Marshal()
	store.Set(types.GetWasmContractAddressKey(wasmContractAddress1), legacyBz1)
	store.Set(types.GetWasmContractAddressKey(wasmContractAddress2), legacyBz2)

	// run migration
	err := migrations.V1ToV2(ctx, app.AccessControlKeeper.GetStoreKey())
	require.Nil(t, err)

	// verify new format is set
	newBz1 := store.Get(types.GetWasmContractAddressKey(wasmContractAddress1))
	newMapping1 := acltypes.WasmDependencyMapping{}
	err = newMapping1.Unmarshal(newBz1)
	require.Nil(t, err)
	require.Equal(t, 2, len(newMapping1.BaseAccessOps))
	require.Equal(t, acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_READ,
		ResourceType:       acltypes.ResourceType_KV,
		IdentifierTemplate: "*",
	}, *newMapping1.BaseAccessOps[0].Operation)
	require.Equal(t, acltypes.AccessOperationSelectorType_NONE, newMapping1.BaseAccessOps[0].SelectorType)
	require.Equal(t, "", newMapping1.BaseAccessOps[0].Selector)
	require.Equal(t, acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_WRITE,
		ResourceType:       acltypes.ResourceType_KV_AUTH,
		IdentifierTemplate: "acct%s",
	}, *newMapping1.BaseAccessOps[1].Operation)
	require.Equal(t, acltypes.AccessOperationSelectorType_JQ, newMapping1.BaseAccessOps[1].SelectorType)
	require.Equal(t, ".send.to", newMapping1.BaseAccessOps[1].Selector)
	require.Equal(t, wasmContractAddress1.String(), newMapping1.ContractAddress)
	require.Equal(t, "", newMapping1.ResetReason)
	require.Equal(t, 0, len(newMapping1.ExecuteAccessOps))
	require.Equal(t, 0, len(newMapping1.QueryAccessOps))
	require.Equal(t, 0, len(newMapping1.BaseContractReferences))
	require.Equal(t, 0, len(newMapping1.ExecuteContractReferences))
	require.Equal(t, 0, len(newMapping1.QueryContractReferences))

	newBz2 := store.Get(types.GetWasmContractAddressKey(wasmContractAddress2))
	newMapping2 := acltypes.WasmDependencyMapping{}
	err = newMapping2.Unmarshal(newBz2)
	require.Nil(t, err)
	require.Equal(t, 1, len(newMapping2.BaseAccessOps))
	require.Equal(t, acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_COMMIT,
		ResourceType:       acltypes.ResourceType_ANY,
		IdentifierTemplate: "*",
	}, *newMapping2.BaseAccessOps[0].Operation)
	require.Equal(t, acltypes.AccessOperationSelectorType_NONE, newMapping2.BaseAccessOps[0].SelectorType)
	require.Equal(t, "", newMapping2.BaseAccessOps[0].Selector)
	require.Equal(t, wasmContractAddress2.String(), newMapping2.ContractAddress)
	require.Equal(t, "bad", newMapping2.ResetReason)
	require.Equal(t, 0, len(newMapping2.ExecuteAccessOps))
	require.Equal(t, 0, len(newMapping2.QueryAccessOps))
	require.Equal(t, 0, len(newMapping2.BaseContractReferences))
	require.Equal(t, 0, len(newMapping2.ExecuteContractReferences))
	require.Equal(t, 0, len(newMapping2.QueryContractReferences))
}
