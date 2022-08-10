package contract

import (
	"context"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/utils/tracing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/store/whitelist/multi"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	dexkeeperabci "github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	dexkeeperutils "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dextypeswasm "github.com/sei-protocol/sei-chain/x/dex/types/wasm"
	"github.com/sei-protocol/sei-chain/x/store"
	otrace "go.opentelemetry.io/otel/trace"
)

type environment struct {
	validContractsInfo          []types.ContractInfo
	failedContractAddresses     datastructures.SyncSet[string]
	finalizeBlockMessages       *datastructures.TypedSyncMap[string, *dextypeswasm.SudoFinalizeBlockMsg]
	settlementsByContract       *datastructures.TypedSyncMap[string, []*types.SettlementEntry]
	executionTerminationSignals *datastructures.TypedSyncMap[string, chan struct{}]

	finalizeMsgMutex *sync.Mutex
}

func EndBlockerAtomic(ctx sdk.Context, keeper *keeper.Keeper, validContractsInfo []types.ContractInfo, tracingInfo *tracing.Info) ([]types.ContractInfo, bool) {
	tracer := tracingInfo.Tracer
	spanCtx, span := (*tracer).Start(tracingInfo.TracerContext, "DexEndBlockerAtomic")
	defer span.End()
	env := newEnv(validContractsInfo)
	ctx, msCached := cacheAndDecorateContext(ctx, env)
	memStateCopy := keeper.MemState.DeepCopy()

	handleDeposits(ctx, env, keeper, tracer)

	runner := NewParallelRunner(func(contract types.ContractInfo) {
		orderMatchingRunnable(ctx, env, keeper, contract, tracer, spanCtx)
	}, validContractsInfo)
	runner.Run()

	handleSettlements(spanCtx, ctx, env, keeper, tracer)
	handleFinalizedBlocks(spanCtx, ctx, env, keeper, tracer)

	// No error is thrown for any contract. This should happen most of the time.
	if env.failedContractAddresses.Size() == 0 {
		msCached.Write()
		return env.validContractsInfo, true
	}
	// restore keeper in-memory state
	*keeper.MemState = *memStateCopy

	return filterNewValidContracts(env, keeper), false
}

func newEnv(validContractsInfo []types.ContractInfo) *environment {
	finalizeBlockMessages := datastructures.NewTypedSyncMap[string, *dextypeswasm.SudoFinalizeBlockMsg]()
	settlementsByContract := datastructures.NewTypedSyncMap[string, []*types.SettlementEntry]()
	executionTerminationSignals := datastructures.NewTypedSyncMap[string, chan struct{}]()
	for _, contract := range validContractsInfo {
		finalizeBlockMessages.Store(contract.ContractAddr, dextypeswasm.NewSudoFinalizeBlockMsg())
		settlementsByContract.Store(contract.ContractAddr, []*types.SettlementEntry{})
		executionTerminationSignals.Store(contract.ContractAddr, make(chan struct{}, 1))
	}
	return &environment{
		validContractsInfo:          validContractsInfo,
		failedContractAddresses:     datastructures.NewSyncSet([]string{}),
		finalizeBlockMessages:       finalizeBlockMessages,
		settlementsByContract:       settlementsByContract,
		executionTerminationSignals: executionTerminationSignals,
		finalizeMsgMutex:            &sync.Mutex{},
	}
}

func cacheAndDecorateContext(ctx sdk.Context, env *environment) (sdk.Context, sdk.CacheMultiStore) {
	cachedCtx, msCached := store.GetCachedContext(ctx)
	goCtx := context.WithValue(cachedCtx.Context(), dexcache.CtxKeyExecTermSignal, env.executionTerminationSignals)
	cachedCtx = cachedCtx.WithContext(goCtx)
	return cachedCtx, msCached
}

func decorateContextForContract(ctx sdk.Context, contractInfo types.ContractInfo) sdk.Context {
	goCtx := context.WithValue(ctx.Context(), dexcache.CtxKeyExecutingContract, contractInfo)
	return ctx.WithContext(goCtx).WithMultiStore(multi.NewStore(ctx.MultiStore(), GetWhitelistMap(contractInfo.ContractAddr)))
}

func handleDeposits(ctx sdk.Context, env *environment, keeper *keeper.Keeper, tracer *otrace.Tracer) {
	// Handle deposit sequentially since they mutate `bank` state which is shared by all contracts
	keeperWrapper := dexkeeperabci.KeeperWrapper{Keeper: keeper}
	for _, contract := range env.validContractsInfo {
		if err := keeperWrapper.HandleEBDeposit(ctx.Context(), ctx, tracer, contract.ContractAddr); err != nil {
			env.failedContractAddresses.Add(contract.ContractAddr)
		}
	}
}

func handleSettlements(ctx context.Context, sdkCtx sdk.Context, env *environment, keeper *keeper.Keeper, tracer *otrace.Tracer) {
	_, span := (*tracer).Start(ctx, "DexEndBlockerHandleSettlements")
	defer span.End()
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
			env.failedContractAddresses.Add(contractAddr)
		}
		return true
	})
}

func handleFinalizedBlocks(ctx context.Context, sdkCtx sdk.Context, env *environment, keeper *keeper.Keeper, tracer *otrace.Tracer) {
	_, span := (*tracer).Start(ctx, "DexEndBlockerHandleFinalizedBlocks")
	defer span.End()
	contractsNeedHook := datastructures.NewSyncSet([]string{})
	for _, contract := range env.validContractsInfo {
		if contract.NeedHook {
			contractsNeedHook.Add(contract.ContractAddr)
		}
	}
	env.finalizeBlockMessages.Range(func(contractAddr string, finalizeBlockMsg *dextypeswasm.SudoFinalizeBlockMsg) bool {
		if !contractsNeedHook.Contains(contractAddr) {
			return true
		}
		if _, err := dexkeeperutils.CallContractSudo(sdkCtx, keeper, contractAddr, finalizeBlockMsg); err != nil {
			sdkCtx.Logger().Error(fmt.Sprintf("Error calling FinalizeBlock of %s", contractAddr))
			env.failedContractAddresses.Add(contractAddr)
		}
		return true
	})
}

func orderMatchingRunnable(ctx sdk.Context, env *environment, keeper *keeper.Keeper, contractInfo types.ContractInfo, tracer *otrace.Tracer, spanCtx context.Context) {
	defer utils.PanicHandler(func(err any) { orderMatchingRecoverCallback(err, ctx, env, contractInfo) })()
	defer func() {
		if channel, ok := env.executionTerminationSignals.Load(contractInfo.ContractAddr); ok {
			channel <- struct{}{}
		}
	}()

	if !contractInfo.NeedOrderMatching {
		return
	}
	ctx = decorateContextForContract(ctx, contractInfo)
	ctx.Logger().Info(fmt.Sprintf("End block for %s", contractInfo.ContractAddr))
	if orderResultsMap, settlements, err := HandleExecutionForContract(ctx, contractInfo, keeper, tracer, spanCtx); err != nil {
		ctx.Logger().Error(fmt.Sprintf("Error for EndBlock of %s", contractInfo.ContractAddr))
		env.failedContractAddresses.Add(contractInfo.ContractAddr)
	} else {
		for account, orderResults := range orderResultsMap {
			// only add to finalize message for contract addresses
			if msg, ok := env.finalizeBlockMessages.Load(account); ok {
				env.finalizeMsgMutex.Lock()
				msg.AddContractResult(orderResults)
				env.finalizeMsgMutex.Unlock()
			}
		}
		env.settlementsByContract.Store(contractInfo.ContractAddr, settlements)
	}
}

func orderMatchingRecoverCallback(err any, ctx sdk.Context, env *environment, contractInfo types.ContractInfo) {
	utils.MetricsPanicCallback(err, ctx, fmt.Sprintf("%s%s", types.ModuleName, "endblockpanic"))
	// idempotent
	env.failedContractAddresses.Add(contractInfo.ContractAddr)
}

func filterNewValidContracts(env *environment, keeper *keeper.Keeper) []types.ContractInfo {
	newValidContracts := []types.ContractInfo{}
	for _, contract := range env.validContractsInfo {
		if !env.failedContractAddresses.Contains(contract.ContractAddr) {
			newValidContracts = append(newValidContracts, contract)
		}
	}
	for _, failedContractAddress := range env.failedContractAddresses.ToOrderedSlice(datastructures.StringComparator) {
		keeper.MemState.DeepFilterAccount(failedContractAddress)
	}
	return newValidContracts
}
