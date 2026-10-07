package node

import (
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
	// mockAppPhaseParse covers decoding each transaction and recovering its sender.
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
// hash, but executes nothing. With a store it resumes after its last committed block on restart.
type MockApp struct {
	abci.BaseApplication

	app   abci.Application
	state utils.RWMutex[*mockAppState]
	store utils.Option[*mockAppStore]
	// finalizePhases times FinalizeBlock, which the execute loop calls from one goroutine.
	finalizePhases *seidbmetrics.PhaseTimer
}

// NewMockApp returns a MockApp that keeps its state in memory only.
func NewMockApp(app abci.Application) *MockApp {
	return &MockApp{
		app:            app,
		finalizePhases: seidbmetrics.NewPhaseTimer(otel.Meter("mock_app"), "mock_app_finalize"),
		state: utils.NewRWMutex(&mockAppState{
			nextNonce:      map[common.Address]uint64{},
			nextTransition: mockAppTransitionInitialize,
			dirtyNonces:    map[common.Address]struct{}{},
		}),
	}
}

// OpenMockApp returns a MockApp that persists its state in dir and restores it from there.
func OpenMockApp(app abci.Application, dir string) (*MockApp, error) {
	store, err := openMockAppStore(dir)
	if err != nil {
		return nil, err
	}
	mock := NewMockApp(app)
	mock.store = utils.Some(store)
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
	return mock, nil
}

// Close closes the store, when there is one.
func (app *MockApp) Close() error {
	if store, ok := app.store.Get(); ok {
		return store.Close()
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

func (app *MockApp) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	if req.Header == nil {
		return nil, errMockAppMissingHeader
	}
	defer app.finalizePhases.Reset()
	app.finalizePhases.SetPhase(mockAppPhaseParse)
	txs, err := parseMockAppTxs(req.Txs)
	if err != nil {
		return nil, err
	}
	app.finalizePhases.SetPhase(mockAppPhaseLockWait)
	for state := range app.state.Lock() {
		app.finalizePhases.SetPhase(mockAppPhaseApply)
		return state.finalizeBlock(req, txs)
	}
	panic("unreachable")
}

func (app *MockApp) Commit(context.Context) (*abci.ResponseCommit, error) {
	for state := range app.state.Lock() {
		if err := state.checkTransition(mockAppTransitionCommit); err != nil {
			return nil, err
		}
		if store, ok := app.store.Get(); ok {
			if err := store.save(state, state.dirtyNonces, !state.validatorsSaved); err != nil {
				return nil, fmt.Errorf("save mock app state at height %d: %w", state.lastBlockHeight, err)
			}
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

func (state *mockAppState) checkTransition(want mockAppTransition) error {
	if state.nextTransition != want {
		return fmt.Errorf("mock app unexpected transition: got %s, want %s", state.nextTransition, want)
	}
	return nil
}

func parseMockAppTxs(txs [][]byte) ([]mockAppTx, error) {
	parsed := make([]mockAppTx, len(txs))
	workers := min(runtime.GOMAXPROCS(0), len(txs))
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
