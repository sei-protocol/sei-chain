package wasmbinding

import (
	"encoding/json"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	acl "github.com/cosmos/cosmos-sdk/x/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	"github.com/sei-protocol/sei-chain/utils"
)

const (
	OracleRoute       = "oracle"
	DexRoute          = "dex"
	EpochRoute        = "epoch"
	TokenFactoryRoute = "tokenfactory"
)

type SeiQueryWrapper struct {
	// specifies which module handler should handle the query
	Route string `json:"route,omitempty"`
	// The query data that should be parsed into the module query
	QueryData json.RawMessage `json:"query_data,omitempty"`
}

func CustomQuerier(qp *QueryPlugin) func(ctx sdk.Context, request json.RawMessage) ([]byte, error) {
	return func(ctx sdk.Context, request json.RawMessage) ([]byte, error) {
		var contractQuery SeiQueryWrapper
		if err := json.Unmarshal(request, &contractQuery); err != nil {
			return nil, sdkerrors.Wrap(err, "Error parsing request data")
		}
		switch contractQuery.Route {
		case OracleRoute:
			return qp.HandleOracleQuery(ctx, contractQuery.QueryData)
		case DexRoute:
			return qp.HandleDexQuery(ctx, contractQuery.QueryData)
		case EpochRoute:
			return qp.HandleEpochQuery(ctx, contractQuery.QueryData)
		case TokenFactoryRoute:
			return qp.HandleTokenFactoryQuery(ctx, contractQuery.QueryData)
		default:
			return nil, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Query Route"}
		}
	}
}

type CustomQueryHandler struct {
	QueryPlugins wasmkeeper.QueryPlugins
	aclKeeper    aclkeeper.Keeper
}

func (queryHandler CustomQueryHandler) HandleQuery(ctx sdk.Context, caller sdk.AccAddress, request wasmvmtypes.QueryRequest) ([]byte, error) {
	// TODO: we need to carry wasmDependency in ctx instead of loading again here since here has no access to original msg payload
	//       which is required for populating id correctly.
	wasmDependency, err := queryHandler.aclKeeper.GetWasmDependencyMapping(ctx, caller, []byte{}, false)
	// If no mapping exists, or mapping is disabled, this message would behave as blocking for all resources
	needToCheckDependencies := true
	if err == aclkeeper.ErrWasmDependencyMappingNotFound {
		// no mapping, we can just continue
		needToCheckDependencies = false
	}
	if err != nil {
		return nil, err
	}
	if !wasmDependency.Enabled {
		needToCheckDependencies = false
	}
	lookupMap := BuildWasmDependencyLookupMap(
		utils.Map(wasmDependency.AccessOps, func(op accesscontrol.AccessOperationWithSelector) accesscontrol.AccessOperation { return *op.Operation }),
	)
	if request.Bank != nil {
		// check for BANK resource type
		accessOp := accesscontrol.AccessOperation{
			ResourceType: accesscontrol.ResourceType_KV_BANK,
			AccessType:   accesscontrol.AccessType_READ,
			// TODO: should IdentifierTemplate be based on the actual request?
			IdentifierTemplate: "*",
		}
		if needToCheckDependencies {
			if !AreDependenciesFulfilled(lookupMap, accessOp) {
				emitIncorrectDependencyWasmEvent(ctx, caller.String())
				return nil, acl.ErrUnexpectedWasmDependency
			}
		}
		return queryHandler.QueryPlugins.Bank(ctx, request.Bank)

	}
	if request.Custom != nil {
		// TODO: specially break down the custom
		var contractQuery SeiQueryWrapper
		if err := json.Unmarshal(request.Custom, &contractQuery); err != nil {
			return nil, sdkerrors.Wrap(err, "Error parsing request data")
		}
		resourceType := accesscontrol.ResourceType_ANY
		switch contractQuery.Route {
		case OracleRoute:
			resourceType = accesscontrol.ResourceType_KV_ORACLE
		case DexRoute:
			resourceType = accesscontrol.ResourceType_KV_DEX
		case EpochRoute:
			resourceType = accesscontrol.ResourceType_KV_EPOCH
		case TokenFactoryRoute:
			resourceType = accesscontrol.ResourceType_KV // TODO: change this to tokenfactory when rebasing a newer sei cosmos version with the enum
		}
		accessOp := accesscontrol.AccessOperation{
			ResourceType:       resourceType,
			AccessType:         accesscontrol.AccessType_READ,
			IdentifierTemplate: "*",
		}
		if needToCheckDependencies {
			if !AreDependenciesFulfilled(lookupMap, accessOp) {
				emitIncorrectDependencyWasmEvent(ctx, caller.String())
				return nil, acl.ErrUnexpectedWasmDependency
			}
		}
		return queryHandler.QueryPlugins.Custom(ctx, request.Custom)
	}
	if request.IBC != nil {
		// check for ANY resource type
		// TODO: do we need a special resource type for IBC?
		accessOp := accesscontrol.AccessOperation{
			ResourceType:       accesscontrol.ResourceType_ANY,
			AccessType:         accesscontrol.AccessType_READ,
			IdentifierTemplate: "*",
		}
		if needToCheckDependencies {
			if !AreDependenciesFulfilled(lookupMap, accessOp) {
				emitIncorrectDependencyWasmEvent(ctx, caller.String())
				return nil, acl.ErrUnexpectedWasmDependency
			}
		}
		return queryHandler.QueryPlugins.IBC(ctx, caller, request.IBC)
	}
	if request.Staking != nil {
		// check for STAKING resource type
		accessOp := accesscontrol.AccessOperation{
			ResourceType:       accesscontrol.ResourceType_KV_STAKING,
			AccessType:         accesscontrol.AccessType_READ,
			IdentifierTemplate: "*",
		}
		if needToCheckDependencies {
			if !AreDependenciesFulfilled(lookupMap, accessOp) {
				emitIncorrectDependencyWasmEvent(ctx, caller.String())
				return nil, acl.ErrUnexpectedWasmDependency
			}
		}
		return queryHandler.QueryPlugins.Staking(ctx, request.Staking)
	}
	if request.Stargate != nil {
		// check for ANY resource type
		// TODO: determine what Stargate dependency granularity looks like
		accessOp := accesscontrol.AccessOperation{
			ResourceType:       accesscontrol.ResourceType_ANY,
			AccessType:         accesscontrol.AccessType_READ,
			IdentifierTemplate: "*",
		}
		if needToCheckDependencies {
			if !AreDependenciesFulfilled(lookupMap, accessOp) {
				emitIncorrectDependencyWasmEvent(ctx, caller.String())
				return nil, acl.ErrUnexpectedWasmDependency
			}
		}
		return queryHandler.QueryPlugins.Stargate(ctx, request.Stargate)
	}
	if request.Wasm != nil {
		// check for WASM resource type
		accessOp := accesscontrol.AccessOperation{
			ResourceType:       accesscontrol.ResourceType_KV_WASM,
			AccessType:         accesscontrol.AccessType_READ,
			IdentifierTemplate: "*",
		}
		if needToCheckDependencies {
			if !AreDependenciesFulfilled(lookupMap, accessOp) {
				emitIncorrectDependencyWasmEvent(ctx, caller.String())
				return nil, acl.ErrUnexpectedWasmDependency
			}
		}
		return queryHandler.QueryPlugins.Wasm(ctx, request.Wasm)
	}
	return nil, wasmvmtypes.Unknown{}
}

func NewCustomQueryHandler(queryPlugins wasmkeeper.QueryPlugins, aclKeeper aclkeeper.Keeper) wasmkeeper.WasmVMQueryHandler {
	return CustomQueryHandler{
		QueryPlugins: queryPlugins,
		aclKeeper:    aclKeeper,
	}
}

func CustomQueryHandlerDecorator(aclKeeper aclkeeper.Keeper, customQueryPlugin QueryPlugin) func(wasmkeeper.WasmVMQueryHandler) wasmkeeper.WasmVMQueryHandler {
	// validate stuff, otherwise use default handler
	return func(old wasmkeeper.WasmVMQueryHandler) wasmkeeper.WasmVMQueryHandler {
		queryPlugins, ok := old.(wasmkeeper.QueryPlugins)
		if !ok {
			panic("Invalid query plugins")
		}

		queryPlugins = queryPlugins.Merge(&wasmkeeper.QueryPlugins{
			Custom: CustomQuerier(&customQueryPlugin),
		})
		return NewCustomQueryHandler(queryPlugins, aclKeeper)
	}
}
