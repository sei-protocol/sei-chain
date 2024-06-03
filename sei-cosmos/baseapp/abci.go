package baseapp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/armon/go-metrics"
	"github.com/gogo/protobuf/proto"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/cosmos/cosmos-sdk/codec"
	snapshottypes "github.com/cosmos/cosmos-sdk/snapshots/types"
	"github.com/cosmos/cosmos-sdk/tasks"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/legacytm"
	"github.com/cosmos/cosmos-sdk/utils"
)

// InitChain implements the ABCI interface. It runs the initialization logic
// directly on the CommitMultiStore.
func (app *BaseApp) InitChain(ctx context.Context, req *abci.RequestInitChain) (res *abci.ResponseInitChain, err error) {
	// On a new chain, we consider the init chain block height as 0, even though
	// req.InitialHeight is 1 by default.
	initHeader := tmproto.Header{ChainID: req.ChainId, Time: req.Time}
	app.ChainID = req.ChainId

	// If req.InitialHeight is > 1, then we set the initial version in the
	// stores.
	if req.InitialHeight > 1 {
		app.initialHeight = req.InitialHeight
		initHeader = tmproto.Header{ChainID: req.ChainId, Height: req.InitialHeight, Time: req.Time}
		err := app.cms.SetInitialVersion(req.InitialHeight)
		if err != nil {
			return nil, err
		}
	}

	// initialize the deliver state and check state with a correct header
	app.setDeliverState(initHeader)
	app.setCheckState(initHeader)
	app.setPrepareProposalState(initHeader)
	app.setProcessProposalState(initHeader)

	// Store the consensus params in the BaseApp's paramstore. Note, this must be
	// done after the deliver state and context have been set as it's persisted
	// to state.
	if req.ConsensusParams != nil {
		app.StoreConsensusParams(app.deliverState.ctx, req.ConsensusParams)
		app.StoreConsensusParams(app.prepareProposalState.ctx, req.ConsensusParams)
		app.StoreConsensusParams(app.processProposalState.ctx, req.ConsensusParams)
		app.StoreConsensusParams(app.checkState.ctx, req.ConsensusParams)
	}

	app.SetDeliverStateToCommit()

	if app.initChainer == nil {
		return
	}

	resp := app.initChainer(app.deliverState.ctx, *req)
	app.initChainer(app.prepareProposalState.ctx, *req)
	app.initChainer(app.processProposalState.ctx, *req)
	res = &resp

	// sanity check
	if len(req.Validators) > 0 {
		if len(req.Validators) != len(res.Validators) {
			return nil,
				fmt.Errorf(
					"len(RequestInitChain.Validators) != len(GenesisValidators) (%d != %d)",
					len(req.Validators), len(res.Validators),
				)
		}

		sort.Sort(abci.ValidatorUpdates(req.Validators))
		sort.Sort(abci.ValidatorUpdates(res.Validators))

		for i := range res.Validators {
			if !proto.Equal(&res.Validators[i], &req.Validators[i]) {
				return nil, fmt.Errorf("genesisValidators[%d] != req.Validators[%d] ", i, i)
			}
		}
	}

	// In the case of a new chain, AppHash will be the hash of an empty string.
	// During an upgrade, it'll be the hash of the last committed block.
	var appHash []byte
	if !app.LastCommitID().IsZero() {
		appHash = app.LastCommitID().Hash
	} else {
		// $ echo -n '' | sha256sum
		// e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
		emptyHash := sha256.Sum256([]byte{})
		appHash = emptyHash[:]
	}

	// NOTE: We don't commit, but BeginBlock for block `initial_height` starts from this
	// deliverState.
	return &abci.ResponseInitChain{
		ConsensusParams: res.ConsensusParams,
		Validators:      res.Validators,
		AppHash:         appHash,
	}, nil
}

// Info implements the ABCI interface.
func (app *BaseApp) Info(ctx context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	lastCommitID := app.cms.LastCommitID()

	return &abci.ResponseInfo{
		Data:             app.name,
		Version:          app.version,
		AppVersion:       app.appVersion,
		LastBlockHeight:  lastCommitID.Version,
		LastBlockAppHash: lastCommitID.Hash,
		MinimumGasPrices: app.minGasPrices.String(),
	}, nil
}

// BeginBlock implements the ABCI application interface.
func (app *BaseApp) BeginBlock(ctx sdk.Context, req abci.RequestBeginBlock) (res abci.ResponseBeginBlock) {
	defer telemetry.MeasureSince(time.Now(), "abci", "begin_block")

	if err := app.validateHeight(req); err != nil {
		panic(err)
	}

	if app.beginBlocker != nil {
		res = app.beginBlocker(ctx, req)
		res.Events = sdk.MarkEventsToIndex(res.Events, app.indexEvents)
	}

	// call the streaming service hooks with the EndBlock messages
	for _, streamingListener := range app.abciListeners {
		if err := streamingListener.ListenBeginBlock(app.deliverState.ctx, req, res); err != nil {
			app.logger.Error("EndBlock listening hook failed", "height", req.Header.Height, "err", err)
		}
	}
	return res
}

func (app *BaseApp) MidBlock(ctx sdk.Context, height int64) (events []abci.Event) {
	defer telemetry.MeasureSince(time.Now(), "abci", "mid_block")

	if app.midBlocker != nil {
		midBlockEvents := app.midBlocker(ctx, height)
		events = sdk.MarkEventsToIndex(midBlockEvents, app.indexEvents)
	}
	// TODO: add listener handling
	// // call the streaming service hooks with the EndBlock messages
	// for _, streamingListener := range app.abciListeners {
	// 	if err := streamingListener.ListenMidBlock(app.deliverState.ctx, req, res); err != nil {
	// 		app.logger.Error("MidBlock listening hook failed", "height", req.Height, "err", err)
	// 	}
	// }

	return events
}

// EndBlock implements the ABCI interface.
func (app *BaseApp) EndBlock(ctx sdk.Context, req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	// Clear DeliverTx Events
	ctx.MultiStore().ResetEvents()

	defer telemetry.MeasureSince(time.Now(), "abci", "end_block")

	if app.endBlocker != nil {
		res = app.endBlocker(ctx, req)
		res.Events = sdk.MarkEventsToIndex(res.Events, app.indexEvents)
	}

	if cp := app.GetConsensusParams(ctx); cp != nil {
		res.ConsensusParamUpdates = legacytm.ABCIToLegacyConsensusParams(cp)
	}

	// call the streaming service hooks with the EndBlock messages
	for _, streamingListener := range app.abciListeners {
		if err := streamingListener.ListenEndBlock(app.deliverState.ctx, req, res); err != nil {
			app.logger.Error("EndBlock listening hook failed", "height", req.Height, "err", err)
		}
	}

	return res
}

// CheckTx implements the ABCI interface and executes a tx in CheckTx mode. In
// CheckTx mode, messages are not executed. This means messages are only validated
// and only the AnteHandler is executed. State is persisted to the BaseApp's
// internal CheckTx state if the AnteHandler passes. Otherwise, the ResponseCheckTx
// will contain releveant error information. Regardless of tx execution outcome,
// the ResponseCheckTx will contain relevant gas execution context.
func (app *BaseApp) CheckTx(ctx context.Context, req *abci.RequestCheckTx) (*abci.ResponseCheckTxV2, error) {
	defer telemetry.MeasureSince(time.Now(), "abci", "check_tx")

	var mode runTxMode

	switch {
	case req.Type == abci.CheckTxType_New:
		mode = runTxModeCheck

	case req.Type == abci.CheckTxType_Recheck:
		mode = runTxModeReCheck

	default:
		panic(fmt.Sprintf("unknown RequestCheckTx type: %s", req.Type))
	}

	sdkCtx := app.getContextForTx(mode, req.Tx)
	tx, err := app.txDecoder(req.Tx)
	if err != nil {
		res := sdkerrors.ResponseCheckTx(err, 0, 0, app.trace)
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &res}, err
	}
	gInfo, result, _, priority, pendingTxChecker, expireTxHandler, txCtx, err := app.runTx(sdkCtx, mode, tx, sha256.Sum256(req.Tx))
	if err != nil {
		res := sdkerrors.ResponseCheckTx(err, gInfo.GasWanted, gInfo.GasUsed, app.trace)
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &res}, err
	}

	res := &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			GasWanted: int64(gInfo.GasWanted), // TODO: Should type accept unsigned ints?
			Data:      result.Data,
			Priority:  priority,
		},
		ExpireTxHandler:  expireTxHandler,
		EVMNonce:         txCtx.EVMNonce(),
		EVMSenderAddress: txCtx.EVMSenderAddress(),
		IsEVM:            txCtx.IsEVM(),
	}
	if pendingTxChecker != nil {
		res.IsPendingTransaction = true
		res.Checker = pendingTxChecker
	}

	return res, nil
}

// DeliverTxBatch executes multiple txs
func (app *BaseApp) DeliverTxBatch(ctx sdk.Context, req sdk.DeliverTxBatchRequest) (res sdk.DeliverTxBatchResponse) {
	responses := make([]*sdk.DeliverTxResult, 0, len(req.TxEntries))

	if len(req.TxEntries) == 0 {
		return sdk.DeliverTxBatchResponse{Results: responses}
	}

	// avoid overhead for empty batches
	scheduler := tasks.NewScheduler(app.concurrencyWorkers, app.TracingInfo, app.DeliverTx)
	txRes, err := scheduler.ProcessAll(ctx, req.TxEntries)
	if err != nil {
		ctx.Logger().Error("error while processing scheduler", "err", err)
		panic(err)
	}
	for _, tx := range txRes {
		responses = append(responses, &sdk.DeliverTxResult{Response: tx})
	}

	return sdk.DeliverTxBatchResponse{Results: responses}
}

// DeliverTx implements the ABCI interface and executes a tx in DeliverTx mode.
// State only gets persisted if all messages are valid and get executed successfully.
// Otherwise, the ResponseDeliverTx will contain relevant error information.
// Regardless of tx execution outcome, the ResponseDeliverTx will contain relevant
// gas execution context.
func (app *BaseApp) DeliverTx(ctx sdk.Context, req abci.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) (res abci.ResponseDeliverTx) {
	defer telemetry.MeasureSince(time.Now(), "abci", "deliver_tx")
	defer func() {
		for _, streamingListener := range app.abciListeners {
			if err := streamingListener.ListenDeliverTx(app.deliverState.ctx, req, res); err != nil {
				app.logger.Error("DeliverTx listening hook failed", "err", err)
			}
		}
	}()

	gInfo := sdk.GasInfo{}
	resultStr := "successful"

	defer func() {
		telemetry.IncrCounter(1, "tx", "count")
		telemetry.IncrCounter(1, "tx", resultStr)
		telemetry.SetGauge(float32(gInfo.GasUsed), "tx", "gas", "used")
		telemetry.SetGauge(float32(gInfo.GasWanted), "tx", "gas", "wanted")
	}()

	gInfo, result, anteEvents, _, _, _, resCtx, err := app.runTx(ctx.WithTxBytes(req.Tx).WithVoteInfos(app.voteInfos), runTxModeDeliver, tx, checksum)
	if err != nil {
		resultStr = "failed"
		// if we have a result, use those events instead of just the anteEvents
		if result != nil {
			return sdkerrors.ResponseDeliverTxWithEvents(err, gInfo.GasWanted, gInfo.GasUsed, sdk.MarkEventsToIndex(result.Events, app.indexEvents), app.trace)
		}
		return sdkerrors.ResponseDeliverTxWithEvents(err, gInfo.GasWanted, gInfo.GasUsed, sdk.MarkEventsToIndex(anteEvents, app.indexEvents), app.trace)
	}

	res = abci.ResponseDeliverTx{
		GasWanted: int64(gInfo.GasWanted), // TODO: Should type accept unsigned ints?
		GasUsed:   int64(gInfo.GasUsed),   // TODO: Should type accept unsigned ints?
		Log:       result.Log,
		Data:      result.Data,
		Events:    sdk.MarkEventsToIndex(result.Events, app.indexEvents),
	}
	if resCtx.IsEVM() {
		res.EvmTxInfo = &abci.EvmTxInfo{
			SenderAddress: resCtx.EVMSenderAddress(),
			Nonce:         resCtx.EVMNonce(),
			TxHash:        resCtx.EVMTxHash(),
			VmError:       result.EvmError,
		}
		// TODO: populate error data for EVM err
		if result.EvmError != "" {
			evmErr := sdkerrors.Wrap(sdkerrors.ErrEVMVMError, result.EvmError)
			res.Codespace, res.Code, res.Log = sdkerrors.ABCIInfo(evmErr, app.trace)
			resultStr = "failed"
		}
	}
	return
}

func (app *BaseApp) WriteState() sdk.CommitMultiStore {
	app.stateToCommit.ms.Write()
	return app.cms
}

func (app *BaseApp) GetWorkingHash() []byte {
	hash, err := app.cms.GetWorkingHash()
	if err != nil {
		// this should never happen
		panic(fmt.Errorf("error when getting working hash: %s", err))
	}
	return hash
}

func (app *BaseApp) SetProcessProposalStateToCommit() {
	app.stateToCommit = app.processProposalState
}

func (app *BaseApp) SetDeliverStateToCommit() {
	app.stateToCommit = app.deliverState
}

// Commit implements the ABCI interface. It will commit all state that exists in
// the deliver state's multi-store and includes the resulting commit ID in the
// returned abci.ResponseCommit. Commit will set the check state based on the
// latest header and reset the deliver state. Also, if a non-zero halt height is
// defined in config, Commit will execute a deferred function call to check
// against that height and gracefully halt if it matches the latest committed
// height.
func (app *BaseApp) Commit(ctx context.Context) (res *abci.ResponseCommit, err error) {
	defer telemetry.MeasureSince(time.Now(), "abci", "commit")
	app.commitLock.Lock()
	defer app.commitLock.Unlock()

	if app.stateToCommit == nil {
		panic("no state to commit")
	}
	header := app.stateToCommit.ctx.BlockHeader()
	retainHeight := app.GetBlockRetentionHeight(header.Height)

	app.WriteState()
	app.GetWorkingHash()
	app.cms.Commit(true)

	// Reset the Check state to the latest committed.
	//
	// NOTE: This is safe because Tendermint holds a lock on the mempool for
	// Commit. Use the header from this latest block.
	app.setCheckState(header)

	// empty/reset the deliver state
	app.resetStatesExceptCheckState()

	var halt bool

	switch {
	case app.haltHeight > 0 && uint64(header.Height) >= app.haltHeight:
		halt = true

	case app.haltTime > 0 && header.Time.Unix() >= int64(app.haltTime):
		halt = true
	}

	if halt {
		// Halt the binary and allow Tendermint to receive the ResponseCommit
		// response with the commit ID hash. This will allow the node to successfully
		// restart and process blocks assuming the halt configuration has been
		// reset or moved to a more distant value.
		app.halt()
	}

	app.SnapshotIfApplicable(uint64(header.Height))

	return &abci.ResponseCommit{
		RetainHeight: retainHeight,
	}, nil
}

func (app *BaseApp) SnapshotIfApplicable(height uint64) {
	if app.snapshotInterval > 0 && height%app.snapshotInterval == 0 {
		go app.Snapshot(int64(height))
	}
}

// halt attempts to gracefully shutdown the node via SIGINT and SIGTERM falling
// back on os.Exit if both fail.
func (app *BaseApp) halt() {
	app.logger.Info("halting node per configuration", "height", app.haltHeight, "time", app.haltTime)

	p, err := os.FindProcess(os.Getpid())
	if err == nil {
		// attempt cascading signals in case SIGINT fails (os dependent)
		sigIntErr := p.Signal(syscall.SIGINT)
		sigTermErr := p.Signal(syscall.SIGTERM)

		if sigIntErr == nil || sigTermErr == nil {
			return
		}
	}

	// Resort to exiting immediately if the process could not be found or killed
	// via SIGINT/SIGTERM signals.
	app.logger.Info("failed to send SIGINT/SIGTERM; exiting...")
	os.Exit(0)
}

// Snapshot takes a snapshot of the current state and prunes any old snapshottypes.
func (app *BaseApp) Snapshot(height int64) {
	if app.snapshotManager == nil {
		app.logger.Info("snapshot manager not configured")
		return
	}

	app.logger.Info("creating state snapshot", "height", height)

	snapshot, err := app.snapshotManager.Create(uint64(height))
	if err != nil {
		app.logger.Error("failed to create state snapshot", "height", height, "err", err)
		return
	}

	app.logger.Info("completed state snapshot", "height", height, "format", snapshot.Format)

	if app.snapshotKeepRecent > 0 {
		app.logger.Debug("pruning state snapshots")

		pruned, err := app.snapshotManager.Prune(app.snapshotKeepRecent)
		if err != nil {
			app.logger.Error("Failed to prune state snapshots", "err", err)
			return
		}

		app.logger.Debug("pruned state snapshots", "pruned", pruned)
	}
}

// Query implements the ABCI interface. It delegates to CommitMultiStore if it
// implements Queryable.
func (app *BaseApp) Query(ctx context.Context, req *abci.RequestQuery) (res *abci.ResponseQuery, err error) {
	defer telemetry.MeasureSinceWithLabels([]string{"abci", "query"}, time.Now(), []metrics.Label{{Name: "path", Value: req.Path}})

	// Add panic recovery for all queries.
	// ref: https://github.com/cosmos/cosmos-sdk/pull/8039
	defer func() {
		if r := recover(); r != nil {
			resp := sdkerrors.QueryResultWithDebug(sdkerrors.Wrapf(sdkerrors.ErrPanic, "%v", r), app.trace)
			res = &resp
		}
	}()

	// when a client did not provide a query height, manually inject the latest
	if req.Height == 0 {
		req.Height = app.LastBlockHeight()
	}

	// handle gRPC routes first rather than calling splitPath because '/' characters
	// are used as part of gRPC paths
	if grpcHandler := app.grpcQueryRouter.Route(req.Path); grpcHandler != nil {
		resp := app.handleQueryGRPC(grpcHandler, *req)
		return &resp, nil
	}

	path := splitPath(req.Path)

	var resp abci.ResponseQuery
	if len(path) == 0 {
		resp = sdkerrors.QueryResultWithDebug(sdkerrors.Wrap(sdkerrors.ErrUnknownRequest, "no query path provided"), app.trace)
		return &resp, nil
	}

	switch path[0] {
	// "/app" prefix for special application queries
	case "app":
		resp = handleQueryApp(app, path, *req)

	case "store":
		resp = handleQueryStore(app, path, *req)

	case "p2p":
		resp = handleQueryP2P(app, path)

	case "custom":
		resp = handleQueryCustom(app, path, *req)
	default:
		resp = sdkerrors.QueryResultWithDebug(sdkerrors.Wrap(sdkerrors.ErrUnknownRequest, "unknown query path"), app.trace)
	}
	return &resp, nil
}

// ListSnapshots implements the ABCI interface. It delegates to app.snapshotManager if set.
func (app *BaseApp) ListSnapshots(context context.Context, req *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
	resp := &abci.ResponseListSnapshots{Snapshots: []*abci.Snapshot{}}
	if app.snapshotManager == nil {
		return resp, nil
	}

	snapshots, err := app.snapshotManager.List()
	if err != nil {
		app.logger.Error("failed to list snapshots", "err", err)
		return resp, nil
	}

	for _, snapshot := range snapshots {
		abciSnapshot, err := snapshot.ToABCI()
		if err != nil {
			app.logger.Error("failed to list snapshots", "err", err)
			return resp, nil
		}
		resp.Snapshots = append(resp.Snapshots, &abciSnapshot)
	}

	return resp, nil
}

// LoadSnapshotChunk implements the ABCI interface. It delegates to app.snapshotManager if set.
func (app *BaseApp) LoadSnapshotChunk(context context.Context, req *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
	if app.snapshotManager == nil {
		return &abci.ResponseLoadSnapshotChunk{}, nil
	}
	chunk, err := app.snapshotManager.LoadChunk(req.Height, req.Format, req.Chunk)
	if err != nil {
		app.logger.Error(
			"failed to load snapshot chunk",
			"height", req.Height,
			"format", req.Format,
			"chunk", req.Chunk,
			"err", err,
		)
		return &abci.ResponseLoadSnapshotChunk{}, nil
	}
	return &abci.ResponseLoadSnapshotChunk{Chunk: chunk}, nil
}

// OfferSnapshot implements the ABCI interface. It delegates to app.snapshotManager if set.
func (app *BaseApp) OfferSnapshot(context context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	if app.snapshotManager == nil {
		app.logger.Error("snapshot manager not configured")
		return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ABORT}, nil
	}

	if req.Snapshot == nil {
		app.logger.Error("received nil snapshot")
		return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT}, nil
	}

	snapshot, err := snapshottypes.SnapshotFromABCI(req.Snapshot)
	if err != nil {
		app.logger.Error("failed to decode snapshot metadata", "err", err)
		return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT}, nil
	}

	err = app.snapshotManager.Restore(snapshot)
	switch {
	case err == nil:
		return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ACCEPT}, nil

	case errors.Is(err, snapshottypes.ErrUnknownFormat):
		return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT_FORMAT}, nil

	case errors.Is(err, snapshottypes.ErrInvalidMetadata):
		app.logger.Error(
			"rejecting invalid snapshot",
			"height", req.Snapshot.Height,
			"format", req.Snapshot.Format,
			"err", err,
		)
		return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT}, nil

	default:
		app.logger.Error(
			"failed to restore snapshot",
			"height", req.Snapshot.Height,
			"format", req.Snapshot.Format,
			"err", err,
		)

		// We currently don't support resetting the IAVL stores and retrying a different snapshot,
		// so we ask Tendermint to abort all snapshot restoration.
		return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ABORT}, nil
	}
}

// ApplySnapshotChunk implements the ABCI interface. It delegates to app.snapshotManager if set.
func (app *BaseApp) ApplySnapshotChunk(context context.Context, req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	if app.snapshotManager == nil {
		app.logger.Error("snapshot manager not configured")
		return &abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ABORT}, nil
	}

	done, err := app.snapshotManager.RestoreChunk(req.Chunk)
	switch {
	case err == nil:
		if done {
			if app.interBlockCache != nil {
				app.interBlockCache.Reset()
			}
		}
		return &abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT}, nil

	case errors.Is(err, snapshottypes.ErrChunkHashMismatch):
		app.logger.Error(
			"chunk checksum mismatch; rejecting sender and requesting refetch",
			"chunk", req.Index,
			"sender", req.Sender,
			"err", err,
		)
		return &abci.ResponseApplySnapshotChunk{
			Result:        abci.ResponseApplySnapshotChunk_RETRY,
			RefetchChunks: []uint32{req.Index},
			RejectSenders: []string{req.Sender},
		}, nil

	default:
		app.logger.Error("failed to restore snapshot", "err", err)
		return &abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ABORT}, nil
	}
}

func (app *BaseApp) handleQueryGRPC(handler GRPCQueryHandler, req abci.RequestQuery) abci.ResponseQuery {
	ctx, err := app.CreateQueryContext(req.Height, req.Prove)
	if err != nil {
		return sdkerrors.QueryResultWithDebug(err, app.trace)
	}

	res, err := handler(ctx, req)
	if err != nil {
		res = sdkerrors.QueryResultWithDebug(gRPCErrorToSDKError(err), app.trace)
		res.Height = req.Height
		return res
	}

	return res
}

func gRPCErrorToSDKError(err error) error {
	status, ok := grpcstatus.FromError(err)
	if !ok {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	switch status.Code() {
	case codes.NotFound:
		return sdkerrors.Wrap(sdkerrors.ErrKeyNotFound, err.Error())
	case codes.InvalidArgument:
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	case codes.FailedPrecondition:
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	case codes.Unauthenticated:
		return sdkerrors.Wrap(sdkerrors.ErrUnauthorized, err.Error())
	default:
		return sdkerrors.Wrap(sdkerrors.ErrUnknownRequest, err.Error())
	}
}

func checkNegativeHeight(height int64) error {
	if height < 0 {
		// Reject invalid heights.
		return sdkerrors.Wrap(
			sdkerrors.ErrInvalidRequest,
			"cannot query with height < 0; please provide a valid height",
		)
	}
	return nil
}

// CreateQueryContext creates a new sdk.Context for a query, taking as args
// the block height and whether the query needs a proof or not.
func (app *BaseApp) CreateQueryContext(height int64, prove bool) (sdk.Context, error) {
	if err := checkNegativeHeight(height); err != nil {
		return sdk.Context{}, err
	}

	lastBlockHeight := app.LastBlockHeight()
	if height > lastBlockHeight {
		return sdk.Context{},
			sdkerrors.Wrap(
				sdkerrors.ErrInvalidHeight,
				"cannot query with height in the future; please provide a valid height",
			)
	}

	// when a client did not provide a query height, manually inject the latest
	if height == 0 {
		height = lastBlockHeight
	}

	if height <= 1 && prove {
		return sdk.Context{},
			sdkerrors.Wrap(
				sdkerrors.ErrInvalidRequest,
				"cannot query with proof when height <= 1; please provide a valid height",
			)
	}

	cacheMS, err := app.cms.CacheMultiStoreWithVersion(height)
	if err != nil {
		return sdk.Context{},
			sdkerrors.Wrapf(
				sdkerrors.ErrInvalidRequest,
				"failed to load state at height %d; %s (latest height: %d)", height, err, lastBlockHeight,
			)
	}

	checkStateCtx := app.checkState.Context()
	// branch the commit-multistore for safety
	ctx := sdk.NewContext(
		cacheMS, checkStateCtx.BlockHeader(), true, app.logger,
	).WithMinGasPrices(app.minGasPrices).WithBlockHeight(height)

	return ctx, nil
}

// GetBlockRetentionHeight returns the height for which all blocks below this height
// are pruned from Tendermint. Given a commitment height and a non-zero local
// minRetainBlocks configuration, the retentionHeight is the smallest height that
// satisfies:
//
// - Unbonding (safety threshold) time: The block interval in which validators
// can be economically punished for misbehavior. Blocks in this interval must be
// auditable e.g. by the light client.
//
// - Logical store snapshot interval: The block interval at which the underlying
// logical store database is persisted to disk, e.g. every 10000 heights. Blocks
// since the last IAVL snapshot must be available for replay on application restart.
//
// - State sync snapshots: Blocks since the oldest available snapshot must be
// available for state sync nodes to catch up (oldest because a node may be
// restoring an old snapshot while a new snapshot was taken).
//
// - Local (minRetainBlocks) config: Archive nodes may want to retain more or
// all blocks, e.g. via a local config option min-retain-blocks. There may also
// be a need to vary retention for other nodes, e.g. sentry nodes which do not
// need historical blocks.
func (app *BaseApp) GetBlockRetentionHeight(commitHeight int64) int64 {
	// pruning is disabled if minRetainBlocks is zero
	if app.minRetainBlocks == 0 {
		return 0
	}

	minNonZero := func(x, y int64) int64 {
		switch {
		case x == 0:
			return y
		case y == 0:
			return x
		case x < y:
			return x
		default:
			return y
		}
	}

	// Define retentionHeight as the minimum value that satisfies all non-zero
	// constraints. All blocks below (commitHeight-retentionHeight) are pruned
	// from Tendermint.
	var retentionHeight int64

	// Define the number of blocks needed to protect against misbehaving validators
	// which allows light clients to operate safely. Note, we piggy back of the
	// evidence parameters instead of computing an estimated nubmer of blocks based
	// on the unbonding period and block commitment time as the two should be
	// equivalent.
	cp := app.GetConsensusParams(app.deliverState.ctx)
	if cp != nil && cp.Evidence != nil && cp.Evidence.MaxAgeNumBlocks > 0 {
		retentionHeight = commitHeight - cp.Evidence.MaxAgeNumBlocks
	}

	// Define the state pruning offset, i.e. the block offset at which the
	// underlying logical database is persisted to disk.
	statePruningOffset := int64(app.cms.GetPruning().KeepEvery)
	if statePruningOffset > 0 {
		if commitHeight > statePruningOffset {
			v := commitHeight - (commitHeight % statePruningOffset)
			retentionHeight = minNonZero(retentionHeight, v)
		} else {
			// Hitting this case means we have persisting enabled but have yet to reach
			// a height in which we persist state, so we return zero regardless of other
			// conditions. Otherwise, we could end up pruning blocks without having
			// any state committed to disk.
			return 0
		}
	}

	if app.snapshotInterval > 0 && app.snapshotKeepRecent > 0 {
		v := commitHeight - int64((app.snapshotInterval * uint64(app.snapshotKeepRecent)))
		retentionHeight = minNonZero(retentionHeight, v)
	}

	v := commitHeight - int64(app.minRetainBlocks)
	retentionHeight = minNonZero(retentionHeight, v)

	if retentionHeight <= 0 {
		// prune nothing in the case of a non-positive height
		return 0
	}

	return retentionHeight
}

func handleQueryApp(app *BaseApp, path []string, req abci.RequestQuery) abci.ResponseQuery {
	if len(path) >= 2 {
		switch path[1] {
		case "simulate":
			txBytes := req.Data

			gInfo, res, err := app.Simulate(txBytes)
			if err != nil {
				return sdkerrors.QueryResultWithDebug(sdkerrors.Wrap(err, "failed to simulate tx"), app.trace)
			}

			simRes := &sdk.SimulationResponse{
				GasInfo: gInfo,
				Result:  res,
			}

			bz, err := codec.ProtoMarshalJSON(simRes, app.interfaceRegistry)
			if err != nil {
				return sdkerrors.QueryResultWithDebug(sdkerrors.Wrap(err, "failed to JSON encode simulation response"), app.trace)
			}

			return abci.ResponseQuery{
				Codespace: sdkerrors.RootCodespace,
				Height:    req.Height,
				Value:     bz,
			}

		case "version":
			return abci.ResponseQuery{
				Codespace: sdkerrors.RootCodespace,
				Height:    req.Height,
				Value:     []byte(app.version),
			}

		case "snapshots":
			var responseValue []byte

			response, err := app.ListSnapshots(context.Background(), &abci.RequestListSnapshots{})
			if err != nil {
				return sdkerrors.QueryResult(sdkerrors.Wrap(err, "list snapshots error"))
			}

			responseValue, err = json.Marshal(response)
			if err != nil {
				return sdkerrors.QueryResult(sdkerrors.Wrap(err, fmt.Sprintf("failed to marshal list snapshots response %v", response)))
			}

			return abci.ResponseQuery{
				Codespace: sdkerrors.RootCodespace,
				Height:    req.Height,
				Value:     responseValue,
			}

		default:
			return sdkerrors.QueryResultWithDebug(sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unknown query: %s", path), app.trace)
		}
	}

	return sdkerrors.QueryResultWithDebug(
		sdkerrors.Wrap(
			sdkerrors.ErrUnknownRequest,
			"expected second parameter to be either 'simulate', 'version' or 'snapshot', neither was present",
		), app.trace)
}

func handleQueryStore(app *BaseApp, path []string, req abci.RequestQuery) abci.ResponseQuery {
	// "/store" prefix for store queries
	queryable, ok := app.cms.(sdk.Queryable)
	if !ok {
		return sdkerrors.QueryResultWithDebug(sdkerrors.Wrap(sdkerrors.ErrUnknownRequest, "multistore doesn't support queries"), app.trace)
	}

	req.Path = "/" + strings.Join(path[1:], "/")

	if req.Height <= 1 && req.Prove {
		return sdkerrors.QueryResultWithDebug(
			sdkerrors.Wrap(
				sdkerrors.ErrInvalidRequest,
				"cannot query with proof when height <= 1; please provide a valid height",
			), app.trace)
	}

	resp := queryable.Query(req)
	resp.Height = req.Height

	return resp
}

func handleQueryCustom(app *BaseApp, path []string, req abci.RequestQuery) abci.ResponseQuery {
	// path[0] should be "custom" because "/custom" prefix is required for keeper
	// queries.
	//
	// The QueryRouter routes using path[1]. For example, in the path
	// "custom/gov/proposal", QueryRouter routes using "gov".
	if len(path) < 2 || path[1] == "" {
		return sdkerrors.QueryResultWithDebug(sdkerrors.Wrap(sdkerrors.ErrUnknownRequest, "no route for custom query specified"), app.trace)
	}

	querier := app.queryRouter.Route(path[1])
	if querier == nil {
		return sdkerrors.QueryResultWithDebug(sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "no custom querier found for route %s", path[1]), app.trace)
	}

	ctx, err := app.CreateQueryContext(req.Height, req.Prove)
	if err != nil {
		return sdkerrors.QueryResultWithDebug(err, app.trace)
	}

	// Passes the rest of the path as an argument to the querier.
	//
	// For example, in the path "custom/gov/proposal/test", the gov querier gets
	// []string{"proposal", "test"} as the path.
	resBytes, err := querier(ctx, path[2:], req)
	if err != nil {
		res := sdkerrors.QueryResultWithDebug(err, app.trace)
		res.Height = req.Height
		return res
	}

	return abci.ResponseQuery{
		Height: req.Height,
		Value:  resBytes,
	}
}

// splitPath splits a string path using the delimiter '/'.
//
// e.g. "this/is/funny" becomes []string{"this", "is", "funny"}
func splitPath(requestPath string) (path []string) {
	path = strings.Split(requestPath, "/")

	// first element is empty string
	if len(path) > 0 && path[0] == "" {
		path = path[1:]
	}

	return path
}

// ABCI++
func (app *BaseApp) PrepareProposal(ctx context.Context, req *abci.RequestPrepareProposal) (resp *abci.ResponsePrepareProposal, err error) {
	defer telemetry.MeasureSince(time.Now(), "abci", "prepare_proposal")

	header := tmproto.Header{
		ChainID:            app.ChainID,
		Height:             req.Height,
		Time:               req.Time,
		ProposerAddress:    req.ProposerAddress,
		AppHash:            req.AppHash,
		NextValidatorsHash: req.NextValidatorsHash,
		DataHash:           req.DataHash,
		ConsensusHash:      req.ConsensusHash,
		EvidenceHash:       req.EvidenceHash,
		ValidatorsHash:     req.ValidatorsHash,
		LastCommitHash:     req.LastCommitHash,
		LastResultsHash:    req.LastResultsHash,
		LastBlockId: tmproto.BlockID{
			Hash: req.LastBlockHash,
			PartSetHeader: tmproto.PartSetHeader{
				Total: uint32(req.LastBlockPartSetTotal),
				Hash:  req.LastBlockPartSetHash,
			},
		},
	}
	if app.prepareProposalState == nil {
		app.setPrepareProposalState(header)
	} else {
		// In the first block, app.prepareProposalState.ctx will already be initialized
		// by InitChain. Context is now updated with Header information.
		app.setPrepareProposalHeader(header)
	}

	app.preparePrepareProposalState()

	defer func() {
		if err := recover(); err != nil {
			app.logger.Error(
				"panic recovered in PrepareProposal",
				"height", req.Height,
				"time", req.Time,
				"panic", err,
			)

			resp = &abci.ResponsePrepareProposal{
				TxRecords: utils.Map(req.Txs, func(tx []byte) *abci.TxRecord {
					return &abci.TxRecord{Action: abci.TxRecord_UNMODIFIED, Tx: tx}
				}),
			}
		}
	}()

	if app.prepareProposalHandler != nil {
		resp, err = app.prepareProposalHandler(app.prepareProposalState.ctx, req)
		if err != nil {
			return nil, err
		}

		if cp := app.GetConsensusParams(app.prepareProposalState.ctx); cp != nil {
			resp.ConsensusParamUpdates = cp
		}

		return resp, nil
	}

	return nil, errors.New("no prepare proposal handler")
}

func (app *BaseApp) ProcessProposal(ctx context.Context, req *abci.RequestProcessProposal) (resp *abci.ResponseProcessProposal, err error) {
	defer telemetry.MeasureSince(time.Now(), "abci", "process_proposal")

	header := tmproto.Header{
		ChainID:            app.ChainID,
		Height:             req.Height,
		Time:               req.Time,
		ProposerAddress:    req.ProposerAddress,
		AppHash:            req.AppHash,
		NextValidatorsHash: req.NextValidatorsHash,
		DataHash:           req.DataHash,
		ConsensusHash:      req.ConsensusHash,
		EvidenceHash:       req.EvidenceHash,
		ValidatorsHash:     req.ValidatorsHash,
		LastCommitHash:     req.LastCommitHash,
		LastResultsHash:    req.LastResultsHash,
		LastBlockId: tmproto.BlockID{
			Hash: req.LastBlockHash,
			PartSetHeader: tmproto.PartSetHeader{
				Total: uint32(req.LastBlockPartSetTotal),
				Hash:  req.LastBlockPartSetHash,
			},
		},
	}
	if app.processProposalState == nil {
		app.setProcessProposalState(header)
	} else {
		// In the first block, app.processProposalState.ctx will already be initialized
		// by InitChain. Context is now updated with Header information.
		app.setProcessProposalHeader(header)
	}

	// NOTE: header hash is not set in NewContext, so we manually set it here

	app.prepareProcessProposalState(req.Hash)

	defer func() {
		if err := recover(); err != nil {
			app.logger.Error(
				"panic recovered in ProcessProposal",
				"height", req.Height,
				"time", req.Time,
				"hash", fmt.Sprintf("%X", req.Hash),
				"panic", err,
			)

			resp = &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}
		}
	}()

	defer func() {
		if err := recover(); err != nil {
			app.logger.Error(
				"panic recovered in ProcessProposal",
				"height", req.Height,
				"time", req.Time,
				"hash", fmt.Sprintf("%X", req.Hash),
				"panic", err,
			)

			resp = &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}
		}
	}()

	if app.processProposalHandler != nil {
		resp, err = app.processProposalHandler(app.processProposalState.ctx, req)
		if err != nil {
			return nil, err
		}

		if cp := app.GetConsensusParams(app.processProposalState.ctx); cp != nil {
			resp.ConsensusParamUpdates = cp
		}

		return resp, nil
	}

	return nil, errors.New("no process proposal handler")
}

func (app *BaseApp) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	defer telemetry.MeasureSince(time.Now(), "abci", "finalize_block")

	if app.cms.TracingEnabled() {
		app.cms.SetTracingContext(sdk.TraceContext(
			map[string]interface{}{"blockHeight": req.Height},
		))
	}

	// Initialize the DeliverTx state. If this is the first block, it should
	// already be initialized in InitChain. Otherwise app.deliverState will be
	// nil, since it is reset on Commit.
	header := tmproto.Header{
		ChainID:            app.ChainID,
		Height:             req.Height,
		Time:               req.Time,
		ProposerAddress:    req.ProposerAddress,
		AppHash:            req.AppHash,
		NextValidatorsHash: req.NextValidatorsHash,
		DataHash:           req.DataHash,
		ConsensusHash:      req.ConsensusHash,
		EvidenceHash:       req.EvidenceHash,
		ValidatorsHash:     req.ValidatorsHash,
		LastCommitHash:     req.LastCommitHash,
		LastResultsHash:    req.LastResultsHash,
		LastBlockId: tmproto.BlockID{
			Hash: req.LastBlockHash,
			PartSetHeader: tmproto.PartSetHeader{
				Total: uint32(req.LastBlockPartSetTotal),
				Hash:  req.LastBlockPartSetHash,
			},
		},
	}
	if app.deliverState == nil {
		app.setDeliverState(header)
	} else {
		// In the first block, app.deliverState.ctx will already be initialized
		// by InitChain. Context is now updated with Header information.
		app.setDeliverStateHeader(header)
	}

	// NOTE: header hash is not set in NewContext, so we manually set it here

	app.prepareDeliverState(req.Hash)

	// we also set block gas meter to checkState in case the application needs to
	// verify gas consumption during (Re)CheckTx
	if app.checkState != nil {
		app.checkState.SetContext(app.checkState.ctx.WithHeaderHash(req.Hash))
	}

	if app.finalizeBlocker != nil {
		res, err := app.finalizeBlocker(app.deliverState.ctx, req)
		if err != nil {
			return nil, err
		}
		res.Events = sdk.MarkEventsToIndex(res.Events, app.indexEvents)
		// set the signed validators for addition to context in deliverTx
		app.setVotesInfo(req.DecidedLastCommit.GetVotes())

		return res, nil
	} else {
		return nil, errors.New("finalize block handler not set")
	}
}

func (app *BaseApp) ExtendVote(ctx context.Context, req *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
	return &abci.ResponseExtendVote{}, nil
}

func (app *BaseApp) VerifyVoteExtension(ctx context.Context, req *abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error) {
	return &abci.ResponseVerifyVoteExtension{}, nil
}

func (app *BaseApp) LoadLatest(ctx context.Context, req *abci.RequestLoadLatest) (*abci.ResponseLoadLatest, error) {
	if err := app.LoadLatestVersion(); err != nil {
		return nil, err
	}
	app.initialHeight = app.cms.LastCommitID().Version
	return &abci.ResponseLoadLatest{}, nil
}
