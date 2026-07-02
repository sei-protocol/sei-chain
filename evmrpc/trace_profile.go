package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

type traceExecutionPhaseDurations struct {
	PrepareTxNanos   int64 `json:"prepareTxNanos"`
	ExecutionNanos   int64 `json:"executionNanos"`
	TraceResultNanos int64 `json:"traceResultNanos"`
}

type TraceTransactionProfilePhases struct {
	LookupTransactionNanos   int64 `json:"lookupTransactionNanos"`
	LoadBlockNanos           int64 `json:"loadBlockNanos"`
	ReplayHistoricalTxsNanos int64 `json:"replayHistoricalTxsNanos"`
	BuildBlockContextNanos   int64 `json:"buildBlockContextNanos"`
	traceExecutionPhaseDurations
}

type TraceTransactionProfile struct {
	TotalNanos              int64                         `json:"totalNanos"`
	HistoricalDBLookupNanos int64                         `json:"historicalDbLookupNanos"`
	OtherNanos              int64                         `json:"otherNanos"`
	Phases                  TraceTransactionProfilePhases `json:"phases"`
	Store                   *sdk.StoreTraceDump           `json:"store,omitempty"`
}

type TraceTransactionProfileResponse struct {
	Trace   interface{}             `json:"trace"`
	Profile TraceTransactionProfile `json:"profile"`
}

func (api *DebugAPI) TraceTransactionProfile(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "debug_traceTransactionProfile", api.connectionType, startTime, returnErr, recover())
	}()

	if returnErr = api.guardHistoricalDebugTraceByTxHash(ctx, "debug_traceTransactionProfile", hash); returnErr != nil {
		return nil, returnErr
	}

	ctx, done, err := api.prepareTraceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	profileStart := time.Now()
	tracingBackend := api.newProfileTracingBackend()
	var phases TraceTransactionProfilePhases

	lookupStart := time.Now()
	found, tx, blockHash, _, index, err := tracingBackend.GetTransaction(ctx, hash)
	phases.LookupTransactionNanos = time.Since(lookupStart).Nanoseconds()
	if err != nil {
		return nil, err
	}
	if !found || tx == nil {
		return nil, errors.New("transaction not found")
	}

	loadBlockStart := time.Now()
	block, _, err := tracingBackend.BlockByHash(ctx, blockHash)
	phases.LoadBlockNanos = time.Since(loadBlockStart).Nanoseconds()
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("block %s not found", blockHash.Hex())
	}
	if block.NumberU64() == 0 {
		return nil, errors.New("genesis is not traceable")
	}

	replayStart := time.Now()
	statedb, _, err := tracingBackend.ReplayTransactionTillIndex(ctx, block, int(index)-1) //nolint:gosec
	phases.ReplayHistoricalTxsNanos = time.Since(replayStart).Nanoseconds()
	if err != nil {
		return nil, err
	}

	blockContextStart := time.Now()
	blockCtx, err := tracingBackend.GetBlockContext(ctx, block, statedb, tracingBackend)
	phases.BuildBlockContextNanos = time.Since(blockContextStart).Nanoseconds()
	if err != nil {
		return nil, fmt.Errorf("cannot get block context: %w", err)
	}

	signer := gethtypes.MakeSigner(tracingBackend.ChainConfig(), block.Number(), block.Time())
	msg, _ := core.TransactionToMessage(tx, signer, block.BaseFee())
	txctx := &tracers.Context{
		BlockHash:   blockHash,
		BlockNumber: block.Number(),
		TxIndex:     int(index), //nolint:gosec
		TxHash:      tx.Hash(),
	}

	if config == nil {
		config = &tracers.TraceConfig{}
	}
	api.clampDefaultStructLogLimit(config)
	traceResult, err := api.profiledTraceTx(ctx, tx, msg, txctx, blockCtx, statedb, config, nil, false, &phases.traceExecutionPhaseDurations)
	if err != nil {
		return nil, err
	}

	storeDump := dumpStoreTrace(statedb)
	historicalLookupNanos := historicalLookupNanos(storeDump)
	totalNanos := time.Since(profileStart).Nanoseconds()
	otherNanos := totalNanos - historicalLookupNanos - phases.ExecutionNanos
	if otherNanos < 0 {
		otherNanos = 0
	}

	return TraceTransactionProfileResponse{
		Trace: traceResult,
		Profile: TraceTransactionProfile{
			TotalNanos:              totalNanos,
			HistoricalDBLookupNanos: historicalLookupNanos,
			OtherNanos:              otherNanos,
			Phases:                  phases,
			Store:                   storeDump,
		},
	}, nil
}

func (api *DebugAPI) newProfileTracingBackend() *Backend {
	tracingBackend := *api.backend
	tracingBackend.ctxProvider = func(height int64) sdk.Context {
		return api.ctxProvider(height).WithIsTracing(true)
	}
	return &tracingBackend
}

func dumpStoreTrace(statedb vm.StateDB) *sdk.StoreTraceDump {
	typedStateDB := state.GetDBImpl(statedb)
	if typedStateDB == nil || typedStateDB.Ctx().StoreTracer() == nil {
		return nil
	}
	if dumpable, ok := typedStateDB.Ctx().StoreTracer().(interface{ Dump() sdk.StoreTraceDump }); ok {
		dump := dumpable.Dump()
		return &dump
	}
	return nil
}

func historicalLookupNanos(storeDump *sdk.StoreTraceDump) int64 {
	if storeDump == nil {
		return 0
	}
	var total int64
	for _, key := range []string{"get", "has", "iterator", "iteratorNext"} {
		total += storeDump.Stats[key].TotalNanos
	}
	return total
}
