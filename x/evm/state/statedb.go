package state

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/utils"
)

// Initialized for each transaction individually
type DBImpl struct {
	ctx             sdk.Context
	snapshottedCtxs []sdk.Context

	tempStateCurrent *TemporaryState
	tempStatesHist   []*TemporaryState
	// If err is not nil at the end of the execution, the transaction will be rolled
	// back.
	err error
	// whenever this is set, the same error would also cause EVM to revert, which is
	// why we don't put it in `tempState`, since we still want to be able to access it later.
	precompileErr error

	// a temporary address that collects fees for this particular transaction so that there is
	// no single bottleneck for fee collection. Its account state and balance will be deleted
	// before the block commits
	coinbaseAddress    sdk.AccAddress
	coinbaseEvmAddress common.Address

	k          EVMKeeper
	simulation bool

	// for cases like bank.send_native, we want to suppress transfer events
	eventsSuppressed bool

	logger *tracing.Hooks
}

func NewDBImpl(ctx sdk.Context, k EVMKeeper, simulation bool) *DBImpl {
	feeCollector, _ := k.GetFeeCollectorAddress(ctx)
	s := &DBImpl{
		ctx:                ctx,
		k:                  k,
		snapshottedCtxs:    []sdk.Context{},
		coinbaseAddress:    GetCoinbaseAddress(ctx.TxIndex()),
		simulation:         simulation,
		tempStateCurrent:   NewTemporaryState(&accessList{Addresses: make(map[common.Address]int), Slots: []map[common.Hash]struct{}{}}),
		coinbaseEvmAddress: feeCollector,
	}
	s.Snapshot() // take an initial snapshot for GetCommitted
	return s
}

func (s *DBImpl) AddSurplus(surplus sdk.Int) {
	if surplus.IsNil() || surplus.IsZero() {
		return
	}
	s.tempStateCurrent.surplus = s.tempStateCurrent.surplus.Add(surplus)
}

func (s *DBImpl) DisableEvents() {
	s.eventsSuppressed = true
}

func (s *DBImpl) EnableEvents() {
	s.eventsSuppressed = false
}

func (s *DBImpl) SetLogger(logger *tracing.Hooks) {
	s.logger = logger
}

// for interface compliance
func (s *DBImpl) SetEVM(evm *vm.EVM) {}

// AddPreimage records a SHA3 preimage seen by the VM.
// AddPreimage performs a no-op since the EnablePreimageRecording flag is disabled
// on the vm.Config during state transitions. No store trie preimages are written
// to the database.
func (s *DBImpl) AddPreimage(_ common.Hash, _ []byte) {}

func (s *DBImpl) Finalize() (surplus sdk.Int, err error) {
	if s.simulation {
		panic("should never call finalize on a simulation DB")
	}
	if s.err != nil {
		err = s.err
		return
	}

	// delete state of self-destructed accounts
	s.handleResidualFundsInDestructedAccounts(s.tempStateCurrent)
	s.clearAccountStateIfDestructed(s.tempStateCurrent)
	for _, ts := range s.tempStatesHist {
		s.handleResidualFundsInDestructedAccounts(ts)
		s.clearAccountStateIfDestructed(ts)
	}

	// remove transient states
	// write cache to underlying
	s.flushCtx(s.ctx)
	// write all snapshotted caches in reverse order, except the very first one (base) which will be written by baseapp::runTx
	for i := len(s.snapshottedCtxs) - 1; i > 0; i-- {
		s.flushCtx(s.snapshottedCtxs[i])
	}

	surplus = s.tempStateCurrent.surplus
	for _, ts := range s.tempStatesHist {
		surplus = surplus.Add(ts.surplus)
	}
	return
}

func (s *DBImpl) flushCtx(ctx sdk.Context) {
	ctx.MultiStore().(sdk.CacheMultiStore).Write()
}

// Backward-compatibility functions
func (s *DBImpl) Error() error {
	return s.Err()
}

func (s *DBImpl) GetStorageRoot(common.Address) common.Hash {
	panic("GetStorageRoot is not implemented and called unexpectedly")
}

func (s *DBImpl) Copy() vm.StateDB {
	newCtx := s.ctx.WithMultiStore(s.ctx.MultiStore().CacheMultiStore())
	return &DBImpl{
		ctx:                newCtx,
		snapshottedCtxs:    append(s.snapshottedCtxs, s.ctx),
		tempStateCurrent:   NewTemporaryState(s.tempStateCurrent.transientAccessLists.Copy()),
		tempStatesHist:     append(s.tempStatesHist, s.tempStateCurrent),
		k:                  s.k,
		coinbaseAddress:    s.coinbaseAddress,
		coinbaseEvmAddress: s.coinbaseEvmAddress,
		simulation:         s.simulation,
		err:                s.err,
		precompileErr:      s.precompileErr,
		logger:             s.logger,
	}
}

func (s *DBImpl) Finalise(bool) {
	s.ctx.Logger().Info("Finalise should only be called during simulation and will no-op")
}

func (s *DBImpl) Commit(uint64, bool) (common.Hash, error) {
	panic("Commit is not implemented and called unexpectedly")
}

func (s *DBImpl) SetTxContext(common.Hash, int) {
	//noop
}

func (s *DBImpl) IntermediateRoot(bool) common.Hash {
	panic("IntermediateRoot is not implemented and called unexpectedly")
}

func (s *DBImpl) TxIndex() int {
	return s.ctx.TxIndex()
}

func (s *DBImpl) Preimages() map[common.Hash][]byte {
	return map[common.Hash][]byte{}
}

func (s *DBImpl) SetPrecompileError(err error) {
	s.precompileErr = err
}

func (s *DBImpl) GetPrecompileError() error {
	return s.precompileErr
}

// ** TEST ONLY FUNCTIONS **//
func (s *DBImpl) Err() error {
	return s.err
}

func (s *DBImpl) WithErr(err error) {
	s.err = err
}

func (s *DBImpl) Ctx() sdk.Context {
	return s.ctx
}

func (s *DBImpl) WithCtx(ctx sdk.Context) {
	s.ctx = ctx
}

// in-memory state that's generated by a specific
// EVM snapshot in a single transaction
type TemporaryState struct {
	logs                  []*ethtypes.Log
	transientStates       map[string]map[string]common.Hash
	transientAccounts     map[string][]byte
	transientModuleStates map[string][]byte
	transientAccessLists  *accessList
	surplus               sdk.Int // in wei
}

func NewTemporaryState(al *accessList) *TemporaryState {
	return &TemporaryState{
		logs:                  []*ethtypes.Log{},
		transientStates:       make(map[string]map[string]common.Hash),
		transientAccounts:     make(map[string][]byte),
		transientModuleStates: make(map[string][]byte),
		transientAccessLists:  al,
		surplus:               utils.Sdk0,
	}
}
