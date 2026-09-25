package evmonlyapp

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"runtime"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"go.opentelemetry.io/otel"
)

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

// checkedSendersCap bounds the senders remembered from CheckTx per generation.
// Entries are dropped as their transactions execute; the cap only guards against
// admitted transactions that never reach a block.
const checkedSendersCap = 1 << 18

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
	state            utils.Mutex[*evmOnlyState]
	// checkedSenders maps the hash of every transaction this process admitted
	// in CheckTx to the sender recovered there, so execution does not recover
	// it again.
	checkedSenders utils.Mutex[*senderCache]
	// finalizePhases times FinalizeBlock's stages around the executor. It is a
	// field so each application instance has its own last-phase clock.
	// FinalizeBlock is serialized by state, so one timer is enough per app.
	finalizePhases *seidbmetrics.PhaseTimer
}

const (
	// finalizeMeterName is the OTel meter FinalizeBlock's phase timer records to,
	// as evmonly_finalize_phase_duration_seconds_total.
	finalizeMeterName = "evmonly_app"
	finalizeTimerName = "evmonly_finalize"

	finalizePhaseTakeSenders = "take_senders"
	finalizePhaseExecute     = "execute"
	finalizePhaseTxResults   = "tx_results"
)

type evmOnlyState struct {
	executor        utils.Option[*evmonly.Executor]
	gasLimit        uint64
	nextHeight      int64
	committedHeight int64
	appHash         common.Hash
	parentHash      common.Hash
	pending         utils.Option[evmOnlyPending]
	// lastBlockTime is the Time of the most recently committed block, used by
	// EvmCall to reproduce that block's execution context for a read-only call.
	lastBlockTime uint64
	// pendingBlockTime is the Time of the block staged in pending; Commit
	// promotes it to lastBlockTime.
	pendingBlockTime uint64
}

type evmOnlyPending struct {
	height    int64
	appHash   common.Hash
	blockHash common.Hash
}

var _ abci.Application = (*evmOnlyApplication)(nil)

// NewEVMOnlyApplication returns the raw-Ethereum application used by Autobahn
// load tests. State, receipts, and blocks are owned by storage; execution sizes the executor
// and prices admission.
func NewEVMOnlyApplication(
	chainID uint64,
	validators []abci.ValidatorUpdate,
	storage *bootstrap.GigaStorageManager,
	changeSetEncoder evmonly.NamedChangeSetEncoder,
	execution gigaconfig.ExecutionConfig,
) abci.Application {
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = new(big.Int).SetUint64(chainID)
	return &evmOnlyApplication{
		chainID:          new(big.Int).SetUint64(chainID),
		chainConfig:      &chainConfig,
		execution:        execution,
		minGasPrice:      evmOnlyAdmissionMinGasPrice(execution.MinGasPrice),
		storage:          storage,
		changeSetEncoder: changeSetEncoder,
		validators:       slices.Clone(validators),
		state:            utils.NewMutex(&evmOnlyState{}),
		checkedSenders:   utils.NewMutex(utils.Alloc(newSenderCache())),
		finalizePhases:   seidbmetrics.NewPhaseTimer(otel.Meter(finalizeMeterName), finalizeTimerName),
	}
}

func (a *evmOnlyApplication) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	if req.InitialHeight <= 0 {
		return nil, fmt.Errorf("EVM-only initial height must be positive: %d", req.InitialHeight)
	}
	gasLimit, err := evmOnlyGasLimit(req)
	if err != nil {
		return nil, err
	}
	if err := a.seedInitialStateVersion(req.InitialHeight); err != nil {
		return nil, err
	}
	for state := range a.state.Lock() {
		if state.executor.IsPresent() {
			return nil, fmt.Errorf("EVM-only application already initialized")
		}
		state.executor = utils.Some(evmonly.NewExecutor(evmonly.Config{
			ChainConfig:         a.chainConfig,
			MinGasPrice:         big.NewInt(evmOnlyBlockMinGasPrice),
			OCCWorkers:          workersOrGOMAXPROCS(a.execution.OCCWorkers),
			ParseWorkers:        workersOrGOMAXPROCS(a.execution.ParseWorkers),
			BlockResultPoolSize: a.execution.BlockResultPoolSize,
		},
			evmonly.WithStorageManager(a.storage, a.changeSetEncoder),
			evmonly.WithMissingAccountState(evmOnlyFundedState{}),
		))
		state.gasLimit = gasLimit
		state.nextHeight = req.InitialHeight
		state.committedHeight = req.InitialHeight - 1
		return &abci.ResponseInitChain{}, nil
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) seedInitialStateVersion(initialHeight int64) error {
	stateStore := a.storage.SC()
	if stateStore == nil || initialHeight == 1 {
		return nil
	}
	latest, err := stateStore.GetLatestVersion()
	if err != nil {
		return fmt.Errorf("read EVM-only state version: %w", err)
	}
	if latest != 0 {
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
	for state := range a.state.Lock() {
		return &abci.ResponseInfo{
			Data:             "evmonly",
			LastBlockHeight:  state.committedHeight,
			LastBlockAppHash: append([]byte(nil), state.appHash[:]...),
		}
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) LastBlockHeight() int64 {
	for state := range a.state.Lock() {
		return state.committedHeight
	}
	panic("unreachable")
}

// EvmGasLimit returns the gas limit of the most recently committed block.
// This application never changes it after InitChain, so it is also the gas
// limit of every earlier committed block.
func (a *evmOnlyApplication) EvmGasLimit() uint64 {
	for state := range a.state.Lock() {
		return state.gasLimit
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
	out := make([]utils.Option[common.Address], len(txs))
	for senders := range a.checkedSenders.Lock() {
		for i, raw := range txs {
			out[i] = senders.take(crypto.Keccak256Hash(raw))
		}
	}
	return out
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

func (a *evmOnlyApplication) EvmNonce(address common.Address) uint64 {
	snapshot := a.storage.StateDB().OpenView()
	defer snapshot.Close()
	return snapshot.GetNonce(evmOnlyStoreAddress(address))
}

func (a *evmOnlyApplication) EvmBalance(address common.Address, _ []byte) uint256.Int {
	snapshot := a.storage.StateDB().OpenView()
	defer snapshot.Close()
	if !snapshot.AccountExists(evmOnlyStoreAddress(address)) {
		return *uint256.MustFromBig(evmOnlyBaseBalance)
	}
	balance := snapshot.GetBalance(evmOnlyStoreAddress(address))
	return *new(uint256.Int).SetBytes(balance[:])
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
	snapshot := a.storage.StateDB().OpenView()
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
	for state := range a.state.Lock() {
		executor, ok := state.executor.Get()
		if !ok {
			return nil, evmonly.BlockContext{}, fmt.Errorf("EVM-only %s attempted before InitChain", action)
		}
		if state.pending.IsPresent() {
			return nil, evmonly.BlockContext{}, fmt.Errorf("EVM-only %s attempted before committing the finalized block", action)
		}
		number, ok := utils.SafeCast[uint64](state.committedHeight)
		if !ok {
			return nil, evmonly.BlockContext{}, fmt.Errorf("EVM-only committed height exceeds uint64: %d", state.committedHeight)
		}
		// Coinbase and ParentHash are left zero.
		//
		// Executor opens its own state snapshot later, outside this lock, so a
		// commit landing in between can pair this BlockContext with a newer one.
		return executor, evmonly.BlockContext{
			Number:      number,
			Time:        state.lastBlockTime,
			GasLimit:    state.gasLimit,
			ChainID:     new(big.Int).Set(a.chainID),
			BaseFee:     evmOnlyBaseFee(),
			BlobBaseFee: new(big.Int),
			BlockHash:   state.parentHash,
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
	for state := range a.state.Lock() {
		executor, ok := state.executor.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only block finalized before InitChain")
		}
		if state.pending.IsPresent() {
			return nil, fmt.Errorf("EVM-only block %d finalized before committing the previous block", height)
		}
		if height != state.nextHeight {
			return nil, fmt.Errorf("EVM-only block height %d does not match next height %d", height, state.nextHeight)
		}
		// Closes the stage in flight, so the gap until the next block is charged to neither.
		defer a.finalizePhases.Reset()
		a.finalizePhases.SetPhase(finalizePhaseTakeSenders)
		senders := a.takeSenders(req.Txs)
		// The executor's own timer breaks execution down further.
		a.finalizePhases.SetPhase(finalizePhaseExecute)
		result, err := executor.ExecuteBlock(ctx, evmonly.BlockRequest{
			Context: evmonly.BlockContext{
				Number:      number,
				Time:        timestamp,
				GasLimit:    state.gasLimit,
				ChainID:     new(big.Int).Set(a.chainID),
				BaseFee:     evmOnlyBaseFee(),
				BlobBaseFee: new(big.Int),
				ParentHash:  state.parentHash,
				BlockHash:   blockHash,
				PrevRandao:  evmOnlyPrevRandao(timestamp),
			},
			Txs:     req.Txs,
			Senders: senders,
		})
		if err != nil {
			return nil, err
		}
		defer result.Release()
		appHash, err := hashEVMOnlyResult(state.appHash, number, blockHash, result)
		if err != nil {
			return nil, err
		}
		state.pending = utils.Some(evmOnlyPending{height: height, appHash: appHash, blockHash: blockHash})
		state.pendingBlockTime = timestamp
		a.finalizePhases.SetPhase(finalizePhaseTxResults)
		return &abci.ResponseFinalizeBlock{
			AppHash:   append([]byte(nil), appHash[:]...),
			TxResults: evmOnlyABCIResults(result),
		}, nil
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) Commit(context.Context) (*abci.ResponseCommit, error) {
	for state := range a.state.Lock() {
		pending, ok := state.pending.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only Commit called without a finalized block")
		}
		state.committedHeight = pending.height
		state.nextHeight = pending.height + 1
		state.appHash = pending.appHash
		state.parentHash = pending.blockHash
		state.lastBlockTime = state.pendingBlockTime
		state.pending = utils.None[evmOnlyPending]()
		return &abci.ResponseCommit{}, nil
	}
	panic("unreachable")
}

// evmOnlyABCIResults reports a block's executed transactions to consensus, each
// one an OK result carrying the EVM failure reason, if it had one, in its log.
//
// A reverted transaction is a successfully executed one at this layer: it consumed
// its nonce and gas, and the receipt status carries its failure, which is why every
// result here is OK. A non-OK code would put the hash in the mempool's failed set,
// which holds a transaction for a second chance rather than recording it as
// executed. The log is safe to vary with the failure because the results hash
// covers only the code, data and gas.
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
	_, _ = h.Write(previous[:])
	_, _ = h.Write(binary.BigEndian.AppendUint64(nil, height))
	_, _ = h.Write(blockHash[:])
	_, _ = h.Write(binary.BigEndian.AppendUint64(nil, result.GasUsed))
	changesets, err := evmonly.EncodeMemoryStoreChangeSet(result.ChangeSet)
	if err != nil {
		return common.Hash{}, err
	}
	for _, changeset := range changesets {
		writeEVMOnlyHashBytes(h, []byte(changeset.Name))
		for _, pair := range changeset.Changeset.Pairs {
			writeEVMOnlyHashBytes(h, pair.Key)
			if pair.Delete {
				_, _ = h.Write([]byte{1})
			} else {
				_, _ = h.Write([]byte{0})
			}
			writeEVMOnlyHashBytes(h, pair.Value)
		}
	}
	return common.BytesToHash(h.Sum(nil)), nil
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeEVMOnlyHashBytes(w byteWriter, value []byte) {
	_, _ = w.Write(binary.BigEndian.AppendUint64(nil, uint64(len(value))))
	_, _ = w.Write(value)
}

type evmOnlyFundedState struct{}

func (evmOnlyFundedState) GetBalance(common.Address) *big.Int {
	return new(big.Int).Set(evmOnlyBaseBalance)
}
func (evmOnlyFundedState) GetNonce(common.Address) uint64                   { return 0 }
func (evmOnlyFundedState) GetCode(common.Address) []byte                    { return nil }
func (evmOnlyFundedState) GetState(common.Address, common.Hash) common.Hash { return common.Hash{} }
