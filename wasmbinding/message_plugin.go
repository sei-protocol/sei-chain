package wasmbinding

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type CustomRouter struct {
	wasmkeeper.MessageRouter

	evmKeeper *evmkeeper.Keeper
}

func (r *CustomRouter) Handler(msg sdk.Msg) baseapp.MsgServiceHandler {
	switch m := msg.(type) {
	case *evmtypes.MsgInternalEVMCall:
		return func(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
			return r.evmKeeper.HandleInternalEVMCall(ctx, m)
		}
	default:
		return r.MessageRouter.Handler(msg)
	}
}

// forked from wasm
func CustomMessageHandler(
	router wasmkeeper.MessageRouter,
	channelKeeper wasmtypes.ChannelKeeper,
	capabilityKeeper wasmtypes.CapabilityKeeper,
	bankKeeper wasmtypes.Burner,
	evmKeeper *evmkeeper.Keeper,
	unpacker codectypes.AnyUnpacker,
	portSource wasmtypes.ICS20TransferPortSource,
	_ aclkeeper.Keeper,
) wasmkeeper.Messenger {
	encoders := wasmkeeper.DefaultEncoders(unpacker, portSource)
	encoders = encoders.Merge(
		&wasmkeeper.MessageEncoders{
			Custom: CustomEncoder,
		})
	return wasmkeeper.NewMessageHandlerChain(
		wasmkeeper.NewSDKMessageHandler(&CustomRouter{MessageRouter: router, evmKeeper: evmKeeper}, encoders),
		wasmkeeper.NewIBCRawPacketHandler(channelKeeper, capabilityKeeper),
		wasmkeeper.NewBurnCoinMessageHandler(bankKeeper),
	)
}

func BuildWasmDependencyLookupMap(accessOps []sdkacltypes.AccessOperation) map[acltypes.ResourceAccess]map[string]struct{} {
	lookupMap := make(map[acltypes.ResourceAccess]map[string]struct{})
	for _, accessOp := range accessOps {
		resourceAccess := acltypes.ResourceAccess{
			ResourceType: accessOp.ResourceType,
			AccessType:   accessOp.AccessType,
		}
		if _, ok := lookupMap[resourceAccess]; !ok {
			// we haven't added any identifiers for this resource type, so lets initialize the nested map (set)
			lookupMap[resourceAccess] = make(map[string]struct{})
		}
		lookupMap[resourceAccess][accessOp.IdentifierTemplate] = struct{}{}
	}
	return lookupMap
}

func GenerateAllowedResourceAccess(resource sdkacltypes.ResourceType, access sdkacltypes.AccessType) []acltypes.ResourceAccess {
	// by default, write, and unknown are ok
	accesses := []acltypes.ResourceAccess{
		{
			ResourceType: resource,
			AccessType:   sdkacltypes.AccessType_WRITE,
		},
		{
			ResourceType: resource,
			AccessType:   sdkacltypes.AccessType_UNKNOWN,
		},
	}
	if access == sdkacltypes.AccessType_READ {
		accesses = append(accesses, acltypes.ResourceAccess{
			ResourceType: resource,
			AccessType:   access,
		})
	}
	return accesses
}

func AreDependenciesFulfilled(lookupMap map[acltypes.ResourceAccess]map[string]struct{}, accessOp sdkacltypes.AccessOperation) bool {
	currResourceAccesses := GenerateAllowedResourceAccess(accessOp.ResourceType, accessOp.AccessType)
	for _, currResourceAccess := range currResourceAccesses {
		if identifierMap, ok := lookupMap[currResourceAccess]; ok {
			if _, ok := identifierMap[accessOp.IdentifierTemplate]; ok {
				// we found a proper listed dependency, we can go to the next access op
				return true
			}
		}
	}

	// what about parent resources
	parentResources := accessOp.ResourceType.GetParentResources()
	// for each of the parent resources, we need at least one to be defined in the wasmDependencies
	for _, parentResource := range parentResources {
		// make parent resource access with same access type
		parentResourceAccesses := GenerateAllowedResourceAccess(parentResource, accessOp.AccessType)
		// for each of the parent resources, we check to see if its in the lookup map (identifier doesnt matter bc parent)
		for _, parentResourceAccess := range parentResourceAccesses {
			if _, parentResourcePresent := lookupMap[parentResourceAccess]; parentResourcePresent {
				// we can continue to the next access op
				return true
			}
		}
	}
	return false
}
