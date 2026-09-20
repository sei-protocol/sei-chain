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
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"

	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const evmOnlyMinGasPrice = 1_000_000_000

var logger = seilog.NewLogger("tendermint", "internal", "evmonlyapp")

// evmOnlyBaseFee is the base fee this application executes every block at.
// Admission and block validity both price against it, so they cannot diverge.
func evmOnlyBaseFee() *big.Int { return new(big.Int) }

var evmOnlyBaseBalance = new(big.Int).Lsh(big.NewInt(1), 200)

// checkedSendersCap bounds the senders remembered from CheckTx. Entries are
// dropped as their transactions execute; the cap only guards against admitted
// transactions that never reach a block.
const checkedSendersCap = 1 << 18

// minTxsPerHashWorker is the minimum transaction count assigned to a hash worker.
const minTxsPerHashWorker = 64

type evmOnlyApplication struct {
	abci.BaseApplication

	chainID          *big.Int
	chainConfig      *params.ChainConfig
	storage          *bootstrap.GigaStorageManager
	changeSetEncoder evmonly.NamedChangeSetEncoder
	validators       []abci.ValidatorUpdate
	// executor is held for the whole of a block's execution, so it serializes
	// FinalizeBlock and InitChain against each other. EvmCall only takes it to
	// read the executor out; the call itself runs unlocked.
	executor utils.Mutex[*utils.Option[*evmonly.Executor]]
	// settler publishes the same executor to readers of committed state that
	// must not wait for a block to finish executing; they settle its
	// background commit before opening a store view.
	settler utils.AtomicSend[utils.Option[*evmonly.Executor]]
	// Lock order: executor before cursor. FinalizeBlock holds executor while
	// the block's cursor encoder takes cursor.
	cursor utils.Mutex[*evmOnlyCursorState]
	// checkedSenders maps the hash of every transaction this process admitted
	// in CheckTx to the sender recovered there, so execution does not recover
	// it again.
	checkedSenders utils.Mutex[map[common.Hash]common.Address]
	// settleFailureLogged is set once a failed commit has been logged by a
	// committed-state reader; the failure is latched, so it is logged once.
	settleFailureLogged atomic.Bool
	// finalizePhases breaks FinalizeBlock into its stages around the executor.
	// FinalizeBlock is serialized by executor, so one timer serves the app; it
	// is only touched with that lock held.
	finalizePhases *seidbmetrics.PhaseTimer
}

// finalizeMeterName is the OTel meter FinalizeBlock's phase timer records to,
// as evmonly_finalize_phase_duration_seconds_total.
const finalizeMeterName = "evmonly_app"

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
// load tests. State, receipts, and blocks are owned by storage. A storage that
// already holds committed blocks resumes from its durable cursor, so Info
// reports the stored height and InitChain is refused.
func NewEVMOnlyApplication(
	chainID uint64,
	validators []abci.ValidatorUpdate,
	storage *bootstrap.GigaStorageManager,
	changeSetEncoder evmonly.NamedChangeSetEncoder,
) (abci.Application, error) {
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = new(big.Int).SetUint64(chainID)
	a := &evmOnlyApplication{
		chainID:          new(big.Int).SetUint64(chainID),
		chainConfig:      &chainConfig,
		storage:          storage,
		changeSetEncoder: changeSetEncoder,
		validators:       slices.Clone(validators),
		executor:         utils.NewMutex(new(utils.Option[*evmonly.Executor])),
		finalizePhases:   seidbmetrics.NewPhaseTimer(otel.Meter(finalizeMeterName), "evmonly_finalize"),
		settler:          utils.NewAtomicSend(utils.None[*evmonly.Executor]()),
		cursor:           utils.NewMutex(&evmOnlyCursorState{}),
		checkedSenders:   utils.NewMutex(map[common.Hash]common.Address{}),
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

// AwaitCommits blocks until every block finalized so far is in the store and
// reports the first commit that failed. The store must be settled before it is
// closed, and before a reader opens a view that has to include the last
// finalized block.
func (a *evmOnlyApplication) AwaitCommits() error {
	executor, ok := a.settler.Load().Get()
	if !ok {
		return nil
	}
	return executor.AwaitCommits()
}

func (a *evmOnlyApplication) newExecutor() *evmonly.Executor {
	return evmonly.NewExecutor(evmonly.Config{
		ChainConfig:  a.chainConfig,
		MinGasPrice:  big.NewInt(evmOnlyMinGasPrice),
		OCCWorkers:   runtime.GOMAXPROCS(0),
		ParseWorkers: runtime.GOMAXPROCS(0),
		// Autobahn orders transactions without validating them, so a block can hold one
		// the executor cannot apply; failing the block would halt every validator.
		RejectUnappliableTxs: true,
		BlockResultPoolSize:  1,
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
			height:     height,
			appHash:    appHash,
			blockHash:  block.BlockHash,
			prevRandao: block.PrevRandao,
			gasLimit:   block.GasLimit,
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
		if len(senders) >= checkedSendersCap {
			clear(senders)
		}
		senders[hash] = sender
	}
}

// takeSenders returns, aligned with txs, the sender CheckTx recovered for each
// transaction this process admitted, and forgets those entries. The hash of a
// raw transaction is the keccak of its bytes for every transaction type, so no
// decoding is needed.
func (a *evmOnlyApplication) takeSenders(txs [][]byte) []utils.Option[common.Address] {
	out := make([]utils.Option[common.Address], len(txs))
	// Hashed outside the lock; CheckTx writes this map constantly.
	hashes := hashRawTxs(txs)
	for senders := range a.checkedSenders.Lock() {
		for i, hash := range hashes {
			if sender, ok := senders[hash]; ok {
				out[i] = utils.Some(sender)
				delete(senders, hash)
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
	var wg sync.WaitGroup
	for start := 0; start < len(txs); start += chunk {
		end := min(start+chunk, len(txs))
		wg.Add(1)
		go func() {
			defer wg.Done()
			hashRawTxRange(txs, hashes, start, end)
		}()
	}
	wg.Wait()
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
	if evmonly.EffectiveGasPrice(tx, evmOnlyBaseFee()).Cmp(big.NewInt(evmOnlyMinGasPrice)) < 0 {
		return nil, common.Address{}, fmt.Errorf("ethereum transaction effective gas price is below %d", evmOnlyMinGasPrice)
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

// openSettledView opens a store view that holds every block finalized so far.
// A failed commit is logged once rather than returned: the view is still a
// consistent version, and the failure halts the node through the next
// FinalizeBlock.
func (a *evmOnlyApplication) openSettledView() gigatypes.StateView {
	if err := a.AwaitCommits(); err != nil && !a.settleFailureLogged.Swap(true) {
		logger.Error("EVM-only committed state is behind a failed block commit", "err", err)
	}
	return a.storage.StateDB().OpenView()
}

// callBlockContext returns the block context of the committed block, and
// refuses while a finalized block awaits Commit.
func (a *evmOnlyApplication) callBlockContext() (evmonly.BlockContext, error) {
	for state := range a.cursor.Lock() {
		if state.pending.IsPresent() {
			// The store already has this block's writes; NUMBER/TIMESTAMP/PrevRandao advance only on Commit.
			return evmonly.BlockContext{}, fmt.Errorf("EVM-only call attempted before committing the finalized block")
		}
		number, ok := utils.SafeCast[uint64](state.committed.height)
		if !ok {
			return evmonly.BlockContext{}, fmt.Errorf("EVM-only committed height exceeds uint64: %d", state.committed.height)
		}
		// Coinbase and ParentHash are left zero: no coinbase is tracked outside
		// FinalizeBlock, and only the current block's hash is tracked at all.
		return evmonly.BlockContext{
			Number:      number,
			Time:        state.lastBlockTime,
			GasLimit:    state.committed.gasLimit,
			ChainID:     new(big.Int).Set(a.chainID),
			BaseFee:     evmOnlyBaseFee(),
			BlobBaseFee: new(big.Int),
			BlockHash:   state.committed.blockHash,
			PrevRandao:  state.committed.prevRandao,
		}, nil
	}
	panic("unreachable")
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

// EvmMinGasPrice returns the minimum effective gas price this application
// admits a transaction at.
func (a *evmOnlyApplication) EvmMinGasPrice() *big.Int {
	return big.NewInt(evmOnlyMinGasPrice)
}

// EvmCall executes msg as a read-only call against the most recently
// committed EVM state and returns the execution result.
func (a *evmOnlyApplication) EvmCall(ctx context.Context, msg *ethcore.Message) (*ethcore.ExecutionResult, error) {
	var executor *evmonly.Executor
	for exec := range a.executor.Lock() {
		got, ok := exec.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only call attempted before InitChain")
		}
		executor = got
	}
	// The committed block's write may still be in flight, and a block may be
	// finalized and committed while it is waited for. The context is taken
	// before settling and confirmed unchanged after, so the store holds the
	// advertised block and no later one has been committed to the cursor.
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		blockCtx, err := a.callBlockContext()
		if err != nil {
			return nil, err
		}
		if err := executor.AwaitCommits(); err != nil {
			return nil, err
		}
		settled, err := a.callBlockContext()
		if err != nil {
			return nil, err
		}
		if settled.Number == blockCtx.Number {
			return executor.Call(ctx, blockCtx, msg)
		}
	}
}

func (a *evmOnlyApplication) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	height := req.Header.Height
	if height <= 0 {
		return nil, fmt.Errorf("EVM-only block height must be positive: %d", height)
	}
	number, ok := utils.SafeCast[uint64](height)
	if !ok {
		return nil, fmt.Errorf("EVM-only block height exceeds uint64: %d", height)
	}
	timestamp, ok := utils.SafeCast[uint64](req.Header.Time.Unix())
	if !ok {
		return nil, fmt.Errorf("EVM-only block timestamp is negative: %s", req.Header.Time)
	}
	blockHash := common.BytesToHash(req.Hash)
	for executor := range a.executor.Lock() {
		executor, ok := executor.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only block finalized before InitChain")
		}
		return a.finalizeBlockLocked(ctx, executor, req, number, timestamp, blockHash)
	}
	panic("unreachable")
}

// finalizeBlockLocked runs a block on the executor and stages its cursor.
// The caller holds executor for the whole call.
func (a *evmOnlyApplication) finalizeBlockLocked(
	ctx context.Context,
	executor *evmonly.Executor,
	req *abci.RequestFinalizeBlock,
	number, timestamp uint64,
	blockHash common.Hash,
) (*abci.ResponseFinalizeBlock, error) {
	height := req.Header.Height
	parent, err := a.beginBlock(height)
	if err != nil {
		return nil, err
	}
	// Closes the stage in flight, so the gap until the next block is charged to neither.
	defer a.finalizePhases.Reset()
	a.finalizePhases.SetPhase("take_senders")
	senders := a.takeSenders(req.Txs)
	result, err := executeBlockPipelined(ctx, executor, a.finalizePhases, evmonly.BlockRequest{
		Context: evmonly.BlockContext{
			Number:      number,
			Time:        timestamp,
			GasLimit:    parent.gasLimit,
			ChainID:     new(big.Int).Set(a.chainID),
			BaseFee:     evmOnlyBaseFee(),
			BlobBaseFee: new(big.Int),
			ParentHash:  parent.blockHash,
			BlockHash:   blockHash,
			PrevRandao:  parent.appHash,
		},
		Txs:     req.Txs,
		Senders: senders,
	})
	if err != nil {
		return nil, errors.Join(err, a.abandonPending(executor, height))
	}
	defer result.Release()
	pending, err := a.pendingCursor(height)
	if err != nil {
		return nil, err
	}
	a.finalizePhases.SetPhase("tx_results")
	return &abci.ResponseFinalizeBlock{
		AppHash:   append([]byte(nil), pending.appHash[:]...),
		TxResults: evmOnlyABCIResults(result),
	}, nil
}

// executeBlockPipelined executes the block and returns once its state commit
// has been started, leaving the commit to run while the next block executes.
// The executor lands the previous block's commit before starting this one and
// reads its changes through an overlay in the meantime, so committed-state
// readers settle through AwaitCommits rather than this returning.
//
// Preparation (decoding and recovering the senders CheckTx did not) and execution are
// timed as separate phases; the executor's own timer breaks execution down further.
func executeBlockPipelined(ctx context.Context, executor *evmonly.Executor, phases *seidbmetrics.PhaseTimer, req evmonly.BlockRequest) (*evmonly.BlockResult, error) {
	phases.SetPhase("prepare")
	prepared, err := executor.PrepareBlock(ctx, req)
	if err != nil {
		return nil, err
	}
	phases.SetPhase("execute")
	return executor.ExecutePreparedBlock(ctx, prepared)
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

// Commit acknowledges the finalized block as the one the chain builds on: the
// height it advances is what RPC serves as latest. Neither the block's state
// commit nor its queued receipt write is waited for here, since that would put
// the write back on the block loop, so the newest block's receipts can trail
// latest briefly. A commit that fails halts the node through the next
// FinalizeBlock, and a restart resumes from the store's own version.
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

type evmOnlyFundedState struct{}

func (evmOnlyFundedState) GetBalance(common.Address) *big.Int {
	return new(big.Int).Set(evmOnlyBaseBalance)
}
func (evmOnlyFundedState) GetNonce(common.Address) uint64                   { return 0 }
func (evmOnlyFundedState) GetCode(common.Address) []byte                    { return nil }
func (evmOnlyFundedState) GetState(common.Address, common.Hash) common.Hash { return common.Hash{} }
