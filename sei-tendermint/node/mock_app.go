package node

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
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
	nextNonce       map[common.Address]uint64
	lastBlockHeight int64
	validators      []abci.ValidatorUpdate
	nextTransition  mockAppTransition
}

// MockApp is an in-memory ABCI app for EVM transaction load tests.
type MockApp struct {
	abci.BaseApplication

	app   abci.Application
	state utils.RWMutex[*mockAppState]

	// Receives the hashes of every block finalized.
	appHashBuilder builder.AppHashBuilder
}

func NewMockApp(app abci.Application, appHashBuilder builder.AppHashBuilder) *MockApp {
	return &MockApp{
		app:            app,
		appHashBuilder: appHashBuilder,
		state: utils.NewRWMutex(&mockAppState{
			nextNonce:      map[common.Address]uint64{},
			nextTransition: mockAppTransitionInitialize,
		}),
	}
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
		state.validators = slices.Clone(validators)
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
		info.LastBlockAppHash = nil
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

func (app *MockApp) EvmChainID() uint64 {
	return app.app.EvmChainID()
}

func (app *MockApp) ProcessProposal(_ context.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	return nil, errMockAppProcessProposal
}

func (app *MockApp) FinalizeBlock(
	ctx context.Context,
	req *abci.RequestFinalizeBlock,
) (*abci.ResponseFinalizeBlock, error) {
	if req.Header == nil {
		return nil, errMockAppMissingHeader
	}
	txs, err := parseMockAppTxs(req.Txs)
	if err != nil {
		return nil, err
	}
	var resp *abci.ResponseFinalizeBlock
	for state := range app.state.Lock() {
		resp, err = state.finalizeBlock(req, txs)
	}
	if err != nil {
		return nil, err
	}
	if err := app.reportBlock(ctx, req); err != nil {
		return nil, err
	}
	return resp, nil
}

// reportBlock reports a finalized block to the app hash builder. Only the block hash is real: the mock app commits
// no state and produces no receipts.
func (app *MockApp) reportBlock(ctx context.Context, req *abci.RequestFinalizeBlock) error {
	height := uint64(req.Header.Height) //nolint:gosec // FinalizeBlock requires sequential positive heights
	var blockHash [32]byte
	copy(blockHash[:], req.Hash)
	if err := app.appHashBuilder.ReportBlockHash(ctx, height, blockHash); err != nil {
		return fmt.Errorf("report the block hash of block %d: %w", height, err)
	}
	if err := app.appHashBuilder.ReportStateHash(ctx, height, [32]byte{}, nil); err != nil {
		return fmt.Errorf("report the state hash of block %d: %w", height, err)
	}
	if err := app.appHashBuilder.ReportBUDHash(ctx, height, [32]byte{}); err != nil {
		return fmt.Errorf("report the BUD hash of block %d: %w", height, err)
	}
	if err := app.appHashBuilder.ReportReceiptHash(ctx, height, [32]byte{}); err != nil {
		return fmt.Errorf("report the receipt hash of block %d: %w", height, err)
	}
	return nil
}

func (app *MockApp) Commit(context.Context) (*abci.ResponseCommit, error) {
	for state := range app.state.Lock() {
		if err := state.checkTransition(mockAppTransitionCommit); err != nil {
			return nil, err
		}
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
	state.nextTransition = mockAppTransitionCommit
	return &abci.ResponseFinalizeBlock{
		TxResults: txResults,
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
