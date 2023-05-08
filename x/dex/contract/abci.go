package contract

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	seiutils "github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/logging"
	"github.com/sei-protocol/sei-chain/utils/tracing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	"github.com/sei-protocol/sei-chain/store/whitelist/multi"
	seisync "github.com/sei-protocol/sei-chain/sync"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	dexkeeperabci "github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	dexkeeperutils "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	"github.com/sei-protocol/sei-chain/x/store"
	otrace "go.opentelemetry.io/otel/trace"
)

const LogRunnerRunAfter = 10 * time.Second
const LogExecSigSendAfter = 2 * time.Second

type environment struct {
	validContractsInfo          []types.ContractInfoV2
	failedContractAddresses     datastructures.SyncSet[string]
	outOfRentContractAddresses  datastructures.SyncSet[string]
	settlementsByContract       *datastructures.TypedSyncMap[string, []*types.SettlementEntry]
	executionTerminationSignals *datastructures.TypedSyncMap[string, chan struct{}]
	registeredPairs             *datastructures.TypedSyncMap[string, []types.Pair]
	orderBooks                  *datastructures.TypedNestedSyncMap[string, types.PairString, *types.OrderBook]

	finalizeMsgMutex  *sync.Mutex
	eventManagerMutex *sync.Mutex
}

func EndBlockerAtomic(ctx sdk.Context, keeper *keeper.Keeper, validContractsInfo []types.ContractInfoV2, tracingInfo *tracing.Info) ([]types.ContractInfoV2, []types.ContractInfoV2, sdk.Context, bool) {
	tracer := tracingInfo.Tracer
	spanCtx, span := tracingInfo.Start("DexEndBlockerAtomic")
	defer span.End()
	defer telemetry.MeasureSince(time.Now(), "dex", "end_blocker_atomic")

	env := newEnv(ctx, validContractsInfo, keeper)
	cachedCtx, msCached := cacheContext(ctx, env)
	memStateCopy := dexutils.GetMemState(cachedCtx.Context()).DeepCopy()
	preRunRents := keeper.GetRentsForContracts(cachedCtx, seiutils.Map(validContractsInfo, func(c types.ContractInfoV2) string { return c.ContractAddr }))

	handleDeposits(spanCtx, cachedCtx, env, keeper, tracer)

	runner := NewParallelRunner(func(contract types.ContractInfoV2) {
		orderMatchingRunnable(spanCtx, cachedCtx, env, keeper, contract, tracer)
	}, validContractsInfo, cachedCtx)

	_, err := logging.LogIfNotDoneAfter(ctx.Logger(), func() (struct{}, error) {
		runner.Run()
		return struct{}{}, nil
	}, LogRunnerRunAfter, "runner run")
	if err != nil {
		// this should never happen
		panic(err)
	}

	handleSettlements(spanCtx, cachedCtx, env, keeper, tracer)
	handleUnfulfilledMarketOrders(spanCtx, cachedCtx, env, keeper, tracer)

	telemetry.IncrCounter(float32(env.failedContractAddresses.Size()), "dex", "total_failed_contracts")
	// No error is thrown for any contract. This should happen most of the time.
	if env.failedContractAddresses.Size() == 0 {
		postRunRents := keeper.GetRentsForContracts(cachedCtx, seiutils.Map(validContractsInfo, func(c types.ContractInfoV2) string { return c.ContractAddr }))
		TransferRentFromDexToCollector(ctx, keeper.BankKeeper, preRunRents, postRunRents)
		msCached.Write()
		return env.validContractsInfo, []types.ContractInfoV2{}, ctx, true
	}

	// persistent contract rent charges for failed contracts and discard everything else
	for _, failedContractAddress := range env.failedContractAddresses.ToOrderedSlice(datastructures.StringComparator) {
		cachedContract, err := keeper.GetContract(cachedCtx, failedContractAddress)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error %s when getting updated contract %s to persist rent balance", err, failedContractAddress))
			continue
		}
		contract, err := keeper.GetContract(ctx, failedContractAddress)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error %s when getting contract %s to persist rent balance", err, failedContractAddress))
			continue
		}
		contract.RentBalance = cachedContract.RentBalance
		err = keeper.SetContract(ctx, &contract)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error %s when persisting contract %s's rent balance", err, failedContractAddress))
			continue
		}
	}

	// restore keeper in-memory state
	newGoContext := context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, memStateCopy)
	return filterNewValidContracts(ctx, env), getOutOfRentContracts(env), ctx.WithContext(newGoContext), false
}

func newEnv(ctx sdk.Context, validContractsInfo []types.ContractInfoV2, keeper *keeper.Keeper) *environment {
	settlementsByContract := datastructures.NewTypedSyncMap[string, []*types.SettlementEntry]()
	executionTerminationSignals := datastructures.NewTypedSyncMap[string, chan struct{}]()
	registeredPairs := datastructures.NewTypedSyncMap[string, []types.Pair]()
	allContractAndPairs := map[string][]types.Pair{}
	for _, contract := range validContractsInfo {
		settlementsByContract.Store(contract.ContractAddr, []*types.SettlementEntry{})
		executionTerminationSignals.Store(contract.ContractAddr, make(chan struct{}, 1))
		contractPairs := keeper.GetAllRegisteredPairs(ctx, contract.ContractAddr)
		registeredPairs.Store(contract.ContractAddr, contractPairs)
		allContractAndPairs[contract.ContractAddr] = contractPairs
	}
	// Parallelize populating orderbooks for performance improvements
	orderBooks := dexkeeperutils.PopulateAllOrderbooks(ctx, keeper, allContractAndPairs)
	return &environment{
		validContractsInfo:          validContractsInfo,
		failedContractAddresses:     datastructures.NewSyncSet([]string{}),
		outOfRentContractAddresses:  datastructures.NewSyncSet([]string{}),
		settlementsByContract:       settlementsByContract,
		executionTerminationSignals: executionTerminationSignals,
		registeredPairs:             registeredPairs,
		orderBooks:                  orderBooks,
		finalizeMsgMutex:            &sync.Mutex{},
		eventManagerMutex:           &sync.Mutex{},
	}
}

func (e *environment) addError(contractAddr string, err error) {
	if err == types.ErrInsufficientRent {
		e.outOfRentContractAddresses.Add(contractAddr)
		return
	}
	e.failedContractAddresses.Add(contractAddr)
}

func cacheContext(ctx sdk.Context, env *environment) (sdk.Context, sdk.CacheMultiStore) {
	cachedCtx, msCached := store.GetCachedContext(ctx)
	goCtx := context.WithValue(cachedCtx.Context(), dexcache.CtxKeyExecTermSignal, env.executionTerminationSignals)
	cachedCtx = cachedCtx.WithContext(goCtx)
	return cachedCtx, msCached
}

func decorateContextForContract(ctx sdk.Context, contractInfo types.ContractInfoV2, gasLimit uint64) sdk.Context {
	goCtx := context.WithValue(ctx.Context(), dexcache.CtxKeyExecutingContract, contractInfo)
	whitelistedStore := multi.NewStore(ctx.MultiStore(), GetWhitelistMap(contractInfo.ContractAddr))
	newEventManager := sdk.NewEventManager()
	return ctx.WithContext(goCtx).WithMultiStore(whitelistedStore).WithEventManager(newEventManager).WithGasMeter(
		seisync.NewGasWrapper(dexutils.GetGasMeterForLimit(gasLimit)),
	)
}

func handleDeposits(spanCtx context.Context, ctx sdk.Context, env *environment, keeper *keeper.Keeper, tracer *otrace.Tracer) {
	// Handle deposit sequentially since they mutate `bank` state which is shared by all contracts
	_, span := (*tracer).Start(spanCtx, "handleDeposits")
	defer span.End()
	defer telemetry.MeasureSince(time.Now(), "dex", "handle_deposits")
	keeperWrapper := dexkeeperabci.KeeperWrapper{Keeper: keeper}
	for _, contract := range env.validContractsInfo {
		if !contract.NeedOrderMatching {
			continue
		}
		if err := keeperWrapper.HandleEBDeposit(spanCtx, ctx, tracer, contract.ContractAddr); err != nil {
			env.addError(contract.ContractAddr, err)
		}
	}
}

func handleSettlements(ctx context.Context, sdkCtx sdk.Context, env *environment, keeper *keeper.Keeper, tracer *otrace.Tracer) {
	_, span := (*tracer).Start(ctx, "DexEndBlockerHandleSettlements")
	defer span.End()
	defer telemetry.MeasureSince(time.Now(), "dex", "handle_settlements")
	contractsNeedOrderMatching := datastructures.NewSyncSet([]string{})
	for _, contract := range env.validContractsInfo {
		if contract.NeedOrderMatching {
			contractsNeedOrderMatching.Add(contract.ContractAddr)
		}
	}
	env.settlementsByContract.Range(func(contractAddr string, settlements []*types.SettlementEntry) bool {
		if !contractsNeedOrderMatching.Contains(contractAddr) {
			return true
		}
		if err := HandleSettlements(sdkCtx, contractAddr, keeper, settlements); err != nil {
			sdkCtx.Logger().Error(fmt.Sprintf("Error handling settlements for %s", contractAddr))
			env.addError(contractAddr, err)
		}
		return true
	})
}

func handleUnfulfilledMarketOrders(ctx context.Context, sdkCtx sdk.Context, env *environment, keeper *keeper.Keeper, tracer *otrace.Tracer) {
	// Cancel unfilled market orders
	defer telemetry.MeasureSince(time.Now(), "dex", "handle_unfulfilled_market_orders")
	for _, contract := range env.validContractsInfo {
		if contract.NeedOrderMatching {
			registeredPairs, found := env.registeredPairs.Load(contract.ContractAddr)
			if !found {
				continue
			}
			if err := CancelUnfulfilledMarketOrders(ctx, sdkCtx, contract.ContractAddr, keeper, registeredPairs, tracer); err != nil {
				sdkCtx.Logger().Error(fmt.Sprintf("Error cancelling unfulfilled market orders for %s", contract.ContractAddr))
				env.addError(contract.ContractAddr, err)
			}
		}
	}
}

func orderMatchingRunnable(ctx context.Context, sdkContext sdk.Context, env *environment, keeper *keeper.Keeper, contractInfo types.ContractInfoV2, tracer *otrace.Tracer) {
	_, span := (*tracer).Start(ctx, "orderMatchingRunnable")
	defer span.End()
	defer telemetry.MeasureSince(time.Now(), "dex", "order_matching_runnable")
	defer func() {
		if channel, ok := env.executionTerminationSignals.Load(contractInfo.ContractAddr); ok {
			_, err := logging.LogIfNotDoneAfter(sdkContext.Logger(), func() (struct{}, error) {
				channel <- struct{}{}
				return struct{}{}, nil
			}, LogExecSigSendAfter, fmt.Sprintf("send execution terminal signal for %s", contractInfo.ContractAddr))
			if err != nil {
				// this should never happen
				panic(err)
			}
		}
	}()
	if !contractInfo.NeedOrderMatching {
		return
	}
	parentSdkContext := sdkContext
	sdkContext = decorateContextForContract(sdkContext, contractInfo, keeper.GetParams(sdkContext).EndBlockGasLimit)
	sdkContext.Logger().Debug(fmt.Sprintf("End block for %s with balance of %d", contractInfo.ContractAddr, contractInfo.RentBalance))
	pairs, pairFound := env.registeredPairs.Load(contractInfo.ContractAddr)
	orderBooks, found := env.orderBooks.Load(contractInfo.ContractAddr)

	if !pairFound || !found {
		sdkContext.Logger().Error(fmt.Sprintf("No pair or order book for %s", contractInfo.ContractAddr))
		env.addError(contractInfo.ContractAddr, errors.New("no pair found (internal error)"))
	} else if settlements, err := HandleExecutionForContract(ctx, sdkContext, contractInfo, keeper, pairs, orderBooks, tracer); err != nil {
		sdkContext.Logger().Error(fmt.Sprintf("Error for EndBlock of %s", contractInfo.ContractAddr))
		env.addError(contractInfo.ContractAddr, err)
	} else {
		env.settlementsByContract.Store(contractInfo.ContractAddr, settlements)
	}

	// ordering of events doesn't matter since events aren't part of consensus
	env.eventManagerMutex.Lock()
	defer env.eventManagerMutex.Unlock()
	parentSdkContext.EventManager().EmitEvents(sdkContext.EventManager().Events())
}

func filterNewValidContracts(ctx sdk.Context, env *environment) []types.ContractInfoV2 {
	newValidContracts := []types.ContractInfoV2{}
	for _, contract := range env.validContractsInfo {
		if !env.failedContractAddresses.Contains(contract.ContractAddr) && !env.outOfRentContractAddresses.Contains(contract.ContractAddr) {
			newValidContracts = append(newValidContracts, contract)
		}
	}
	for _, failedContractAddress := range env.failedContractAddresses.ToOrderedSlice(datastructures.StringComparator) {
		dexutils.GetMemState(ctx.Context()).DeepFilterAccount(ctx, failedContractAddress)
	}
	for _, outOfRentContractAddress := range env.outOfRentContractAddresses.ToOrderedSlice(datastructures.StringComparator) {
		dexutils.GetMemState(ctx.Context()).DeepFilterAccount(ctx, outOfRentContractAddress)
	}
	return newValidContracts
}

func getOutOfRentContracts(env *environment) []types.ContractInfoV2 {
	outOfRentContracts := []types.ContractInfoV2{}
	for _, contract := range env.validContractsInfo {
		if env.outOfRentContractAddresses.Contains(contract.ContractAddr) {
			outOfRentContracts = append(outOfRentContracts, contract)
		}
	}
	return outOfRentContracts
}

func TransferRentFromDexToCollector(ctx sdk.Context, bankKeeper bankkeeper.Keeper, preRents map[string]uint64, postRents map[string]uint64) {
	total := uint64(0)
	for addr, preRent := range preRents {
		if postRent, ok := postRents[addr]; ok {
			total += preRent - postRent
		} else {
			total += preRent
		}
	}
	if err := bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, authtypes.FeeCollectorName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(int64(total))))); err != nil {
		ctx.Logger().Error("sending coints from dex to fee collector failed due to %s", err)
	}
}
