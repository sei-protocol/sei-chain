package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"go.opentelemetry.io/otel"

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

var _ abci.Application = (*MockApp)(nil)

var (
	errMockAppProcessProposal = errors.New("mock app does not support ProcessProposal")
	errMockAppMissingHeader   = errors.New("mock app FinalizeBlock requires header")
	baseBalance               = *uint256.MustFromBig(new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000_000_000)))
)

type mockAppTransition int

const blocksToRetain = 10_000

// Constant EVM fee and gas answers, in wei and gas units, so EVM-only load
// generators can price transactions against the mock app.
const (
	mockAppMinGasPrice = 1_000_000_000
	mockAppGasLimit    = 10_000_000_000
)

// Stages of MockApp.FinalizeBlock, published as mock_app_finalize_phase_duration_seconds_total.
const (
	// mockAppPhaseParse covers waiting for the parse PrepareBlock started, or parsing the
	// transactions here when it did not start one.
	mockAppPhaseParse = "parse"
	// mockAppPhaseLockWait covers waiting for the state lock, which nonce reads also take.
	mockAppPhaseLockWait = "lock_wait"
	// mockAppPhaseApply covers the nonce checks, the transaction results, and the app hash.
	mockAppPhaseApply = "apply"
)

const (
	mockAppTransitionInitialize mockAppTransition = iota
	mockAppTransitionFinalize
	mockAppTransitionCommit
)

func (t mockAppTransition) String() string {
	switch t {
	case mockAppTransitionInitialize:
		return "initialize"
	case mockAppTransitionFinalize:
		return "finalize"
	case mockAppTransitionCommit:
		return "commit"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

type mockAppState struct {
	nextNonce        map[common.Address]uint64
	lastBlockHeight  int64
	lastBlockAppHash []byte
	validators       []abci.ValidatorUpdate
	nextTransition   mockAppTransition
	// dirtyNonces holds the senders whose nonce changed since the last Commit.
	dirtyNonces map[common.Address]struct{}
	// validatorsSaved reports whether the store holds the current validator set.
	validatorsSaved bool
}

// MockApp is an ABCI app for EVM transaction load tests. It checks nonces and rolls an app
// hash, but executes nothing. With a store it resumes after its last saved block on restart.
type MockApp struct {
	abci.BaseApplication

	app   abci.Application
	state utils.RWMutex[*mockAppState]
	saver utils.Option[*mockAppSaver]
	// finalizePhases times FinalizeBlock, which the execute loop calls from one goroutine.
	finalizePhases *seidbmetrics.PhaseTimer
	// prepared holds blocks PrepareBlock started parsing ahead of their FinalizeBlock.
	prepared utils.Mutex[*[]*mockAppPreparedBlock]
	// parseWorkers is the number of goroutines that parse one block; 0 uses GOMAXPROCS.
	parseWorkers int
}

// mockAppPreparedBlock is a block whose transactions PrepareBlock parses in the background.
// FinalizeBlock uses it only for the block with this height and hash. txs and err are set
// before done closes.
type mockAppPreparedBlock struct {
	height int64
	hash   []byte
	done   chan struct{}
	txs    []mockAppTx
	err    error
}

// maxMockAppPreparedBlocks bounds the blocks parsed ahead: the block about to execute and the
// ones after it that the execute loop's fetcher has reached.
const maxMockAppPreparedBlocks = 3

// NewMockApp returns a MockApp that keeps its state in memory only.
func NewMockApp(app abci.Application) *MockApp {
	return &MockApp{
		app:            app,
		finalizePhases: seidbmetrics.NewPhaseTimer(otel.Meter("mock_app"), "mock_app_finalize"),
		prepared:       utils.NewMutex(&[]*mockAppPreparedBlock{}),
		state: utils.NewRWMutex(&mockAppState{
			nextNonce:      map[common.Address]uint64{},
			nextTransition: mockAppTransitionInitialize,
			dirtyNonces:    map[common.Address]struct{}{},
		}),
	}
}

// OpenMockApp returns a MockApp that persists its state in dir and restores it from there, and
// parses each block with parseWorkers goroutines (0 uses GOMAXPROCS).
func OpenMockApp(app abci.Application, dir string, parseWorkers int) (*MockApp, error) {
	store, err := openMockAppStore(dir)
	if err != nil {
		return nil, err
	}
	mock := NewMockApp(app)
	mock.parseWorkers = parseWorkers
	for state := range mock.state.Lock() {
		restored, err := store.load(state)
		if err != nil {
			return nil, errors.Join(err, store.Close())
		}
		if restored {
			state.nextTransition = mockAppTransitionFinalize
			state.validatorsSaved = true
		}
	}
	mock.saver = utils.Some(newMockAppSaver(store))
	return mock, nil
}

// Close writes any queued state and closes the store, when there is one.
func (app *MockApp) Close() error {
	if saver, ok := app.saver.Get(); ok {
		return saver.Close()
	}
	return nil
}

func (app *MockApp) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	if req.InitialHeight <= 0 {
		return nil, fmt.Errorf("mock app InitChain initial height must be > 0: %d", req.InitialHeight)
	}
	for state := range app.state.Lock() {
		if err := state.checkTransition(mockAppTransitionInitialize); err != nil {
			return nil, err
		}
		res, err := app.app.InitChain(req)
		if err != nil {
			return nil, err
		}
		validators := app.app.GetValidators()
		state.lastBlockHeight = req.InitialHeight - 1
		state.lastBlockAppHash = nil
		state.validators = slices.Clone(validators)
		state.validatorsSaved = false
		state.nextTransition = mockAppTransitionFinalize
		res.AppHash = nil
		return res, nil
	}
	panic("unreachable")
}

func (app *MockApp) InitLastHeader(lastHeader *tmproto.Header) {}

func (app *MockApp) Info() *abci.ResponseInfo {
	info := app.app.Info()
	for state := range app.state.RLock() {
		info.LastBlockHeight = state.lastBlockHeight
		info.LastBlockAppHash = slices.Clone(state.lastBlockAppHash)
		return info
	}
	panic("unreachable")
}

func (app *MockApp) GetValidators() []abci.ValidatorUpdate {
	for state := range app.state.RLock() {
		return slices.Clone(state.validators)
	}
	panic("unreachable")
}

func (app *MockApp) LastBlockHeight() int64 {
	for state := range app.state.RLock() {
		return state.lastBlockHeight
	}
	panic("unreachable")
}

func (app *MockApp) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	res, err := parseFastCheckTx(req.Tx)
	if err != nil {
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Code: 1, Log: err.Error()}}
	}
	return res
}

func (app *MockApp) GetTxPriorityHint(context.Context, *abci.RequestGetTxPriorityHintV2) (*abci.ResponseGetTxPriorityHint, error) {
	return &abci.ResponseGetTxPriorityHint{}, nil
}

func (app *MockApp) EvmNonce(addr common.Address) uint64 {
	for state := range app.state.RLock() {
		return state.nextNonce[addr]
	}
	panic("unreachable")
}

func (app *MockApp) EvmBalance(common.Address, []byte) uint256.Int { return baseBalance }

// EvmMinGasPrice returns a constant floor, since the mock app does not price transactions.
func (app *MockApp) EvmMinGasPrice() *big.Int { return big.NewInt(mockAppMinGasPrice) }

// EvmBaseFee returns the same constant as EvmMinGasPrice.
func (app *MockApp) EvmBaseFee() *big.Int { return big.NewInt(mockAppMinGasPrice) }

// EvmGasLimit returns a constant block gas limit, since the mock app does not meter gas.
func (app *MockApp) EvmGasLimit() uint64 { return mockAppGasLimit }

func (app *MockApp) EvmChainID() uint64 {
	return app.app.EvmChainID()
}

func (app *MockApp) ProcessProposal(_ context.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	return nil, errMockAppProcessProposal
}

func (app *MockApp) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	if req.Header == nil {
		return nil, errMockAppMissingHeader
	}
	defer app.finalizePhases.Reset()
	app.finalizePhases.SetPhase(mockAppPhaseParse)
	txs, ok, err := app.awaitPrepared(ctx, req.Header.Height, req.Hash)
	if err != nil {
		return nil, err
	}
	if !ok {
		if txs, err = parseMockAppTxs(req.Txs, app.parseWorkers); err != nil {
			return nil, err
		}
	}
	app.finalizePhases.SetPhase(mockAppPhaseLockWait)
	for state := range app.state.Lock() {
		app.finalizePhases.SetPhase(mockAppPhaseApply)
		return state.finalizeBlock(req, txs)
	}
	panic("unreachable")
}

// PrepareBlock starts parsing a block's transactions and recovering their senders in the
// background, and returns without waiting. The parse of one block then overlaps the parse of the
// next and the execution of the previous one. A parse error is left for FinalizeBlock, which
// parses the block again.
func (app *MockApp) PrepareBlock(_ context.Context, req *abci.RequestFinalizeBlock) error {
	if req.Header == nil {
		return nil
	}
	block := &mockAppPreparedBlock{height: req.Header.Height, hash: slices.Clone(req.Hash), done: make(chan struct{})}
	next := app.LastBlockHeight() + 1
	queued := false
	for queue := range app.prepared.Lock() {
		kept := slices.DeleteFunc(*queue, func(b *mockAppPreparedBlock) bool {
			return b.height < next || b.height == block.height
		})
		if len(kept) < maxMockAppPreparedBlocks {
			kept = append(kept, block)
			queued = true
		}
		*queue = kept
	}
	if queued {
		go func(txs [][]byte) {
			defer close(block.done)
			block.txs, block.err = parseMockAppTxs(txs, app.parseWorkers)
		}(req.Txs)
	}
	return nil
}

// awaitPrepared waits for the parse PrepareBlock started for this height and hash. It reports
// false when there is none or the parse failed, and returns an error only when ctx ends first.
func (app *MockApp) awaitPrepared(ctx context.Context, height int64, hash []byte) ([]mockAppTx, bool, error) {
	prepared, ok := app.takePrepared(height, hash)
	if !ok {
		return nil, false, nil
	}
	select {
	case <-prepared.done:
		return prepared.txs, prepared.err == nil, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

// takePrepared removes and returns the block prepared for this height and hash, and drops every
// prepared block below height.
func (app *MockApp) takePrepared(height int64, hash []byte) (*mockAppPreparedBlock, bool) {
	for queue := range app.prepared.Lock() {
		*queue = slices.DeleteFunc(*queue, func(b *mockAppPreparedBlock) bool { return b.height < height })
		i := slices.IndexFunc(*queue, func(b *mockAppPreparedBlock) bool {
			return b.height == height && bytes.Equal(b.hash, hash)
		})
		if i < 0 {
			return nil, false
		}
		block := (*queue)[i]
		*queue = slices.Delete(*queue, i, i+1)
		return block, true
	}
	panic("unreachable")
}

func (app *MockApp) Commit(context.Context) (*abci.ResponseCommit, error) {
	for state := range app.state.Lock() {
		if err := state.checkTransition(mockAppTransitionCommit); err != nil {
			return nil, err
		}
		if saver, ok := app.saver.Get(); ok {
			saver.put(state.snapshot())
			state.validatorsSaved = true
		}
		clear(state.dirtyNonces)
		state.nextTransition = mockAppTransitionFinalize
		return &abci.ResponseCommit{RetainHeight: max(state.lastBlockHeight-blocksToRetain, 0)}, nil
	}
	panic("unreachable")
}

type mockAppTx struct {
	checkTx  *abci.ResponseCheckTxV2
	parseErr error
}

func (state *mockAppState) finalizeBlock(req *abci.RequestFinalizeBlock, txs []mockAppTx) (*abci.ResponseFinalizeBlock, error) {
	if err := state.checkTransition(mockAppTransitionFinalize); err != nil {
		return nil, err
	}
	if req.Header.Height != state.lastBlockHeight+1 {
		return nil, fmt.Errorf("mock app FinalizeBlock non-sequential height: got %d, want %d", req.Header.Height, state.lastBlockHeight+1)
	}

	txResults := make([]*abci.ExecTxResult, len(txs))
	for i, parsed := range txs {
		if parsed.parseErr != nil {
			txResults[i] = &abci.ExecTxResult{
				Code: 1,
				Log:  parsed.parseErr.Error(),
			}
			continue
		}
		tx := parsed.checkTx
		wantNonce := state.nextNonce[tx.EVMSenderAddress]
		if tx.EVMNonce == wantNonce {
			txResults[i] = &abci.ExecTxResult{
				Code:      abci.CodeTypeOK,
				GasWanted: tx.GasWanted,
				GasUsed:   tx.GasWanted,
			}
			state.nextNonce[tx.EVMSenderAddress]++
			state.dirtyNonces[tx.EVMSenderAddress] = struct{}{}
		} else {
			logger.Warn(
				"unexpected nonce",
				"height", req.Header.Height,
				"addr", tx.EVMSenderAddress,
				"got", tx.EVMNonce,
				"want", wantNonce,
			)
			err := sdkerrors.ErrWrongSequence
			txResults[i] = &abci.ExecTxResult{
				Codespace: err.Codespace(),
				Code:      err.ABCICode(),
				Log:       err.Error(),
			}
		}
	}
	state.lastBlockHeight = req.Header.Height
	state.lastBlockAppHash = mockAppHash(state.lastBlockAppHash, req.Hash)
	state.nextTransition = mockAppTransitionCommit
	return &abci.ResponseFinalizeBlock{
		TxResults: txResults,
		AppHash:   slices.Clone(state.lastBlockAppHash),
	}, nil
}

// snapshot returns the committed tip, the nonces changed since the last Commit, and the validator
// set when the store does not hold it yet.
func (state *mockAppState) snapshot() mockAppSnapshot {
	snap := mockAppSnapshot{
		height:  state.lastBlockHeight,
		appHash: slices.Clone(state.lastBlockAppHash),
		nonces:  make(map[common.Address]uint64, len(state.dirtyNonces)),
	}
	for addr := range state.dirtyNonces {
		snap.nonces[addr] = state.nextNonce[addr]
	}
	if !state.validatorsSaved {
		snap.validators = utils.Some(slices.Clone(state.validators))
	}
	return snap
}

func (state *mockAppState) checkTransition(want mockAppTransition) error {
	if state.nextTransition != want {
		return fmt.Errorf("mock app unexpected transition: got %s, want %s", state.nextTransition, want)
	}
	return nil
}

// parseMockAppTxs decodes txs and recovers their senders with up to workers goroutines; workers <= 0
// uses GOMAXPROCS.
func parseMockAppTxs(txs [][]byte, workers int) ([]mockAppTx, error) {
	parsed := make([]mockAppTx, len(txs))
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	workers = min(workers, len(txs))
	if workers == 0 {
		return parsed, nil
	}
	if err := scope.Parallel(func(s scope.ParallelScope) error {
		for worker := range workers {
			s.Spawn(func() error {
				for i := worker; i < len(txs); i += workers {
					res, err := parseFastCheckTx(txs[i])
					if err != nil {
						parsed[i].parseErr = err
						continue
					}
					parsed[i].checkTx = res
				}
				return nil
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return parsed, nil
}

func mockAppHash(prevAppHash []byte, blockHash []byte) []byte {
	h := sha256.New()
	_, _ = h.Write(prevAppHash)
	_, _ = h.Write(blockHash)
	return h.Sum(nil)
}
