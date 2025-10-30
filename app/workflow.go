package app

// Block Processing Workflow Architecture
//
// This file defines a workflow-based architecture for block processing that separates
// concerns into clear, testable stages:
//
//   1. IngestStage       - Block reception and transaction decoding
//   2. PreProcessStage   - Stateless validation and EVM sender recovery
//   3. ExecuteStage      - BeginBlock, DeliverTx (prioritized + regular), MidBlock, EndBlock
//   4. PostProcessStage  - Event aggregation, gas calculation, state updates
//   5. FlushStateStage   - WriteState, invariance checks, prepare for commit
//
// The workflow is orchestrated by ProcessBlockWorkflow, which maintains backward
// compatibility with the existing ProcessBlock method.
//
// Usage:
//   - ProcessBlock() automatically uses the workflow (backward compatible)
//   - ProcessBlockWorkflow() can be called directly for more control
//   - Individual stages can be tested and replaced independently
//   - FlushStateStage is typically called after ProcessBlockWorkflow in FinalizeBlocker
//
// Example of replacing a stage:
//   - Override IngestStage to add custom decoding logic
//   - Override PreProcessStage to add custom validation
//   - Each stage returns an error, making error handling clear

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// BlockWorkflow holds the state that flows through the block processing stages
type BlockWorkflow struct {
	// Inputs (set at start)
	Ctx        sdk.Context
	Txs        [][]byte
	Req        BlockProcessRequest
	LastCommit abci.CommitInfo
	Simulate   bool

	// Stage outputs
	TypedTxs            []sdk.Tx                      // Output from Ingest
	EvmTxs              []*evmtypes.MsgEVMTransaction // Output from PreProcess
	PrioritizedTxs      [][]byte                      // Output from PreProcess
	OtherTxs            [][]byte                      // Output from PreProcess
	PrioritizedTypedTxs []sdk.Tx                      // Output from PreProcess
	OtherTypedTxs       []sdk.Tx                      // Output from PreProcess
	PrioritizedIndices  []int                         // Output from PreProcess
	OtherIndices        []int                         // Output from PreProcess

	// Execution outputs
	BeginBlockResp     abci.ResponseBeginBlock
	PrioritizedResults []*abci.ExecTxResult
	OtherResults       []*abci.ExecTxResult
	TxResults          []*abci.ExecTxResult
	MidBlockEvents     []abci.Event
	EndBlockResp       abci.ResponseEndBlock

	// Final outputs
	Events  []abci.Event
	AppHash []byte
}

// IngestStage handles block reception and transaction decoding
// This stage decodes raw transaction bytes into typed transactions
func (app *App) IngestStage(wf *BlockWorkflow) error {
	wf.Ctx.Logger().Debug("Starting Ingest stage")

	// Decode all transactions concurrently
	wf.TypedTxs = app.DecodeTransactionsConcurrently(wf.Ctx, wf.Txs)
	wf.EvmTxs = make([]*evmtypes.MsgEVMTransaction, len(wf.Txs))

	wf.Ctx.Logger().Debug("Completed Ingest stage", "txCount", len(wf.TypedTxs))
	return nil
}

// PreProcessStage handles stateless validation and EVM sender recovery
// This stage performs validation that doesn't require state access
func (app *App) PreProcessStage(wf *BlockWorkflow) error {
	wf.Ctx.Logger().Debug("Starting PreProcess stage")

	// Partition transactions into prioritized and regular
	wf.PrioritizedTxs, wf.OtherTxs, wf.PrioritizedTypedTxs, wf.OtherTypedTxs,
		wf.PrioritizedIndices, wf.OtherIndices = app.PartitionPrioritizedTxs(
		wf.Ctx, wf.Txs, wf.TypedTxs)

	// Prepare EVM transaction tracking arrays
	wf.TxResults = make([]*abci.ExecTxResult, len(wf.Txs))

	wf.Ctx.Logger().Debug("Completed PreProcess stage",
		"prioritizedCount", len(wf.PrioritizedTxs),
		"otherCount", len(wf.OtherTxs))
	return nil
}

// ExecuteStage handles transaction execution
// This stage runs BeginBlock, DeliverTx for each transaction, MidBlock, and EndBlock
func (app *App) ExecuteStage(wf *BlockWorkflow) error {
	wf.Ctx.Logger().Debug("Starting Execute stage")

	// Prepare BeginBlock request
	beginBlockReq := abci.RequestBeginBlock{
		Hash: wf.Req.GetHash(),
		ByzantineValidators: utils.Map(wf.Req.GetByzantineValidators(), func(mis abci.Misbehavior) abci.Evidence {
			return abci.Evidence(mis)
		}),
		LastCommitInfo: abci.LastCommitInfo{
			Round: wf.LastCommit.Round,
			Votes: utils.Map(wf.LastCommit.Votes, func(vote abci.VoteInfo) abci.VoteInfo {
				return abci.VoteInfo{
					Validator:       vote.Validator,
					SignedLastBlock: vote.SignedLastBlock,
				}
			}),
		},
		Header: tmproto.Header{
			ChainID:         app.ChainID,
			Height:          wf.Req.GetHeight(),
			Time:            wf.Req.GetTime(),
			ProposerAddress: wf.Ctx.BlockHeader().ProposerAddress,
		},
		Simulate: wf.Simulate,
	}

	// BeginBlock
	wf.BeginBlockResp = app.BeginBlock(wf.Ctx, beginBlockReq)
	wf.Events = append(wf.Events, wf.BeginBlockResp.Events...)

	// Execute prioritized transactions
	if len(wf.PrioritizedTxs) > 0 {
		wf.PrioritizedResults, wf.Ctx = app.ExecuteTxsConcurrently(
			wf.Ctx, wf.PrioritizedTxs, wf.PrioritizedTypedTxs, wf.PrioritizedIndices)
		for relativePrioritizedIndex, originalIndex := range wf.PrioritizedIndices {
			wf.TxResults[originalIndex] = wf.PrioritizedResults[relativePrioritizedIndex]
			wf.EvmTxs[originalIndex] = app.GetEVMMsg(wf.PrioritizedTypedTxs[relativePrioritizedIndex])
		}

		// Finalize Bank Module Transfers for prioritized txs
		deferredWriteEvents := app.BankKeeper.WriteDeferredBalances(wf.Ctx)
		wf.Events = append(wf.Events, deferredWriteEvents...)
	}

	// MidBlock
	wf.MidBlockEvents = app.MidBlock(wf.Ctx, wf.Req.GetHeight())
	wf.Events = append(wf.Events, wf.MidBlockEvents...)

	// Execute other transactions
	if len(wf.OtherTxs) > 0 {
		wf.OtherResults, wf.Ctx = app.ExecuteTxsConcurrently(
			wf.Ctx, wf.OtherTxs, wf.OtherTypedTxs, wf.OtherIndices)
		for relativeOtherIndex, originalIndex := range wf.OtherIndices {
			wf.TxResults[originalIndex] = wf.OtherResults[relativeOtherIndex]
			wf.EvmTxs[originalIndex] = app.GetEVMMsg(wf.OtherTypedTxs[relativeOtherIndex])
		}

		// Finalize Bank Module Transfers for other txs
		lazyWriteEvents := app.BankKeeper.WriteDeferredBalances(wf.Ctx)
		wf.Events = append(wf.Events, lazyWriteEvents...)
	}

	wf.Ctx.Logger().Debug("Completed Execute stage")
	return nil
}

// PostProcessStage handles post-execution tasks like event aggregation and state updates
func (app *App) PostProcessStage(wf *BlockWorkflow) error {
	wf.Ctx.Logger().Debug("Starting PostProcess stage")

	// Set EVM keeper state
	app.EvmKeeper.SetTxResults(wf.TxResults)
	app.EvmKeeper.SetMsgs(wf.EvmTxs)

	// Calculate total EVM gas used
	evmTotalGasUsed := int64(0)
	for _, txResult := range wf.TxResults {
		if txResult.EvmTxInfo != nil {
			evmTotalGasUsed += txResult.GasUsed
		}
	}

	// EndBlock
	wf.EndBlockResp = app.EndBlock(wf.Ctx, abci.RequestEndBlock{
		Height:       wf.Req.GetHeight(),
		BlockGasUsed: evmTotalGasUsed,
	})
	wf.Events = append(wf.Events, wf.EndBlockResp.Events...)

	wf.Ctx.Logger().Debug("Completed PostProcess stage")
	return nil
}

// FlushStateStage prepares state for commit (WriteState, but not the actual Commit call)
// The actual Commit call happens separately via ABCI Commit
func (app *App) FlushStateStage(wf *BlockWorkflow) error {
	wf.Ctx.Logger().Debug("Starting FlushState stage")

	// Set deliver state to commit state
	app.SetDeliverStateToCommit()

	// Write state (prepares state for commit)
	cms := app.WriteState()

	// Light invariance checks
	app.LightInvarianceChecks(cms, app.lightInvarianceConfig)

	// Get the app hash
	wf.AppHash = app.GetWorkingHash()

	wf.Ctx.Logger().Debug("Completed FlushState stage")
	return nil
}

// ProcessBlockWorkflow orchestrates all the stages in order
// This replaces the previous ProcessBlock method with a clearer workflow structure
func (app *App) ProcessBlockWorkflow(
	ctx sdk.Context,
	txs [][]byte,
	req BlockProcessRequest,
	lastCommit abci.CommitInfo,
	simulate bool,
) (events []abci.Event, txResults []*abci.ExecTxResult, endBlockResp abci.ResponseEndBlock, err error) {
	defer func() {
		if r := recover(); r != nil {
			panicMsg := fmt.Sprintf("%v", r)
			// Re-panic for upgrade-related panics to allow proper upgrade mechanism
			if upgradePanicRe.MatchString(panicMsg) {
				ctx.Logger().Error("upgrade panic detected, panicking to trigger upgrade", "panic", r)
				panic(r) // Re-panic to trigger upgrade mechanism
			}
			ctx.Logger().Error("panic recovered in ProcessBlockWorkflow", "panic", r)
			err = fmt.Errorf("ProcessBlockWorkflow panic: %v", r)
			events = nil
			txResults = nil
			endBlockResp = abci.ResponseEndBlock{}
		}
	}()

	defer func() {
		if !app.httpServerStartSignalSent {
			app.httpServerStartSignalSent = true
			app.httpServerStartSignal <- struct{}{}
		}
		if !app.wsServerStartSignalSent {
			app.wsServerStartSignalSent = true
			app.wsServerStartSignal <- struct{}{}
		}
	}()

	ctx = ctx.WithIsOCCEnabled(app.OccEnabled())

	// Initialize workflow state
	wf := &BlockWorkflow{
		Ctx:        ctx,
		Txs:        txs,
		Req:        req,
		LastCommit: lastCommit,
		Simulate:   simulate,
		Events:     []abci.Event{},
	}

	// Execute workflow stages in order
	if err := app.IngestStage(wf); err != nil {
		return nil, nil, abci.ResponseEndBlock{}, fmt.Errorf("ingest stage failed: %w", err)
	}

	if err := app.PreProcessStage(wf); err != nil {
		return nil, nil, abci.ResponseEndBlock{}, fmt.Errorf("preprocess stage failed: %w", err)
	}

	if err := app.ExecuteStage(wf); err != nil {
		return nil, nil, abci.ResponseEndBlock{}, fmt.Errorf("execute stage failed: %w", err)
	}

	if err := app.PostProcessStage(wf); err != nil {
		return nil, nil, abci.ResponseEndBlock{}, fmt.Errorf("postprocess stage failed: %w", err)
	}

	// Note: FlushStateStage is typically called after ProcessBlockWorkflow,
	// from FinalizeBlocker. We don't call it here to maintain the existing
	// separation between ProcessBlock and state flushing.

	return wf.Events, wf.TxResults, wf.EndBlockResp, nil
}
