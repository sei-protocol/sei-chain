package evmonlyapp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"maps"
	"math"
	"math/big"
	"runtime"
	"slices"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var logger = seilog.NewLogger("tendermint", "internal", "evmonlyapp")

// evmOnlyBaseFee is the base fee this application executes every block at.
// Admission and block validity both price against it, so they cannot diverge.
func evmOnlyBaseFee() *big.Int { return new(big.Int) }

// evmOnlyBlockMinGasPrice is the effective gas price, in wei, below which a
// transaction invalidates the block containing it. Every node must agree on
// it, so it is not an operator setting.
const evmOnlyBlockMinGasPrice = 1_000_000_000

// evmOnlyAdmissionMinGasPrice returns the local admission floor for a configured
// value, never below the block-validity floor.
func evmOnlyAdmissionMinGasPrice(configured uint64) *big.Int {
	return new(big.Int).SetUint64(max(configured, evmOnlyBlockMinGasPrice))
}

var evmOnlyBaseBalance = new(big.Int).Lsh(big.NewInt(1), 200)

type evmOnlyApplication struct {
	chainID     *big.Int
	chainConfig *params.ChainConfig
	execution   gigaconfig.ExecutionConfig
	minGasPrice *big.Int
	storage     *bootstrap.GigaStorageManager
	// epochRegistry is the epoch registry each executed block advances.
	epochRegistry *EpochRegistry
	validators    abevm.CommitteeWeights
	// customPrecompiles are the native contracts every executor runs.
	customPrecompiles utils.Option[precompiles.Registry]
	// alloc, when set, funds the chain at its initial height, and every
	// other account starts empty. Without it, every account the store holds
	// nothing for has evmOnlyBaseBalance.
	alloc utils.Option[GenesisAlloc]
	// upgradeInfoPath is where a halt on a due upgrade plan writes
	// UpgradeInfoFileName.
	upgradeInfoPath utils.Option[string]
	executor        utils.Mutex[*utils.Option[*evmonly.Executor]]
	// Lock order: executor before cursor. FinalizeBlock holds executor while
	// the block's cursor encoder takes cursor.
	cursor utils.Mutex[*evmOnlyCursorState]
	// settler publishes the executor to readers of committed state, which settle
	// its in-flight block commit before opening a store view without waiting for
	// FinalizeBlock to release executor.
	settler utils.AtomicSend[utils.Option[*evmonly.Executor]]
	// settleFailureLogged is set once a committed-state reader has logged a
	// failed commit; the failure is latched, so it is logged once.
	settleFailureLogged atomic.Bool
	// finalizePhases times FinalizeBlock's stages around the executor. It is a
	// field so each application instance has its own last-phase clock.
	// FinalizeBlock is serialized by executor, so one timer is enough per app.
	finalizePhases *seidbmetrics.PhaseTimer
}

const (
	// finalizeMeterName is the OTel meter FinalizeBlock's phase timer records to,
	// as evmonly_finalize_phase_duration_seconds_total.
	finalizeMeterName = "evmonly_app"
	finalizeTimerName = "evmonly_finalize"

	finalizePhasePrepareTxs = "prepare_txs"
	finalizePhaseExecute    = "execute"
)

// evmOnlyCursorState holds the latest finalized cursor and the cursor being finalized.
type evmOnlyCursorState struct {
	committed evmOnlyCursor
	pending   utils.Option[evmOnlyCursor]
	// lastBlockTime is the Time of the most recently finalized block, used by
	// EvmCall to reproduce that block's execution context for a read-only call.
	lastBlockTime uint64
	// pendingBlockTime is the timestamp of the block being finalized.
	pendingBlockTime uint64
}

var _ abevm.Application = (*evmOnlyApplication)(nil)

// NewEVMOnlyApplication returns the raw-Ethereum application Autobahn runs.
// Storage owns state, receipts and blocks; execution sizes the executor and
// prices admission; customPrecompiles are the native contracts the chain runs;
// alloc, when set, is the chain's genesis accounts. epochRegistry is the registry each block
// advances; it is opened from storage with OpenEpochRegistry.
// A block that halts on a due upgrade plan writes UpgradeInfoFileName under
// homeDir's data directory, when homeDir is set, and prints the halt line to
// stderr. A storage that already holds committed blocks resumes from its
// durable cursor, so Info reports the stored height and InitChain is refused.
func NewEVMOnlyApplication(
	chainID uint64,
	validators abevm.CommitteeWeights,
	storage *bootstrap.GigaStorageManager,
	epochRegistry *EpochRegistry,
	execution gigaconfig.ExecutionConfig,
	customPrecompiles utils.Option[precompiles.Registry],
	alloc utils.Option[GenesisAlloc],
	homeDir utils.Option[string],
) (abevm.Application, error) {
	if epochRegistry == nil {
		return nil, errors.New("EVM-only application requires an epoch registry")
	}
	if alloc, ok := alloc.Get(); ok {
		if alloc.InitialHeight <= 0 {
			return nil, fmt.Errorf("EVM-only genesis alloc initial height must be positive: %d", alloc.InitialHeight)
		}
		if !alloc.setsBalance() {
			return nil, errors.New("EVM-only genesis alloc funds no account")
		}
		if addr, ok := alloc.protocolAccount(); ok {
			return nil, fmt.Errorf("EVM-only genesis alloc sets protocol account %s", addr)
		}
	}
	chainConfig := *evmonly.DefaultChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(chainID)
	a := &evmOnlyApplication{
		chainID:           new(big.Int).SetUint64(chainID),
		chainConfig:       &chainConfig,
		execution:         execution,
		minGasPrice:       evmOnlyAdmissionMinGasPrice(execution.MinGasPrice),
		storage:           storage,
		epochRegistry:     epochRegistry,
		validators:        maps.Clone(validators),
		customPrecompiles: customPrecompiles,
		alloc:             alloc,
		upgradeInfoPath:   homeDir.Map(upgradeInfoPath),
		executor:          utils.NewMutex(new(utils.Option[*evmonly.Executor])),
		cursor:            utils.NewMutex(&evmOnlyCursorState{}),
		settler:           utils.NewAtomicSend(utils.None[*evmonly.Executor]()),
		finalizePhases:    seidbmetrics.NewPhaseTimer(otel.Meter(finalizeMeterName), finalizeTimerName),
	}
	cursor, err := loadEVMOnlyCursor(storage.StateDB().SC())
	if err != nil {
		return nil, err
	}
	if cursor, ok := cursor.Get(); ok {
		if err := a.checkStartedWithAlloc(storage.StateDB().SC()); err != nil {
			return nil, err
		}
		if err := a.checkStartedWithChainID(storage.StateDB().SC()); err != nil {
			return nil, err
		}
		for executor := range a.executor.Lock() {
			// A chain without an alloc resumes past its initial height, so it has nothing to set up.
			a.installExecutor(executor, a.allocInitialHeight())
		}
		for state := range a.cursor.Lock() {
			state.committed = cursor
		}
	}
	return a, nil
}

// installExecutor creates the executor, setting up the chain at initialHeight when given, and
// publishes it to both the block serializer and the settler. Called with the executor lock held.
func (a *evmOnlyApplication) installExecutor(slot *utils.Option[*evmonly.Executor], initialHeight utils.Option[int64]) {
	executor := a.newExecutor(initialHeight)
	*slot = utils.Some(executor)
	a.settler.Store(utils.Some(executor))
}

// newExecutor returns an executor for the chain that sets up the genesis alloc and the protocol
// accounts at initialHeight, when given.
func (a *evmOnlyApplication) newExecutor(initialHeight utils.Option[int64]) *evmonly.Executor {
	opts := []evmonly.Option{
		evmonly.WithStorageManager(a.storage),
		evmonly.WithStoreIndependentBlockChangeSetEncoder(a.encodeCursorChangeSet),
	}
	if !a.alloc.IsPresent() {
		opts = append(opts, evmonly.WithMissingAccountState(evmOnlyFundedState{}))
	}
	if height, ok := initialHeight.Get(); ok {
		accounts := a.alloc.Map(func(g GenesisAlloc) map[common.Address]evmonly.GenesisAccount { return g.Accounts })
		// NewEVMOnlyApplication and InitChain refuse a height that is not positive.
		opts = append(opts, evmonly.WithGenesisAlloc(uint64(height), accounts.Or(nil))) //nolint:gosec // G115: positive.
	}
	return evmonly.NewExecutor(evmonly.Config{
		ChainConfig:  a.chainConfig,
		MinGasPrice:  big.NewInt(evmOnlyBlockMinGasPrice),
		OCCWorkers:   workersOrGOMAXPROCS(a.execution.OCCWorkers),
		ParseWorkers: workersOrGOMAXPROCS(a.execution.ParseWorkers),
		// Autobahn orders transactions without validating them, so a block can hold one
		// the executor cannot apply; failing the block would halt every validator.
		RejectUnappliableTxs: true,
		BlockResultPoolSize:  a.execution.BlockResultPoolSize,
		CustomPrecompiles:    a.customPrecompiles.Or(nil),
	}, opts...)
}

// encodeCursorChangeSet chains the block into the app hash and stages the
// resulting cursor as pending, returning it as the write committed with
// the block's state. The epoch registry row is written with it when the block
// is the initial road or closes an epoch.
func (a *evmOnlyApplication) encodeCursorChangeSet(
	block evmonly.BlockContext,
	result *evmonly.BlockResult,
) ([]gigatypes.MiscChange, error) {
	height, ok := utils.SafeCast[int64](block.Number)
	if !ok {
		return nil, fmt.Errorf("EVM-only block number exceeds int64: %d", block.Number)
	}
	for state := range a.cursor.Lock() {
		if height != state.committed.height+1 {
			return nil, fmt.Errorf("EVM-only block height %d does not follow committed height %d", height, state.committed.height)
		}
		epochChanges, err := a.epochRegistry.Changes(atypes.RoadHeight(block.Number))
		if err != nil {
			return nil, err
		}
		appHash, err := hashEVMOnlyResult(state.committed.appHash, block.Number, block.BlockHash, result)
		if err != nil {
			return nil, err
		}
		next := evmOnlyCursor{
			height:    height,
			appHash:   appHash,
			blockHash: block.BlockHash,
			gasLimit:  block.GasLimit,
		}
		state.pending = utils.Some(next)
		state.pendingBlockTime = block.Time
		changes := append([]gigatypes.MiscChange{next.miscChange(), a.chainIDMiscChange()}, epochChanges...)
		if alloc, ok := a.alloc.Get(); ok && height == alloc.InitialHeight {
			changes = append(changes, alloc.miscChange())
		}
		return changes, nil
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) InitChain(req abevm.InitConfig) error {
	initialHeight, ok := utils.SafeCast[int64](req.InitialHeight)
	if !ok || initialHeight <= 0 {
		return fmt.Errorf("EVM-only initial height must be positive and fit in int64: %d", req.InitialHeight)
	}
	gasLimit := req.GasLimit
	if gasLimit == 0 || gasLimit > math.MaxInt64 {
		return fmt.Errorf("EVM-only gas limit must be positive and fit in int64: %d", gasLimit)
	}
	if alloc, ok := a.alloc.Get(); ok && alloc.InitialHeight != initialHeight {
		return fmt.Errorf("EVM-only genesis alloc is for initial height %d, not %d", alloc.InitialHeight, req.InitialHeight)
	}
	for executor := range a.executor.Lock() {
		if executor.IsPresent() {
			return fmt.Errorf("EVM-only application already initialized")
		}
		if err := a.seedInitialStateVersion(initialHeight); err != nil {
			return err
		}
		a.installExecutor(executor, utils.Some(initialHeight))
		for state := range a.cursor.Lock() {
			state.committed = evmOnlyCursor{height: initialHeight - 1, gasLimit: gasLimit}
		}
		return nil
	}
	panic("unreachable")
}

// seedInitialStateVersion moves an empty state store to the version preceding
// initialHeight. A store already seeded there is accepted, since a crash
// between InitChain and the first block leaves it that way; a store holding
// any block is refused.
func (a *evmOnlyApplication) seedInitialStateVersion(initialHeight int64) error {
	stateStore := a.storage.StateDB().SC()
	latest, err := stateStore.GetLatestVersion()
	if err != nil {
		return fmt.Errorf("read EVM-only state version: %w", err)
	}
	switch {
	case latest == initialHeight-1:
		return nil
	case latest != 0:
		return fmt.Errorf("EVM-only state is already at height %d before InitChain", latest)
	}
	if err := stateStore.SetInitialVersion(initialHeight); err != nil {
		return fmt.Errorf("seed EVM-only initial state version %d: %w", initialHeight, err)
	}
	return nil
}

func (a *evmOnlyApplication) Info() *abevm.ResponseInfo {
	for state := range a.cursor.Lock() {
		return &abevm.ResponseInfo{
			Data:             "evmonly",
			LastBlockHeight:  state.committed.height,
			LastBlockAppHash: append([]byte(nil), state.committed.appHash[:]...),
		}
	}
	panic("unreachable")
}

// RestoreBlockTime sets the committed block time after restart.
func (a *evmOnlyApplication) RestoreBlockTime(blockTime time.Time) {
	if blockTime.Unix() < 0 {
		return
	}
	for state := range a.cursor.Lock() {
		state.lastBlockTime = uint64(blockTime.Unix()) //nolint:gosec // The timestamp is non-negative above.
	}
}

func (a *evmOnlyApplication) LastBlockHeight() int64 {
	for state := range a.cursor.Lock() {
		return state.committed.height
	}
	panic("unreachable")
}

// EvmGasLimit returns the gas limit of the most recently committed block.
// This application never changes it after InitChain, so it is also the gas
// limit of every earlier committed block.
func (a *evmOnlyApplication) EvmGasLimit() uint64 {
	for state := range a.cursor.Lock() {
		return state.committed.gasLimit
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) GetValidators() abevm.CommitteeWeights {
	return maps.Clone(a.validators)
}

func (a *evmOnlyApplication) CheckTx(tx abevm.EvmTx) (abevm.AdmittedTx, error) {
	// TODO(evmonly-production): close the gap between admission and block validity
	// before accepting arbitrary traffic; this test app assumes executable load-test transactions.
	if err := a.checkAdmissionPolicy(tx); err != nil {
		return abevm.AdmittedTx{}, err
	}
	if tx.Gas() > math.MaxInt64 {
		return abevm.AdmittedTx{}, errors.New("transaction gas limit exceeds int64")
	}
	if err := a.checkGasCoversIntrinsic(tx.Transaction(), tx.Sender()); err != nil {
		return abevm.AdmittedTx{}, err
	}
	if err := a.checkSenderCanPay(tx.Transaction(), tx.Sender()); err != nil {
		return abevm.AdmittedTx{}, err
	}
	return abevm.AdmittedTx{GasEstimated: tx.Gas()}, nil
}

// checkGasCoversIntrinsic refuses tx when its gas limit is below the intrinsic gas, or the EIP-7623
// calldata floor, that the next block charges it before execution.
func (a *evmOnlyApplication) checkGasCoversIntrinsic(tx *ethtypes.Transaction, sender common.Address) error {
	value, overflow := uint256.FromBig(tx.Value())
	if overflow {
		return errors.New("ethereum transaction value exceeds 256 bits")
	}
	rules := a.nextBlockRules()
	intrinsic, err := ethcore.IntrinsicGas(tx.Data(), tx.AccessList(), tx.SetCodeAuthorizations(), sender, tx.To(), value, rules)
	if err != nil {
		return err
	}
	if tx.Gas() < intrinsic {
		return fmt.Errorf("%w: have %d, want %d", ethcore.ErrIntrinsicGas, tx.Gas(), intrinsic)
	}
	if !rules.IsPrague {
		return nil
	}
	floor, err := ethcore.FloorDataGas(rules, sender, tx.To(), value, tx.Data(), tx.AccessList())
	if err != nil {
		return err
	}
	if tx.Gas() < floor {
		return fmt.Errorf("%w: have %d, want %d", ethcore.ErrFloorDataGas, tx.Gas(), floor)
	}
	return nil
}

// nextBlockRules returns the fork rules of the block after the committed one, at the committed
// block's time.
func (a *evmOnlyApplication) nextBlockRules() params.Rules {
	for state := range a.cursor.Lock() {
		// Every block's context sets PrevRandao, so the executor always runs post-merge rules.
		return a.chainConfig.Rules(big.NewInt(state.committed.height+1), true, state.lastBlockTime)
	}
	panic("unreachable")
}

// checkSenderCanPay refuses tx when sender's committed balance does not cover its gas limit at its fee
// cap plus its value. Before the alloc's block commits, a sender holds its alloc.
func (a *evmOnlyApplication) checkSenderCanPay(tx *ethtypes.Transaction, sender common.Address) error {
	cost, overflow := uint256.FromBig(tx.Cost())
	if overflow {
		return errors.New("ethereum transaction cost exceeds 256 bits")
	}
	balance := a.admissionBalance(sender)
	if balance.Lt(cost) {
		return fmt.Errorf("insufficient funds for gas * price + value: address %s have %s want %s", sender, balance.Dec(), cost.Dec())
	}
	return nil
}

// admissionBalance returns sender's balance in the latest committed state, which is its alloc until
// that state holds the alloc's block.
func (a *evmOnlyApplication) admissionBalance(sender common.Address) uint256.Int {
	view := a.storage.StateDB().OpenView()
	defer view.Close()
	if alloc, ok := a.alloc.Get(); ok && view.GetBlockHeight() < alloc.InitialHeight {
		if account, ok := alloc.Accounts[sender]; ok && account.Balance != nil {
			return *account.Balance
		}
		return uint256.Int{}
	}
	return a.balance(view, sender)
}

func (a *evmOnlyApplication) checkAdmissionPolicy(tx abevm.EvmTx) error {
	// The predicate block validity uses, not tx.GasPrice(): on a dynamic-fee tx
	// that is the fee cap, so admitting on it let through transactions the
	// executor then refused — and an executor refusal is a node panic, not a
	// failed receipt.
	if evmonly.EffectiveGasPrice(tx.Transaction(), evmOnlyBaseFee()).Cmp(a.minGasPrice) < 0 {
		return fmt.Errorf("insufficient fee: effective gas price is below %s", a.minGasPrice)
	}
	return nil
}

func evmOnlyStoreAddress(address common.Address) gigatypes.Address {
	var storeAddress gigatypes.Address
	copy(storeAddress[:], address[:])
	return storeAddress
}

// AwaitCommits blocks until every block this application has finalized is in
// its store, and reports the first commit that failed.
func (a *evmOnlyApplication) AwaitCommits() error {
	executor, ok := a.settler.Load().Get()
	if !ok {
		return nil
	}
	return executor.AwaitCommits()
}

// openSettledView opens a store view that holds every block finalized so far.
// A failed commit is logged once rather than returned: the view is still a
// consistent version, and the failure halts the node through the next
// FinalizeBlock.
func (a *evmOnlyApplication) openSettledView() gigatypes.StateView {
	if err := a.AwaitCommits(); err != nil && !a.settleFailureLogged.Swap(true) {
		logger.Error("EVM-only block commit failed; serving the last committed state", "err", err)
	}
	return a.storage.StateDB().OpenView()
}

func (a *evmOnlyApplication) EvmNonce(address common.Address) uint64 {
	snapshot := a.openSettledView()
	defer snapshot.Close()
	return snapshot.GetNonce(evmOnlyStoreAddress(address))
}

// EvmCommittedNonce returns address's nonce in the last committed state, without waiting for the
// commit in flight, so it can trail EvmNonce by the most recently finalized block.
func (a *evmOnlyApplication) EvmCommittedNonce(address common.Address) uint64 {
	snapshot := a.storage.StateDB().OpenView()
	defer snapshot.Close()
	return snapshot.GetNonce(evmOnlyStoreAddress(address))
}

func (a *evmOnlyApplication) EvmBalance(address common.Address) uint256.Int {
	snapshot := a.openSettledView()
	defer snapshot.Close()
	return a.balance(snapshot, address)
}

// balance reads address's balance from view. Without a genesis alloc, an account view holds no state
// for has evmOnlyBaseBalance.
func (a *evmOnlyApplication) balance(view gigatypes.StateView, address common.Address) uint256.Int {
	if !a.alloc.IsPresent() && !view.AccountExists(evmOnlyStoreAddress(address)) {
		return *uint256.MustFromBig(evmOnlyBaseBalance)
	}
	balance := view.GetBalance(evmOnlyStoreAddress(address))
	return *new(uint256.Int).SetBytes(balance[:])
}

// evmStateAt opens the committed EVM state after height: the settled view when it stands at height,
// and the state store's history otherwise. It fails when neither holds that height.
func (a *evmOnlyApplication) evmStateAt(height int64) (gigatypes.StateView, error) {
	// The newest block is read from the settled view, since the state store applies blocks behind it.
	view := a.openSettledView()
	if view.GetBlockHeight() == height {
		return view, nil
	}
	view.Close()
	view, ok := a.storage.StateDB().OpenViewAt(height)
	if !ok {
		return nil, fmt.Errorf("the EVM state after block %d is not held by this node", height)
	}
	return view, nil
}

// EvmNonceAt returns address's nonce in the committed EVM state after height.
func (a *evmOnlyApplication) EvmNonceAt(address common.Address, height int64) (uint64, error) {
	view, err := a.evmStateAt(height)
	if err != nil {
		return 0, err
	}
	defer view.Close()
	return view.GetNonce(evmOnlyStoreAddress(address)), nil
}

// EvmBalanceAt returns address's balance in the committed EVM state after height.
func (a *evmOnlyApplication) EvmBalanceAt(address common.Address, height int64) (uint256.Int, error) {
	view, err := a.evmStateAt(height)
	if err != nil {
		return uint256.Int{}, err
	}
	defer view.Close()
	return a.balance(view, address), nil
}

// EvmCodeAt returns the contract code at address in the committed EVM state after height, or nil when
// the account held none.
func (a *evmOnlyApplication) EvmCodeAt(address common.Address, height int64) ([]byte, error) {
	view, err := a.evmStateAt(height)
	if err != nil {
		return nil, err
	}
	defer view.Close()
	return slices.Clone(view.GetCode(evmOnlyStoreAddress(address))), nil
}

// EvmStorageAt returns the value at key in address's storage in the committed EVM state after height,
// or the zero hash when the slot was unset.
func (a *evmOnlyApplication) EvmStorageAt(address common.Address, key common.Hash, height int64) (common.Hash, error) {
	view, err := a.evmStateAt(height)
	if err != nil {
		return common.Hash{}, err
	}
	defer view.Close()
	return view.GetStorage(evmOnlyStoreAddress(address), key), nil
}

// EvmMinGasPrice returns the minimum effective gas price this application admits a transaction
// at. Admission and eth_gasPrice's suggestion both price against it, so they cannot diverge.
func (a *evmOnlyApplication) EvmMinGasPrice() *big.Int {
	return new(big.Int).Set(a.minGasPrice)
}

// workersOrGOMAXPROCS returns n, or GOMAXPROCS when n is 0.
func workersOrGOMAXPROCS(n int) int {
	if n == 0 {
		return runtime.GOMAXPROCS(0)
	}
	return n
}

// EvmCode returns the contract code at address in the most recently committed
// EVM state, or nil when the account holds none.
func (a *evmOnlyApplication) EvmCode(address common.Address) []byte {
	snapshot := a.openSettledView()
	defer snapshot.Close()
	return slices.Clone(snapshot.GetCode(evmOnlyStoreAddress(address)))
}

// EvmStorage returns the value at key in address's storage in the most
// recently committed EVM state, or the zero hash when the slot is unset.
func (a *evmOnlyApplication) EvmStorage(address common.Address, key common.Hash) common.Hash {
	snapshot := a.openSettledView()
	defer snapshot.Close()
	return snapshot.GetStorage(evmOnlyStoreAddress(address), key)
}

func (a *evmOnlyApplication) EvmChainID() uint64 {
	return a.chainID.Uint64()
}

// EvmChainConfig returns the EVM chain configuration this node executes against.
func (a *evmOnlyApplication) EvmChainConfig() *params.ChainConfig {
	return a.chainConfig
}

// EvmBaseFee returns the base fee this application executes every block at.
func (a *evmOnlyApplication) EvmBaseFee() *big.Int {
	return evmOnlyBaseFee()
}

// evmOnlyPrevRandao derives a deterministic PrevRandao from a block timestamp.
func evmOnlyPrevRandao(timestamp uint64) common.Hash {
	return crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, timestamp))
}

// blockContext returns the context this application executes a block at. Coinbase is zero, and
// the base fee and blob base fee are always zero.
func (a *evmOnlyApplication) blockContext(number, timestamp, gasLimit uint64, parentHash, blockHash common.Hash) evmonly.BlockContext {
	return evmonly.BlockContext{
		Number:      number,
		Time:        timestamp,
		GasLimit:    gasLimit,
		ChainID:     new(big.Int).Set(a.chainID),
		BaseFee:     evmOnlyBaseFee(),
		BlobBaseFee: new(big.Int),
		ParentHash:  parentHash,
		BlockHash:   blockHash,
		PrevRandao:  evmOnlyPrevRandao(timestamp),
	}
}

// currentExecutionContext returns the executor and block context for a
// read-only EVM execution against the most recently committed state. action
// names the caller for its error messages, e.g. "call" or "gas estimate".
func (a *evmOnlyApplication) currentExecutionContext(action string) (*evmonly.Executor, evmonly.BlockContext, error) {
	for exec := range a.executor.Lock() {
		executor, ok := exec.Get()
		if !ok {
			return nil, evmonly.BlockContext{}, fmt.Errorf("EVM-only %s attempted before InitChain", action)
		}
		blockCtx, err := a.committedBlockContext(action)
		if err != nil {
			return nil, evmonly.BlockContext{}, err
		}
		// The committed block's state may still be landing. Holding executor keeps
		// the next block from starting, so once settled the store is at blockCtx.Number.
		if err := executor.AwaitCommits(); err != nil {
			return nil, evmonly.BlockContext{}, err
		}
		// Executor opens its own state snapshot later, outside this lock, so a
		// commit landing in between can pair this BlockContext with a newer one.
		return executor, blockCtx, nil
	}
	panic("unreachable")
}

// committedBlockContext returns the block context of the latest finalized block, and
// refuses while finalization is incomplete. action names the caller for
// its error messages.
func (a *evmOnlyApplication) committedBlockContext(action string) (evmonly.BlockContext, error) {
	for state := range a.cursor.Lock() {
		if state.pending.IsPresent() {
			// A staged cursor has not been published with its block context yet.
			return evmonly.BlockContext{}, fmt.Errorf("EVM-only %s attempted while block finalization is incomplete", action)
		}
		number, ok := utils.SafeCast[uint64](state.committed.height)
		if !ok {
			return evmonly.BlockContext{}, fmt.Errorf("EVM-only committed height exceeds uint64: %d", state.committed.height)
		}
		// ParentHash is left zero.
		return a.blockContext(number, state.lastBlockTime, state.committed.gasLimit, common.Hash{}, state.committed.blockHash), nil
	}
	panic("unreachable")
}

// EvmCall executes msg as a read-only call against the most recently
// committed EVM state, with overrides applied, and returns the execution result.
func (a *evmOnlyApplication) EvmCall(ctx context.Context, msg *ethcore.Message, overrides abevm.CallOverrides) (*ethcore.ExecutionResult, error) {
	executor, blockCtx, err := a.currentExecutionContext("call")
	if err != nil {
		return nil, err
	}
	view, err := executor.OpenCurrentView(ctx, blockCtx)
	if err != nil {
		return nil, err
	}
	defer view.Close()
	return executor.CallAt(ctx, view, blockCtx, msg, overrides)
}

// EvmEstimateGas returns the lowest gas limit that lets msg execute
// successfully against the most recently committed EVM state, with overrides
// applied. Like EvmCall it creates no transaction and persists no state change.
func (a *evmOnlyApplication) EvmEstimateGas(ctx context.Context, msg *ethcore.Message, gasCap uint64, overrides abevm.CallOverrides) (uint64, []byte, error) {
	executor, blockCtx, err := a.currentExecutionContext("gas estimate")
	if err != nil {
		return 0, nil, err
	}
	view, err := executor.OpenCurrentView(ctx, blockCtx)
	if err != nil {
		return 0, nil, err
	}
	defer view.Close()
	return executor.EstimateGasAt(ctx, view, blockCtx, msg, gasCap, overrides)
}

// EvmCallAt executes msg against the committed state after block, with overrides applied.
func (a *evmOnlyApplication) EvmCallAt(ctx context.Context, msg *ethcore.Message, block abevm.BlockRef, overrides abevm.CallOverrides) (_ *ethcore.ExecutionResult, err error) {
	executor, view, blockCtx, err := a.pastExecutionContext("call", block)
	if err != nil {
		return nil, err
	}
	defer view.Close()
	defer recoverPastExecution(view, "call", &err)
	return executor.CallAt(ctx, view, blockCtx, msg, overrides)
}

// EvmEstimateGasAt returns the lowest gas limit that lets msg execute successfully against the
// committed EVM state after block, in the context that block executed at, with overrides
// applied.
func (a *evmOnlyApplication) EvmEstimateGasAt(ctx context.Context, msg *ethcore.Message, gasCap uint64, block abevm.BlockRef, overrides abevm.CallOverrides) (_ uint64, _ []byte, err error) {
	executor, view, blockCtx, err := a.pastExecutionContext("gas estimate", block)
	if err != nil {
		return 0, nil, err
	}
	defer view.Close()
	defer recoverPastExecution(view, "gas estimate", &err)
	return executor.EstimateGasAt(ctx, view, blockCtx, msg, gasCap, overrides)
}

// pastExecutionContext returns the executor, the committed EVM state after block, which the
// caller must close, and the context that block executed at, for a read-only execution. action names
// the caller for its error messages.
func (a *evmOnlyApplication) pastExecutionContext(action string, block abevm.BlockRef) (*evmonly.Executor, gigatypes.StateView, evmonly.BlockContext, error) {
	executor, err := a.loadedExecutor(action)
	if err != nil {
		return nil, nil, evmonly.BlockContext{}, err
	}
	number, err := blockNumber(block)
	if err != nil {
		return nil, nil, evmonly.BlockContext{}, err
	}
	view, err := a.evmStateAt(int64(number)) //nolint:gosec // G115: blockNumber bounds number to int64.
	if err != nil {
		return nil, nil, evmonly.BlockContext{}, err
	}
	return executor, view, a.traceBlockContext(number, block), nil
}

// recoverPastExecution turns a panic of a read-only execution against view, such as a read of a height
// pruned during it, into *err. It must be deferred.
func recoverPastExecution(view gigatypes.StateView, action string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("EVM-only %s against the state after block %d: %v", action, view.GetBlockHeight(), r)
	}
}

// EvmEstimateGasAfterCalls returns what EvmEstimateGas returns for msg against the most recently
// committed EVM state with overrides and then calls applied to it first, in order.
func (a *evmOnlyApplication) EvmEstimateGasAfterCalls(ctx context.Context, calls []*ethcore.Message, msg *ethcore.Message, gasCap uint64, overrides abevm.CallOverrides) (uint64, []byte, error) {
	executor, blockCtx, err := a.currentExecutionContext("gas estimate")
	if err != nil {
		return 0, nil, err
	}
	view, err := executor.OpenCurrentView(ctx, blockCtx)
	if err != nil {
		return 0, nil, err
	}
	defer view.Close()
	return executor.EstimateGasAfterCallsAt(ctx, view, blockCtx, calls, msg, gasCap, overrides)
}

// EvmEstimateGasAfterCallsAt estimates msg after applying overrides and calls to the state after block.
func (a *evmOnlyApplication) EvmEstimateGasAfterCallsAt(ctx context.Context, calls []*ethcore.Message, msg *ethcore.Message, gasCap uint64, block abevm.BlockRef, overrides abevm.CallOverrides) (_ uint64, _ []byte, err error) {
	executor, view, blockCtx, err := a.pastExecutionContext("gas estimate", block)
	if err != nil {
		return 0, nil, err
	}
	defer view.Close()
	defer recoverPastExecution(view, "gas estimate", &err)
	return executor.EstimateGasAfterCallsAt(ctx, view, blockCtx, calls, msg, gasCap, overrides)
}

// EvmCreateAccessList returns the access list msg touches when executed against the most recently
// committed EVM state, and the result of executing msg with that list.
func (a *evmOnlyApplication) EvmCreateAccessList(ctx context.Context, msg *ethcore.Message) (ethtypes.AccessList, *ethcore.ExecutionResult, error) {
	executor, blockCtx, err := a.currentExecutionContext("access list")
	if err != nil {
		return nil, nil, err
	}
	return executor.CreateAccessList(ctx, blockCtx, msg)
}

// EvmCreateAccessListAt returns the access list msg touches when executed against the committed EVM
// state after block, in the context that block executed at, and the result of executing
// msg with that list.
func (a *evmOnlyApplication) EvmCreateAccessListAt(ctx context.Context, msg *ethcore.Message, block abevm.BlockRef) (_ ethtypes.AccessList, _ *ethcore.ExecutionResult, err error) {
	executor, view, blockCtx, err := a.pastExecutionContext("access list", block)
	if err != nil {
		return nil, nil, err
	}
	defer view.Close()
	defer recoverPastExecution(view, "access list", &err)
	return executor.CreateAccessListAt(ctx, view, blockCtx, msg)
}

func (a *evmOnlyApplication) FinalizeBlock(ctx context.Context, req abevm.FinalizeBlockReq) (abevm.FinalizeBlockResp, error) {
	response, err := a.finalizeBlock(ctx, req)
	var needed *upgrade.NeededError
	if errors.As(err, &needed) {
		return abevm.FinalizeBlockResp{}, a.haltForUpgrade(needed, err)
	}
	return response, err
}

func (a *evmOnlyApplication) finalizeBlock(ctx context.Context, req abevm.FinalizeBlockReq) (abevm.FinalizeBlockResp, error) {
	height, validHeight := utils.SafeCast[int64](req.Height)
	if !validHeight || height <= 0 {
		return abevm.FinalizeBlockResp{}, fmt.Errorf("EVM-only block height must be positive and fit in int64: %d", req.Height)
	}
	number := uint64(req.Height)
	timestamp, ok := utils.SafeCast[uint64](req.Time.Unix())
	if !ok {
		return abevm.FinalizeBlockResp{}, fmt.Errorf("EVM-only block timestamp is negative: %s", req.Time)
	}
	blockHash := req.Hash
	for executor := range a.executor.Lock() {
		executor, ok := executor.Get()
		if !ok {
			return abevm.FinalizeBlockResp{}, fmt.Errorf("EVM-only block finalized before InitChain")
		}
		parent, err := a.beginBlock(height)
		if err != nil {
			return abevm.FinalizeBlockResp{}, err
		}
		// Closes the stage in flight, so the gap until the next block is charged to neither.
		defer a.finalizePhases.Reset()
		result, err := a.executeBlockPipelined(ctx, executor,
			a.blockContext(number, timestamp, parent.gasLimit, parent.blockHash, blockHash), req.Txs)
		if err != nil {
			return abevm.FinalizeBlockResp{}, errors.Join(err, a.abandonPending(executor, height))
		}
		defer result.Release()
		if err := a.finishBlock(height); err != nil {
			return abevm.FinalizeBlockResp{}, err
		}
		return abevm.FinalizeBlockResp{GasUsed: result.GasUsed}, nil
	}
	panic("unreachable")
}

// beginBlock checks height is the next block to finalize and returns the
// committed cursor it builds on.
func (a *evmOnlyApplication) beginBlock(height int64) (evmOnlyCursor, error) {
	for state := range a.cursor.Lock() {
		if state.pending.IsPresent() {
			return evmOnlyCursor{}, fmt.Errorf("EVM-only block %d finalized before the previous block finished finalizing", height)
		}
		if next := state.committed.height + 1; height != next {
			return evmOnlyCursor{}, fmt.Errorf("EVM-only block height %d does not match next height %d", height, next)
		}
		return state.committed, nil
	}
	panic("unreachable")
}

// abandonPending waits for storage commits and drops the staged cursor if its block is not durable.
// A durable cursor remains pending after a failed finalization.
func (a *evmOnlyApplication) abandonPending(executor *evmonly.Executor, height int64) error {
	commitErr := executor.AwaitCommits()
	latest, err := a.storage.StateDB().SC().GetLatestVersion()
	if err != nil {
		return errors.Join(commitErr, fmt.Errorf("read EVM-only state version: %w", err))
	}
	if latest >= height {
		return commitErr
	}
	for state := range a.cursor.Lock() {
		state.pending = utils.None[evmOnlyCursor]()
	}
	return commitErr
}

// finishBlock publishes the cursor and timestamp of the finalized block.
func (a *evmOnlyApplication) finishBlock(height int64) error {
	for state := range a.cursor.Lock() {
		pending, ok := state.pending.Get()
		if !ok || pending.height != height {
			return fmt.Errorf("EVM-only block %d finalized without staging its cursor", height)
		}
		state.committed = pending
		state.lastBlockTime = state.pendingBlockTime
		state.pending = utils.None[evmOnlyCursor]()
		return nil
	}
	panic("unreachable")
}

// executeBlockPipelined executes the block and returns once its state commit
// has started, leaving the commit to land while the next block executes.
// Readers of committed state settle it through AwaitCommits.
func (a *evmOnlyApplication) executeBlockPipelined(ctx context.Context, executor *evmonly.Executor, block evmonly.BlockContext, txs []abevm.EvmTx) (*evmonly.BlockResult, error) {
	a.finalizePhases.SetPhase(finalizePhasePrepareTxs)
	prepared, err := executor.PrepareDecodedBlock(ctx, block, txs)
	if err != nil {
		return nil, err
	}
	// The executor's own timer breaks execution down further.
	a.finalizePhases.SetPhase(finalizePhaseExecute)
	return executor.ExecutePreparedBlock(ctx, prepared)
}

func hashEVMOnlyResult(previous common.Hash, height uint64, blockHash common.Hash, result *evmonly.BlockResult) (common.Hash, error) {
	h := sha256.New()
	w := newEVMOnlyHashWriter(h)
	w.write(previous[:])
	w.writeUint64(height)
	w.write(blockHash[:])
	w.writeUint64(result.GasUsed)
	changesets, err := evmonly.EncodeMemoryStoreChangeSet(result.ChangeSet)
	if err != nil {
		return common.Hash{}, err
	}
	for _, changeset := range changesets {
		w.writeSizedString(changeset.Name)
		for _, pair := range changeset.Changeset.Pairs {
			w.writeSized(pair.Key)
			if pair.Delete {
				w.writeByte(1)
			} else {
				w.writeByte(0)
			}
			w.writeSized(pair.Value)
		}
	}
	if err := w.flush(); err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(h.Sum(nil)), nil
}

// evmOnlyHashBufferSize is the buffer between the stream and the hash.
const evmOnlyHashBufferSize = 32 << 10

// evmOnlyHashWriter buffers the app-hash byte stream into a hash; flush before reading the digest.
type evmOnlyHashWriter struct {
	buf     *bufio.Writer
	scratch [8]byte
}

func newEVMOnlyHashWriter(h hash.Hash) *evmOnlyHashWriter {
	return &evmOnlyHashWriter{buf: bufio.NewWriterSize(h, evmOnlyHashBufferSize)}
}

func (w *evmOnlyHashWriter) write(value []byte) {
	_, _ = w.buf.Write(value)
}

func (w *evmOnlyHashWriter) writeByte(value byte) {
	_ = w.buf.WriteByte(value)
}

func (w *evmOnlyHashWriter) writeUint64(value uint64) {
	binary.BigEndian.PutUint64(w.scratch[:], value)
	_, _ = w.buf.Write(w.scratch[:])
}

// writeSized writes value behind its length.
func (w *evmOnlyHashWriter) writeSized(value []byte) {
	w.writeUint64(uint64(len(value)))
	_, _ = w.buf.Write(value)
}

func (w *evmOnlyHashWriter) writeSizedString(value string) {
	w.writeUint64(uint64(len(value)))
	_, _ = w.buf.WriteString(value)
}

func (w *evmOnlyHashWriter) flush() error {
	return w.buf.Flush()
}

// GenesisAlloc is each listed account's balance, nonce, code and storage at the chain's initial
// height.
type GenesisAlloc struct {
	InitialHeight int64
	Accounts      map[common.Address]evmonly.GenesisAccount
}

// allocInitialHeight returns the genesis alloc's initial height, or None without an alloc.
func (a *evmOnlyApplication) allocInitialHeight() utils.Option[int64] {
	return a.alloc.Map(func(g GenesisAlloc) int64 { return g.InitialHeight })
}

// protocolAccount returns an account g sets that every chain holds as a
// protocol account.
func (g GenesisAlloc) protocolAccount() (common.Address, bool) {
	for addr := range evmonly.ProtocolAccounts() {
		if _, ok := g.Accounts[addr]; ok {
			return addr, true
		}
	}
	return common.Address{}, false
}

// setsBalance reports whether any account has a Balance.
func (g GenesisAlloc) setsBalance() bool {
	for _, account := range g.Accounts {
		if account.Balance != nil {
			return true
		}
	}
	return false
}

// evmOnlyGenesisAllocKey holds the digest of the alloc a chain started with. Like the cursor it is
// written outside the EVM state hash, with the initial height's block.
const evmOnlyGenesisAllocKey = "genesis_alloc"

func (g GenesisAlloc) digest() common.Hash {
	// An alloc of balances alone keeps the digest it had before accounts carried more than a
	// balance, so a chain started with one still resumes.
	if !g.balancesOnly() {
		return g.accountsDigest()
	}
	buf := make([]byte, 0, 8+len(g.Accounts)*(common.AddressLength+32))
	buf = binary.BigEndian.AppendUint64(buf, uint64(g.InitialHeight)) //nolint:gosec // G115: NewEVMOnlyApplication refuses a height that is not positive.
	for _, addr := range slices.SortedFunc(maps.Keys(g.Accounts), common.Address.Cmp) {
		balance := g.Accounts[addr].Balance.Bytes32()
		buf = append(append(buf, addr[:]...), balance[:]...)
	}
	return crypto.Keccak256Hash(buf)
}

// evmOnlyGenesisAccountsDigestTag prefixes the digest input of an alloc with an account that sets
// more or less than a balance.
const evmOnlyGenesisAccountsDigestTag = "accounts-v1"

// accountsDigest is the digest of an alloc with an account that sets more or less than a balance.
func (g GenesisAlloc) accountsDigest() common.Hash {
	buf := []byte(evmOnlyGenesisAccountsDigestTag)
	buf = binary.BigEndian.AppendUint64(buf, uint64(g.InitialHeight)) //nolint:gosec // G115: NewEVMOnlyApplication refuses a height that is not positive.
	for _, addr := range slices.SortedFunc(maps.Keys(g.Accounts), common.Address.Cmp) {
		account := g.Accounts[addr]
		buf = append(buf, addr[:]...)
		if account.Balance != nil {
			balance := account.Balance.Bytes32()
			buf = append(append(buf, 1), balance[:]...)
		} else {
			buf = append(buf, 0)
		}
		buf = binary.BigEndian.AppendUint64(buf, account.Nonce)
		buf = append(buf, crypto.Keccak256(account.Code)...)
		buf = binary.BigEndian.AppendUint64(buf, uint64(len(account.Storage)))
		for _, key := range slices.SortedFunc(maps.Keys(account.Storage), common.Hash.Cmp) {
			value := account.Storage[key]
			buf = append(append(buf, key[:]...), value[:]...)
		}
	}
	return crypto.Keccak256Hash(buf)
}

// balancesOnly reports whether every account sets a balance and nothing else.
func (g GenesisAlloc) balancesOnly() bool {
	for _, account := range g.Accounts {
		if account.Balance == nil || account.Nonce != 0 || len(account.Code) > 0 || len(account.Storage) > 0 {
			return false
		}
	}
	return true
}

func (g GenesisAlloc) miscChange() gigatypes.MiscChange {
	digest := g.digest()
	return gigatypes.MiscChange{
		Module: evmOnlyCursorModule,
		Key:    []byte(evmOnlyGenesisAllocKey),
		Value:  digest[:],
	}
}

// checkStartedWithAlloc refuses to resume a chain with an alloc other than the one it started with:
// adding one would empty every account the chain has not written, and dropping one would give each
// 2^200 wei.
func (a *evmOnlyApplication) checkStartedWithAlloc(store *flatkv.CommitStore) error {
	started, found := store.Get(evmOnlyCursorModule, []byte(evmOnlyGenesisAllocKey))
	alloc, ok := a.alloc.Get()
	switch {
	case !ok && found:
		return errors.New("EVM-only chain started with a genesis alloc, but none is set")
	case ok && !found:
		return errors.New("EVM-only chain started without a genesis alloc, but one is set")
	case ok && !bytes.Equal(started, alloc.digest().Bytes()):
		return errors.New("EVM-only genesis alloc differs from the one the chain started with")
	}
	return nil
}

// evmOnlyChainIDKey holds the chain ID the chain runs on, written outside the EVM state hash with
// every block.
const evmOnlyChainIDKey = "chain_id"

func (a *evmOnlyApplication) chainIDMiscChange() gigatypes.MiscChange {
	return gigatypes.MiscChange{
		Module: evmOnlyCursorModule,
		Key:    []byte(evmOnlyChainIDKey),
		Value:  binary.BigEndian.AppendUint64(nil, a.chainID.Uint64()),
	}
}

// checkStartedWithChainID refuses to resume a chain on a chain ID other than the one it ran on, which
// would reject its transactions and change what CHAINID returns.
func (a *evmOnlyApplication) checkStartedWithChainID(store *flatkv.CommitStore) error {
	raw, found := store.Get(evmOnlyCursorModule, []byte(evmOnlyChainIDKey))
	if !found {
		return errors.New("EVM-only chain has no recorded chain ID; reset it onto a new evm_chain_id")
	}
	if len(raw) != 8 {
		return fmt.Errorf("EVM-only chain ID record has %d bytes, want 8", len(raw))
	}
	started := binary.BigEndian.Uint64(raw)
	if got := a.chainID.Uint64(); got != started {
		return fmt.Errorf("EVM-only chain ID %d differs from %d, the one the chain ran on", got, started)
	}
	return nil
}

type evmOnlyFundedState struct{}

func (evmOnlyFundedState) GetBalance(common.Address) *big.Int {
	return new(big.Int).Set(evmOnlyBaseBalance)
}
func (evmOnlyFundedState) GetNonce(common.Address) uint64                   { return 0 }
func (evmOnlyFundedState) GetCode(common.Address) []byte                    { return nil }
func (evmOnlyFundedState) GetState(common.Address, common.Hash) common.Hash { return common.Hash{} }
