package evmrpc

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/armon/go-metrics"
	"math/big"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/tracersutils"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/lib/ethapi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	abci "github.com/tendermint/tendermint/abci/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

type CtxIsWasmdPrecompileCallKeyType string

const CtxIsWasmdPrecompileCallKey CtxIsWasmdPrecompileCallKeyType = "CtxIsWasmdPrecompileCallKey"

type SimulationAPI struct {
	backend        *Backend
	connectionType ConnectionType
	requestLimiter *semaphore.Weighted
}

var (
	gasWindowTotal     uint64 // total gas in current 10-s window
	ethCallWindwoTotal uint64
)

func NewSimulationAPI(
	ctxProvider func(int64) sdk.Context,
	keeper *keeper.Keeper,
	txConfig client.TxConfig,
	tmClient rpcclient.Client,
	config *SimulateConfig,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	connectionType ConnectionType,
) *SimulationAPI {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		for range ticker.C {
			// atomically grab & reset the bucket
			gas := atomic.SwapUint64(&gasWindowTotal, 0)
			ethCall := atomic.SwapUint64(&ethCallWindwoTotal, 0)

			// 10-second window → divide by 10 for gas/sec
			gasPerSec := float64(gas) / 10.0
			ethCallPerSec := float64(ethCall) / 10
			// developer log for easy grepping
			fmt.Printf("[Debug] eth_call gas rate: %.1f gas/s, request rate: %.1f req/s\n",
				gasPerSec, ethCallPerSec)

		}
	}()
	fmt.Printf("Building simulation with max concurent calls:%d\n", config.MaxConcurrentSimulationCalls)
	return &SimulationAPI{
		backend:        NewBackend(ctxProvider, keeper, txConfig, tmClient, config, app, antehandler),
		connectionType: connectionType,
		requestLimiter: semaphore.NewWeighted(int64(config.MaxConcurrentSimulationCalls)), // max concurrent requests
	}
}

type AccessListResult struct {
	Accesslist *ethtypes.AccessList `json:"accessList"`
	Error      string               `json:"error,omitempty"`
	GasUsed    hexutil.Uint64       `json:"gasUsed"`
}

func (s *SimulationAPI) CreateAccessList(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash) (result *AccessListResult, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_createAccessList", s.connectionType, startTime, returnErr == nil)
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
	ctx = context.WithValue(ctx, CtxIsWasmdPrecompileCallKey, wasmd.IsWasmdCall(args.To))
	acl, gasUsed, vmerr, err := ethapi.AccessList(ctx, s.backend, bNrOrHash, args)
	if err != nil {
		return nil, err
	}
	result = &AccessListResult{Accesslist: &acl, GasUsed: hexutil.Uint64(gasUsed)}
	if vmerr != nil {
		result.Error = vmerr.Error()
	}
	return result, nil
}

func (s *SimulationAPI) EstimateGas(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *ethapi.StateOverride) (result hexutil.Uint64, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_estimateGas", s.connectionType, startTime, returnErr)
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
	ctx = context.WithValue(ctx, CtxIsWasmdPrecompileCallKey, wasmd.IsWasmdCall(args.To))
	estimate, err := ethapi.DoEstimateGas(ctx, s.backend, args, bNrOrHash, overrides, s.backend.RPCGasCap())
	return estimate, err
}

func (s *SimulationAPI) EstimateGasAfterCalls(ctx context.Context, args ethapi.TransactionArgs, calls []ethapi.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *ethapi.StateOverride) (result hexutil.Uint64, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_estimateGasAfterCalls", s.connectionType, startTime, returnErr)
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
	ctx = context.WithValue(ctx, CtxIsWasmdPrecompileCallKey, wasmd.IsWasmdCall(args.To))
	estimate, err := ethapi.DoEstimateGasAfterCalls(ctx, s.backend, args, calls, bNrOrHash, overrides, s.backend.RPCEVMTimeout(), s.backend.RPCGasCap())
	return estimate, err
}

func (s *SimulationAPI) Call(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *ethapi.StateOverride, blockOverrides *ethapi.BlockOverrides) (result hexutil.Bytes, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetrics("eth_call", s.connectionType, startTime, returnErr == nil)
		fmt.Printf("[Debug] Finished eth_call with latency %s\n", time.Since(startTime))
	}()
	/* ---------- fail‑fast limiter ---------- */
	if !s.requestLimiter.TryAcquire(1) {
		fmt.Printf("[Debug] Rejected simulation call\n")
		returnErr = errors.New("eth_call rejected due to rate limit: server busy")
		return
	}
	defer s.requestLimiter.Release(1)
	defer func() {
		if r := recover(); r != nil {
			if strings.Contains(fmt.Sprintf("%s", r), "Int overflow") {
				returnErr = errors.New("error: balance override overflow")
			} else {
				returnErr = fmt.Errorf("something went wrong: %v", r)
			}
		}
	}()
	if blockNrOrHash == nil {
		latest := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
		blockNrOrHash = &latest
	}
	ctx = context.WithValue(ctx, CtxIsWasmdPrecompileCallKey, wasmd.IsWasmdCall(args.To))
	callResult, err := ethapi.DoCall(ctx, s.backend, args, *blockNrOrHash, overrides, blockOverrides, s.backend.RPCEVMTimeout(), s.backend.RPCGasCap())
	if err != nil {
		return nil, err
	}
	metrics.IncrCounter([]string{"eth_call", "gas_used"}, float32(callResult.UsedGas))
	atomic.AddUint64(&gasWindowTotal, callResult.UsedGas)
	atomic.AddUint64(&ethCallWindwoTotal, 1)
	// If the result contains a revert reason, try to unpack and return it.
	if len(callResult.Revert()) > 0 {
		return nil, NewRevertError(callResult)
	}
	return callResult.Return(), callResult.Err
}

func NewRevertError(result *core.ExecutionResult) *RevertError {
	reason, errUnpack := abi.UnpackRevert(result.Revert())
	err := errors.New("execution reverted")
	if errUnpack == nil {
		err = fmt.Errorf("execution reverted: %v", reason)
	}
	return &RevertError{
		error:  err,
		reason: hexutil.Encode(result.Revert()),
	}
}

// RevertError is an API error that encompasses an EVM revertal with JSON error
// code and a binary data blob.
type RevertError struct {
	error
	reason string // revert reason hex encoded
}

// ErrorCode returns the JSON error code for a revertal.
// See: https://github.com/ethereum/wiki/wiki/JSON-RPC-Error-Codes-Improvement-Proposal
func (e *RevertError) ErrorCode() int {
	return 3
}

// ErrorData returns the hex encoded revert reason.
func (e *RevertError) ErrorData() interface{} {
	return e.reason
}

type SimulateConfig struct {
	GasCap                       uint64
	EVMTimeout                   time.Duration
	MaxConcurrentSimulationCalls uint64
}

var _ tracers.Backend = (*Backend)(nil)

type Backend struct {
	*eth.EthAPIBackend
	ctxProvider func(int64) sdk.Context
	txConfig    client.TxConfig
	keeper      *keeper.Keeper
	tmClient    rpcclient.Client
	config      *SimulateConfig
	app         *baseapp.BaseApp
	antehandler sdk.AnteHandler
}

func NewBackend(ctxProvider func(int64) sdk.Context, keeper *keeper.Keeper, txConfig client.TxConfig, tmClient rpcclient.Client, config *SimulateConfig, app *baseapp.BaseApp, antehandler sdk.AnteHandler) *Backend {
	return &Backend{
		ctxProvider: ctxProvider,
		keeper:      keeper,
		txConfig:    txConfig,
		tmClient:    tmClient,
		config:      config,
		app:         app,
		antehandler: antehandler,
	}
}

func (b *Backend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (vm.StateDB, *ethtypes.Header, error) {
	height, err := b.getBlockHeight(ctx, blockNrOrHash)
	if err != nil {
		return nil, nil, err
	}
	isWasmdCall, ok := ctx.Value(CtxIsWasmdPrecompileCallKey).(bool)
	sdkCtx := b.ctxProvider(height).WithIsEVM(true).WithEVMEntryViaWasmdPrecompile(ok && isWasmdCall)
	if err := CheckVersion(sdkCtx, b.keeper); err != nil {
		return nil, nil, err
	}
	header := b.getHeader(big.NewInt(height))
	header.BaseFee = b.keeper.GetCurrBaseFeePerGas(b.ctxProvider(LatestCtxHeight)).TruncateInt().BigInt()
	return state.NewDBImpl(sdkCtx, b.keeper, true), header, nil
}

func (b *Backend) GetTransaction(ctx context.Context, txHash common.Hash) (tx *ethtypes.Transaction, blockHash common.Hash, blockNumber uint64, index uint64, err error) {
	sdkCtx := b.ctxProvider(LatestCtxHeight)
	receipt, err := b.keeper.GetReceipt(sdkCtx, txHash)
	if err != nil {
		return nil, common.Hash{}, 0, 0, err
	}
	txHeight := int64(receipt.BlockNumber)
	block, err := blockByNumber(ctx, b.tmClient, &txHeight)
	if err != nil {
		return nil, common.Hash{}, 0, 0, err
	}
	txIndex := hexutil.Uint(receipt.TransactionIndex)
	tmTx := block.Block.Txs[int(txIndex)]
	tx = getEthTxForTxBz(tmTx, b.txConfig.TxDecoder())
	blockHash = common.BytesToHash(block.Block.Header.Hash().Bytes())
	return tx, blockHash, uint64(txHeight), uint64(txIndex), nil
}

func (b *Backend) ChainDb() ethdb.Database {
	panic("implement me")
}

func (b Backend) ConvertBlockNumber(bn rpc.BlockNumber) int64 {
	blockNum := bn.Int64()
	switch blockNum {
	case rpc.SafeBlockNumber.Int64(), rpc.FinalizedBlockNumber.Int64(), rpc.LatestBlockNumber.Int64():
		blockNum = b.ctxProvider(LatestCtxHeight).BlockHeight()
	case rpc.EarliestBlockNumber.Int64():
		genesisRes, err := b.tmClient.Genesis(context.Background())
		if err != nil {
			panic("could not get genesis info from tendermint")
		}
		blockNum = genesisRes.Genesis.InitialHeight
	case rpc.PendingBlockNumber.Int64():
		panic("tracing on pending block is not supported")
	}
	return blockNum
}

func (b Backend) BlockByNumber(ctx context.Context, bn rpc.BlockNumber) (*ethtypes.Block, []tracersutils.TraceBlockMetadata, error) {
	blockNum := b.ConvertBlockNumber(bn)
	tmBlock, err := blockByNumber(ctx, b.tmClient, &blockNum)
	if err != nil {
		return nil, nil, err
	}
	blockRes, err := b.tmClient.BlockResults(ctx, &tmBlock.Block.Height)
	if err != nil {
		return nil, nil, err
	}
	sdkCtx := b.ctxProvider(LatestCtxHeight)
	var txs []*ethtypes.Transaction
	var metadata []tracersutils.TraceBlockMetadata
	for i := range blockRes.TxsResults {
		decoded, err := b.txConfig.TxDecoder()(tmBlock.Block.Txs[i])
		if err != nil {
			return nil, nil, err
		}
		isPrioritized := utils.IsTxPrioritized(decoded)
		if isPrioritized {
			continue
		}
		shouldTrace := false
		for _, msg := range decoded.GetMsgs() {
			switch m := msg.(type) {
			case *types.MsgEVMTransaction:
				if m.IsAssociateTx() {
					continue
				}
				ethtx, _ := m.AsTransaction()
				receipt, err := b.keeper.GetReceipt(sdkCtx, ethtx.Hash())
				if err != nil || receipt.BlockNumber != uint64(tmBlock.Block.Height) || isReceiptFromAnteError(receipt) {
					continue
				}
				TraceReceiptIfApplicable(ctx, receipt)
				shouldTrace = true
				metadata = append(metadata, tracersutils.TraceBlockMetadata{
					ShouldIncludeInTraceResult: true,
					IdxInEthBlock:              len(txs),
				})
				txs = append(txs, ethtx)
			}
		}
		if !shouldTrace {
			metadata = append(metadata, tracersutils.TraceBlockMetadata{
				ShouldIncludeInTraceResult: false,
				IdxInEthBlock:              -1,
				TraceRunnable: func(sd vm.StateDB) {
					typedStateDB := sd.(*state.DBImpl)
					_ = b.app.DeliverTx(typedStateDB.Ctx(), abci.RequestDeliverTx{}, decoded, sha256.Sum256(tmBlock.Block.Txs[i]))
				},
			})
		}
	}
	header := b.getHeader(big.NewInt(blockNum))
	block := &ethtypes.Block{
		Header_: header,
		Txs:     txs,
	}
	block.OverwriteHash(common.BytesToHash(tmBlock.BlockID.Hash))
	return block, metadata, nil
}

func (b Backend) BlockByHash(ctx context.Context, hash common.Hash) (*ethtypes.Block, []tracersutils.TraceBlockMetadata, error) {
	tmBlock, err := blockByHash(ctx, b.tmClient, hash.Bytes())
	if err != nil {
		return nil, nil, err
	}
	blockNumber := rpc.BlockNumber(tmBlock.Block.Height)
	return b.BlockByNumber(ctx, blockNumber)
}

func (b *Backend) RPCGasCap() uint64 { return b.config.GasCap }

func (b *Backend) RPCEVMTimeout() time.Duration { return b.config.EVMTimeout }

func (b *Backend) ChainConfig() *params.ChainConfig {
	ctx := b.ctxProvider(LatestCtxHeight)
	return types.DefaultChainConfig().EthereumConfig(b.keeper.ChainID(ctx))
}

func (b *Backend) GetPoolNonce(_ context.Context, addr common.Address) (uint64, error) {
	return state.NewDBImpl(b.ctxProvider(LatestCtxHeight), b.keeper, true).GetNonce(addr), nil
}

func (b *Backend) Engine() consensus.Engine {
	return &Engine{ctxProvider: b.ctxProvider, keeper: b.keeper}
}

func (b *Backend) HeaderByNumber(ctx context.Context, bn rpc.BlockNumber) (*ethtypes.Header, error) {
	height, err := b.getBlockHeight(ctx, rpc.BlockNumberOrHashWithNumber(bn))
	if err != nil {
		return nil, err
	}
	return b.getHeader(big.NewInt(height)), nil
}

func (b *Backend) StateAtTransaction(ctx context.Context, block *ethtypes.Block, txIndex int, reexec uint64) (*ethtypes.Transaction, vm.BlockContext, vm.StateDB, tracers.StateReleaseFunc, error) {
	emptyRelease := func() {}
	stateDB, txs, err := b.ReplayTransactionTillIndex(ctx, block, txIndex-1)
	if err != nil {
		return nil, vm.BlockContext{}, nil, emptyRelease, err
	}
	blockContext, err := b.keeper.GetVMBlockContext(stateDB.(*state.DBImpl).Ctx(), core.GasPool(b.RPCGasCap()))
	if err != nil {
		return nil, vm.BlockContext{}, nil, emptyRelease, err
	}
	if txIndex > len(txs)-1 {
		return nil, vm.BlockContext{}, nil, emptyRelease, errors.New("transaction not found")
	}
	tx := txs[txIndex]
	sdkTx, err := b.txConfig.TxDecoder()(tx)
	if err != nil {
		panic(err)
	}
	if utils.IsTxPrioritized(sdkTx) {
		return nil, vm.BlockContext{}, nil, emptyRelease, errors.New("cannot trace oracle tx")
	}
	var evmMsg *types.MsgEVMTransaction
	if msgs := sdkTx.GetMsgs(); len(msgs) != 1 {
		return nil, vm.BlockContext{}, nil, emptyRelease, fmt.Errorf("cannot replay non-EVM transaction %d at block %d", txIndex, block.Number().Int64())
	} else if msg, ok := msgs[0].(*types.MsgEVMTransaction); !ok {
		return nil, vm.BlockContext{}, nil, emptyRelease, fmt.Errorf("cannot replay non-EVM transaction %d at block %d", txIndex, block.Number().Int64())
	} else {
		evmMsg = msg
	}
	ethTx, _ := evmMsg.AsTransaction()
	return ethTx, *blockContext, stateDB, emptyRelease, nil
}

func (b *Backend) ReplayTransactionTillIndex(ctx context.Context, block *ethtypes.Block, txIndex int) (vm.StateDB, tmtypes.Txs, error) {
	// Short circuit if it's genesis block.
	if block.Number().Int64() == 0 {
		return nil, nil, errors.New("no transaction in genesis")
	}
	sdkCtx, tmBlock, err := b.initializeBlock(ctx, block)
	if err != nil {
		return nil, nil, err
	}
	if txIndex > len(tmBlock.Block.Txs)-1 {
		return nil, nil, errors.New("did not find transaction")
	}
	if txIndex < 0 {
		return state.NewDBImpl(sdkCtx.WithIsEVM(true), b.keeper, true), tmBlock.Block.Txs, nil
	}
	for idx, tx := range tmBlock.Block.Txs {
		if idx > txIndex {
			break
		}
		sdkTx, err := b.txConfig.TxDecoder()(tx)
		if err != nil {
			panic(err)
		}
		if utils.IsTxPrioritized(sdkTx) {
			continue
		}
		_ = b.app.DeliverTx(sdkCtx, abci.RequestDeliverTx{Tx: tx}, sdkTx, sha256.Sum256(tx))
	}
	return state.NewDBImpl(sdkCtx.WithIsEVM(true), b.keeper, true), tmBlock.Block.Txs, nil
}

func (b *Backend) StateAtBlock(ctx context.Context, block *ethtypes.Block, reexec uint64, base vm.StateDB, readOnly bool, preferDisk bool) (vm.StateDB, tracers.StateReleaseFunc, error) {
	emptyRelease := func() {}
	sdkCtx, _, err := b.initializeBlock(ctx, block)
	if err != nil {
		return nil, emptyRelease, err
	}
	statedb := state.NewDBImpl(sdkCtx, b.keeper, true)
	return statedb, emptyRelease, nil
}

func (b *Backend) initializeBlock(ctx context.Context, block *ethtypes.Block) (sdk.Context, *coretypes.ResultBlock, error) {
	// get the parent block using block.parentHash
	prevBlockHeight := block.Number().Int64() - 1

	blockNumber := block.Number().Int64()
	tmBlock, err := b.tmClient.Block(ctx, &blockNumber)
	if err != nil {
		return sdk.Context{}, nil, fmt.Errorf("cannot find block %d from tendermint", blockNumber)
	}
	res, err := b.tmClient.Validators(ctx, &prevBlockHeight, nil, nil) // todo: load all
	if err != nil {
		return sdk.Context{}, nil, fmt.Errorf("failed to load validators for block %d from tendermint", prevBlockHeight)
	}
	reqBeginBlock := tmBlock.Block.ToReqBeginBlock(res.Validators)
	reqBeginBlock.Simulate = true
	sdkCtx := b.ctxProvider(prevBlockHeight).WithBlockHeight(blockNumber).WithBlockTime(tmBlock.Block.Time)
	_ = b.app.BeginBlock(sdkCtx, reqBeginBlock)
	sdkCtx = sdkCtx.WithNextMs(
		b.ctxProvider(sdkCtx.BlockHeight()).MultiStore(),
		[]string{"oracle", "oracle_mem"},
	)
	return sdkCtx, tmBlock, nil
}

func (b *Backend) GetEVM(_ context.Context, msg *core.Message, stateDB vm.StateDB, h *ethtypes.Header, vmConfig *vm.Config, blockCtx *vm.BlockContext) *vm.EVM {
	txContext := core.NewEVMTxContext(msg)
	if blockCtx == nil {
		blockCtx, _ = b.keeper.GetVMBlockContext(b.ctxProvider(LatestCtxHeight).WithIsEVM(true).WithEVMEntryViaWasmdPrecompile(wasmd.IsWasmdCall(msg.To)), core.GasPool(b.RPCGasCap()))
	}
	return vm.NewEVM(*blockCtx, txContext, stateDB, b.ChainConfig(), *vmConfig, b.keeper.CustomPrecompiles(b.ctxProvider(h.Number.Int64())))
}

func (b *Backend) CurrentHeader() *ethtypes.Header {
	header := b.getHeader(big.NewInt(b.ctxProvider(LatestCtxHeight).BlockHeight()))
	header.BaseFee = b.keeper.GetCurrBaseFeePerGas(b.ctxProvider(LatestCtxHeight)).TruncateInt().BigInt()
	return header
}

func (b *Backend) SuggestGasTipCap(context.Context) (*big.Int, error) {
	return utils.Big0, nil
}

func (b *Backend) getBlockHeight(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (int64, error) {
	var block *coretypes.ResultBlock
	var err error
	if blockNr, ok := blockNrOrHash.Number(); ok {
		blockNumber, blockNumErr := getBlockNumber(ctx, b.tmClient, blockNr)
		if blockNumErr != nil {
			return 0, blockNumErr
		}
		if blockNumber == nil {
			// we don't want to get the latest block from Tendermint's perspective, because
			// Tendermint writes store in TM store before commits application state. The
			// latest block in Tendermint may not have its application state committed yet.
			currentHeight := b.ctxProvider(LatestCtxHeight).BlockHeight()
			blockNumber = &currentHeight
		}
		block, err = blockByNumber(ctx, b.tmClient, blockNumber)
	} else {
		block, err = blockByHash(ctx, b.tmClient, blockNrOrHash.BlockHash[:])
	}
	if err != nil {
		return 0, err
	}
	return block.Block.Height, nil
}

func (b *Backend) getHeader(blockNumber *big.Int) *ethtypes.Header {
	zeroExcessBlobGas := uint64(0)
	header := &ethtypes.Header{
		Difficulty:    common.Big0,
		Number:        blockNumber,
		BaseFee:       nil,
		GasLimit:      b.config.GasCap,
		Time:          uint64(time.Now().Unix()),
		ExcessBlobGas: &zeroExcessBlobGas,
	}
	number := blockNumber.Int64()
	block, err := blockByNumber(context.Background(), b.tmClient, &number)
	//TODO: what should happen if an err occurs here?
	if err == nil {
		header.ParentHash = common.BytesToHash(block.BlockID.Hash)
		header.Time = uint64(block.Block.Header.Time.Unix())
	}
	return header
}

func (b *Backend) GetCustomPrecompiles(h int64) map[common.Address]vm.PrecompiledContract {
	return b.keeper.CustomPrecompiles(b.ctxProvider(h))
}

func (b *Backend) PrepareTx(statedb vm.StateDB, tx *ethtypes.Transaction) error {
	typedStateDB := statedb.(*state.DBImpl)
	typedStateDB.CleanupForTracer()
	ctx, _ := b.keeper.PrepareCtxForEVMTransaction(typedStateDB.Ctx(), tx)
	ctx = ctx.WithIsEVM(true)
	if noSignatureSet(tx) {
		// skip ante if no signature is set
		return nil
	}
	txData, err := ethtx.NewTxDataFromTx(tx)
	if err != nil {
		return fmt.Errorf("transaction cannot be converted to TxData due to %s", err)
	}
	msg, err := types.NewMsgEVMTransaction(txData)
	if err != nil {
		return fmt.Errorf("transaction cannot be converted to MsgEVMTransaction due to %s", err)
	}
	tb := b.txConfig.NewTxBuilder()
	_ = tb.SetMsgs(msg)
	newCtx, err := b.antehandler(ctx, tb.GetTx(), false)
	if err != nil {
		return fmt.Errorf("transaction failed ante handler due to %s", err)
	}
	typedStateDB.WithCtx(newCtx)
	return nil
}

func (b *Backend) GetBlockContext(ctx context.Context, block *ethtypes.Block, statedb vm.StateDB, backend ethapi.ChainContextBackend) (vm.BlockContext, error) {
	blockCtx, err := b.keeper.GetVMBlockContext(statedb.(*state.DBImpl).Ctx(), core.GasPool(b.RPCGasCap()))
	if err != nil {
		return vm.BlockContext{}, nil
	}
	return *blockCtx, nil
}

func noSignatureSet(tx *ethtypes.Transaction) bool {
	isBigIntEmpty := func(b *big.Int) bool {
		return b == nil || b.Cmp(utils.Big0) == 0 || b.Cmp(&big.Int{}) == 0
	}
	v, r, s := tx.RawSignatureValues()
	return isBigIntEmpty(v) && isBigIntEmpty(r) && isBigIntEmpty(s)
}

type Engine struct {
	*ethash.Ethash
	ctxProvider func(int64) sdk.Context
	keeper      *keeper.Keeper
}

func (e *Engine) Author(*ethtypes.Header) (common.Address, error) {
	return e.keeper.GetFeeCollectorAddress(e.ctxProvider(LatestCtxHeight))
}
