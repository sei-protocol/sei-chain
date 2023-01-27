package keeper

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/armon/go-metrics"
	"github.com/savaki/jq"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/yourbasic/graph"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/types/address"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
)

// Option is an extension point to instantiate keeper with non default values
type Option interface {
	apply(*Keeper)
}

type MessageDependencyGenerator func(keeper Keeper, ctx sdk.Context, msg sdk.Msg) ([]acltypes.AccessOperation, error)

type DependencyGeneratorMap map[types.MessageKey]MessageDependencyGenerator

type (
	Keeper struct {
		cdc                              codec.BinaryCodec
		storeKey                         sdk.StoreKey
		paramSpace                       paramtypes.Subspace
		MessageDependencyGeneratorMapper DependencyGeneratorMap
		AccountKeeper                    authkeeper.AccountKeeper
		StakingKeeper                    stakingkeeper.Keeper
	}
)

var ErrWasmDependencyMappingNotFound = fmt.Errorf("wasm dependency mapping not found")

func NewKeeper(
	cdc codec.Codec,
	storeKey sdk.StoreKey,
	paramSpace paramtypes.Subspace,
	ak authkeeper.AccountKeeper,
	sk stakingkeeper.Keeper,
	opts ...Option,
) Keeper {
	if !paramSpace.HasKeyTable() {
		paramSpace = paramSpace.WithKeyTable(types.ParamKeyTable())
	}

	keeper := &Keeper{
		cdc:                              cdc,
		storeKey:                         storeKey,
		paramSpace:                       paramSpace,
		MessageDependencyGeneratorMapper: DefaultMessageDependencyGenerator(),
		AccountKeeper:                    ak,
		StakingKeeper:                    sk,
	}

	for _, o := range opts {
		o.apply(keeper)
	}

	return *keeper
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

func (k Keeper) GetResourceDependencyMapping(ctx sdk.Context, messageKey types.MessageKey) acltypes.MessageDependencyMapping {
	store := ctx.KVStore(k.storeKey)
	depMapping := store.Get(types.GetResourceDependencyKey(messageKey))
	if depMapping == nil {
		// If the storage key doesn't exist in the mapping then assume synchronous processing
		return types.SynchronousMessageDependencyMapping(messageKey)
	}

	dependencyMapping := acltypes.MessageDependencyMapping{}
	k.cdc.MustUnmarshal(depMapping, &dependencyMapping)
	return dependencyMapping
}

func (k Keeper) SetResourceDependencyMapping(
	ctx sdk.Context,
	dependencyMapping acltypes.MessageDependencyMapping,
) error {
	err := types.ValidateMessageDependencyMapping(dependencyMapping)
	if err != nil {
		return err
	}
	store := ctx.KVStore(k.storeKey)
	b := k.cdc.MustMarshal(&dependencyMapping)
	resourceKey := types.GetResourceDependencyKey(types.MessageKey(dependencyMapping.GetMessageKey()))
	store.Set(resourceKey, b)
	return nil
}

func (k Keeper) IterateResourceKeys(ctx sdk.Context, handler func(dependencyMapping acltypes.MessageDependencyMapping) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.GetResourceDependencyMappingKey())
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		dependencyMapping := acltypes.MessageDependencyMapping{}
		k.cdc.MustUnmarshal(iter.Value(), &dependencyMapping)
		if handler(dependencyMapping) {
			break
		}
	}
}

func (k Keeper) SetDependencyMappingDynamicFlag(ctx sdk.Context, messageKey types.MessageKey, enabled bool) error {
	dependencyMapping := k.GetResourceDependencyMapping(ctx, messageKey)
	dependencyMapping.DynamicEnabled = enabled
	return k.SetResourceDependencyMapping(ctx, dependencyMapping)
}

type ContractReferenceLookupMap map[string]struct{}

func (k Keeper) GetRawWasmDependencyMapping(ctx sdk.Context, contractAddress sdk.AccAddress) (*acltypes.WasmDependencyMapping, error) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.GetWasmContractAddressKey(contractAddress))
	if b == nil {
		return nil, sdkerrors.ErrKeyNotFound
	}
	dependencyMapping := acltypes.WasmDependencyMapping{}
	if err := k.cdc.Unmarshal(b, &dependencyMapping); err != nil {
		return nil, err
	}
	return &dependencyMapping, nil
}

func GetCircularDependencyIdentifier(contractAddr sdk.AccAddress, msgInfo *types.WasmMessageInfo) string {
	separator := ";"
	identifier := contractAddr.String() + separator + msgInfo.MessageType.String() + separator + msgInfo.MessageName
	return identifier
}

func FilterReadOnlyAccessOps(accessOps []*acltypes.WasmAccessOperation) []*acltypes.WasmAccessOperation {
	filteredOps := []*acltypes.WasmAccessOperation{}
	for _, accessOp := range accessOps {
		if accessOp.Operation.AccessType != acltypes.AccessType_WRITE {
			// if access type is UNKNOWN, convert it to READ so it becomes non blocking since KNOW queries can't perform writes
			if accessOp.Operation.AccessType == acltypes.AccessType_UNKNOWN {
				accessOp.Operation.AccessType = acltypes.AccessType_READ
			}
			filteredOps = append(filteredOps, accessOp)
		}
	}
	return filteredOps
}

func (k Keeper) GetWasmDependencyAccessOps(ctx sdk.Context, contractAddress sdk.AccAddress, senderBech string, msgInfo *types.WasmMessageInfo, circularDepLookup ContractReferenceLookupMap) ([]acltypes.AccessOperation, error) {
	uniqueIdentifier := GetCircularDependencyIdentifier(contractAddress, msgInfo)
	if _, ok := circularDepLookup[uniqueIdentifier]; ok {
		// we've already seen this identifier, we should simply return synchronous access Ops
		ctx.Logger().Error("Circular dependency encountered, using synchronous access ops instead")
		return types.SynchronousAccessOps(), nil
	}
	// add to our lookup so we know we've seen this identifier
	circularDepLookup[uniqueIdentifier] = struct{}{}

	dependencyMapping, err := k.GetRawWasmDependencyMapping(ctx, contractAddress)
	if err != nil {
		if err == sdkerrors.ErrKeyNotFound {
			return types.SynchronousAccessOps(), nil
		}
		return nil, err
	}

	accessOps := dependencyMapping.BaseAccessOps
	if msgInfo.MessageType == acltypes.WasmMessageSubtype_QUERY {
		// If we have a query, filter out any WRITES
		accessOps = FilterReadOnlyAccessOps(accessOps)
	}
	specificAccessOpsMapping := []*acltypes.WasmAccessOperations{}
	if msgInfo.MessageType == acltypes.WasmMessageSubtype_EXECUTE && len(dependencyMapping.ExecuteAccessOps) > 0 {
		specificAccessOpsMapping = dependencyMapping.ExecuteAccessOps
	} else if msgInfo.MessageType == acltypes.WasmMessageSubtype_QUERY && len(dependencyMapping.QueryAccessOps) > 0 {
		specificAccessOpsMapping = dependencyMapping.QueryAccessOps
	}

	for _, specificAccessOps := range specificAccessOpsMapping {
		if specificAccessOps.MessageName == msgInfo.MessageName {
			accessOps = append(accessOps, specificAccessOps.WasmOperations...)
			break
		}
	}

	selectedAccessOps, err := k.BuildSelectorOps(ctx, contractAddress, accessOps, senderBech, msgInfo, circularDepLookup)
	if err != nil {
		return nil, err
	}

	// imports base contract references
	contractRefs := dependencyMapping.BaseContractReferences
	// add the specific execute or query contract references based on message type + name
	specificContractRefs := []*acltypes.WasmContractReferences{}
	if msgInfo.MessageType == acltypes.WasmMessageSubtype_EXECUTE && len(dependencyMapping.ExecuteContractReferences) > 0 {
		specificContractRefs = dependencyMapping.ExecuteContractReferences
	} else if msgInfo.MessageType == acltypes.WasmMessageSubtype_QUERY && len(dependencyMapping.QueryContractReferences) > 0 {
		specificContractRefs = dependencyMapping.QueryContractReferences
	}
	for _, specificContractRef := range specificContractRefs {
		if specificContractRef.MessageName == msgInfo.MessageName {
			contractRefs = append(contractRefs, specificContractRef.ContractReferences...)
			break
		}
	}
	importedAccessOps, err := k.ImportContractReferences(ctx, contractAddress, contractRefs, senderBech, msgInfo, circularDepLookup)
	if err != nil {
		return nil, err
	}
	// combine the access ops to get the definitive list of access ops for the contract
	selectedAccessOps.Merge(importedAccessOps)

	return selectedAccessOps.ToSlice(), nil
}

func ParseContractReferenceAddress(maybeContractAddress string, sender string, msgInfo *types.WasmMessageInfo) string {
	// sender in case the use case is expected to be one contract calling another expecting a separate call
	const reservedSender = "_sender"
	if maybeContractAddress == reservedSender {
		return sender
	}
	// parse the jq instruction from the template - if we can't then assume that its ACTUALLY an address
	op, err := jq.Parse(maybeContractAddress)
	if err != nil {
		return maybeContractAddress
	}
	// retrieve the appropriate item from the original msg
	data, err := op.Apply(msgInfo.MessageFullBody)
	// if we do have a jq selector but it doesn't apply properly, return maybeContractAddress
	if err != nil {
		return maybeContractAddress
	}
	// if we parse it properly convert to string and return
	var newValBytes string
	err = json.Unmarshal(data, &newValBytes)
	if err != nil {
		return maybeContractAddress
	}
	return newValBytes
}

func (k Keeper) ImportContractReferences(ctx sdk.Context, contractAddr sdk.AccAddress, contractReferences []*acltypes.WasmContractReference, senderBech string, msgInfo *types.WasmMessageInfo, circularDepLookup ContractReferenceLookupMap) (*types.AccessOperationSet, error) {
	importedAccessOps := types.NewEmptyAccessOperationSet()

	jsonTranslator := types.NewWasmMessageTranslator(senderBech, contractAddr.String(), msgInfo)
	for _, contractReference := range contractReferences {
		parsedContractReferenceAddress := ParseContractReferenceAddress(contractReference.ContractAddress, senderBech, msgInfo)
		// if parsing failed and contractAddress is invalid, this step will error and indicate invalid address
		importContractAddress, err := sdk.AccAddressFromBech32(parsedContractReferenceAddress)
		if err != nil {
			return nil, err
		}
		newJson, err := jsonTranslator.TranslateMessageBody([]byte(contractReference.JsonTranslationTemplate))
		if err != nil {
			// if there's a problem translating, log it and then pass in empty json
			ctx.Logger().Error("Error translating JSON body", err)
			newJson = []byte(fmt.Sprintf("{\"%s\":{}}", contractReference.MessageName))
		}
		var msgInfo *types.WasmMessageInfo
		if contractReference.MessageType == acltypes.WasmMessageSubtype_EXECUTE {
			msgInfo, err = types.NewExecuteMessageInfo(newJson)
			if err != nil {
				return nil, err
			}
		} else if contractReference.MessageType == acltypes.WasmMessageSubtype_QUERY {
			msgInfo, err = types.NewQueryMessageInfo(newJson)
			if err != nil {
				return nil, err
			}
		}
		// We use this to import the dependencies from another contract address
		wasmDeps, err := k.GetWasmDependencyAccessOps(ctx, importContractAddress, contractAddr.String(), msgInfo, circularDepLookup)

		if err != nil {
			// if we have an error fetching the dependency mapping or the mapping is disabled,
			// we want to return the error and the fallback behavior can be defined in the caller function
			// recommended fallback behavior is to use synchronous wasm access ops
			return nil, err
		} else {
			// if we did get deps properly and they are enabled, now we want to add them to our access operations
			importedAccessOps.AddMultiple(wasmDeps)
		}
	}
	// if we imported all relevant contract references properly, we can return the access ops generated
	return importedAccessOps, nil
}

func (k Keeper) BuildSelectorOps(ctx sdk.Context, contractAddr sdk.AccAddress, accessOps []*acltypes.WasmAccessOperation, senderBech string, msgInfo *types.WasmMessageInfo, circularDepLookup ContractReferenceLookupMap) (*types.AccessOperationSet, error) {
	selectedAccessOps := types.NewEmptyAccessOperationSet()
	// when we build selector ops here, we want to generate "*" if the proper fields aren't present
	// if size of circular dep map > 1 then it means we're in a contract reference
	// as a result, if the selector doesn't match properly, we need to conservatively assume "*" for the identifier
	withinContractReference := len(circularDepLookup) > 1
	for _, opWithSelector := range accessOps {
	selectorSwitch:
		switch opWithSelector.SelectorType {
		case acltypes.AccessOperationSelectorType_JQ:
			op, err := jq.Parse(opWithSelector.Selector)
			if err != nil {
				return nil, err
			}
			data, err := op.Apply(msgInfo.MessageFullBody)
			if err != nil {
				if withinContractReference {
					opWithSelector.Operation.IdentifierTemplate = "*"
					break selectorSwitch
				}
				// if the operation is not applicable to the message, skip it
				continue
			}
			trimmedData := strings.Trim(string(data), "\"") // we need to trim the quotes around the string
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hex.EncodeToString([]byte(trimmedData)),
			)
		case acltypes.AccessOperationSelectorType_JQ_BECH32_ADDRESS:
			op, err := jq.Parse(opWithSelector.Selector)
			if err != nil {
				return nil, err
			}
			data, err := op.Apply(msgInfo.MessageFullBody)
			if err != nil {
				if withinContractReference {
					opWithSelector.Operation.IdentifierTemplate = "*"
					break selectorSwitch
				}
				// if the operation is not applicable to the message, skip it
				continue
			}
			bech32Addr := strings.Trim(string(data), "\"") // we need to trim the quotes around the string
			// we expect a bech32 prefixed address, so lets convert to account address
			accAddr, err := sdk.AccAddressFromBech32(bech32Addr)
			if err != nil {
				return nil, err
			}
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hex.EncodeToString(accAddr),
			)
		case acltypes.AccessOperationSelectorType_JQ_LENGTH_PREFIXED_ADDRESS:
			op, err := jq.Parse(opWithSelector.Selector)
			if err != nil {
				return nil, err
			}
			data, err := op.Apply(msgInfo.MessageFullBody)
			if err != nil {
				if withinContractReference {
					opWithSelector.Operation.IdentifierTemplate = "*"
					break selectorSwitch
				}
				// if the operation is not applicable to the message, skip it
				continue
			}
			bech32Addr := strings.Trim(string(data), "\"") // we need to trim the quotes around the string
			// we expect a bech32 prefixed address, so lets convert to account address
			accAddr, err := sdk.AccAddressFromBech32(bech32Addr)
			if err != nil {
				return nil, err
			}
			lengthPrefixed := address.MustLengthPrefix(accAddr)
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hex.EncodeToString(lengthPrefixed),
			)
		case acltypes.AccessOperationSelectorType_SENDER_BECH32_ADDRESS:
			senderAccAddress, err := sdk.AccAddressFromBech32(senderBech)
			if err != nil {
				return nil, err
			}
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hex.EncodeToString(senderAccAddress),
			)
		case acltypes.AccessOperationSelectorType_SENDER_LENGTH_PREFIXED_ADDRESS:
			senderAccAddress, err := sdk.AccAddressFromBech32(senderBech)
			if err != nil {
				return nil, err
			}
			lengthPrefixed := address.MustLengthPrefix(senderAccAddress)
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hex.EncodeToString(lengthPrefixed),
			)
		case acltypes.AccessOperationSelectorType_CONTRACT_ADDRESS:
			contractAddress, err := sdk.AccAddressFromBech32(opWithSelector.Selector)
			if err != nil {
				return nil, err
			}
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hex.EncodeToString(contractAddress),
			)
		case acltypes.AccessOperationSelectorType_JQ_MESSAGE_CONDITIONAL:
			op, err := jq.Parse(opWithSelector.Selector)
			if err != nil {
				return nil, err
			}
			_, err = op.Apply(msgInfo.MessageFullBody)
			// if we are in a contract reference, we have to assume that this is necessary
			// TODO: after partitioning changes are merged, the MESSAGE_CONDITIONAL can be deprecated in favor of partitioned deps
			if err != nil && !withinContractReference {
				// if the operation is not applicable to the message, skip it
				continue
			}
		case acltypes.AccessOperationSelectorType_CONSTANT_STRING_TO_HEX:
			hexStr := hex.EncodeToString([]byte(opWithSelector.Selector))
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hexStr,
			)
		case acltypes.AccessOperationSelectorType_CONTRACT_REFERENCE:
			// Deprecated for ImportContractReference function
			continue
		}
		selectedAccessOps.Add(*opWithSelector.Operation)
	}

	return selectedAccessOps, nil
}

func (k Keeper) SetWasmDependencyMapping(
	ctx sdk.Context,
	dependencyMapping acltypes.WasmDependencyMapping,
) error {
	err := types.ValidateWasmDependencyMapping(dependencyMapping)
	if err != nil {
		return err
	}
	store := ctx.KVStore(k.storeKey)
	b := k.cdc.MustMarshal(&dependencyMapping)

	contractAddr, err := sdk.AccAddressFromBech32(dependencyMapping.ContractAddress)
	if err != nil {
		return err
	}
	resourceKey := types.GetWasmContractAddressKey(contractAddr)
	store.Set(resourceKey, b)
	return nil
}

func (k Keeper) ResetWasmDependencyMapping(
	ctx sdk.Context,
	contractAddress sdk.AccAddress,
	reason string,
) error {
	dependencyMapping, err := k.GetRawWasmDependencyMapping(ctx, contractAddress)
	if err != nil {
		return err
	}
	store := ctx.KVStore(k.storeKey)
	// keep `Enabled` true so that it won't cause all WASM resources to be synchronous
	dependencyMapping.BaseAccessOps = types.SynchronousWasmAccessOps()
	dependencyMapping.QueryAccessOps = []*acltypes.WasmAccessOperations{}
	dependencyMapping.ExecuteAccessOps = []*acltypes.WasmAccessOperations{}
	dependencyMapping.ResetReason = reason
	b := k.cdc.MustMarshal(dependencyMapping)
	resourceKey := types.GetWasmContractAddressKey(contractAddress)
	store.Set(resourceKey, b)
	return nil
}

func (k Keeper) IterateWasmDependencies(ctx sdk.Context, handler func(wasmDependencyMapping acltypes.WasmDependencyMapping) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.GetWasmMappingKey())
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		dependencyMapping := acltypes.WasmDependencyMapping{}
		k.cdc.MustUnmarshal(iter.Value(), &dependencyMapping)
		if handler(dependencyMapping) {
			break
		}
	}
}

// use -1 to indicate that it is prior to msgs in the tx
const ANTE_MSG_INDEX = int(-1)

func (k Keeper) BuildDependencyDag(ctx sdk.Context, txDecoder sdk.TxDecoder, anteDepGen sdk.AnteDepGenerator, txs [][]byte) (*types.Dag, error) {
	defer MeasureBuildDagDuration(time.Now(), "BuildDependencyDag")
	// contains the latest msg index for a specific Access Operation
	dependencyDag := types.NewDag()
	for txIndex, txBytes := range txs {
		tx, err := txDecoder(txBytes) // TODO: results in repetitive decoding for txs with runtx decode (potential optimization)
		if err != nil {
			return nil, err
		}
		// get the ante dependencies and add them to the dag
		anteDeps, err := anteDepGen([]acltypes.AccessOperation{}, tx)
		anteDepSet := make(map[acltypes.AccessOperation]struct{})
		for _, dep := range anteDeps {
			anteDepSet[dep] = struct{}{}
		}
		// pass through set to dedup
		if err != nil {
			return nil, err
		}
		for accessOp := range anteDepSet {
			err = types.ValidateAccessOp(accessOp)
			if err != nil {
				return nil, err
			}
			dependencyDag.AddNodeBuildDependency(ANTE_MSG_INDEX, txIndex, accessOp)
		}

		msgs := tx.GetMsgs()
		for messageIndex, msg := range msgs {
			if types.IsGovMessage(msg) {
				return nil, types.ErrGovMsgInBlock
			}
			msgDependencies := k.GetMessageDependencies(ctx, msg)
			dependencyDag.AddAccessOpsForMsg(messageIndex, txIndex, msgDependencies)
			for _, accessOp := range msgDependencies {
				// make a new node in the dependency dag
				dependencyDag.AddNodeBuildDependency(messageIndex, txIndex, accessOp)
			}
		}

	}
	if !graph.Acyclic(&dependencyDag) {
		return nil, types.ErrCycleInDAG
	}
	return &dependencyDag, nil
}

// Measures the time taken to build dependency dag
// Metric Names:
//
//	sei_dag_build_duration_miliseconds
//	sei_dag_build_duration_miliseconds_count
//	sei_dag_build_duration_miliseconds_sum
func MeasureBuildDagDuration(start time.Time, method string) {
	metrics.MeasureSinceWithLabels(
		[]string{"sei", "dag", "build", "milliseconds"},
		start.UTC(),
		[]metrics.Label{telemetry.NewLabel("method", method)},
	)
}

func (k Keeper) GetMessageDependencies(ctx sdk.Context, msg sdk.Msg) []acltypes.AccessOperation {
	// Default behavior is to get the static dependency mapping for the message
	messageKey := types.GenerateMessageKey(msg)
	dependencyMapping := k.GetResourceDependencyMapping(ctx, messageKey)
	if dependencyGenerator, ok := k.MessageDependencyGeneratorMapper[types.GenerateMessageKey(msg)]; dependencyMapping.DynamicEnabled && ok {
		// if we have a dependency generator AND dynamic is enabled, use it
		if dependencies, err := dependencyGenerator(k, ctx, msg); err == nil {
			// validate the access ops before using them
			validateErr := types.ValidateAccessOps(dependencies)
			if validateErr == nil {
				return dependencies
			} else {
				errorMessage := fmt.Sprintf("Invalid Access Ops for message=%s. %s", messageKey, validateErr.Error())
				ctx.Logger().Error(errorMessage)
			}
		} else {
			ctx.Logger().Error("Error generating message dependencies: ", err)
		}
	}
	if dependencyMapping.DynamicEnabled {
		// there was an issue with dynamic generation, so lets disable it
		err := k.SetDependencyMappingDynamicFlag(ctx, messageKey, false)
		if err != nil {
			ctx.Logger().Error("Error disabling dynamic enabled: ", err)
		}
	}
	return dependencyMapping.AccessOps
}

func DefaultMessageDependencyGenerator() DependencyGeneratorMap {
	return DependencyGeneratorMap{
		//TODO: define default granular behavior here
	}
}

func (k Keeper) GetStoreKey() sdk.StoreKey {
	return k.storeKey
}
