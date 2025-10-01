package keeper

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/armon/go-metrics"
	"github.com/savaki/jq"
	"github.com/yourbasic/graph"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/multiversion"
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
	Apply(*Keeper)
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
		ResourceTypeStoreKeyMapping      acltypes.ResourceTypeToStoreKeyMap
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
		o.Apply(keeper)
	}

	return *keeper
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
	// doesn't actually return any errors, just returns nil
	op, _ := jq.Parse(maybeContractAddress)

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

func (k Keeper) ImportContractReferences(
	ctx sdk.Context,
	contractAddr sdk.AccAddress,
	contractReferences []*acltypes.WasmContractReference,
	senderBech string,
	msgInfo *types.WasmMessageInfo,
	circularDepLookup ContractReferenceLookupMap,
) (*types.AccessOperationSet, error) {
	importedAccessOps := types.NewEmptyAccessOperationSet()

	jsonTranslator := types.NewWasmMessageTranslator(senderBech, contractAddr.String(), msgInfo)

	// msgInfo can't be nil, it will panic
	if msgInfo == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalidMsgInfo, "msgInfo cannot be nil")
	}

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

type storeKeyMap map[string]sdk.StoreKey

func (k Keeper) GetStoreKeyMap(ctx sdk.Context) storeKeyMap {
	storeKeyMap := make(storeKeyMap)
	for _, storeKey := range ctx.MultiStore().StoreKeys() {
		storeKeyMap[storeKey.Name()] = storeKey
	}
	return storeKeyMap
}

func (k Keeper) UpdateWritesetsWithAccessOps(accessOps []acltypes.AccessOperation, mappedWritesets sdk.MappedWritesets, storeKeyMap storeKeyMap) sdk.MappedWritesets {
	for _, accessOp := range accessOps {
		// we only want writes and unknowns (assumed writes)
		if accessOp.AccessType != acltypes.AccessType_WRITE && accessOp.AccessType != acltypes.AccessType_UNKNOWN {
			continue
		}
		// the accessOps should only have SPECIFIC identifiers (we don't want wildcards)
		if accessOp.IdentifierTemplate == "*" {
			continue
		}
		// check the resource type to store key map for potential store key
		if storeKeyStr, ok := k.ResourceTypeStoreKeyMapping[accessOp.ResourceType]; ok {
			// check that we have a storekey corresponding to that string
			if storeKey, ok2 := storeKeyMap[storeKeyStr]; ok2 {
				// if we have a StoreKey, add it to the writeset - writing empty bytes is ok because it will be saved as EstimatedWriteset
				if _, ok := mappedWritesets[storeKey]; !ok {
					mappedWritesets[storeKey] = make(multiversion.WriteSet)
				}
				mappedWritesets[storeKey][accessOp.IdentifierTemplate] = []byte{}
			}
		}

	}
	return mappedWritesets
}

// GenerateEstimatedWritesets utilizes the existing patterns for access operation generation to estimate the writesets for a transaction
func (k Keeper) GenerateEstimatedWritesets(ctx sdk.Context, anteDepGen sdk.AnteDepGenerator, txIndex int, tx sdk.Tx) (sdk.MappedWritesets, error) {
	storeKeyMap := k.GetStoreKeyMap(ctx)
	writesets := make(sdk.MappedWritesets)
	// generate antedeps accessOps for tx
	anteDeps, err := anteDepGen([]acltypes.AccessOperation{}, tx, txIndex)
	if err != nil {
		return nil, err
	}
	writesets = k.UpdateWritesetsWithAccessOps(anteDeps, writesets, storeKeyMap)

	// generate accessOps for each message
	msgs := tx.GetMsgs()
	for _, msg := range msgs {
		msgDependencies := k.GetMessageDependencies(ctx, msg)
		// update estimated writeset for each message deps
		writesets = k.UpdateWritesetsWithAccessOps(msgDependencies, writesets, storeKeyMap)
	}
	return writesets, nil
}

func (k Keeper) BuildDependencyDag(ctx sdk.Context, anteDepGen sdk.AnteDepGenerator, txs []sdk.Tx) (*types.Dag, error) {
	defer MeasureBuildDagDuration(time.Now(), "BuildDependencyDag")
	// contains the latest msg index for a specific Access Operation
	dependencyDag := types.NewDag()
	for txIndex, tx := range txs {
		if tx == nil {
			// this implies decoding error
			return nil, sdkerrors.ErrTxDecode
		}
		// get the ante dependencies and add them to the dag
		anteDeps, err := anteDepGen([]acltypes.AccessOperation{}, tx, txIndex)
		if err != nil {
			return nil, err
		}
		anteDepSet := make(map[acltypes.AccessOperation]struct{})
		anteAccessOpsList := []acltypes.AccessOperation{}
		for _, accessOp := range anteDeps {
			// if found in set, we've already included this access Op in out ante dependencies, so skip it
			if _, found := anteDepSet[accessOp]; found {
				continue
			}
			anteDepSet[accessOp] = struct{}{}
			err = types.ValidateAccessOp(accessOp)
			if err != nil {
				return nil, err
			}
			dependencyDag.AddNodeBuildDependency(acltypes.ANTE_MSG_INDEX, txIndex, accessOp)
			anteAccessOpsList = append(anteAccessOpsList, accessOp)
		}
		// add Access ops for msg for anteMsg
		dependencyDag.AddAccessOpsForMsg(acltypes.ANTE_MSG_INDEX, txIndex, anteAccessOpsList)

		ctx = ctx.WithTxIndex(txIndex)
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
	// This should never happen base on existing DAG algorithm but it's not a significant
	// performance overhead (@BenchmarkAccessOpsBuildDependencyDag),
	// it would be better to keep this check. If a cyclic dependency
	// is ever found it may cause the chain to halt
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
			}
			errorMessage := fmt.Sprintf("Invalid Access Ops for message=%s. %s", messageKey, validateErr.Error())
			ctx.Logger().Error(errorMessage)
		}
	}
	return dependencyMapping.AccessOps
}

func DefaultMessageDependencyGenerator() DependencyGeneratorMap {
	//TODO: define default granular behavior here
	return DependencyGeneratorMap{}
}

func (m *DependencyGeneratorMap) Contains(key string) bool {
	_, ok := (*m)[types.MessageKey(key)]
	return ok
}

func (k Keeper) GetStoreKey() sdk.StoreKey {
	return k.storeKey
}
