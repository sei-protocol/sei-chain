package types_test

import (
	"testing"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/stretchr/testify/require"
)

func TestWasmDependencyDeprecatedSelectors(t *testing.T) {
	wasmDependencyMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_CONTRACT_REFERENCE,
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
	}
	require.Error(t, types.ValidateWasmDependencyMapping(wasmDependencyMapping))

	wasmDependencyMapping = acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ExecuteAccessOps: []*acltypes.WasmAccessOperations{
			{
				MessageName: "message_name",
				WasmOperations: []*acltypes.WasmAccessOperation{
					{
						Operation:    types.CommitAccessOp(),
						SelectorType: acltypes.AccessOperationSelectorType_CONTRACT_REFERENCE,
					},
				},
			},
		},
	}
	require.Error(t, types.ValidateWasmDependencyMapping(wasmDependencyMapping))

	wasmDependencyMapping = acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		QueryAccessOps: []*acltypes.WasmAccessOperations{
			{
				MessageName: "message_name",
				WasmOperations: []*acltypes.WasmAccessOperation{
					{
						Operation:    types.CommitAccessOp(),
						SelectorType: acltypes.AccessOperationSelectorType_CONTRACT_REFERENCE,
					},
				},
			},
		},
	}
	require.Error(t, types.ValidateWasmDependencyMapping(wasmDependencyMapping))
}

func TestWasmDependencyDuplicateMessageNameInContractReference(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmDependencyMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ExecuteContractReferences: []*acltypes.WasmContractReferences{
			{
				MessageName: "some_message",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress: wasmContractAddress.String(),
						MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
						MessageName:     "some_message",
					},
				},
			},
			{
				MessageName: "some_message",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress: wasmContractAddress.String(),
						MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
						MessageName:     "some_message",
					},
				},
			},
		},
	}
	require.Error(t, types.ValidateWasmDependencyMapping(wasmDependencyMapping))

	wasmDependencyMapping = acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		QueryContractReferences: []*acltypes.WasmContractReferences{
			{
				MessageName: "some_message",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress: wasmContractAddress.String(),
						MessageType:     acltypes.WasmMessageSubtype_QUERY,
						MessageName:     "some_message",
					},
				},
			},
			{
				MessageName: "some_message",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress: wasmContractAddress.String(),
						MessageType:     acltypes.WasmMessageSubtype_QUERY,
						MessageName:     "some_message",
					},
				},
			},
		},
	}
	require.Error(t, types.ValidateWasmDependencyMapping(wasmDependencyMapping))

	// duplicate message names in different section (query and execute partitions) shouldnt error
	wasmDependencyMapping = acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		QueryContractReferences: []*acltypes.WasmContractReferences{
			{
				MessageName: "some_message",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress: wasmContractAddress.String(),
						MessageType:     acltypes.WasmMessageSubtype_QUERY,
						MessageName:     "some_message",
					},
				},
			},
		},
		ExecuteContractReferences: []*acltypes.WasmContractReferences{
			{
				MessageName: "some_message",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress: wasmContractAddress.String(),
						MessageType:     acltypes.WasmMessageSubtype_QUERY,
						MessageName:     "some_message",
					},
				},
			},
		},
	}
	require.NoError(t, types.ValidateWasmDependencyMapping(wasmDependencyMapping))
}
