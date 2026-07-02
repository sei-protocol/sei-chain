package evmonly

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"runtime"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Executor runs raw EVM transactions against an EVM-native state backend.
type Executor struct {
	cfg        Config
	state      StateReader
	resultSink ResultSink
	occPool    *occWorkerPool
}

type Option func(*Executor)

func WithState(state StateReader) Option {
	return func(e *Executor) {
		if state != nil {
			e.state = state
		}
	}
}

func WithResultSink(sink ResultSink) Option {
	return func(e *Executor) {
		e.resultSink = sink
	}
}

func NewExecutor(cfg Config, opts ...Option) *Executor {
	e := &Executor{
		cfg:   cfg.WithDefaults(),
		state: NewMemoryState(),
	}
	if e.cfg.OCCWorkers > 1 {
		e.occPool = newOCCWorkerPool(e.cfg.OCCWorkers, e.cfg.PinOCCWorkers, e.cfg.OCCWorkerCPUOffset)
		runtime.SetFinalizer(e, (*Executor).Close)
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *Executor) Close() {
	if e == nil || e.occPool == nil {
		return
	}
	runtime.SetFinalizer(e, nil)
	e.occPool.Close()
	e.occPool = nil
}

func (e *Executor) Config() Config {
	return e.cfg
}

func (e *Executor) ExecuteBlock(ctx context.Context, req BlockRequest) (*BlockResult, error) {
	prepared, err := e.PrepareBlock(ctx, req)
	if err != nil {
		return nil, err
	}
	return e.ExecutePreparedBlock(ctx, prepared)
}

func (e *Executor) PrepareBlock(ctx context.Context, req BlockRequest) (PreparedBlock, error) {
	chainConfig := e.chainConfig(req.Context)
	signer := ethtypes.MakeSigner(chainConfig, new(big.Int).SetUint64(req.Context.Number), req.Context.Time)
	parsed, err := parseBlockTxs(ctx, req.Txs, signer)
	if err != nil {
		return PreparedBlock{}, err
	}
	return PreparedBlock{
		Context: req.Context,
		Txs:     parsed,
	}, nil
}

func (e *Executor) ExecutePreparedBlock(ctx context.Context, req PreparedBlock) (*BlockResult, error) {
	var result *BlockResult
	var err error
	if len(req.Txs) == 0 {
		result = &BlockResult{}
	} else if e.useOCC(len(req.Txs)) {
		result, err = e.executeBlockOCC(ctx, req)
	} else {
		result, err = e.executeBlockSequential(ctx, req)
	}
	if err != nil {
		return nil, err
	}
	if err := e.sinkBlockResult(ctx, req.Context.Number, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (e *Executor) sinkBlockResult(ctx context.Context, height uint64, result *BlockResult) error {
	if e.resultSink == nil || result == nil {
		return nil
	}
	if err := e.resultSink.StoreChangeSet(ctx, height, result.ChangeSet); err != nil {
		return fmt.Errorf("store changeset for block %d: %w", height, err)
	}
	if err := e.resultSink.StoreReceipts(ctx, height, result.Receipts); err != nil {
		return fmt.Errorf("store receipts for block %d: %w", height, err)
	}
	return nil
}

func (e *Executor) useOCC(txCount int) bool {
	if e.cfg.OCCWorkers <= 1 || txCount <= 1 {
		return false
	}
	if e.cfg.CustomPrecompiles == nil {
		return true
	}
	return len(e.cfg.CustomPrecompiles.Addresses()) == 0
}

func (e *Executor) executeBlockSequential(ctx context.Context, req PreparedBlock) (*BlockResult, error) {
	chainConfig := e.chainConfig(req.Context)

	stateDB := newNativeStateDB(e.state)
	blockCtx := buildBlockContext(req.Context)
	evm := vm.NewEVM(blockCtx, stateDB, chainConfig, vm.Config{}, customPrecompileMap(e.cfg.CustomPrecompiles))
	stateDB.SetEVM(evm)

	gasLimit := req.Context.GasLimit
	if gasLimit == 0 {
		gasLimit = math.MaxUint64
	}
	gasPool := new(core.GasPool).AddGas(gasLimit)
	baseFee := cloneBig(req.Context.BaseFee)

	result := &BlockResult{
		Txs:      make([]TxResult, 0, len(req.Txs)),
		Receipts: make(ethtypes.Receipts, 0, len(req.Txs)),
	}
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
	result.ChangeSet = stateDB.ChangeSet()
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
	if !e.cfg.DisableGasPriceCheck && e.cfg.MinGasPrice != nil {
		// MinGasPrice is block-validity policy; unlike EVM call failures, it
		// does not produce a receipt for an otherwise invalid block.
		if effectiveGasPrice(tx, baseFee).Cmp(e.cfg.MinGasPrice) < 0 {
			return TxResult{Hash: tx.Hash(), Sender: p.Sender, To: tx.To(), Err: errInsufficientGasPrice},
				nil,
				errInsufficientGasPrice
		}
	}

	msg := transactionToPreparedMessage(p, baseFee)
	msg.SkipNonceChecks = e.cfg.DisableNonceCheck

	stateDB.setTxContext(tx.Hash(), txIndex, txIndexUint)
	logStart := len(stateDB.logs)
	evm.SetTxContext(core.NewEVMTxContext(msg))
	execResult, err := core.ApplyMessage(evm, msg, gasPool)
	if err != nil {
		return TxResult{Hash: tx.Hash(), Sender: p.Sender, To: tx.To(), Err: err}, nil, err
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
		EffectiveGasPrice: effectiveGasPrice(tx, baseFee),
		BlockHash:         block.BlockHash,
		BlockNumber:       new(big.Int).SetUint64(block.Number),
		TransactionIndex:  txIndexUint,
	}
	if tx.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(p.Sender, tx.Nonce())
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
	baseFee := cloneBig(ctx.BaseFee)
	blobBaseFee := cloneBig(ctx.BlobBaseFee)
	gasLimit := ctx.GasLimit
	if gasLimit == 0 {
		gasLimit = math.MaxUint64
	}
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
		GasLimit:    gasLimit,
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

func effectiveGasPrice(tx *ethtypes.Transaction, baseFee *big.Int) *big.Int {
	if baseFee == nil {
		return tx.GasPrice()
	}
	if tx.Type() == ethtypes.DynamicFeeTxType || tx.Type() == ethtypes.BlobTxType || tx.Type() == ethtypes.SetCodeTxType {
		return new(big.Int).Add(baseFee, tx.EffectiveGasTipValue(baseFee))
	}
	return tx.GasPrice()
}

var errInsufficientGasPrice = fmt.Errorf("insufficient gas price")
