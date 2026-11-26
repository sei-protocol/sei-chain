package node

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"runtime"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

var _ abevm.Application = (*MockApp)(nil)

var (
	errMockAppNoExecution = errors.New("mock app does not execute EVM calls")
	baseBalance           = *uint256.MustFromBig(new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000_000_000)))
)

type mockAppTransition int

const (
	mockAppTransitionInitialize mockAppTransition = iota
	mockAppTransitionFinalize
	mockAppTransitionReportHashes
)

func (t mockAppTransition) String() string {
	switch t {
	case mockAppTransitionInitialize:
		return "initialize"
	case mockAppTransitionFinalize:
		return "finalize"
	case mockAppTransitionReportHashes:
		return "report_hashes"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

type mockAppState struct {
	nextNonce        map[common.Address]uint64
	lastBlockHeight  int64
	lastBlockAppHash []byte
	validators       abevm.CommitteeWeights
	nextTransition   mockAppTransition
	// finalized is the height of the last block whose FinalizeBlock has finished.
	finalized utils.AtomicSend[int64]
}

// MockApp is an in-memory Autobahn application for EVM transaction load tests.
type MockApp struct {
	app         abevm.Application
	chainConfig *params.ChainConfig
	state       utils.RWMutex[*mockAppState]
	// finalized follows state.finalized, so FinalizeBlock waits for its turn without the lock.
	finalized utils.AtomicRecv[int64]
	// storage receives an empty block for each finalized block. None disables storage writes.
	storage utils.Option[*bootstrap.GigaStorageManager]
	// epochRegistry is the registry Commit advances. None means Commit leaves it alone.
	epochRegistry *evmonlyapp.EpochRegistry
}

func NewMockApp(app abevm.Application) *MockApp {
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = new(big.Int).SetUint64(app.EvmChainID())
	state := &mockAppState{
		nextNonce:      map[common.Address]uint64{},
		nextTransition: mockAppTransitionInitialize,
		finalized:      utils.NewAtomicSend[int64](0),
	}
	return &MockApp{
		app:         app,
		chainConfig: &chainConfig,
		state:       utils.NewRWMutex(state),
		finalized:   state.finalized.Subscribe(),
	}
}

func (app *MockApp) InitChain(req abevm.InitConfig) error {
	initialHeight, ok := utils.SafeCast[int64](req.InitialHeight)
	if !ok || initialHeight <= 0 {
		return fmt.Errorf("mock app InitChain initial height must be > 0: %d", req.InitialHeight)
	}
	for state := range app.state.Lock() {
		if err := state.checkTransition(mockAppTransitionInitialize); err != nil {
			return err
		}
		err := app.app.InitChain(req)
		if err != nil {
			return err
		}
		validators := app.app.GetValidators()
		state.lastBlockHeight = initialHeight - 1
		state.lastBlockAppHash = nil
		state.validators = maps.Clone(validators)
		state.nextTransition = mockAppTransitionFinalize
		state.finalized.Store(state.lastBlockHeight)
		return nil
	}
	panic("unreachable")
}

func (app *MockApp) RestoreBlockTime(time.Time) {}

// BlockTxsHashes returns an empty transaction list.
func (app *MockApp) BlockTxsHashes(height int64) (utils.Option[[]common.Hash], error) {
	if height > app.EvmBlockNumber() {
		return utils.None[[]common.Hash](), nil
	}
	return utils.Some([]common.Hash{}), nil
}

// EvmBlockNumber returns the latest height available for EVM block queries.
func (app *MockApp) EvmBlockNumber() int64 {
	height := app.LastBlockHeight()
	if storage, ok := app.storage.Get(); ok {
		receipts := storage.ReceiptDB()
		if receipts == nil {
			return 0
		}
		height = min(height, receipts.LatestVersion())
	}
	return max(0, height)
}

func (app *MockApp) Info() *abevm.ResponseInfo {
	for state := range app.state.RLock() {
		return &abevm.ResponseInfo{
			LastBlockHeight:  state.lastBlockHeight,
			LastBlockAppHash: slices.Clone(state.lastBlockAppHash),
		}
	}
	panic("unreachable")
}

func (app *MockApp) GetValidators() abevm.CommitteeWeights {
	for state := range app.state.RLock() {
		return maps.Clone(state.validators)
	}
	panic("unreachable")
}

func (app *MockApp) LastBlockHeight() int64 {
	for state := range app.state.RLock() {
		return state.lastBlockHeight
	}
	panic("unreachable")
}

func (app *MockApp) CheckTx(tx abevm.EvmTx) (abevm.AdmittedTx, error) {
	return checkMockTx(tx)
}

func (app *MockApp) EvmNonce(addr common.Address) uint64 {
	for state := range app.state.RLock() {
		return state.nextNonce[addr]
	}
	panic("unreachable")
}

// EvmCommittedNonce is the same as EvmNonce: the mock app commits synchronously.
func (app *MockApp) EvmCommittedNonce(addr common.Address) uint64 {
	return app.EvmNonce(addr)
}

func (app *MockApp) EvmBalance(common.Address) uint256.Int { return baseBalance }

// AwaitCommits returns immediately.
func (app *MockApp) AwaitCommits() error { return nil }

// EvmCode returns no contract code.
func (app *MockApp) EvmCode(common.Address) []byte { return nil }

// EvmStorage returns zero for every storage slot.
func (app *MockApp) EvmStorage(common.Address, common.Hash) common.Hash { return common.Hash{} }

func (app *MockApp) EvmBalanceAt(common.Address, int64) (uint256.Int, error) {
	return uint256.Int{}, errMockAppNoExecution
}

func (app *MockApp) EvmCodeAt(common.Address, int64) ([]byte, error) {
	return nil, errMockAppNoExecution
}

func (app *MockApp) EvmNonceAt(common.Address, int64) (uint64, error) {
	return 0, errMockAppNoExecution
}

func (app *MockApp) EvmStorageAt(common.Address, common.Hash, int64) (common.Hash, error) {
	return common.Hash{}, errMockAppNoExecution
}

// EvmChainID returns the chain ID of the EVM-only app this app wraps.
func (app *MockApp) EvmChainID() uint64 { return app.app.EvmChainID() }

// EvmChainConfig returns the dev chain configuration with the Autobahn EVM-only
// chain ID.
func (app *MockApp) EvmChainConfig() *params.ChainConfig { return app.chainConfig }

// EvmCall always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmCall(context.Context, *ethcore.Message, abevm.CallOverrides) (*ethcore.ExecutionResult, error) {
	return nil, errMockAppNoExecution
}

// EvmEstimateGas always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmEstimateGas(context.Context, *ethcore.Message, uint64, abevm.CallOverrides) (uint64, []byte, error) {
	return 0, nil, errMockAppNoExecution
}

// EvmCallAt always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmCallAt(context.Context, *ethcore.Message, abevm.BlockRef, abevm.CallOverrides) (*ethcore.ExecutionResult, error) {
	return nil, errMockAppNoExecution
}

// EvmEstimateGasAt always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmEstimateGasAt(context.Context, *ethcore.Message, uint64, abevm.BlockRef, abevm.CallOverrides) (uint64, []byte, error) {
	return 0, nil, errMockAppNoExecution
}

// EvmEstimateGasAfterCalls always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmEstimateGasAfterCalls(context.Context, []*ethcore.Message, *ethcore.Message, uint64, abevm.CallOverrides) (uint64, []byte, error) {
	return 0, nil, errMockAppNoExecution
}

// EvmEstimateGasAfterCallsAt always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmEstimateGasAfterCallsAt(context.Context, []*ethcore.Message, *ethcore.Message, uint64, abevm.BlockRef, abevm.CallOverrides) (uint64, []byte, error) {
	return 0, nil, errMockAppNoExecution
}

// EvmCreateAccessList always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmCreateAccessList(context.Context, *ethcore.Message) (ethtypes.AccessList, *ethcore.ExecutionResult, error) {
	return nil, nil, errMockAppNoExecution
}

// EvmCreateAccessListAt always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmCreateAccessListAt(context.Context, *ethcore.Message, abevm.BlockRef) (ethtypes.AccessList, *ethcore.ExecutionResult, error) {
	return nil, nil, errMockAppNoExecution
}

func (app *MockApp) EvmBaseFee() *big.Int { return new(big.Int) }

// EvmGasLimit returns 0: the mock app does not meter block gas.
func (app *MockApp) EvmGasLimit() uint64 { return 0 }

func (app *MockApp) EvmMinGasPrice() *big.Int { return new(big.Int) }

// EvmBlockContext always errors: the mock app keeps no EVM state to trace against.
func (app *MockApp) EvmBlockContext(abevm.BlockRef) (vm.BlockContext, error) {
	return vm.BlockContext{}, errMockAppNoExecution
}

// EvmStateAt always errors: the mock app keeps no EVM state to trace against.
func (app *MockApp) EvmStateAt(context.Context, int64) (*abevm.StateHandle, error) {
	return nil, errMockAppNoExecution
}

// EvmStateBeforeTx always errors: the mock app keeps no EVM state to trace against.
func (app *MockApp) EvmStateBeforeTx(context.Context, abevm.BlockRef, []abevm.RawTx) (*abevm.StateHandle, error) {
	return nil, errMockAppNoExecution
}

func (app *MockApp) FinalizeBlock(ctx context.Context, req abevm.FinalizeBlockReq) (abevm.FinalizeBlockResp, error) {
	txs, err := parseMockAppTxs(req.Txs)
	if err != nil {
		return abevm.FinalizeBlockResp{}, err
	}
	if _, err := app.finalized.Wait(ctx, func(h int64) bool { return h+1 >= utils.Clamp[int64](req.Height) }); err != nil {
		return abevm.FinalizeBlockResp{}, err
	}
	// A cancel must not abort a block after some stores have taken its writes.
	ctx = context.WithoutCancel(ctx)
	var response abevm.FinalizeBlockResp
	var height int64
	for state := range app.state.Lock() {
		response, err = state.finalizeBlock(req, txs)
		if err != nil {
			return abevm.FinalizeBlockResp{}, err
		}
		height = state.lastBlockHeight
	}
	if err := app.commitEmptyBlock(ctx, height); err != nil {
		return abevm.FinalizeBlockResp{}, err
	}
	for state := range app.state.Lock() {
		state.nextTransition = mockAppTransitionFinalize
		state.finalized.Store(height)
	}
	return response, nil
}

// commitEmptyBlock feeds a committed mock block to every store as a block with no account changes
// and no receipts. It writes the epoch registry row of that road.
func (app *MockApp) commitEmptyBlock(ctx context.Context, height int64) error {
	if height < 1 {
		return fmt.Errorf("mock app commit height = %d, want >= 1", height)
	}
	var misc []gigatypes.MiscChange
	if app.epochRegistry != nil {
		road, ok := utils.SafeCast[atypes.RoadHeight](height)
		if !ok {
			return fmt.Errorf("mock app commit height = %d does not fit a road height", height)
		}
		var err error
		misc, err = app.epochRegistry.Changes(road)
		if err != nil {
			return err
		}
	}
	storage, ok := app.storage.Get()
	if !ok {
		return nil
	}
	if err := storage.StateDB().CommitStateChanges(height, &gigatypes.StateChangeSet{Misc: misc}); err != nil {
		return fmt.Errorf("commit the state of block %d: %w", height, err)
	}
	if receiptDB := storage.ReceiptDB(); receiptDB != nil {
		receiptCtx := sdk.NewContext(nil, tmproto.Header{Height: height}, false).WithContext(ctx)
		if err := receiptDB.SetReceipts(receiptCtx, nil); err != nil {
			return fmt.Errorf("store the receipts of block %d: %w", height, err)
		}
	}
	//nolint:gosec // height is at least 1
	if err := storage.ReceiptHasher().Submit(ctx, uint64(height), nil); err != nil {
		return fmt.Errorf("submit the receipts of block %d to be hashed: %w", height, err)
	}
	return nil
}

type mockAppTx struct {
	tx       abevm.EvmTx
	parseErr error
}

func (state *mockAppState) finalizeBlock(req abevm.FinalizeBlockReq, txs []mockAppTx) (abevm.FinalizeBlockResp, error) {
	if err := state.checkTransition(mockAppTransitionFinalize); err != nil {
		return abevm.FinalizeBlockResp{}, err
	}
	height, ok := utils.SafeCast[int64](req.Height)
	if !ok || height != state.lastBlockHeight+1 {
		return abevm.FinalizeBlockResp{}, fmt.Errorf("mock app FinalizeBlock non-sequential height: got %d, want %d", req.Height, state.lastBlockHeight+1)
	}
	var gasUsed uint64
	for _, parsed := range txs {
		if parsed.parseErr != nil {
			continue
		}
		tx := parsed.tx
		sender := parsed.tx.Sender()
		wantNonce := state.nextNonce[sender]
		if tx.Nonce() == wantNonce {
			gasUsed += tx.Gas()
			state.nextNonce[sender]++
		} else {
			logger.Warn("unexpected nonce", "height", req.Height, "addr", sender, "got", tx.Nonce(), "want", wantNonce)
		}
	}
	state.lastBlockHeight = height
	state.lastBlockAppHash = mockAppHash(state.lastBlockAppHash, req.Hash[:])
	state.nextTransition = mockAppTransitionReportHashes
	return abevm.FinalizeBlockResp{GasUsed: gasUsed}, nil
}

func (state *mockAppState) checkTransition(want mockAppTransition) error {
	if state.nextTransition != want {
		return fmt.Errorf("mock app unexpected transition: got %s, want %s", state.nextTransition, want)
	}
	return nil
}

func parseMockAppTxs(txs []abevm.EvmTx) ([]mockAppTx, error) {
	parsed := make([]mockAppTx, len(txs))
	workers := min(runtime.GOMAXPROCS(0), len(txs))
	if workers == 0 {
		return parsed, nil
	}
	if err := scope.Parallel(func(s scope.ParallelScope) error {
		for worker := range workers {
			s.Spawn(func() error {
				for i := worker; i < len(txs); i += workers {
					_, err := checkMockTx(txs[i])
					if err != nil {
						parsed[i].parseErr = err
						continue
					}
					parsed[i].tx = txs[i]
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

func checkMockTx(ethTx abevm.EvmTx) (abevm.AdmittedTx, error) {
	gas := ethTx.Gas()
	if gas > math.MaxInt64 {
		return abevm.AdmittedTx{}, fmt.Errorf("EVM gas wanted exceeds int64 max")
	}
	return abevm.AdmittedTx{GasEstimated: gas}, nil
}

// EpochRegistry returns the mock application's retained epoch window.
func (app *MockApp) EpochRegistry() (*atypes.EpochRegistry, error) {
	if app.epochRegistry != nil {
		return app.epochRegistry.Registry(), nil
	}
	return app.app.EpochRegistry()
}
