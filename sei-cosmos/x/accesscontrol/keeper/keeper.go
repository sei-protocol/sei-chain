package keeper

import (
	"encoding/hex"
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

func (k Keeper) GetWasmDependencyMapping(ctx sdk.Context, contractAddress sdk.AccAddress, senderBech string, msgBody []byte, applySelector bool) (acltypes.WasmDependencyMapping, error) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.GetWasmContractAddressKey(contractAddress))
	if b == nil {
		// return default (synchronous) dependency mapping so that wasm resource type as a whole
		// won't have dynamic mapping disabled if already enabled
		return acltypes.WasmDependencyMapping{
			Enabled:   true, // if wasm resource type as a whole is disabled, this will be ignored anyway
			AccessOps: types.SynchronousAccessOpsWithSelector(),
		}, nil
	}
	dependencyMapping := acltypes.WasmDependencyMapping{}
	k.cdc.MustUnmarshal(b, &dependencyMapping)
	if dependencyMapping.Enabled && applySelector {
		fmt.Printf("Build Selector ops from msg: %v\n", string(msgBody))
		selectedAccessOps, err := BuildSelectorOps(dependencyMapping.AccessOps, senderBech, msgBody)
		if err != nil {
			return acltypes.WasmDependencyMapping{}, err
		}
		dependencyMapping.AccessOps = selectedAccessOps
	}
	return dependencyMapping, nil
}

func BuildSelectorOps(accessOps []acltypes.AccessOperationWithSelector, senderBech string, msgBody []byte) ([]acltypes.AccessOperationWithSelector, error) {
	selectedAccessOps := []acltypes.AccessOperationWithSelector{}
	for _, opWithSelector := range accessOps {
		switch opWithSelector.SelectorType {
		case acltypes.AccessOperationSelectorType_JQ:
			op, err := jq.Parse(opWithSelector.Selector)
			if err != nil {
				return []acltypes.AccessOperationWithSelector{}, err
			}
			data, err := op.Apply(msgBody)
			if err != nil {
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
				return []acltypes.AccessOperationWithSelector{}, err
			}
			data, err := op.Apply(msgBody)
			if err != nil {
				// if the operation is not applicable to the message, skip it
				continue
			}
			bech32Addr := strings.Trim(string(data), "\"") // we need to trim the quotes around the string
			// we expect a bech32 prefixed address, so lets convert to account address
			accAddr, err := sdk.AccAddressFromBech32(bech32Addr)
			if err != nil {
				return []acltypes.AccessOperationWithSelector{}, err
			}
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hex.EncodeToString(accAddr),
			)
		case acltypes.AccessOperationSelectorType_JQ_LENGTH_PREFIXED_ADDRESS:
			op, err := jq.Parse(opWithSelector.Selector)
			if err != nil {
				return []acltypes.AccessOperationWithSelector{}, err
			}
			data, err := op.Apply(msgBody)
			if err != nil {
				// if the operation is not applicable to the message, skip it
				continue
			}
			bech32Addr := strings.Trim(string(data), "\"") // we need to trim the quotes around the string
			// we expect a bech32 prefixed address, so lets convert to account address
			accAddr, err := sdk.AccAddressFromBech32(bech32Addr)
			if err != nil {
				return []acltypes.AccessOperationWithSelector{}, err
			}
			lengthPrefixed := address.MustLengthPrefix(accAddr)
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hex.EncodeToString(lengthPrefixed),
			)
		case acltypes.AccessOperationSelectorType_SENDER_BECH32_ADDRESS:
			senderAccAddress, err := sdk.AccAddressFromBech32(senderBech)
			if err != nil {
				return []acltypes.AccessOperationWithSelector{}, err
			}
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hex.EncodeToString(senderAccAddress),
			)
		case acltypes.AccessOperationSelectorType_SENDER_LENGTH_PREFIXED_ADDRESS:
			senderAccAddress, err := sdk.AccAddressFromBech32(senderBech)
			if err != nil {
				return []acltypes.AccessOperationWithSelector{}, err
			}
			lengthPrefixed := address.MustLengthPrefix(senderAccAddress)
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hex.EncodeToString(lengthPrefixed),
			)
		case acltypes.AccessOperationSelectorType_CONTRACT_ADDRESS:
			contractAddress, err := sdk.AccAddressFromBech32(opWithSelector.Selector)
			if err != nil {
				return []acltypes.AccessOperationWithSelector{}, err
			}
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hex.EncodeToString(contractAddress),
			)
		case acltypes.AccessOperationSelectorType_JQ_MESSAGE_CONDITIONAL:
			op, err := jq.Parse(opWithSelector.Selector)
			if err != nil {
				return []acltypes.AccessOperationWithSelector{}, err
			}
			_, err = op.Apply(msgBody)
			if err != nil {
				// if the operation is not applicable to the message, skip it
				continue
			}
		case acltypes.AccessOperationSelectorType_CONSTANT_STRING_TO_HEX:
			hexStr := hex.EncodeToString([]byte(opWithSelector.Selector))
			opWithSelector.Operation.IdentifierTemplate = fmt.Sprintf(
				opWithSelector.Operation.IdentifierTemplate,
				hexStr,
			)
		}
		selectedAccessOps = append(selectedAccessOps, opWithSelector)
	}
	return selectedAccessOps, nil
}

func (k Keeper) SetWasmDependencyMapping(
	ctx sdk.Context,
	contractAddress sdk.AccAddress,
	dependencyMapping acltypes.WasmDependencyMapping,
) error {
	err := types.ValidateWasmDependencyMapping(dependencyMapping)
	if err != nil {
		return err
	}
	store := ctx.KVStore(k.storeKey)
	b := k.cdc.MustMarshal(&dependencyMapping)
	resourceKey := types.GetWasmContractAddressKey(contractAddress)
	store.Set(resourceKey, b)
	return nil
}

func (k Keeper) ResetWasmDependencyMapping(
	ctx sdk.Context,
	contractAddress sdk.AccAddress,
	reason string,
) error {
	dependencyMapping, err := k.GetWasmDependencyMapping(ctx, contractAddress, "", []byte{}, false)
	if err != nil {
		return err
	}
	store := ctx.KVStore(k.storeKey)
	// keep `Enabled` true so that it won't cause all WASM resources to be synchronous
	dependencyMapping.AccessOps = types.SynchronousAccessOpsWithSelector()
	dependencyMapping.ResetReason = reason
	b := k.cdc.MustMarshal(&dependencyMapping)
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
