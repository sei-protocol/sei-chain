package keeper

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/types/address"

	wasmvm "github.com/CosmWasm/wasmvm"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/tendermint/tendermint/libs/log"

	"github.com/CosmWasm/wasmd/x/wasm/ioutils"
	"github.com/CosmWasm/wasmd/x/wasm/types"
)

// contractMemoryLimit is the memory limit of each contract execution (in MiB)
// constant value so all nodes run with the same limit.
const contractMemoryLimit = 32

type contextKey int

const (
	// private type creates an interface key for Context that cannot be accessed by any other package
	contextKeyQueryStackSize contextKey = iota
)

// Option is an extension point to instantiate keeper with non default values
type Option interface {
	apply(*Keeper)
}

// WasmVMQueryHandler is an extension point for custom query handler implementations
type WasmVMQueryHandler interface {
	// HandleQuery executes the requested query
	HandleQuery(ctx sdk.Context, caller sdk.AccAddress, request wasmvmtypes.QueryRequest) ([]byte, error)
}

type CoinTransferrer interface {
	// TransferCoins sends the coin amounts from the source to the destination with rules applied.
	TransferCoins(ctx sdk.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error
}

// WasmVMResponseHandler is an extension point to handles the response data returned by a contract call.
type WasmVMResponseHandler interface {
	// Handle processes the data returned by a contract invocation.
	Handle(
		ctx sdk.Context,
		contractAddr sdk.AccAddress,
		ibcPort string,
		messages []wasmvmtypes.SubMsg,
		origRspData []byte,
		info wasmvmtypes.MessageInfo,
		codeInfo types.CodeInfo,
	) ([]byte, error)
}

// Keeper will have a reference to Wasmer with it's own data directory.
type Keeper struct {
	storeKey              sdk.StoreKey
	cdc                   codec.Codec
	accountKeeper         types.AccountKeeper
	bank                  CoinTransferrer
	portKeeper            types.PortKeeper
	capabilityKeeper      types.CapabilityKeeper
	paramsKeeper          types.ParamsKeeper
	wasmVM                types.WasmerEngine
	wasmVMQueryHandler    WasmVMQueryHandler
	wasmVMResponseHandler WasmVMResponseHandler
	messenger             Messenger
	// queryGasLimit is the max wasmvm gas that can be spent on executing a query with a contract
	queryGasLimit     uint64
	paramSpace        paramtypes.Subspace
	gasRegister       GasRegister
	maxQueryStackSize uint32
}

type routerWithContext struct {
	router *baseapp.MsgServiceRouter
}

func (rc routerWithContext) Handler(msg sdk.Msg) MsgHandler {
	h := rc.router.Handler(msg)
	return func(ctx sdk.Context, req sdk.Msg) (sdk.Context, *sdk.Result, error) {
		result, err := h(ctx, msg)
		return ctx, result, err
	}
}

// NewKeeper creates a new contract Keeper instance
// If customEncoders is non-nil, we can use this to override some of the message handler, especially custom
func NewKeeper(
	cdc codec.Codec,
	storeKey sdk.StoreKey,
	paramsKeeper types.ParamsKeeper,
	paramSpace paramtypes.Subspace,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	stakingKeeper types.StakingKeeper,
	distKeeper types.DistributionKeeper,
	channelKeeper types.ChannelKeeper,
	portKeeper types.PortKeeper,
	capabilityKeeper types.CapabilityKeeper,
	portSource types.ICS20TransferPortSource,
	router *baseapp.MsgServiceRouter,
	queryRouter GRPCQueryRouter,
	homeDir string,
	wasmConfig types.WasmConfig,
	supportedFeatures string,
	opts ...Option,
) Keeper {
	wasmer, err := wasmvm.NewVM(filepath.Join(homeDir, "wasm"), supportedFeatures, contractMemoryLimit, wasmConfig.ContractDebugMode, wasmConfig.MemoryCacheSize)
	if err != nil {
		panic(err)
	}
	// set KeyTable if it has not already been set
	if !paramSpace.HasKeyTable() {
		paramSpace = paramSpace.WithKeyTable(types.ParamKeyTable())
	}

	routerWithCtx := routerWithContext{router}
	keeper := &Keeper{
		storeKey:          storeKey,
		cdc:               cdc,
		paramsKeeper:      paramsKeeper,
		wasmVM:            NewVMWrapper(wasmer),
		accountKeeper:     accountKeeper,
		bank:              NewBankCoinTransferrer(bankKeeper),
		portKeeper:        portKeeper,
		capabilityKeeper:  capabilityKeeper,
		messenger:         NewDefaultMessageHandler(routerWithCtx, channelKeeper, capabilityKeeper, bankKeeper, cdc, portSource),
		queryGasLimit:     wasmConfig.SmartQueryGasLimit,
		paramSpace:        paramSpace,
		gasRegister:       NewDefaultWasmGasRegister(),
		maxQueryStackSize: types.DefaultMaxQueryStackSize,
	}
	keeper.wasmVMQueryHandler = DefaultQueryPlugins(bankKeeper, stakingKeeper, distKeeper, channelKeeper, queryRouter, keeper)
	for _, o := range opts {
		o.apply(keeper)
	}
	// not updateable, yet
	keeper.wasmVMResponseHandler = NewDefaultWasmVMContractResponseHandler(NewMessageDispatcher(keeper.messenger, keeper))
	return *keeper
}

func (k Keeper) getUploadAccessConfig(ctx sdk.Context) types.AccessConfig {
	var a types.AccessConfig
	k.paramSpace.Get(ctx, types.ParamStoreKeyUploadAccess, &a)
	return a
}

func (k Keeper) getInstantiateAccessConfig(ctx sdk.Context) types.AccessType {
	var a types.AccessType
	k.paramSpace.Get(ctx, types.ParamStoreKeyInstantiateAccess, &a)
	return a
}

// GetParams returns the total set of wasm parameters.
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	var params types.Params
	k.paramSpace.GetParamSet(ctx, &params)
	return params
}

func (k Keeper) SetParams(ctx sdk.Context, ps types.Params) {
	k.paramSpace.SetParamSet(ctx, &ps)
}

func (k Keeper) create(ctx sdk.Context, creator sdk.AccAddress, wasmCode []byte, instantiateAccess *types.AccessConfig, authZ AuthorizationPolicy) (codeID uint64, err error) {
	if creator == nil {
		return 0, sdkerrors.Wrap(sdkerrors.ErrInvalidAddress, "cannot be nil")
	}

	if !authZ.CanCreateCode(k.getUploadAccessConfig(ctx), creator) {
		return 0, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "can not create code")
	}
	// figure out proper instantiate access
	defaultAccessConfig := k.getInstantiateAccessConfig(ctx).With(creator)
	if instantiateAccess == nil {
		instantiateAccess = &defaultAccessConfig
	} else if !instantiateAccess.IsSubset(defaultAccessConfig) {
		// we enforce this must be subset of default upload access
		return 0, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "instantiate access must be subset of default upload access")
	}

	wasmCode, err = ioutils.Uncompress(wasmCode, uint64(types.MaxWasmSize))
	if err != nil {
		return 0, sdkerrors.Wrap(types.ErrCreateFailed, err.Error())
	}
	ctx.GasMeter().ConsumeGas(k.gasRegister.CompileCosts(len(wasmCode)), "Compiling WASM Bytecode")

	checksum, err := k.wasmVM.Create(wasmCode)
	if err != nil {
		return 0, sdkerrors.Wrap(types.ErrCreateFailed, err.Error())
	}
	report, err := k.wasmVM.AnalyzeCode(checksum)
	if err != nil {
		return 0, sdkerrors.Wrap(types.ErrCreateFailed, err.Error())
	}
	codeID = k.autoIncrementID(ctx, types.KeyLastCodeID)
	k.Logger(ctx).Debug("storing new contract", "features", report.RequiredFeatures, "code_id", codeID)
	codeInfo := types.NewCodeInfo(checksum, creator, *instantiateAccess)
	k.storeCodeInfo(ctx, codeID, codeInfo)

	evt := sdk.NewEvent(
		types.EventTypeStoreCode,
		sdk.NewAttribute(types.AttributeKeyCodeID, strconv.FormatUint(codeID, 10)),
	)
	for _, f := range strings.Split(report.RequiredFeatures, ",") {
		evt.AppendAttributes(sdk.NewAttribute(types.AttributeKeyFeature, strings.TrimSpace(f)))
	}
	ctx.EventManager().EmitEvent(evt)

	return codeID, nil
}

func (k Keeper) storeCodeInfo(ctx sdk.Context, codeID uint64, codeInfo types.CodeInfo) {
	store := ctx.KVStore(k.storeKey)
	// 0x01 | codeID (uint64) -> ContractInfo
	store.Set(types.GetCodeKey(codeID), k.cdc.MustMarshal(&codeInfo))
}

func (k Keeper) importCode(ctx sdk.Context, codeID uint64, codeInfo types.CodeInfo, wasmCode []byte) error {
	wasmCode, err := ioutils.Uncompress(wasmCode, uint64(types.MaxWasmSize))
	if err != nil {
		return sdkerrors.Wrap(types.ErrCreateFailed, err.Error())
	}
	newCodeHash, err := k.wasmVM.Create(wasmCode)
	if err != nil {
		return sdkerrors.Wrap(types.ErrCreateFailed, err.Error())
	}
	if !bytes.Equal(codeInfo.CodeHash, newCodeHash) {
		return sdkerrors.Wrap(types.ErrInvalid, "code hashes not same")
	}

	store := ctx.KVStore(k.storeKey)
	key := types.GetCodeKey(codeID)
	if store.Has(key) {
		return sdkerrors.Wrapf(types.ErrDuplicate, "duplicate code: %d", codeID)
	}
	// 0x01 | codeID (uint64) -> ContractInfo
	store.Set(key, k.cdc.MustMarshal(&codeInfo))
	return nil
}

func (k Keeper) instantiate(ctx sdk.Context, codeID uint64, creator, admin sdk.AccAddress, initMsg []byte, label string, deposit sdk.Coins, authZ AuthorizationPolicy) (sdk.AccAddress, []byte, error) {
	defer telemetry.MeasureSince(time.Now(), "wasm", "contract", "instantiate")

	instanceCosts := k.gasRegister.NewContractInstanceCosts(k.IsPinnedCode(ctx, codeID), len(initMsg))
	ctx.GasMeter().ConsumeGas(instanceCosts, "Loading CosmWasm module: instantiate")

	// create contract address
	contractAddress := k.generateContractAddress(ctx, codeID)
	existingAcct := k.accountKeeper.GetAccount(ctx, contractAddress)
	if existingAcct != nil {
		return nil, nil, sdkerrors.Wrap(types.ErrAccountExists, existingAcct.GetAddress().String())
	}

	// deposit initial contract funds
	if !deposit.IsZero() {
		if err := k.bank.TransferCoins(ctx, creator, contractAddress, deposit); err != nil {
			return nil, nil, err
		}
	} else {
		// create an empty account (so we don't have issues later)
		// TODO: can we remove this?
		contractAccount := k.accountKeeper.NewAccountWithAddress(ctx, contractAddress)
		k.accountKeeper.SetAccount(ctx, contractAccount)
	}

	// get contact info
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.GetCodeKey(codeID))
	if bz == nil {
		return nil, nil, sdkerrors.Wrap(types.ErrNotFound, "code")
	}
	var codeInfo types.CodeInfo
	k.cdc.MustUnmarshal(bz, &codeInfo)

	if !authZ.CanInstantiateContract(codeInfo.InstantiateConfig, creator) {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "can not instantiate")
	}

	// prepare params for contract instantiate call
	env := types.NewEnv(ctx, contractAddress)
	info := types.NewInfo(creator, deposit)

	// create prefixed data store
	// 0x03 | BuildContractAddress (sdk.AccAddress)
	prefixStoreKey := types.GetContractStorePrefix(contractAddress)
	prefixStore := types.NewStoreAdapter(prefix.NewStore(ctx.KVStore(k.storeKey), prefixStoreKey))

	// prepare querier
	querier := k.newQueryHandler(ctx, contractAddress)

	// instantiate wasm contract
	gas := k.runtimeGasForContract(ctx)
	res, gasUsed, err := k.wasmVM.Instantiate(codeInfo.CodeHash, env, info, initMsg, prefixStore, cosmwasmAPI, querier, k.gasMeter(ctx), gas, costJSONDeserialization)
	k.consumeRuntimeGas(ctx, gasUsed)
	if err != nil {
		return nil, nil, sdkerrors.Wrap(types.ErrInstantiateFailed, err.Error())
	}

	// persist instance first
	createdAt := types.NewAbsoluteTxPosition(ctx)
	contractInfo := types.NewContractInfo(codeID, creator, admin, label, createdAt)

	// check for IBC flag
	report, err := k.wasmVM.AnalyzeCode(codeInfo.CodeHash)
	if err != nil {
		return nil, nil, sdkerrors.Wrap(types.ErrInstantiateFailed, err.Error())
	}
	if report.HasIBCEntryPoints {
		// register IBC port
		ibcPort, err := k.ensureIbcPort(ctx, contractAddress)
		if err != nil {
			return nil, nil, err
		}
		contractInfo.IBCPortID = ibcPort
	}

	// store contract before dispatch so that contract could be called back
	historyEntry := contractInfo.InitialHistory(initMsg)
	k.addToContractCodeSecondaryIndex(ctx, contractAddress, historyEntry)
	k.appendToContractHistory(ctx, contractAddress, historyEntry)
	k.storeContractInfo(ctx, contractAddress, &contractInfo)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeInstantiate,
		sdk.NewAttribute(types.AttributeKeyContractAddr, contractAddress.String()),
		sdk.NewAttribute(types.AttributeKeyCodeID, strconv.FormatUint(codeID, 10)),
	))

	data, err := k.handleContractResponse(ctx, contractAddress, contractInfo.IBCPortID, res.Messages, res.Attributes, res.Data, res.Events, info, codeInfo)
	if err != nil {
		return nil, nil, sdkerrors.Wrap(err, "dispatch")
	}

	return contractAddress, data, nil
}

// Execute executes the contract instance
func (k Keeper) execute(ctx sdk.Context, contractAddress sdk.AccAddress, caller sdk.AccAddress, msg []byte, coins sdk.Coins) ([]byte, error) {
	defer telemetry.MeasureSince(time.Now(), "wasm", "contract", "execute")
	contractInfo, codeInfo, prefixStore, err := k.contractInstance(ctx, contractAddress)
	if err != nil {
		return nil, err
	}

	executeCosts := k.gasRegister.InstantiateContractCosts(k.IsPinnedCode(ctx, contractInfo.CodeID), len(msg))
	ctx.GasMeter().ConsumeGas(executeCosts, "Loading CosmWasm module: execute")

	// add more funds
	if !coins.IsZero() {
		if err := k.bank.TransferCoins(ctx, caller, contractAddress, coins); err != nil {
			return nil, err
		}
	}

	env := types.NewEnv(ctx, contractAddress)
	info := types.NewInfo(caller, coins)

	// prepare querier
	querier := k.newQueryHandler(ctx, contractAddress)
	gas := k.runtimeGasForContract(ctx)
	res, gasUsed, execErr := k.wasmVM.Execute(codeInfo.CodeHash, env, info, msg, prefixStore, cosmwasmAPI, querier, k.gasMeter(ctx), gas, costJSONDeserialization)
	k.consumeRuntimeGas(ctx, gasUsed)
	if execErr != nil {
		return nil, sdkerrors.Wrap(types.ErrExecuteFailed, execErr.Error())
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeExecute,
		sdk.NewAttribute(types.AttributeKeyContractAddr, contractAddress.String()),
	))

	data, err := k.handleContractResponse(ctx, contractAddress, contractInfo.IBCPortID, res.Messages, res.Attributes, res.Data, res.Events, info, codeInfo)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "dispatch")
	}

	return data, nil
}

func (k Keeper) migrate(ctx sdk.Context, contractAddress sdk.AccAddress, caller sdk.AccAddress, newCodeID uint64, msg []byte, authZ AuthorizationPolicy) ([]byte, error) {
	defer telemetry.MeasureSince(time.Now(), "wasm", "contract", "migrate")
	migrateSetupCosts := k.gasRegister.InstantiateContractCosts(k.IsPinnedCode(ctx, newCodeID), len(msg))
	ctx.GasMeter().ConsumeGas(migrateSetupCosts, "Loading CosmWasm module: migrate")

	contractInfo := k.GetContractInfo(ctx, contractAddress)
	if contractInfo == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "unknown contract")
	}
	if !authZ.CanModifyContract(contractInfo.AdminAddr(), caller) {
		return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "can not migrate")
	}

	newCodeInfo := k.GetCodeInfo(ctx, newCodeID)
	if newCodeInfo == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "unknown code")
	}

	// check for IBC flag
	switch report, err := k.wasmVM.AnalyzeCode(newCodeInfo.CodeHash); {
	case err != nil:
		return nil, sdkerrors.Wrap(types.ErrMigrationFailed, err.Error())
	case !report.HasIBCEntryPoints && contractInfo.IBCPortID != "":
		// prevent update to non ibc contract
		return nil, sdkerrors.Wrap(types.ErrMigrationFailed, "requires ibc callbacks")
	case report.HasIBCEntryPoints && contractInfo.IBCPortID == "":
		// add ibc port
		ibcPort, err := k.ensureIbcPort(ctx, contractAddress)
		if err != nil {
			return nil, err
		}
		contractInfo.IBCPortID = ibcPort
	}

	env := types.NewEnv(ctx, contractAddress)

	// prepare querier
	querier := k.newQueryHandler(ctx, contractAddress)

	prefixStoreKey := types.GetContractStorePrefix(contractAddress)
	prefixStore := types.NewStoreAdapter(prefix.NewStore(ctx.KVStore(k.storeKey), prefixStoreKey))
	gas := k.runtimeGasForContract(ctx)
	res, gasUsed, err := k.wasmVM.Migrate(newCodeInfo.CodeHash, env, msg, prefixStore, cosmwasmAPI, &querier, k.gasMeter(ctx), gas, costJSONDeserialization)
	k.consumeRuntimeGas(ctx, gasUsed)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrMigrationFailed, err.Error())
	}

	// delete old secondary index entry
	k.removeFromContractCodeSecondaryIndex(ctx, contractAddress, k.getLastContractHistoryEntry(ctx, contractAddress))
	// persist migration updates
	historyEntry := contractInfo.AddMigration(ctx, newCodeID, msg)
	k.appendToContractHistory(ctx, contractAddress, historyEntry)
	k.addToContractCodeSecondaryIndex(ctx, contractAddress, historyEntry)
	k.storeContractInfo(ctx, contractAddress, contractInfo)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeMigrate,
		sdk.NewAttribute(types.AttributeKeyCodeID, strconv.FormatUint(newCodeID, 10)),
		sdk.NewAttribute(types.AttributeKeyContractAddr, contractAddress.String()),
	))

	data, err := k.handleContractResponse(ctx, contractAddress, contractInfo.IBCPortID, res.Messages, res.Attributes, res.Data, res.Events, wasmvmtypes.MessageInfo{}, *newCodeInfo)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "dispatch")
	}

	return data, nil
}

// Sudo allows priviledged access to a contract. This can never be called by an external tx, but only by
// another native Go module directly, or on-chain governance (if sudo proposals are enabled). Thus, the keeper doesn't
// place any access controls on it, that is the responsibility or the app developer (who passes the wasm.Keeper in app.go)
func (k Keeper) Sudo(ctx sdk.Context, contractAddress sdk.AccAddress, msg []byte) ([]byte, error) {
	defer telemetry.MeasureSince(time.Now(), "wasm", "contract", "sudo")
	contractInfo, codeInfo, prefixStore, err := k.contractInstance(ctx, contractAddress)
	if err != nil {
		return nil, err
	}

	sudoSetupCosts := k.gasRegister.InstantiateContractCosts(k.IsPinnedCode(ctx, contractInfo.CodeID), len(msg))
	ctx.GasMeter().ConsumeGas(sudoSetupCosts, "Loading CosmWasm module: sudo")

	env := types.NewEnv(ctx, contractAddress)

	// prepare querier
	querier := k.newQueryHandler(ctx, contractAddress)
	gas := k.runtimeGasForContract(ctx)
	res, gasUsed, execErr := k.wasmVM.Sudo(codeInfo.CodeHash, env, msg, prefixStore, cosmwasmAPI, querier, k.gasMeter(ctx), gas, costJSONDeserialization)
	k.consumeRuntimeGas(ctx, gasUsed)
	if execErr != nil {
		return nil, sdkerrors.Wrap(types.ErrExecuteFailed, execErr.Error())
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSudo,
		sdk.NewAttribute(types.AttributeKeyContractAddr, contractAddress.String()),
	))

	data, err := k.handleContractResponse(ctx, contractAddress, contractInfo.IBCPortID, res.Messages, res.Attributes, res.Data, res.Events, wasmvmtypes.MessageInfo{}, codeInfo)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "dispatch")
	}

	return data, nil
}

// reply is only called from keeper internal functions (dispatchSubmessages) after processing the submessage
func (k Keeper) reply(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
	contractInfo, codeInfo, prefixStore, err := k.contractInstance(ctx, contractAddress)
	if err != nil {
		return nil, err
	}

	// always consider this pinned
	replyCosts := k.gasRegister.ReplyCosts(true, reply)
	ctx.GasMeter().ConsumeGas(replyCosts, "Loading CosmWasm module: reply")

	env := types.NewEnv(ctx, contractAddress)

	// prepare querier
	querier := k.newQueryHandler(ctx, contractAddress)
	gas := k.runtimeGasForContract(ctx)
	res, gasUsed, execErr := k.wasmVM.Reply(codeInfo.CodeHash, env, reply, prefixStore, cosmwasmAPI, querier, k.gasMeter(ctx), gas, costJSONDeserialization)
	k.consumeRuntimeGas(ctx, gasUsed)
	if execErr != nil {
		return nil, sdkerrors.Wrap(types.ErrExecuteFailed, execErr.Error())
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeReply,
		sdk.NewAttribute(types.AttributeKeyContractAddr, contractAddress.String()),
	))

	data, err := k.handleContractResponse(ctx, contractAddress, contractInfo.IBCPortID, res.Messages, res.Attributes, res.Data, res.Events, wasmvmtypes.MessageInfo{}, codeInfo)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "dispatch")
	}

	return data, nil
}

// addToContractCodeSecondaryIndex adds element to the index for contracts-by-codeid queries
func (k Keeper) addToContractCodeSecondaryIndex(ctx sdk.Context, contractAddress sdk.AccAddress, entry types.ContractCodeHistoryEntry) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.GetContractByCreatedSecondaryIndexKey(contractAddress, entry), []byte{})
}

// removeFromContractCodeSecondaryIndex removes element to the index for contracts-by-codeid queries
func (k Keeper) removeFromContractCodeSecondaryIndex(ctx sdk.Context, contractAddress sdk.AccAddress, entry types.ContractCodeHistoryEntry) {
	ctx.KVStore(k.storeKey).Delete(types.GetContractByCreatedSecondaryIndexKey(contractAddress, entry))
}

// IterateContractsByCode iterates over all contracts with given codeID ASC on code update time.
func (k Keeper) IterateContractsByCode(ctx sdk.Context, codeID uint64, cb func(address sdk.AccAddress) bool) {
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), types.GetContractByCodeIDSecondaryIndexPrefix(codeID))
	iter := prefixStore.Iterator(nil, nil)
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		if cb(key[types.AbsoluteTxPositionLen:]) {
			return
		}
	}
}

func (k Keeper) setContractAdmin(ctx sdk.Context, contractAddress, caller, newAdmin sdk.AccAddress, authZ AuthorizationPolicy) error {
	contractInfo := k.GetContractInfo(ctx, contractAddress)
	if contractInfo == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "unknown contract")
	}
	if !authZ.CanModifyContract(contractInfo.AdminAddr(), caller) {
		return sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "can not modify contract")
	}
	contractInfo.Admin = newAdmin.String()
	k.storeContractInfo(ctx, contractAddress, contractInfo)
	return nil
}

func (k Keeper) appendToContractHistory(ctx sdk.Context, contractAddr sdk.AccAddress, newEntries ...types.ContractCodeHistoryEntry) {
	store := ctx.KVStore(k.storeKey)
	// find last element position
	var pos uint64
	prefixStore := prefix.NewStore(store, types.GetContractCodeHistoryElementPrefix(contractAddr))
	iter := prefixStore.ReverseIterator(nil, nil)
	defer iter.Close()

	if iter.Valid() {
		pos = sdk.BigEndianToUint64(iter.Value())
	}
	// then store with incrementing position
	for _, e := range newEntries {
		pos++
		key := types.GetContractCodeHistoryElementKey(contractAddr, pos)
		store.Set(key, k.cdc.MustMarshal(&e)) //nolint:gosec
	}
}

func (k Keeper) GetContractHistory(ctx sdk.Context, contractAddr sdk.AccAddress) []types.ContractCodeHistoryEntry {
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), types.GetContractCodeHistoryElementPrefix(contractAddr))
	r := make([]types.ContractCodeHistoryEntry, 0)
	iter := prefixStore.Iterator(nil, nil)
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		var e types.ContractCodeHistoryEntry
		k.cdc.MustUnmarshal(iter.Value(), &e)
		r = append(r, e)
	}
	return r
}

// getLastContractHistoryEntry returns the last element from history. To be used internally only as it panics when none exists
func (k Keeper) getLastContractHistoryEntry(ctx sdk.Context, contractAddr sdk.AccAddress) types.ContractCodeHistoryEntry {
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), types.GetContractCodeHistoryElementPrefix(contractAddr))
	iter := prefixStore.ReverseIterator(nil, nil)
	defer iter.Close()

	var r types.ContractCodeHistoryEntry
	if !iter.Valid() {
		// all contracts have a history
		panic(fmt.Sprintf("no history for %s", contractAddr.String()))
	}
	k.cdc.MustUnmarshal(iter.Value(), &r)
	return r
}

// QuerySmart queries the smart contract itself.
func (k Keeper) QuerySmart(ctx sdk.Context, contractAddr sdk.AccAddress, req []byte) ([]byte, error) {
	defer telemetry.MeasureSince(time.Now(), "wasm", "contract", "query-smart")
	telemetry.IncrCounterWithLabels(
		[]string{"wasm", "contract", "query-smart", "invocation"},
		1,
		[]metrics.Label{telemetry.NewLabel("contract_address", contractAddr.String())},
	)

	// checks and increase query stack size
	ctx, err := checkAndIncreaseQueryStackSize(ctx, k.maxQueryStackSize)
	if err != nil {
		return nil, err
	}

	contractInfo, codeInfo, prefixStore, err := k.contractInstance(ctx, contractAddr)
	if err != nil {
		return nil, err
	}

	smartQuerySetupCosts := k.gasRegister.InstantiateContractCosts(k.IsPinnedCode(ctx, contractInfo.CodeID), len(req))
	ctx.GasMeter().ConsumeGas(smartQuerySetupCosts, "Loading CosmWasm module: query")

	// prepare querier
	querier := k.newQueryHandler(ctx, contractAddr)

	env := types.NewEnv(ctx, contractAddr)
	queryResult, gasUsed, qErr := k.wasmVM.Query(codeInfo.CodeHash, env, req, prefixStore, cosmwasmAPI, querier, k.gasMeter(ctx), k.runtimeGasForContract(ctx), costJSONDeserialization)
	k.consumeRuntimeGas(ctx, gasUsed)
	if qErr != nil {
		// consume ALL remaining gas on error
		k.consumeRemainingGas(ctx)
		return nil, sdkerrors.Wrap(types.ErrQueryFailed, qErr.Error())
	}

	telemetry.SetGaugeWithLabels(
		[]string{"wasm", "contract", "query-smart", "gas-used"},
		float32(gasUsed),
		[]metrics.Label{telemetry.NewLabel("contract_address", contractAddr.String())},
	)
	return queryResult, nil
}

func checkAndIncreaseQueryStackSize(ctx sdk.Context, maxQueryStackSize uint32) (sdk.Context, error) {
	var queryStackSize uint32

	// read current value
	if size := ctx.Context().Value(contextKeyQueryStackSize); size != nil {
		queryStackSize = size.(uint32)
	} else {
		queryStackSize = 0
	}

	// increase
	queryStackSize++

	// did we go too far?
	if queryStackSize > maxQueryStackSize {
		return ctx, types.ErrExceedMaxQueryStackSize
	}

	// set updated stack size
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), contextKeyQueryStackSize, queryStackSize))

	return ctx, nil
}

// QueryRaw returns the contract's state for give key. Returns `nil` when key is `nil`.
func (k Keeper) QueryRaw(ctx sdk.Context, contractAddress sdk.AccAddress, key []byte) []byte {
	defer telemetry.MeasureSince(time.Now(), "wasm", "contract", "query-raw")
	if key == nil {
		return nil
	}
	prefixStoreKey := types.GetContractStorePrefix(contractAddress)
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), prefixStoreKey)
	return prefixStore.Get(key)
}

func (k Keeper) contractInstance(ctx sdk.Context, contractAddress sdk.AccAddress) (types.ContractInfo, types.CodeInfo, wasmvm.KVStore, error) {
	store := ctx.KVStore(k.storeKey)

	contractBz := store.Get(types.GetContractAddressKey(contractAddress))
	if contractBz == nil {
		return types.ContractInfo{}, types.CodeInfo{}, types.NewStoreAdapter(prefix.Store{}), sdkerrors.Wrap(types.ErrNotFound, "contract")
	}
	var contractInfo types.ContractInfo
	k.cdc.MustUnmarshal(contractBz, &contractInfo)

	codeInfoBz := store.Get(types.GetCodeKey(contractInfo.CodeID))
	if codeInfoBz == nil {
		return contractInfo, types.CodeInfo{}, types.NewStoreAdapter(prefix.Store{}), sdkerrors.Wrap(types.ErrNotFound, "code info")
	}
	var codeInfo types.CodeInfo
	k.cdc.MustUnmarshal(codeInfoBz, &codeInfo)
	prefixStoreKey := types.GetContractStorePrefix(contractAddress)
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), prefixStoreKey)
	return contractInfo, codeInfo, types.NewStoreAdapter(prefixStore), nil
}

func (k Keeper) GetContractInfo(ctx sdk.Context, contractAddress sdk.AccAddress) *types.ContractInfo {
	store := ctx.KVStore(k.storeKey)
	var contract types.ContractInfo
	contractBz := store.Get(types.GetContractAddressKey(contractAddress))
	if contractBz == nil {
		return nil
	}
	k.cdc.MustUnmarshal(contractBz, &contract)
	return &contract
}

func (k Keeper) HasContractInfo(ctx sdk.Context, contractAddress sdk.AccAddress) bool {
	store := ctx.KVStore(k.storeKey)
	return store.Has(types.GetContractAddressKey(contractAddress))
}

// storeContractInfo persists the ContractInfo. No secondary index updated here.
func (k Keeper) storeContractInfo(ctx sdk.Context, contractAddress sdk.AccAddress, contract *types.ContractInfo) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.GetContractAddressKey(contractAddress), k.cdc.MustMarshal(contract))
}

func (k Keeper) IterateContractInfo(ctx sdk.Context, cb func(sdk.AccAddress, types.ContractInfo) bool) {
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), types.ContractKeyPrefix)
	iter := prefixStore.Iterator(nil, nil)
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		var contract types.ContractInfo
		k.cdc.MustUnmarshal(iter.Value(), &contract)
		// cb returns true to stop early
		if cb(iter.Key(), contract) {
			break
		}
	}
}

// IterateContractState iterates through all elements of the key value store for the given contract address and passes
// them to the provided callback function. The callback method can return true to abort early.
func (k Keeper) IterateContractState(ctx sdk.Context, contractAddress sdk.AccAddress, cb func(key, value []byte) bool) {
	prefixStoreKey := types.GetContractStorePrefix(contractAddress)
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), prefixStoreKey)
	iter := prefixStore.Iterator(nil, nil)
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		if cb(iter.Key(), iter.Value()) {
			break
		}
	}
}

func (k Keeper) importContractState(ctx sdk.Context, contractAddress sdk.AccAddress, models []types.Model) error {
	prefixStoreKey := types.GetContractStorePrefix(contractAddress)
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), prefixStoreKey)
	for _, model := range models {
		if model.Value == nil {
			model.Value = []byte{}
		}
		if prefixStore.Has(model.Key) {
			return sdkerrors.Wrapf(types.ErrDuplicate, "duplicate key: %x", model.Key)
		}
		prefixStore.Set(model.Key, model.Value)
	}
	return nil
}

func (k Keeper) GetCodeInfo(ctx sdk.Context, codeID uint64) *types.CodeInfo {
	store := ctx.KVStore(k.storeKey)
	var codeInfo types.CodeInfo
	codeInfoBz := store.Get(types.GetCodeKey(codeID))
	if codeInfoBz == nil {
		return nil
	}
	k.cdc.MustUnmarshal(codeInfoBz, &codeInfo)
	return &codeInfo
}

func (k Keeper) containsCodeInfo(ctx sdk.Context, codeID uint64) bool {
	store := ctx.KVStore(k.storeKey)
	return store.Has(types.GetCodeKey(codeID))
}

func (k Keeper) IterateCodeInfos(ctx sdk.Context, cb func(uint64, types.CodeInfo) bool) {
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), types.CodeKeyPrefix)
	iter := prefixStore.Iterator(nil, nil)
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		var c types.CodeInfo
		k.cdc.MustUnmarshal(iter.Value(), &c)
		// cb returns true to stop early
		if cb(binary.BigEndian.Uint64(iter.Key()), c) {
			return
		}
	}
}

func (k Keeper) GetByteCode(ctx sdk.Context, codeID uint64) ([]byte, error) {
	store := ctx.KVStore(k.storeKey)
	var codeInfo types.CodeInfo
	codeInfoBz := store.Get(types.GetCodeKey(codeID))
	if codeInfoBz == nil {
		return nil, nil
	}
	k.cdc.MustUnmarshal(codeInfoBz, &codeInfo)
	return k.wasmVM.GetCode(codeInfo.CodeHash)
}

// PinCode pins the wasm contract in wasmvm cache
func (k Keeper) pinCode(ctx sdk.Context, codeID uint64) error {
	codeInfo := k.GetCodeInfo(ctx, codeID)
	if codeInfo == nil {
		return sdkerrors.Wrap(types.ErrNotFound, "code info")
	}

	if err := k.wasmVM.Pin(codeInfo.CodeHash); err != nil {
		return sdkerrors.Wrap(types.ErrPinContractFailed, err.Error())
	}
	store := ctx.KVStore(k.storeKey)
	// store 1 byte to not run into `nil` debugging issues
	store.Set(types.GetPinnedCodeIndexPrefix(codeID), []byte{1})

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePinCode,
		sdk.NewAttribute(types.AttributeKeyCodeID, strconv.FormatUint(codeID, 10)),
	))
	return nil
}

// UnpinCode removes the wasm contract from wasmvm cache
func (k Keeper) unpinCode(ctx sdk.Context, codeID uint64) error {
	codeInfo := k.GetCodeInfo(ctx, codeID)
	if codeInfo == nil {
		return sdkerrors.Wrap(types.ErrNotFound, "code info")
	}
	if err := k.wasmVM.Unpin(codeInfo.CodeHash); err != nil {
		return sdkerrors.Wrap(types.ErrUnpinContractFailed, err.Error())
	}

	store := ctx.KVStore(k.storeKey)
	store.Delete(types.GetPinnedCodeIndexPrefix(codeID))

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeUnpinCode,
		sdk.NewAttribute(types.AttributeKeyCodeID, strconv.FormatUint(codeID, 10)),
	))
	return nil
}

// IsPinnedCode returns true when codeID is pinned in wasmvm cache
func (k Keeper) IsPinnedCode(ctx sdk.Context, codeID uint64) bool {
	store := ctx.KVStore(k.storeKey)
	return store.Has(types.GetPinnedCodeIndexPrefix(codeID))
}

// InitializePinnedCodes updates wasmvm to pin to cache all contracts marked as pinned
func (k Keeper) InitializePinnedCodes(ctx sdk.Context) error {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PinnedCodeIndexPrefix)
	iter := store.Iterator(nil, nil)
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		codeInfo := k.GetCodeInfo(ctx, types.ParsePinnedCodeIndex(iter.Key()))
		if codeInfo == nil {
			return sdkerrors.Wrap(types.ErrNotFound, "code info")
		}
		if err := k.wasmVM.Pin(codeInfo.CodeHash); err != nil {
			return sdkerrors.Wrap(types.ErrPinContractFailed, err.Error())
		}
	}
	return nil
}

// setContractInfoExtension updates the extension point data that is stored with the contract info
func (k Keeper) setContractInfoExtension(ctx sdk.Context, contractAddr sdk.AccAddress, ext types.ContractInfoExtension) error {
	info := k.GetContractInfo(ctx, contractAddr)
	if info == nil {
		return sdkerrors.Wrap(types.ErrNotFound, "contract info")
	}
	if err := info.SetExtension(ext); err != nil {
		return err
	}
	k.storeContractInfo(ctx, contractAddr, info)
	return nil
}

// setAccessConfig updates the access config of a code id.
func (k Keeper) setAccessConfig(ctx sdk.Context, codeID uint64, config types.AccessConfig) error {
	info := k.GetCodeInfo(ctx, codeID)
	if info == nil {
		return sdkerrors.Wrap(types.ErrNotFound, "code info")
	}
	info.InstantiateConfig = config
	k.storeCodeInfo(ctx, codeID, *info)
	return nil
}

// handleContractResponse processes the contract response data by emitting events and sending sub-/messages.
func (k *Keeper) handleContractResponse(
	ctx sdk.Context,
	contractAddr sdk.AccAddress,
	ibcPort string,
	msgs []wasmvmtypes.SubMsg,
	attrs []wasmvmtypes.EventAttribute,
	data []byte,
	evts wasmvmtypes.Events,
	info wasmvmtypes.MessageInfo,
	codeInfo types.CodeInfo,
) ([]byte, error) {
	attributeGasCost := k.gasRegister.EventCosts(attrs, evts)
	ctx.GasMeter().ConsumeGas(attributeGasCost, "Custom contract event attributes")
	// emit all events from this contract itself
	if len(attrs) != 0 {
		wasmEvents, err := newWasmModuleEvent(attrs, contractAddr)
		if err != nil {
			return nil, err
		}
		ctx.EventManager().EmitEvents(wasmEvents)
	}
	if len(evts) > 0 {
		customEvents, err := newCustomEvents(evts, contractAddr)
		if err != nil {
			return nil, err
		}
		ctx.EventManager().EmitEvents(customEvents)
	}
	return k.wasmVMResponseHandler.Handle(ctx, contractAddr, ibcPort, msgs, data, info, codeInfo)
}

func (k Keeper) runtimeGasForContract(ctx sdk.Context) uint64 {
	meter := ctx.GasMeter()
	if meter.IsOutOfGas() {
		return 0
	}
	if meter.Limit() == 0 { // infinite gas meter with limit=0 and not out of gas
		return math.MaxUint64
	}
	absoluteGasDiff := (meter.Limit() - meter.GasConsumedToLimit())
	numerator, denominator := meter.Multiplier()
	appliedAdjustmentGas := absoluteGasDiff * denominator / numerator
	return k.gasRegister.ToWasmVMGas(appliedAdjustmentGas)
}

func (k Keeper) consumeRuntimeGas(ctx sdk.Context, gas uint64) {
	consumed := k.gasRegister.FromWasmVMGas(gas)
	ctx.GasMeter().ConsumeGas(consumed, "wasm contract")
	// throw OutOfGas error if we ran out (got exactly to zero due to better limit enforcing)
	if ctx.GasMeter().IsOutOfGas() {
		panic(sdk.ErrorOutOfGas{Descriptor: "Wasmer function execution"})
	}
}

func (k Keeper) consumeRemainingGas(ctx sdk.Context) {
	remainingGas := ctx.GasMeter().Limit() - ctx.GasMeter().GasConsumedToLimit()
	ctx.GasMeter().ConsumeGas(remainingGas, "wasm contract")
}

// generates a contract address from codeID + instanceID
func (k Keeper) generateContractAddress(ctx sdk.Context, codeID uint64) sdk.AccAddress {
	instanceID := k.autoIncrementID(ctx, types.KeyLastInstanceID)
	return BuildContractAddress(codeID, instanceID)
}

// BuildContractAddress builds an sdk account address for a contract.
func BuildContractAddress(codeID, instanceID uint64) sdk.AccAddress {
	contractID := make([]byte, 16)
	binary.BigEndian.PutUint64(contractID[:8], codeID)
	binary.BigEndian.PutUint64(contractID[8:], instanceID)
	return address.Module(types.ModuleName, contractID)[:types.ContractAddrLen]
}

func (k Keeper) autoIncrementID(ctx sdk.Context, lastIDKey []byte) uint64 {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(lastIDKey)
	id := uint64(1)
	if bz != nil {
		id = binary.BigEndian.Uint64(bz)
	}
	bz = sdk.Uint64ToBigEndian(id + 1)
	store.Set(lastIDKey, bz)
	return id
}

// PeekAutoIncrementID reads the current value without incrementing it.
func (k Keeper) PeekAutoIncrementID(ctx sdk.Context, lastIDKey []byte) uint64 {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(lastIDKey)
	id := uint64(1)
	if bz != nil {
		id = binary.BigEndian.Uint64(bz)
	}
	return id
}

func (k Keeper) importAutoIncrementID(ctx sdk.Context, lastIDKey []byte, val uint64) error {
	store := ctx.KVStore(k.storeKey)
	if store.Has(lastIDKey) {
		return sdkerrors.Wrapf(types.ErrDuplicate, "autoincrement id: %s", string(lastIDKey))
	}
	bz := sdk.Uint64ToBigEndian(val)
	store.Set(lastIDKey, bz)
	return nil
}

func (k Keeper) importContract(ctx sdk.Context, contractAddr sdk.AccAddress, c *types.ContractInfo, state []types.Model) error {
	if !k.containsCodeInfo(ctx, c.CodeID) {
		return sdkerrors.Wrapf(types.ErrNotFound, "code id: %d", c.CodeID)
	}
	if k.HasContractInfo(ctx, contractAddr) {
		return sdkerrors.Wrapf(types.ErrDuplicate, "contract: %s", contractAddr)
	}

	historyEntry := c.ResetFromGenesis(ctx)
	k.appendToContractHistory(ctx, contractAddr, historyEntry)
	k.storeContractInfo(ctx, contractAddr, c)
	k.addToContractCodeSecondaryIndex(ctx, contractAddr, historyEntry)
	return k.importContractState(ctx, contractAddr, state)
}

func (k Keeper) newQueryHandler(ctx sdk.Context, contractAddress sdk.AccAddress) QueryHandler {
	return NewQueryHandler(ctx, k.wasmVMQueryHandler, contractAddress, k.gasRegister)
}

// MultipliedGasMeter wraps the GasMeter from context and multiplies all reads by out defined multiplier
type MultipliedGasMeter struct {
	originalMeter sdk.GasMeter
	GasRegister   GasRegister
}

func NewMultipliedGasMeter(originalMeter sdk.GasMeter, gr GasRegister) MultipliedGasMeter {
	return MultipliedGasMeter{originalMeter: originalMeter, GasRegister: gr}
}

var _ wasmvm.GasMeter = MultipliedGasMeter{}

func (m MultipliedGasMeter) GasConsumed() sdk.Gas {
	return m.GasRegister.ToWasmVMGas(m.originalMeter.GasConsumed())
}

func (k Keeper) gasMeter(ctx sdk.Context) MultipliedGasMeter {
	return NewMultipliedGasMeter(ctx.GasMeter(), k.gasRegister)
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return moduleLogger(ctx)
}

func moduleLogger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// Querier creates a new grpc querier instance
func Querier(k *Keeper) *grpcQuerier { //nolint:revive
	return NewGrpcQuerier(k.cdc, k.storeKey, k, k.queryGasLimit, k.paramsKeeper)
}

// QueryGasLimit returns the gas limit for smart queries.
func (k Keeper) QueryGasLimit() sdk.Gas {
	return k.queryGasLimit
}

// BankCoinTransferrer replicates the cosmos-sdk behaviour as in
// https://github.com/cosmos/cosmos-sdk/blob/v0.41.4/x/bank/keeper/msg_server.go#L26
type BankCoinTransferrer struct {
	keeper types.BankKeeper
}

func NewBankCoinTransferrer(keeper types.BankKeeper) BankCoinTransferrer {
	return BankCoinTransferrer{
		keeper: keeper,
	}
}

// TransferCoins transfers coins from source to destination account when coin send was enabled for them and the recipient
// is not in the blocked address list.
func (c BankCoinTransferrer) TransferCoins(parentCtx sdk.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amount sdk.Coins) error {
	em := sdk.NewEventManager()
	ctx := parentCtx.WithEventManager(em)
	if err := c.keeper.IsSendEnabledCoins(ctx, amount...); err != nil {
		return err
	}
	if c.keeper.BlockedAddr(toAddr) {
		return sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "%s is not allowed to receive funds", toAddr.String())
	}

	sdkerr := c.keeper.SendCoins(ctx, fromAddr, toAddr, amount)
	if sdkerr != nil {
		return sdkerr
	}
	for _, e := range em.Events() {
		if e.Type == sdk.EventTypeMessage { // skip messages as we talk to the keeper directly
			continue
		}
		parentCtx.EventManager().EmitEvent(e)
	}
	return nil
}

type msgDispatcher interface {
	DispatchSubmessages(ctx sdk.Context, contractAddr sdk.AccAddress, ibcPort string, msgs []wasmvmtypes.SubMsg, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) ([]byte, error)
}

// DefaultWasmVMContractResponseHandler default implementation that first dispatches submessage then normal messages.
// The Submessage execution may include an success/failure response handling by the contract that can overwrite the
// original
type DefaultWasmVMContractResponseHandler struct {
	md msgDispatcher
}

func NewDefaultWasmVMContractResponseHandler(md msgDispatcher) *DefaultWasmVMContractResponseHandler {
	return &DefaultWasmVMContractResponseHandler{md: md}
}

// Handle processes the data returned by a contract invocation.
func (h DefaultWasmVMContractResponseHandler) Handle(ctx sdk.Context, contractAddr sdk.AccAddress, ibcPort string, messages []wasmvmtypes.SubMsg, origRspData []byte, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) ([]byte, error) {
	result := origRspData
	switch rsp, err := h.md.DispatchSubmessages(ctx, contractAddr, ibcPort, messages, info, codeInfo); {
	case err != nil:
		return nil, sdkerrors.Wrap(err, "submessages")
	case rsp != nil:
		result = rsp
	}
	return result, nil
}
