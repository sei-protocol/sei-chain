package evmonlyapp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"runtime"
	"slices"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/seilog"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

// evmOnlyBlockMinGasPrice is the effective gas price, in wei, below which a
// transaction invalidates the block containing it. Every node must agree on
// it, so it is not an operator setting.
const evmOnlyBlockMinGasPrice = 1_000_000_000

// checkedSendersCap bounds the senders remembered from CheckTx per generation.
// Entries are dropped as their transactions execute; the cap only guards against
// admitted transactions that never reach a block.
const checkedSendersCap = 1 << 18

// minTxsPerHashWorker is the minimum transaction count assigned to a hash worker.
const minTxsPerHashWorker = 64

const (
	// finalizeMeterName is the OTel meter FinalizeBlock's phase timer records to,
	// as evmonly_finalize_phase_duration_seconds_total.
	finalizeMeterName = "evmonly_app"
	finalizeTimerName = "evmonly_finalize"

	finalizePhaseTakeSenders = "take_senders"
	finalizePhasePrepare     = "prepare"
	finalizePhaseExecute     = "execute"
	finalizePhaseTxResults   = "tx_results"
)

// evmOnlyHashBufferSize is the buffer between the stream and the hash.
const evmOnlyHashBufferSize = 32 << 10

var logger = seilog.NewLogger("tendermint", "internal", "evmonlyapp")

// evmOnlyBaseFee is the base fee this application executes every block at.
// Admission and block validity both price against it, so they cannot diverge.
func evmOnlyBaseFee() *big.Int { return new(big.Int) }

// evmOnlyAdmissionMinGasPrice returns the local admission floor for a configured
// value, never below the block-validity floor.
func evmOnlyAdmissionMinGasPrice(configured uint64) *big.Int {
	return new(big.Int).SetUint64(max(configured, evmOnlyBlockMinGasPrice))
}

var evmOnlyBaseBalance = new(big.Int).Lsh(big.NewInt(1), 200)

// senderCache remembers the sender recovered for each transaction hash. It keeps
// two generations: inserts go to fresh, and once fresh reaches the cap it
// becomes stale and the previous stale generation is forgotten, so the most
// recent entries always survive a rollover.
type senderCache struct {
	fresh, stale map[common.Hash]common.Address
}

func newSenderCache() senderCache {
	return senderCache{fresh: map[common.Hash]common.Address{}}
}

func (c *senderCache) put(hash common.Hash, sender common.Address) {
	if len(c.fresh) >= checkedSendersCap {
		c.stale, c.fresh = c.fresh, make(map[common.Hash]common.Address, len(c.fresh))
	}
	c.fresh[hash] = sender
}

// peek returns the sender remembered for hash, if any, and keeps it.
func (c *senderCache) peek(hash common.Hash) utils.Option[common.Address] {
	for _, gen := range [...]map[common.Hash]common.Address{c.fresh, c.stale} {
		if sender, ok := gen[hash]; ok {
			return utils.Some(sender)
		}
	}
	return utils.None[common.Address]()
}

// take returns the sender remembered for hash, if any, and forgets it.
func (c *senderCache) take(hash common.Hash) utils.Option[common.Address] {
	for _, gen := range [...]map[common.Hash]common.Address{c.fresh, c.stale} {
		if sender, ok := gen[hash]; ok {
			delete(gen, hash)
			return utils.Some(sender)
		}
	}
	return utils.None[common.Address]()
}

type evmOnlyApplication struct {
	abci.BaseApplication

	chainID          *big.Int
	chainConfig      *params.ChainConfig
	execution        gigaconfig.ExecutionConfig
	minGasPrice      *big.Int
	storage          *bootstrap.GigaStorageManager
	changeSetEncoder evmonly.NamedChangeSetEncoder
	validators       []abci.ValidatorUpdate
	executor         utils.Mutex[*utils.Option[*evmonly.Executor]]
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
	// checkedSenders maps the hash of every transaction this process admitted
	// in CheckTx to the sender recovered there, so execution does not recover
	// it again.
	checkedSenders utils.Mutex[*senderCache]
	// prepared holds the block PrepareBlock decoded ahead of FinalizeBlock, if any.
	prepared utils.Mutex[*utils.Option[preparedBlock]]
	// preparedBlocks counts finalized blocks by whether prepared held them.
	preparedBlocks otelmetric.Int64Counter
	// preparePhases times PrepareBlock's decode of the next block. PrepareBlock is
	// called from the single block fetcher, so one timer serves the app.
	preparePhases *seidbmetrics.PhaseTimer
	// finalizePhases times FinalizeBlock's stages around the executor. It is a
	// field so each application instance has its own last-phase clock.
	// FinalizeBlock is serialized by executor, so one timer is enough per app.
	finalizePhases *seidbmetrics.PhaseTimer
}

// evmOnlyCursorState is the execution position: the block whose state is
// committed to storage and the block finalized but not yet acknowledged by
// Commit.
type evmOnlyCursorState struct {
	committed evmOnlyCursor
	pending   utils.Option[evmOnlyCursor]
	// lastBlockTime is the Time of the most recently committed block, used by
	// EvmCall to reproduce that block's execution context for a read-only call.
	lastBlockTime uint64
	// pendingBlockTime is the Time of the block staged in pending; Commit
	// promotes it to lastBlockTime.
	pendingBlockTime uint64
}

var _ abci.Application = (*evmOnlyApplication)(nil)

// NewEVMOnlyApplication returns the raw-Ethereum application used by Autobahn
// load tests. State, receipts, and blocks are owned by storage; execution sizes the executor
// and prices admission. A storage that already holds committed blocks resumes from its
// durable cursor, so Info reports the stored height and InitChain is refused.
func NewEVMOnlyApplication(
	chainID uint64,
	validators []abci.ValidatorUpdate,
	storage *bootstrap.GigaStorageManager,
	changeSetEncoder evmonly.NamedChangeSetEncoder,
	execution gigaconfig.ExecutionConfig,
) (abci.Application, error) {
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = new(big.Int).SetUint64(chainID)
	a := &evmOnlyApplication{
		chainID:          new(big.Int).SetUint64(chainID),
		chainConfig:      &chainConfig,
		execution:        execution,
		minGasPrice:      evmOnlyAdmissionMinGasPrice(execution.MinGasPrice),
		storage:          storage,
		changeSetEncoder: changeSetEncoder,
		validators:       slices.Clone(validators),
		executor:         utils.NewMutex(new(utils.Option[*evmonly.Executor])),
		cursor:           utils.NewMutex(&evmOnlyCursorState{}),
		settler:          utils.NewAtomicSend(utils.None[*evmonly.Executor]()),
		checkedSenders:   utils.NewMutex(utils.Alloc(newSenderCache())),
		finalizePhases:   seidbmetrics.NewPhaseTimer(otel.Meter(finalizeMeterName), finalizeTimerName),
		prepared:         utils.NewMutex(new(utils.Option[preparedBlock])),
		preparedBlocks:   newPreparedBlocksCounter(otel.Meter(finalizeMeterName)),
		preparePhases:    seidbmetrics.NewPhaseTimer(otel.Meter(finalizeMeterName), prepareTimerName),
	}
	cursor, err := loadEVMOnlyCursor(storage.SC())
	if err != nil {
		return nil, err
	}
	if cursor, ok := cursor.Get(); ok {
		for executor := range a.executor.Lock() {
			a.installExecutor(executor)
		}
		for state := range a.cursor.Lock() {
			state.committed = cursor
		}
	}
	return a, nil
}

// installExecutor creates the executor and publishes it to both the block
// serializer and the settler. Called with the executor lock held.
func (a *evmOnlyApplication) installExecutor(slot *utils.Option[*evmonly.Executor]) {
	executor := a.newExecutor()
	*slot = utils.Some(executor)
	a.settler.Store(utils.Some(executor))
}

func (a *evmOnlyApplication) newExecutor() *evmonly.Executor {
	return evmonly.NewExecutor(evmonly.Config{
		ChainConfig:  a.chainConfig,
		MinGasPrice:  big.NewInt(evmOnlyBlockMinGasPrice),
		OCCWorkers:   workersOrGOMAXPROCS(a.execution.OCCWorkers),
		ParseWorkers: workersOrGOMAXPROCS(a.execution.ParseWorkers),
		// Autobahn orders transactions without validating them, so a block can hold one
		// the executor cannot apply; failing the block would halt every validator.
		RejectUnappliableTxs: true,
		BlockResultPoolSize:  a.execution.BlockResultPoolSize,
	},
		evmonly.WithStorageManager(a.storage, a.changeSetEncoder),
		evmonly.WithMissingAccountState(evmOnlyFundedState{}),
		evmonly.WithStoreIndependentBlockChangeSetEncoder(a.encodeCursorChangeSet),
	)
}

// encodeCursorChangeSet chains the block into the app hash and stages the
// resulting cursor as pending, returning it as the changeset committed with
// the block's state.
func (a *evmOnlyApplication) encodeCursorChangeSet(block evmonly.BlockContext, result *evmonly.BlockResult) ([]*proto.NamedChangeSet, error) {
	height, ok := utils.SafeCast[int64](block.Number)
	if !ok {
		return nil, fmt.Errorf("EVM-only block number exceeds int64: %d", block.Number)
	}
	for state := range a.cursor.Lock() {
		if height != state.committed.height+1 {
			return nil, fmt.Errorf("EVM-only block height %d does not follow committed height %d", height, state.committed.height)
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
		return []*proto.NamedChangeSet{next.changeSet()}, nil
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	if req.InitialHeight <= 0 {
		return nil, fmt.Errorf("EVM-only initial height must be positive: %d", req.InitialHeight)
	}
	gasLimit, err := evmOnlyGasLimit(req)
	if err != nil {
		return nil, err
	}
	for executor := range a.executor.Lock() {
		if executor.IsPresent() {
			return nil, fmt.Errorf("EVM-only application already initialized")
		}
		if err := a.seedInitialStateVersion(req.InitialHeight); err != nil {
			return nil, err
		}
		a.installExecutor(executor)
		for state := range a.cursor.Lock() {
			state.committed = evmOnlyCursor{height: req.InitialHeight - 1, gasLimit: gasLimit}
		}
		return &abci.ResponseInitChain{}, nil
	}
	panic("unreachable")
}

// seedInitialStateVersion moves an empty state store to the version preceding
// initialHeight. A store already seeded there is accepted, since a crash
// between InitChain and the first block leaves it that way; a store holding
// any block is refused.
func (a *evmOnlyApplication) seedInitialStateVersion(initialHeight int64) error {
	stateStore := a.storage.SC()
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

func evmOnlyGasLimit(req *abci.RequestInitChain) (uint64, error) {
	if req.ConsensusParams == nil || req.ConsensusParams.Block == nil || req.ConsensusParams.Block.MaxGas <= 0 {
		return 0, fmt.Errorf("EVM-only max gas must be positive")
	}
	gasLimit, ok := utils.SafeCast[uint64](req.ConsensusParams.Block.MaxGas)
	if !ok {
		return 0, fmt.Errorf("EVM-only max gas exceeds uint64: %d", req.ConsensusParams.Block.MaxGas)
	}
	return gasLimit, nil
}

func (a *evmOnlyApplication) Info() *abci.ResponseInfo {
	for state := range a.cursor.Lock() {
		return &abci.ResponseInfo{
			Data:             "evmonly",
			LastBlockHeight:  state.committed.height,
			LastBlockAppHash: append([]byte(nil), state.committed.appHash[:]...),
		}
	}
	panic("unreachable")
}

// InitLastHeader seeds the committed block time on the router's restart path.
// The cursor carries height, hashes and gas limit but not Time, so without this
// EvmCall would answer with TIMESTAMP 0 until the next Commit.
func (a *evmOnlyApplication) InitLastHeader(lastHeader *tmproto.Header) {
	if lastHeader == nil || lastHeader.Time.Unix() < 0 {
		return
	}
	for state := range a.cursor.Lock() {
		state.lastBlockTime = uint64(lastHeader.Time.Unix()) // nolint:gosec // guarded non-negative above
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

func (a *evmOnlyApplication) GetValidators() []abci.ValidatorUpdate {
	return slices.Clone(a.validators)
}

func (a *evmOnlyApplication) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	// TODO(evmonly-production): close the gap between admission and block validity
	// before accepting arbitrary traffic; this test app assumes executable load-test transactions.
	tx, sender, err := a.parseTx(req.Tx)
	if err != nil {
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Code: 1, Log: err.Error()}}
	}
	gasWanted, ok := utils.SafeCast[int64](tx.Gas())
	if !ok {
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Code: 1, Log: "transaction gas limit exceeds int64"}}
	}
	a.rememberSender(tx.Hash(), sender)
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:         abci.CodeTypeOK,
			GasWanted:    gasWanted,
			GasEstimated: gasWanted,
		},
		IsEVM:            true,
		EVMNonce:         tx.Nonce(),
		EVMHash:          tx.Hash(),
		EVMSenderAddress: sender,
		SeiSenderAddress: append([]byte(nil), sender[:]...),
	}
}

func (a *evmOnlyApplication) rememberSender(hash common.Hash, sender common.Address) {
	for senders := range a.checkedSenders.Lock() {
		senders.put(hash, sender)
	}
}

// takeSenders returns, aligned with txs, the sender CheckTx recovered for each
// transaction this process admitted, and forgets those entries. The hash of a
// raw transaction is the keccak of its bytes for every transaction type, so no
// decoding is needed.
func (a *evmOnlyApplication) takeSenders(txs [][]byte) []utils.Option[common.Address] {
	return a.checkedSendersOf(txs, true)
}

// peekSenders is takeSenders without forgetting the entries.
func (a *evmOnlyApplication) peekSenders(txs [][]byte) []utils.Option[common.Address] {
	return a.checkedSendersOf(txs, false)
}

// forgetSenders drops the CheckTx-recovered senders of txs.
func (a *evmOnlyApplication) forgetSenders(txs [][]byte) {
	a.checkedSendersOf(txs, true)
}

func (a *evmOnlyApplication) checkedSendersOf(txs [][]byte, forget bool) []utils.Option[common.Address] {
	out := make([]utils.Option[common.Address], len(txs))
	// Hashed outside the lock; CheckTx writes this map constantly.
	hashes := hashRawTxs(txs)
	for senders := range a.checkedSenders.Lock() {
		for i, hash := range hashes {
			if forget {
				out[i] = senders.take(hash)
			} else {
				out[i] = senders.peek(hash)
			}
		}
	}
	return out
}

// hashRawTxs returns the keccak of every raw transaction, aligned with txs.
func hashRawTxs(txs [][]byte) []common.Hash {
	hashes := make([]common.Hash, len(txs))
	workers := min(runtime.GOMAXPROCS(0), len(txs))
	if workers <= 1 || len(txs) <= minTxsPerHashWorker {
		hashRawTxRange(txs, hashes, 0, len(txs))
		return hashes
	}
	chunk := max((len(txs)+workers-1)/workers, minTxsPerHashWorker)
	_ = scope.Parallel(func(s scope.ParallelScope) error {
		for start := 0; start < len(txs); start += chunk {
			end := min(start+chunk, len(txs))
			s.Spawn(func() error {
				hashRawTxRange(txs, hashes, start, end)
				return nil
			})
		}
		return nil
	})
	return hashes
}

// hashRawTxRange hashes txs[start:end] into hashes.
func hashRawTxRange(txs [][]byte, hashes []common.Hash, start, end int) {
	state := crypto.NewKeccakState()
	for i := start; i < end; i++ {
		state.Reset()
		_, _ = state.Write(txs[i])
		_, _ = state.Read(hashes[i][:])
	}
}

func (a *evmOnlyApplication) parseTx(raw []byte) (*ethtypes.Transaction, common.Address, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, common.Address{}, err
	}
	if !tx.Protected() {
		return nil, common.Address{}, fmt.Errorf("unprotected Ethereum transaction")
	}
	if tx.ChainId().Cmp(a.chainID) != 0 {
		return nil, common.Address{}, fmt.Errorf("ethereum transaction chain ID does not match %s", a.chainID)
	}
	if tx.Type() == ethtypes.BlobTxType {
		return nil, common.Address{}, fmt.Errorf("blob transactions are not supported")
	}
	// The predicate block validity uses, not tx.GasPrice(): on a dynamic-fee tx
	// that is the fee cap, so admitting on it let through transactions the
	// executor then refused — and an executor refusal is a node panic, not a
	// failed receipt.
	if evmonly.EffectiveGasPrice(tx, evmOnlyBaseFee()).Cmp(a.minGasPrice) < 0 {
		return nil, common.Address{}, fmt.Errorf("ethereum transaction effective gas price is below %s", a.minGasPrice)
	}
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(a.chainID), tx)
	if err != nil {
		return nil, common.Address{}, err
	}
	return tx, sender, nil
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

// latestAccount returns address's balance and nonce after the last finalized block, read through
// the executor's in-flight commit rather than waiting for it. Before InitChain, or once a commit
// has failed, it reads the settled store instead.
func (a *evmOnlyApplication) latestAccount(address common.Address) evmonly.LatestAccount {
	if executor, ok := a.settler.Load().Get(); ok {
		if account, err := executor.ReadLatestAccount(address); err == nil {
			return account
		}
	}
	snapshot := a.openSettledView()
	defer snapshot.Close()
	storeAddress := evmOnlyStoreAddress(address)
	if !snapshot.AccountExists(storeAddress) {
		return evmonly.LatestAccount{Balance: new(big.Int).Set(evmOnlyBaseBalance)}
	}
	balance := snapshot.GetBalance(storeAddress)
	return evmonly.LatestAccount{
		Balance: new(big.Int).SetBytes(balance[:]),
		Nonce:   snapshot.GetNonce(storeAddress),
	}
}

func (a *evmOnlyApplication) EvmNonce(address common.Address) uint64 {
	return a.latestAccount(address).Nonce
}

func (a *evmOnlyApplication) EvmBalance(address common.Address, _ []byte) uint256.Int {
	return *uint256.MustFromBig(a.latestAccount(address).Balance)
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

// committedBlockContext returns the block context of the committed block, and
// refuses while a finalized block awaits Commit. action names the caller for
// its error messages.
func (a *evmOnlyApplication) committedBlockContext(action string) (evmonly.BlockContext, error) {
	for state := range a.cursor.Lock() {
		if state.pending.IsPresent() {
			// The store already has this block's writes; NUMBER/TIMESTAMP/PrevRandao advance only on Commit.
			return evmonly.BlockContext{}, fmt.Errorf("EVM-only %s attempted before committing the finalized block", action)
		}
		number, ok := utils.SafeCast[uint64](state.committed.height)
		if !ok {
			return evmonly.BlockContext{}, fmt.Errorf("EVM-only committed height exceeds uint64: %d", state.committed.height)
		}
		// Coinbase and ParentHash are left zero.
		return evmonly.BlockContext{
			Number:      number,
			Time:        state.lastBlockTime,
			GasLimit:    state.committed.gasLimit,
			ChainID:     new(big.Int).Set(a.chainID),
			BaseFee:     evmOnlyBaseFee(),
			BlobBaseFee: new(big.Int),
			BlockHash:   state.committed.blockHash,
			PrevRandao:  evmOnlyPrevRandao(state.lastBlockTime),
		}, nil
	}
	panic("unreachable")
}

// EvmCall executes msg as a read-only call against the most recently
// committed EVM state and returns the execution result.
func (a *evmOnlyApplication) EvmCall(ctx context.Context, msg *ethcore.Message) (*ethcore.ExecutionResult, error) {
	executor, blockCtx, err := a.currentExecutionContext("call")
	if err != nil {
		return nil, err
	}
	return executor.Call(ctx, blockCtx, msg)
}

// EvmEstimateGas returns the lowest gas limit that lets msg execute
// successfully against the most recently committed EVM state. Like EvmCall
// it creates no transaction and persists no state change.
func (a *evmOnlyApplication) EvmEstimateGas(ctx context.Context, msg *ethcore.Message, gasCap uint64) (uint64, []byte, error) {
	executor, blockCtx, err := a.currentExecutionContext("gas estimate")
	if err != nil {
		return 0, nil, err
	}
	return executor.EstimateGas(ctx, blockCtx, msg, gasCap)
}

// finalizeRequest is the block identity FinalizeBlock and PrepareBlock derive from a request.
type finalizeRequest struct {
	height    int64
	number    uint64
	timestamp uint64
	blockHash common.Hash
}

func parseFinalizeRequest(req *abci.RequestFinalizeBlock) (finalizeRequest, error) {
	height := req.Header.Height
	if height <= 0 {
		return finalizeRequest{}, fmt.Errorf("EVM-only block height must be positive: %d", height)
	}
	number, ok := utils.SafeCast[uint64](height)
	if !ok {
		return finalizeRequest{}, fmt.Errorf("EVM-only block height exceeds uint64: %d", height)
	}
	timestamp, ok := utils.SafeCast[uint64](req.Header.Time.Unix())
	if !ok {
		return finalizeRequest{}, fmt.Errorf("EVM-only block timestamp is negative: %s", req.Header.Time)
	}
	return finalizeRequest{
		height:    height,
		number:    number,
		timestamp: timestamp,
		blockHash: common.BytesToHash(req.Hash),
	}, nil
}

func (a *evmOnlyApplication) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	block, err := parseFinalizeRequest(req)
	if err != nil {
		return nil, err
	}
	for executor := range a.executor.Lock() {
		executor, ok := executor.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only block finalized before InitChain")
		}
		parent, err := a.beginBlock(block.height)
		if err != nil {
			return nil, err
		}
		blockCtx := evmonly.BlockContext{
			Number:      block.number,
			Time:        block.timestamp,
			GasLimit:    parent.gasLimit,
			ChainID:     new(big.Int).Set(a.chainID),
			BaseFee:     evmOnlyBaseFee(),
			BlobBaseFee: new(big.Int),
			ParentHash:  parent.blockHash,
			BlockHash:   block.blockHash,
			PrevRandao:  evmOnlyPrevRandao(block.timestamp),
		}
		// Closes the stage in flight, so the gap until the next block is charged to neither.
		defer a.finalizePhases.Reset()
		result, err := a.executeBlock(ctx, executor, req.Txs, block, blockCtx)
		if err != nil {
			return nil, errors.Join(err, a.abandonPending(executor, block.height))
		}
		defer result.Release()
		pending, err := a.pendingCursor(block.height)
		if err != nil {
			return nil, err
		}
		a.finalizePhases.SetPhase(finalizePhaseTxResults)
		return &abci.ResponseFinalizeBlock{
			AppHash:   append([]byte(nil), pending.appHash[:]...),
			TxResults: evmOnlyABCIResults(result),
		}, nil
	}
	panic("unreachable")
}

// beginBlock checks height is the next block to finalize and returns the
// committed cursor it builds on.
func (a *evmOnlyApplication) beginBlock(height int64) (evmOnlyCursor, error) {
	for state := range a.cursor.Lock() {
		if state.pending.IsPresent() {
			return evmOnlyCursor{}, fmt.Errorf("EVM-only block %d finalized before committing the previous block", height)
		}
		if next := state.committed.height + 1; height != next {
			return evmOnlyCursor{}, fmt.Errorf("EVM-only block height %d does not match next height %d", height, next)
		}
		return state.committed, nil
	}
	panic("unreachable")
}

// abandonPending drops the cursor staged by a failed block unless the store
// already holds that block's version, in which case the cursor is durable and
// stays pending for Commit. The in-flight commit is landed first so the store
// version is final; a commit that failed is reported alongside.
func (a *evmOnlyApplication) abandonPending(executor *evmonly.Executor, height int64) error {
	commitErr := executor.AwaitCommits()
	latest, err := a.storage.SC().GetLatestVersion()
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

func (a *evmOnlyApplication) pendingCursor(height int64) (evmOnlyCursor, error) {
	for state := range a.cursor.Lock() {
		pending, ok := state.pending.Get()
		if !ok || pending.height != height {
			return evmOnlyCursor{}, fmt.Errorf("EVM-only block %d committed without staging its cursor", height)
		}
		return pending, nil
	}
	panic("unreachable")
}

// executeBlock executes the block from its prepared transactions when PrepareBlock
// decoded this block, and decodes it here otherwise.
func (a *evmOnlyApplication) executeBlock(ctx context.Context, executor *evmonly.Executor, txs [][]byte, block finalizeRequest, blockCtx evmonly.BlockContext) (*evmonly.BlockResult, error) {
	prepared, hit := a.takePrepared(block.height, block.blockHash)
	a.preparedBlocks.Add(ctx, 1, otelmetric.WithAttributes(attribute.Bool("prepared", hit)))
	a.finalizePhases.SetPhase(finalizePhaseTakeSenders)
	if !hit {
		return a.executeBlockPipelined(ctx, executor, evmonly.BlockRequest{
			Context: blockCtx,
			Txs:     txs,
			Senders: a.takeSenders(txs),
		})
	}
	a.forgetSenders(txs)
	a.finalizePhases.SetPhase(finalizePhaseExecute)
	return executor.ExecutePreparedBlock(ctx, evmonly.PreparedBlock{Context: blockCtx, Txs: prepared})
}

// executeBlockPipelined executes the block and returns once its state commit
// has started, leaving the commit to land while the next block executes.
// Readers of committed state settle it through AwaitCommits.
func (a *evmOnlyApplication) executeBlockPipelined(ctx context.Context, executor *evmonly.Executor, req evmonly.BlockRequest) (*evmonly.BlockResult, error) {
	a.finalizePhases.SetPhase(finalizePhasePrepare)
	prepared, err := executor.PrepareBlock(ctx, req)
	if err != nil {
		return nil, err
	}
	// The executor's own timer breaks execution down further.
	a.finalizePhases.SetPhase(finalizePhaseExecute)
	return executor.ExecutePreparedBlock(ctx, prepared)
}

// Commit acknowledges the finalized block as the one the chain builds on. The
// block's state commit is not waited for; a commit that fails halts the node
// through the next FinalizeBlock.
func (a *evmOnlyApplication) Commit(context.Context) (*abci.ResponseCommit, error) {
	for state := range a.cursor.Lock() {
		pending, ok := state.pending.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only Commit called without a finalized block")
		}
		state.committed = pending
		state.lastBlockTime = state.pendingBlockTime
		state.pending = utils.None[evmOnlyCursor]()
		return &abci.ResponseCommit{}, nil
	}
	panic("unreachable")
}

// evmOnlyABCIResults reports a block's executed transactions to consensus, each
// one an OK result carrying the EVM failure reason, if it had one, in its log.
//
// Every result is OK, including a reverted or rejected transaction: the hash is
// recorded as executed rather than held in the mempool's failed set for a retry,
// and the receipt status carries the failure. The log may vary with the failure
// because the results hash covers only the code, data and gas.
func evmOnlyABCIResults(result *evmonly.BlockResult) []*abci.ExecTxResult {
	txResults := make([]*abci.ExecTxResult, len(result.Txs))
	for i, tx := range result.Txs {
		gasUsed := utils.Clamp[int64](tx.GasUsed)
		txResults[i] = &abci.ExecTxResult{
			Code:      abci.CodeTypeOK,
			Log:       evmOnlyTxFailureLog(tx),
			GasWanted: gasUsed,
			GasUsed:   gasUsed,
		}
	}
	return txResults
}

// evmOnlyTxFailureLog returns the reason a transaction failed, or the empty string
// when it succeeded.
func evmOnlyTxFailureLog(tx evmonly.TxResult) string {
	if tx.Err == nil {
		return ""
	}
	return tx.Err.Error()
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

type evmOnlyFundedState struct{}

func (evmOnlyFundedState) GetBalance(common.Address) *big.Int {
	return new(big.Int).Set(evmOnlyBaseBalance)
}
func (evmOnlyFundedState) GetNonce(common.Address) uint64                   { return 0 }
func (evmOnlyFundedState) GetCode(common.Address) []byte                    { return nil }
func (evmOnlyFundedState) GetState(common.Address, common.Hash) common.Hash { return common.Hash{} }
