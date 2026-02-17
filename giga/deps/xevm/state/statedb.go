package state

import (
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	ethutils "github.com/ethereum/go-ethereum/trie/utils"
	"github.com/sei-protocol/sei-chain/utils"
)

var tempStatePool = sync.Pool{
	New: func() interface{} {
		return newTemporaryState()
	},
}

var dbImplPool = sync.Pool{}

// Initialized for each transaction individually
type DBImpl struct {
	ctx             sdk.Context
	committedCtx    sdk.Context // original ctx for GetCommittedState (avoids one CMS clone)
	snapshottedCtxs []sdk.Context

	tempState *TemporaryState
	journal   []journalEntry

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
	var s *DBImpl
	if v := dbImplPool.Get(); v != nil {
		s = v.(*DBImpl)
		s.ctx = ctx
		s.committedCtx = ctx
		s.k = k
		s.snapshottedCtxs = s.snapshottedCtxs[:0]
		s.coinbaseAddress = GetCoinbaseAddress(ctx.TxIndex())
		s.coinbaseEvmAddress = feeCollector
		s.simulation = simulation
		s.tempState = NewTemporaryState()
		s.journal = s.journal[:0]
		s.err = nil
		s.precompileErr = nil
		s.eventsSuppressed = false
		s.logger = nil
	} else {
		s = &DBImpl{
			ctx:                ctx,
			committedCtx:       ctx,
			k:                  k,
			snapshottedCtxs:    make([]sdk.Context, 0, 4),
			coinbaseAddress:    GetCoinbaseAddress(ctx.TxIndex()),
			simulation:         simulation,
			tempState:          NewTemporaryState(),
			journal:            make([]journalEntry, 0, 16),
			coinbaseEvmAddress: feeCollector,
		}
	}
	return s
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

func (s *DBImpl) Cleanup() {
	if s.tempState != nil {
		s.tempState.release()
		s.tempState = nil
	}
	s.logger = nil
	// Return DBImpl to pool for reuse (keep allocated slices)
	dbImplPool.Put(s)
}

func (s *DBImpl) CleanupForTracer() {
	s.flushCtxs()
	s.ctx = s.committedCtx
	feeCollector, _ := s.k.GetFeeCollectorAddress(s.Ctx())
	s.coinbaseEvmAddress = feeCollector
	s.tempState = NewTemporaryState()
	s.journal = []journalEntry{}
	s.snapshottedCtxs = []sdk.Context{}
	// For tracing, take an initial snapshot so the committed state is preserved.
	s.Snapshot()
}

func (s *DBImpl) Finalize() (surplus sdk.Int, err error) {
	if s.simulation {
		panic("should never call finalize on a simulation DB")
	}
	if s.err != nil {
		err = s.err
		return
	}

	// delete state of self-destructed accounts
	s.handleResidualFundsInDestructedAccounts(s.tempState)
	s.clearAccountStateIfDestructed(s.tempState)

	s.flushCtxs()
	// write all events in order (skip [0] which is the base/committed ctx)
	for i := 1; i < len(s.snapshottedCtxs); i++ {
		s.flushEvents(s.snapshottedCtxs[i])
	}
	if len(s.snapshottedCtxs) > 0 {
		s.flushEvents(s.ctx)
	}

	surplus = s.tempState.surplus
	return
}

func (s *DBImpl) flushCtxs() {
	if len(s.snapshottedCtxs) == 0 {
		return
	}
	// remove transient states
	// write cache to underlying
	s.flushCtx(s.ctx)
	// write all snapshotted caches in reverse order, except the very first one (base) which will be written by baseapp::runTx
	for i := len(s.snapshottedCtxs) - 1; i > 0; i-- {
		s.flushCtx(s.snapshottedCtxs[i])
	}
}

func (s *DBImpl) flushCtx(ctx sdk.Context) {
	ctx.MultiStore().(sdk.CacheMultiStore).Write()
	ctx.GigaMultiStore().WriteGiga()
}

func (s *DBImpl) flushEvents(ctx sdk.Context) {
	s.committedCtx.EventManager().EmitEvents(ctx.EventManager().Events())
}

// Backward-compatibility functions
func (s *DBImpl) Error() error {
	return s.Err()
}

func (s *DBImpl) GetStorageRoot(common.Address) common.Hash {
	return common.Hash{}
}

func (s *DBImpl) Copy() vm.StateDB {
	newCtx := s.ctx.WithMultiStore(s.ctx.MultiStore().CacheMultiStore()).WithEventManager(sdk.NewEventManager())
	journal := make([]journalEntry, len(s.journal))
	copy(journal, s.journal)
	return &DBImpl{
		ctx:                newCtx,
		snapshottedCtxs:    append(s.snapshottedCtxs, s.ctx),
		tempState:          s.tempState.DeepCopy(),
		journal:            journal,
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

func (s *DBImpl) Commit(uint64, bool, bool) (common.Hash, error) {
	panic("Commit is not implemented and called unexpectedly")
}

func (s *DBImpl) SetTxContext(common.Hash, int) {
	//noop
}

func (s *DBImpl) AccessEvents() *vm.AccessEvents { return nil }

// CreateContract marks the account as created for EIP-6780 purposes.
// This is called regardless of whether the account previously existed
// (e.g., prefunded addresses), ensuring that contracts created and
// self-destructed in the same transaction are properly destroyed.
func (s *DBImpl) CreateContract(acc common.Address) {
	s.MarkAccount(acc, AccountCreated)
}

func (s *DBImpl) PointCache() *ethutils.PointCache {
	return nil
}

func (s *DBImpl) Witness() *stateless.Witness { return nil }

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

func newTemporaryState() *TemporaryState {
	return &TemporaryState{
		logs:                  make([]*ethtypes.Log, 0, 4),
		transientStates:       make(map[string]map[string]common.Hash),
		transientAccounts:     make(map[string][]byte),
		transientModuleStates: make(map[string][]byte),
		transientAccessLists:  &accessList{Addresses: make(map[common.Address]int), Slots: make([]map[common.Hash]struct{}, 0, 4)},
		surplus:               utils.Sdk0,
	}
}

func NewTemporaryState() *TemporaryState {
	ts := tempStatePool.Get().(*TemporaryState)
	ts.reset()
	return ts
}

func (ts *TemporaryState) release() {
	tempStatePool.Put(ts)
}

func (ts *TemporaryState) reset() {
	ts.logs = ts.logs[:0]
	clear(ts.transientStates)
	clear(ts.transientAccounts)
	clear(ts.transientModuleStates)
	clear(ts.transientAccessLists.Addresses)
	ts.transientAccessLists.Slots = ts.transientAccessLists.Slots[:0]
	ts.surplus = utils.Sdk0
}

func (ts *TemporaryState) DeepCopy() *TemporaryState {
	res := &TemporaryState{}
	res.logs = make([]*ethtypes.Log, len(ts.logs))
	copy(res.logs, ts.logs)
	res.transientStates = make(map[string]map[string]common.Hash, len(ts.transientStates))
	for k, v := range ts.transientStates {
		res.transientStates[k] = make(map[string]common.Hash, len(v))
		for k2, v2 := range v {
			res.transientStates[k][k2] = v2
		}
	}
	res.transientAccounts = make(map[string][]byte, len(ts.transientAccounts))
	for k, v := range ts.transientAccounts {
		res.transientAccounts[k] = v
	}
	res.transientModuleStates = make(map[string][]byte, len(ts.transientModuleStates))
	for k, v := range ts.transientModuleStates {
		res.transientModuleStates[k] = v
	}
	res.transientAccessLists = &accessList{}
	res.transientAccessLists.Addresses = make(map[common.Address]int, len(ts.transientAccessLists.Addresses))
	for k, v := range ts.transientAccessLists.Addresses {
		res.transientAccessLists.Addresses[k] = v
	}
	res.transientAccessLists.Slots = make([]map[common.Hash]struct{}, len(ts.transientAccessLists.Slots))
	for i, v := range ts.transientAccessLists.Slots {
		res.transientAccessLists.Slots[i] = make(map[common.Hash]struct{}, len(v))
		for k2, v2 := range v {
			res.transientAccessLists.Slots[i][k2] = v2
		}
	}
	res.surplus = sdk.NewIntFromBigInt(ts.surplus.BigInt())
	return res
}

func GetDBImpl(vmsdb vm.StateDB) *DBImpl {
	if sdb, ok := vmsdb.(*DBImpl); ok {
		return sdb
	}
	if hdb, ok := vmsdb.(*state.HookedStateDB); ok {
		return GetDBImpl(hdb.StateDB)
	}
	return nil
}
