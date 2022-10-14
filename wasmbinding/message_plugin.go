package wasmbinding

import (
	"fmt"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

var ErrUnexpectedWasmDependency = fmt.Errorf("unexpected wasm dependency detected")

// forked from wasm
func CustomMessageHandler(
	router wasmkeeper.MessageRouter,
	channelKeeper wasmtypes.ChannelKeeper,
	capabilityKeeper wasmtypes.CapabilityKeeper,
	bankKeeper wasmtypes.Burner,
	unpacker codectypes.AnyUnpacker,
	portSource wasmtypes.ICS20TransferPortSource,
	aclKeeper aclkeeper.Keeper,
) wasmkeeper.Messenger {
	encoders := wasmkeeper.DefaultEncoders(unpacker, portSource)
	encoders = encoders.Merge(
		&wasmkeeper.MessageEncoders{
			Custom: CustomEncoder,
		})
	return wasmkeeper.NewMessageHandlerChain(
		NewSDKMessageDependencyDecorator(wasmkeeper.NewSDKMessageHandler(router, encoders), aclKeeper, encoders),
		wasmkeeper.NewIBCRawPacketHandler(channelKeeper, capabilityKeeper),
		wasmkeeper.NewBurnCoinMessageHandler(bankKeeper),
	)
}

// SDKMessageHandler can handles messages that can be encoded into sdk.Message types and routed.
type SDKMessageDependencyDecorator struct {
	wrapped   wasmkeeper.Messenger
	aclKeeper aclkeeper.Keeper
	encoders  wasmkeeper.MessageEncoders
}

func NewSDKMessageDependencyDecorator(handler wasmkeeper.Messenger, aclKeeper aclkeeper.Keeper, encoders wasmkeeper.MessageEncoders) SDKMessageDependencyDecorator {
	return SDKMessageDependencyDecorator{
		wrapped:   handler,
		aclKeeper: aclKeeper,
		encoders:  encoders,
	}
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

func (decorator SDKMessageDependencyDecorator) DispatchMsg(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg) (events []sdk.Event, data [][]byte, err error) {
	sdkMsgs, err := decorator.encoders.Encode(ctx, contractAddr, contractIBCPortID, msg)
	if err != nil {
		return nil, nil, err
	}
	// get the dependencies for the contract to validate against
	wasmDependency, err := decorator.aclKeeper.GetWasmDependencyMapping(ctx, contractAddr)
	// If no mapping exists, or mapping is disabled, this message would behave as blocking for all resources
	if err == aclkeeper.ErrWasmDependencyMappingNotFound {
		// no mapping, we can just continue
		return decorator.wrapped.DispatchMsg(ctx, contractAddr, contractIBCPortID, msg)
	}
	if err != nil {
		return nil, nil, err
	}
	if !wasmDependency.Enabled {
		// if not enabled, just move on
		// TODO: confirm that this is ok, is there ever a case where we should still verify dependencies for a disabled dependency? IDTS
		return decorator.wrapped.DispatchMsg(ctx, contractAddr, contractIBCPortID, msg)
	}
	// convert wasm dependency to a map of resource access and identifier we can look up in
	lookupMap := BuildWasmDependencyLookupMap(wasmDependency.AccessOps)
	// wasm dependency enabled, we need to validate the message dependencies
	for _, msg := range sdkMsgs {
		accessOps := decorator.aclKeeper.GetMessageDependencies(ctx, msg)
		// go through each access op, and check if there is a completion signal for it OR a parent
		for _, accessOp := range accessOps {
			// first check for our specific resource access AND identifier template
			depsFulfilled := AreDependenciesFulfilled(lookupMap, accessOp)
			if !depsFulfilled {
				return nil, nil, ErrUnexpectedWasmDependency
			}
		}
	}
	// we've gone through all of the messages
	// and verified their dependencies with the declared dependencies in the wasm contract dependencies, we can process it now
	return decorator.wrapped.DispatchMsg(ctx, contractAddr, contractIBCPortID, msg)
}
