package evmonly

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"go.opentelemetry.io/otel"
)

// executorMeterName is the OTel meter this package's instruments are created on.
const executorMeterName = "evmonly_executor"

// Executor runs raw EVM transactions against snapshots from a giga store.
type Executor struct {
	cfg              Config
	resultSink       ResultSink
	occPool          *occWorkerPool
	parseSizer       *parseSizer
	resultPool       *blockResultPool
	stateDBPool      sync.Pool
	storeMu          sync.Mutex
	stateStore       gigatypes.StateDB
	receiptStore     receipt.ReceiptStore
	changeSetEncoder NamedChangeSetEncoder
	// Optional: nil commits only the state encoder's changesets.
	blockChangeSetEncoder BlockChangeSetEncoder
	// blockEncoderReadsStore is false only for an encoder registered as
	// store-independent, which lets encoding overlap the previous commit.
	blockEncoderReadsStore bool
	missingState           StateReader
	closed                 atomic.Bool

	// Breaks a store-backed block into its stages. That path is serialized by storeMu, so one timer
	// serves the executor.
	blockPhases *seidbmetrics.PhaseTimer
	// Breaks the background persistence of a block into its stages. One block is persisted at a
	// time, so one timer serves it.
	pipelinePhases *seidbmetrics.PhaseTimer

	// The commit running behind the current block, and what it will write. A block reads the latter
	// through an overlay so it need not wait for the former.
	pipelineMu sync.Mutex
	// Closed once the block is fully persisted.
	pipelineDone chan struct{}
	pipelineErr  error
	// The most recent block's receipt write; kept after the block retires so a waiter that arrives
	// late still finds its answer.
	pipelineReceipts *receiptWrite
	pipelineChanges  *pendingChanges
	// Counts commits started, so a reader can tell that a block landed between two of its steps.
	pipelineGeneration uint64
	// The first commit that failed, kept so no caller can miss it.
	pipelineFailure error
}

type Option func(*Executor)

func WithResultSink(sink ResultSink) Option {
	return func(e *Executor) {
		e.resultSink = sink
	}
}

// WithMissingAccountState supplies state for accounts absent from the
// persistent state snapshot.
func WithMissingAccountState(state StateReader) Option {
	return func(e *Executor) {
		e.missingState = state
	}
}

// WithBlockChangeSetEncoder commits the encoder's changesets alongside every
// block's state changes.
// WithStoreIndependentBlockChangeSetEncoder registers an encoder that reads only
// the block context and result. Encoding then overlaps the previous block's
// commit. An encoder that touches the store must use WithBlockChangeSetEncoder.
func WithStoreIndependentBlockChangeSetEncoder(encoder BlockChangeSetEncoder) Option {
	return func(e *Executor) {
		e.blockChangeSetEncoder = encoder
		e.blockEncoderReadsStore = false
	}
}

func WithBlockChangeSetEncoder(encoder BlockChangeSetEncoder) Option {
	return func(e *Executor) {
		e.blockChangeSetEncoder = encoder
		e.blockEncoderReadsStore = true
	}
}

// NewExecutor constructs an EVM-only executor. Call Close to disable future OCC
// execution on this executor.
func NewExecutor(cfg Config, opts ...Option) *Executor {
	e := &Executor{
		cfg:            cfg.WithDefaults(),
		resultPool:     newBlockResultPool(cfg.BlockResultPoolSize),
		blockPhases:    seidbmetrics.NewPhaseTimer(otel.Meter(executorMeterName), "evmonly_block"),
		pipelinePhases: seidbmetrics.NewPhaseTimer(otel.Meter(executorMeterName), "evmonly_pipeline"),
	}
	e.parseSizer = newParseSizer(e.cfg.ParseWorkers)
	if e.cfg.OCCWorkers > 1 {
		e.occPool = newOCCWorkerPool(e.cfg.OCCWorkers)
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *Executor) Close() {
	if e == nil {
		return
	}
	e.closed.Store(true)
	// Land the commit running behind the last block before the pool it may need goes away. The
	// failure is kept rather than reported, for the next AwaitCommits to return.
	_ = e.awaitPipelineCommit()
	if e.occPool != nil {
		e.occPool.Close()
	}
}

func (e *Executor) Config() Config {
	return e.cfg
}

func (e *Executor) ResultPoolStats() BlockResultPoolStats {
	if e == nil {
		return BlockResultPoolStats{}
	}
	return e.resultPool.stats()
}

// waitingForBlockPhase names time an executor loop spends blocked with nothing to run. It is part
// of the phase totals so they account for the loop's whole wall time.
const waitingForBlockPhase = "waiting_for_block"

// MarkWaitingForBlock records that the caller's loop is about to block waiting for a block to
// arrive. The next ExecutePreparedBlock ends the phase.
//
// Without it the phase totals only cover time inside a block, and so describe a share of the work
// rather than a share of the clock.
//
// An executor keeps one phase timer, and that timer is not safe for concurrent use: calling this
// from any goroutine other than the one that drives ExecutePreparedBlock is a data race, not just
// a muddled measurement.
func (e *Executor) MarkWaitingForBlock() {
	if e == nil {
		return
	}
	e.blockPhases.SetPhase(waitingForBlockPhase)
}

// ExecuteBlock prepares and executes a block, and returns once its state is committed.
//
// It is the synchronous entry point. A caller feeding blocks continuously should prepare and
// execute in separate stages instead, where ExecutePreparedBlock leaves the commit running behind
// the next block rather than waiting for it here.
func (e *Executor) ExecuteBlock(ctx context.Context, req BlockRequest) (*BlockResult, error) {
	prepared, err := e.PrepareBlock(ctx, req)
	if err != nil {
		return nil, err
	}
	result, err := e.ExecutePreparedBlock(ctx, prepared)
	if err != nil {
		return nil, err
	}
	if err := e.AwaitCommits(); err != nil {
		result.Release()
		return nil, err
	}
	return result, nil
}

// PrepareBlock decodes the block's transactions and recovers their senders on
// every parse worker.
func (e *Executor) PrepareBlock(ctx context.Context, req BlockRequest) (PreparedBlock, error) {
	return e.PrepareBlockWithin(ctx, req, 0)
}

// PrepareBlockWithin decodes the block's transactions and recovers their senders
// on as few parse workers as the decode is expected to fit in budget on, leaving
// the rest of the processors to whatever runs alongside. A budget of 0 uses every
// parse worker and does not inform the expectation, which comes from the budgeted
// decodes before this one; the first of those is decoded on every worker.
func (e *Executor) PrepareBlockWithin(ctx context.Context, req BlockRequest, budget time.Duration) (PreparedBlock, error) {
	chainConfig := e.chainConfig(req.Context)
	if err := validateBlockContext(chainConfig, req.Context); err != nil {
		return PreparedBlock{}, err
	}
	signer := ethtypes.MakeSigner(chainConfig, new(big.Int).SetUint64(req.Context.Number), req.Context.Time)
	if len(req.Senders) != 0 && len(req.Senders) != len(req.Txs) {
		return PreparedBlock{}, fmt.Errorf("block request has %d senders for %d txs", len(req.Senders), len(req.Txs))
	}
	workers := e.parseSizer.workers(len(req.Txs), budget)
	start := time.Now()
	parsed, err := parseBlockTxs(ctx, req.Txs, signer, req.Senders, workers)
	if err != nil {
		return PreparedBlock{}, err
	}
	if budget > 0 {
		e.parseSizer.observe(len(req.Txs), workers, time.Since(start))
	}
	return PreparedBlock{
		Context: req.Context,
		Txs:     parsed,
	}, nil
}

func (e *Executor) ExecutePreparedBlock(ctx context.Context, req PreparedBlock) (*BlockResult, error) {
	if err := validateBlockContext(e.chainConfig(req.Context), req.Context); err != nil {
		return nil, err
	}
	result, err := e.executePreparedBlockWithStore(ctx, req)
	if err != nil {
		return nil, err
	}
	recordOCCStats(ctx, len(req.Txs), result.OCCStats)
	recordTxExecutionStats(ctx, result.Txs)
	if err := e.sinkBlockResult(ctx, req.Context.Number, result); err != nil {
		result.Release()
		return nil, err
	}
	return result, nil
}

func (e *Executor) executePreparedBlock(ctx context.Context, req PreparedBlock, source StateReader) (*BlockResult, error) {
	if len(req.Txs) == 0 {
		return e.acquireBlockResult(ctx, 0)
	}
	if e.useOCC(len(req.Txs)) {
		return e.executeBlockOCC(ctx, req, source)
	}
	return e.executeBlockSequential(ctx, req, source)
}

func (e *Executor) acquireBlockResult(ctx context.Context, txCapacity int) (*BlockResult, error) {
	if e.resultPool == nil {
		result := &BlockResult{}
		result.prepareForBlock(txCapacity)
		return result, nil
	}
	return e.resultPool.acquire(ctx, txCapacity)
}

func (e *Executor) sinkBlockResult(ctx context.Context, height uint64, result *BlockResult) error {
	if e.resultSink == nil || result == nil {
		return nil
	}
	release := result.retain()
	if err := e.resultSink.StoreBlockResult(ctx, height, result, release); err != nil {
		release()
		return fmt.Errorf("store block result for block %d: %w", height, err)
	}
	return nil
}

func (e *Executor) useOCC(txCount int) bool {
	if e.closed.Load() || e.cfg.OCCWorkers <= 1 || txCount <= 1 {
		return false
	}
	if e.cfg.CustomPrecompiles == nil {
		return true
	}
	return len(e.cfg.CustomPrecompiles.Addresses()) == 0
}

func (e *Executor) acquireStateDB(source StateReader) *nativeStateDB {
	if source == nil {
		source = NewMemoryState()
	}
	if v := e.stateDBPool.Get(); v != nil {
		stateDB := v.(*nativeStateDB)
		stateDB.reset(source)
		return stateDB
	}
	return newNativeStateDB(source)
}

func (e *Executor) releaseStateDB(stateDB *nativeStateDB) {
	if stateDB == nil {
		return
	}
	stateDB.reset(nil)
	e.stateDBPool.Put(stateDB)
}

func (e *Executor) executeBlockSequential(ctx context.Context, req PreparedBlock, source StateReader) (*BlockResult, error) {
	chainConfig := e.chainConfig(req.Context)

	stateDB := e.acquireStateDB(source)
	defer e.releaseStateDB(stateDB)
	blockCtx := buildBlockContext(req.Context)
	evm := vm.NewEVM(blockCtx, stateDB, chainConfig, vm.Config{}, customPrecompileMap(e.cfg.CustomPrecompiles))
	stateDB.SetEVM(evm)

	gasPool := new(core.GasPool).AddGas(req.Context.GasLimit)
	baseFee := cloneOptionalBig(req.Context.BaseFee)

	result, err := e.acquireBlockResult(ctx, len(req.Txs))
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			result.Release()
		}
	}()
	var txIndexUint uint
	for txIndex, p := range req.Txs {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		txResult, receipt, err := e.executeTx(evm, stateDB, gasPool, req.Context, p, txIndex, txIndexUint, baseFee)
		if err != nil {
			return nil, fmt.Errorf("execute tx %d %s: %w", txIndex, p.Tx.Hash(), err)
		}
		txResult.CumulativeGasUsed = result.GasUsed + txResult.GasUsed
		receipt.CumulativeGasUsed = txResult.CumulativeGasUsed
		result.Txs = append(result.Txs, txResult)
		result.Receipts = append(result.Receipts, receipt)
		result.GasUsed += txResult.GasUsed
		txIndexUint++
	}
	stateDB.clearSnapshots()
	stateDB.Finalise(true)
	stateDB.ChangeSetInto(&result.ChangeSet)
	ok = true
	return result, nil
}

func (e *Executor) executeTx(
	evm *vm.EVM,
	stateDB *nativeStateDB,
	gasPool *core.GasPool,
	block BlockContext,
	p PreparedTx,
	txIndex int,
	txIndexUint uint,
	baseFee *big.Int,
) (TxResult, *ethtypes.Receipt, error) {
	tx := p.Tx
	if err := validateSupportedTx(tx); err != nil {
		return TxResult{Hash: tx.Hash(), Sender: p.Sender, To: tx.To(), Err: err}, nil, err
	}
	if !e.cfg.DisableGasPriceCheck && e.cfg.MinGasPrice != nil {
		// MinGasPrice is block-validity policy; unlike EVM call failures, it
		// does not produce a receipt for an otherwise invalid block.
		if EffectiveGasPrice(tx, baseFee).Cmp(e.cfg.MinGasPrice) < 0 {
			return TxResult{Hash: tx.Hash(), Sender: p.Sender, To: tx.To(), Err: errInsufficientGasPrice},
				nil,
				errInsufficientGasPrice
		}
	}

	msg := transactionToPreparedMessage(p, baseFee)
	msg.SkipNonceChecks = e.cfg.DisableNonceCheck

	stateDB.setTxContext(tx.Hash(), txIndex, txIndexUint)
	logStart := len(stateDB.logs)
	snapshot := stateDB.Snapshot()
	// ApplyMessage debits the pool in buyGas before later pre-checks can fail.
	poolGas := gasPool.Gas()
	evm.SetTxContext(core.NewEVMTxContext(msg))
	execResult, err := core.ApplyMessage(evm, msg, gasPool)
	// Read before any revert: RevertToSnapshot restores the recorded error too.
	if stateErr := stateDB.Error(); stateErr != nil {
		return TxResult{Hash: tx.Hash(), Sender: p.Sender, To: tx.To(), Err: stateErr}, nil, stateErr
	}
	if err != nil {
		if !e.cfg.RejectUnappliableTxs {
			return TxResult{Hash: tx.Hash(), Sender: p.Sender, To: tx.To(), Err: err}, nil, err
		}
		stateDB.RevertToSnapshot(snapshot)
		stateDB.clearSnapshots()
		gasPool.SetGas(poolGas)
		txResult, receipt := rejectedTx(p, block, txIndexUint, baseFee, err)
		return txResult, receipt, nil
	}
	stateDB.clearSnapshots()
	stateDB.Finalise(true)

	txLogs := append([]*ethtypes.Log(nil), stateDB.logs[logStart:]...)
	for _, log := range txLogs {
		log.BlockNumber = block.Number
		log.BlockHash = block.BlockHash
		log.TxHash = tx.Hash()
		log.TxIndex = txIndexUint
	}

	status := ethtypes.ReceiptStatusSuccessful
	if execResult.Failed() {
		status = ethtypes.ReceiptStatusFailed
	}
	receipt := &ethtypes.Receipt{
		Type:              tx.Type(),
		Status:            status,
		Logs:              txLogs,
		TxHash:            tx.Hash(),
		GasUsed:           execResult.UsedGas,
		EffectiveGasPrice: EffectiveGasPrice(tx, baseFee),
		BlockHash:         block.BlockHash,
		BlockNumber:       new(big.Int).SetUint64(block.Number),
		TransactionIndex:  txIndexUint,
	}
	if tx.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(p.Sender, tx.Nonce())
	}
	if tx.Type() == ethtypes.BlobTxType {
		// Currently unreachable because blob txs are rejected until block-level
		// blob gas accounting is wired.
		receipt.BlobGasUsed = tx.BlobGas()
		receipt.BlobGasPrice = cloneOptionalBig(block.BlobBaseFee)
	}
	receipt.Bloom = ethtypes.CreateBloom(receipt)

	txResult := TxResult{
		Hash:              tx.Hash(),
		Sender:            p.Sender,
		To:                tx.To(),
		ContractAddress:   receipt.ContractAddress,
		Status:            status,
		GasUsed:           execResult.UsedGas,
		EffectiveGasPrice: new(big.Int).Set(receipt.EffectiveGasPrice),
		Logs:              txLogs,
		Err:               execResult.Err,
	}
	return txResult, receipt, nil
}

// rejectedTx builds the failed, zero-gas receipt and result for a transaction the
// executor did not run.
func rejectedTx(
	p PreparedTx,
	block BlockContext,
	txIndexUint uint,
	baseFee *big.Int,
	cause error,
) (TxResult, *ethtypes.Receipt) {
	tx := p.Tx
	receipt := &ethtypes.Receipt{
		Type:              tx.Type(),
		Status:            ethtypes.ReceiptStatusFailed,
		TxHash:            tx.Hash(),
		EffectiveGasPrice: EffectiveGasPrice(tx, baseFee),
		BlockHash:         block.BlockHash,
		BlockNumber:       new(big.Int).SetUint64(block.Number),
		TransactionIndex:  txIndexUint,
	}
	receipt.Bloom = ethtypes.CreateBloom(receipt)
	return TxResult{
		Hash:              tx.Hash(),
		Sender:            p.Sender,
		To:                tx.To(),
		Status:            ethtypes.ReceiptStatusFailed,
		EffectiveGasPrice: new(big.Int).Set(receipt.EffectiveGasPrice),
		Err:               cause,
		Rejected:          true,
	}, receipt
}

func transactionToPreparedMessage(p PreparedTx, baseFee *big.Int) *core.Message {
	tx := p.Tx
	msg := &core.Message{
		From:                  p.Sender,
		Nonce:                 tx.Nonce(),
		GasLimit:              tx.Gas(),
		GasPrice:              new(big.Int).Set(tx.GasPrice()),
		GasFeeCap:             new(big.Int).Set(tx.GasFeeCap()),
		GasTipCap:             new(big.Int).Set(tx.GasTipCap()),
		To:                    tx.To(),
		Value:                 tx.Value(),
		Data:                  tx.Data(),
		AccessList:            tx.AccessList(),
		SetCodeAuthorizations: tx.SetCodeAuthorizations(),
		SkipNonceChecks:       false,
		SkipFromEOACheck:      false,
		BlobHashes:            tx.BlobHashes(),
		BlobGasFeeCap:         tx.BlobGasFeeCap(),
	}
	if baseFee != nil {
		msg.GasPrice = msg.GasPrice.Add(msg.GasTipCap, baseFee)
		if msg.GasPrice.Cmp(msg.GasFeeCap) > 0 {
			msg.GasPrice = msg.GasFeeCap
		}
	}
	return msg
}

func buildBlockContext(ctx BlockContext) vm.BlockContext {
	prevRandao := ctx.PrevRandao
	baseFee := cloneOptionalBig(ctx.BaseFee)
	blobBaseFee := cloneOptionalBig(ctx.BlobBaseFee)
	return vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash: func(n uint64) common.Hash {
			if ctx.Number > 0 && n == ctx.Number-1 {
				return ctx.ParentHash
			}
			return common.Hash{}
		},
		Coinbase:    ctx.Coinbase,
		GasLimit:    ctx.GasLimit,
		BlockNumber: new(big.Int).SetUint64(ctx.Number),
		Time:        ctx.Time,
		Difficulty:  new(big.Int),
		BaseFee:     baseFee,
		BlobBaseFee: blobBaseFee,
		Random:      &prevRandao,
	}
}

type unresolvedCustomPrecompile struct{}

func (unresolvedCustomPrecompile) RequiredGas([]byte) uint64 {
	return 0
}

func (unresolvedCustomPrecompile) Run(*vm.EVM, common.Address, common.Address, []byte, *big.Int, bool, bool, *tracing.Hooks) ([]byte, error) {
	return nil, precompiles.ErrCustomPrecompilesOpen
}

func customPrecompileMap(registry precompiles.Registry) map[common.Address]vm.PrecompiledContract {
	if registry == nil {
		return nil
	}
	addresses := registry.Addresses()
	if len(addresses) == 0 {
		return nil
	}
	contracts := make(map[common.Address]vm.PrecompiledContract, len(addresses))
	for _, addr := range addresses {
		contracts[addr] = unresolvedCustomPrecompile{}
	}
	return contracts
}

func (e *Executor) chainConfig(ctx BlockContext) *params.ChainConfig {
	var cfg params.ChainConfig
	if e.cfg.ChainConfig != nil {
		cfg = *e.cfg.ChainConfig
	} else {
		cfg = *params.AllDevChainProtocolChanges
	}
	if ctx.ChainID != nil {
		cfg.ChainID = new(big.Int).Set(ctx.ChainID)
	} else if cfg.ChainID != nil {
		cfg.ChainID = new(big.Int).Set(cfg.ChainID)
	} else {
		cfg.ChainID = big.NewInt(1)
	}
	return &cfg
}

func validateSupportedTx(tx *ethtypes.Transaction) error {
	if tx.Type() == ethtypes.BlobTxType {
		return errUnsupportedBlobTx
	}
	return nil
}

func validateBlockContext(chainConfig *params.ChainConfig, ctx BlockContext) error {
	if ctx.GasLimit == 0 {
		return errInvalidBlockGasLimit
	}
	if chainConfig != nil {
		blockNumber := new(big.Int).SetUint64(ctx.Number)
		if chainConfig.IsLondon(blockNumber) && ctx.BaseFee == nil {
			return errMissingBaseFee
		}
		if chainConfig.IsCancun(blockNumber, ctx.Time) && ctx.BlobBaseFee == nil {
			return errMissingBlobBaseFee
		}
	}
	return nil
}

// EffectiveGasPrice is what a transaction actually pays per gas at this base
// fee. Exported because admission has to price on the same quantity block
// validity does; comparing tx.GasPrice() instead admits a dynamic-fee tx on its
// fee cap, and the executor then refuses it fatally.
func EffectiveGasPrice(tx *ethtypes.Transaction, baseFee *big.Int) *big.Int {
	if baseFee == nil {
		return tx.GasPrice()
	}
	if tx.Type() == ethtypes.DynamicFeeTxType || tx.Type() == ethtypes.BlobTxType || tx.Type() == ethtypes.SetCodeTxType {
		return new(big.Int).Add(baseFee, tx.EffectiveGasTipValue(baseFee))
	}
	return tx.GasPrice()
}

var (
	errInsufficientGasPrice = fmt.Errorf("insufficient gas price")
	errInvalidBlockGasLimit = fmt.Errorf("block gas limit must be non-zero")
	errMissingBaseFee       = fmt.Errorf("missing base fee for post-London block")
	errMissingBlobBaseFee   = fmt.Errorf("missing blob base fee for post-Cancun block")
	errUnsupportedBlobTx    = fmt.Errorf("blob transactions require block-level blob gas accounting")
)
