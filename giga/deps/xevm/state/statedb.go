package state

import (
	"maps"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	ethutils "github.com/ethereum/go-ethereum/trie/utils"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("giga", "deps", "xevm", "state")

// Initialized for each transaction individually
type DBImpl struct {
	// ctx is the single CacheMultiStore context used for all KV mutations within this stateDB.
	ctx sdk.Context
	// committedCtx is the pre-stateDB context, used for GetCommittedState reads and event flushing.
	committedCtx sdk.Context

	// validRevisions tracks snapshot points (journal index) for RevertToSnapshot.
	validRevisions []revision
	nextRevisionId int

	// snapshottedEventManagers holds EMs from prior snapshots that survived (not reverted).
	snapshottedEventManagers []*sdk.EventManager

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
	// Create a single CacheMultiStore layer for all KV mutations within this stateDB.
	cacheCtx := ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore()).WithEventManager(sdk.NewEventManager())
	s := &DBImpl{
		ctx:                      cacheCtx,
		committedCtx:             ctx,
		k:                        k,
		validRevisions:           []revision{},
		snapshottedEventManagers: []*sdk.EventManager{},
		coinbaseAddress:          GetCoinbaseAddress(ctx.TxIndex()),
		simulation:               simulation,
		tempState:                NewTemporaryState(),
		journal:                  []journalEntry{},
		coinbaseEvmAddress:       feeCollector,
	}
	s.Snapshot()
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
	s.tempState = nil
	s.logger = nil
	s.snapshottedEventManagers = nil
	s.validRevisions = nil
}

func (s *DBImpl) CleanupForTracer() {
	// Reset back to the committed (pre-stateDB) state by discarding the CMS layer.
	s.ctx = s.committedCtx
	feeCollector, _ := s.k.GetFeeCollectorAddress(s.Ctx())
	s.coinbaseEvmAddress = feeCollector
	s.tempState = NewTemporaryState()
	s.journal = []journalEntry{}
	s.validRevisions = []revision{}
	s.snapshottedEventManagers = []*sdk.EventManager{}
	s.nextRevisionId = 0
	// Re-create the CMS layer for the tracer.
	s.committedCtx = s.ctx
	s.ctx = s.ctx.WithMultiStore(s.ctx.MultiStore().CacheMultiStore()).WithEventManager(sdk.NewEventManager())
	s.Snapshot()
}

// ResetForTracer resets in-memory state for a new transaction without flushing
// the CacheMultiStore hierarchy. This is safe for concurrent use when copies of
// this statedb are being read from other goroutines, since it never calls
// CacheMultiStore.Write() on any shared store layer.
func (s *DBImpl) ResetForTracer() {
	feeCollector, _ := s.k.GetFeeCollectorAddress(s.Ctx())
	s.coinbaseEvmAddress = feeCollector
	s.tempState = NewTemporaryState()
	s.journal = []journalEntry{}
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

	// Write the single CMS layer to the underlying store.
	s.ctx.MultiStore().(sdk.CacheMultiStore).Write()
	s.ctx.GigaMultiStore().WriteGiga()

	// Emit all surviving events (from snapshots + current) to the committed ctx's EventManager.
	for _, em := range s.snapshottedEventManagers {
		s.committedCtx.EventManager().EmitEvents(em.Events())
	}
	s.committedCtx.EventManager().EmitEvents(s.ctx.EventManager().Events())

	surplus = s.tempState.surplus
	return
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
	snapshottedEMs := make([]*sdk.EventManager, len(s.snapshottedEventManagers))
	copy(snapshottedEMs, s.snapshottedEventManagers)
	validRevisions := make([]revision, len(s.validRevisions))
	copy(validRevisions, s.validRevisions)
	return &DBImpl{
		ctx:                      newCtx,
		committedCtx:             s.committedCtx,
		validRevisions:           validRevisions,
		nextRevisionId:           s.nextRevisionId,
		snapshottedEventManagers: snapshottedEMs,
		tempState:                s.tempState.DeepCopy(),
		journal:                  journal,
		k:                        s.k,
		coinbaseAddress:          s.coinbaseAddress,
		coinbaseEvmAddress:       s.coinbaseEvmAddress,
		simulation:               s.simulation,
		err:                      s.err,
		precompileErr:            s.precompileErr,
		logger:                   s.logger,
	}
}

func (s *DBImpl) Finalise(bool) {
	logger.Info("Finalise should only be called during simulation and will no-op")
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

func NewTemporaryState() *TemporaryState {
	return &TemporaryState{
		logs:                  []*ethtypes.Log{},
		transientStates:       make(map[string]map[string]common.Hash),
		transientAccounts:     make(map[string][]byte),
		transientModuleStates: make(map[string][]byte),
		transientAccessLists:  &accessList{Addresses: make(map[common.Address]int), Slots: []map[common.Hash]struct{}{}},
		surplus:               utils.Sdk0,
	}
}

func (ts *TemporaryState) DeepCopy() *TemporaryState {
	res := &TemporaryState{}
	res.logs = make([]*ethtypes.Log, len(ts.logs))
	copy(res.logs, ts.logs)
	res.transientStates = make(map[string]map[string]common.Hash, len(ts.transientStates))
	for k, v := range ts.transientStates {
		dst := make(map[string]common.Hash, len(v))
		maps.Copy(dst, v)
		res.transientStates[k] = dst
	}
	res.transientAccounts = make(map[string][]byte, len(ts.transientAccounts))
	maps.Copy(res.transientAccounts, ts.transientAccounts)
	res.transientModuleStates = make(map[string][]byte, len(ts.transientModuleStates))
	maps.Copy(res.transientModuleStates, ts.transientModuleStates)
	res.transientAccessLists = &accessList{}
	res.transientAccessLists.Addresses = make(map[common.Address]int, len(ts.transientAccessLists.Addresses))
	maps.Copy(res.transientAccessLists.Addresses, ts.transientAccessLists.Addresses)
	res.transientAccessLists.Slots = make([]map[common.Hash]struct{}, len(ts.transientAccessLists.Slots))
	for i, v := range ts.transientAccessLists.Slots {
		dst := make(map[common.Hash]struct{}, len(v))
		maps.Copy(dst, v)
		res.transientAccessLists.Slots[i] = dst
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
