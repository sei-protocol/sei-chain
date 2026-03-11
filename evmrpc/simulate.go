package evmrpc

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

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
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

type CtxIsWasmdPrecompileCallKeyType string

const CtxIsWasmdPrecompileCallKey CtxIsWasmdPrecompileCallKeyType = "CtxIsWasmdPrecompileCallKey"

type SimulationAPI struct {
	backend        *Backend
	connectionType ConnectionType
	requestLimiter *semaphore.Weighted
}

func NewSimulationAPI(
	ctxProvider func(int64) sdk.Context,
	keeper *keeper.Keeper,
	beginBlockKeepers legacyabci.BeginBlockKeepers,
	txConfigProvider func(int64) client.TxConfig,
	tmClient rpcclient.Client,
	config *SimulateConfig,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	connectionType ConnectionType,
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
	watermarks *WatermarkManager,
) *SimulationAPI {
	api := &SimulationAPI{
		backend:        NewBackend(ctxProvider, keeper, beginBlockKeepers, txConfigProvider, tmClient, config, app, antehandler, globalBlockCache, cacheCreationMutex, watermarks),
		connectionType: connectionType,
	}
	if config.MaxConcurrentSimulationCalls > 0 {
		api.requestLimiter = semaphore.NewWeighted(int64(config.MaxConcurrentSimulationCalls))
	}
	return api
}

type AccessListResult struct {
	Accesslist *ethtypes.AccessList `json:"accessList"`
	Error      string               `json:"error,omitempty"`
	GasUsed    hexutil.Uint64       `json:"gasUsed"`
}

func (s *SimulationAPI) CreateAccessList(ctx context.Context, args export.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash) (result *AccessListResult, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_createAccessList", s.connectionType, startTime, returnErr)
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
	ctx = context.WithValue(ctx, CtxIsWasmdPrecompileCallKey, wasmd.IsWasmdCall(args.To))
	acl, gasUsed, vmerr, err := export.AccessList(ctx, s.backend, bNrOrHash, args, nil)
	if err != nil {
		return nil, err
	}
	result = &AccessListResult{Accesslist: &acl, GasUsed: hexutil.Uint64(gasUsed)}
	if vmerr != nil {
		result.Error = vmerr.Error()
	}
	return result, nil
}

func (s *SimulationAPI) EstimateGas(ctx context.Context, args export.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *export.StateOverride) (result hexutil.Uint64, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_estimateGas", s.connectionType, startTime, returnErr)
	/* ---------- fail‑fast limiter ---------- */
	if s.requestLimiter != nil {
		if !s.requestLimiter.TryAcquire(1) {
			returnErr = errors.New("eth_estimateGas rejected due to rate limit: server busy")
			return
		}
		defer s.requestLimiter.Release(1)
	}
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
	ctx = context.WithValue(ctx, CtxIsWasmdPrecompileCallKey, wasmd.IsWasmdCall(args.To))
	estimate, err := export.DoEstimateGas(ctx, s.backend, args, bNrOrHash, overrides, nil, s.backend.RPCGasCap())
	return estimate, err
}

func (s *SimulationAPI) EstimateGasAfterCalls(ctx context.Context, args export.TransactionArgs, calls []export.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *export.StateOverride) (result hexutil.Uint64, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_estimateGasAfterCalls", s.connectionType, startTime, returnErr)
	/* ---------- fail‑fast limiter ---------- */
	if s.requestLimiter != nil {
		if !s.requestLimiter.TryAcquire(1) {
			returnErr = errors.New("eth_estimateGasAfterCalls rejected due to rate limit: server busy")
			return
		}
		defer s.requestLimiter.Release(1)
	}
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
	ctx = context.WithValue(ctx, CtxIsWasmdPrecompileCallKey, wasmd.IsWasmdCall(args.To))
	estimate, err := export.DoEstimateGasAfterCalls(ctx, s.backend, args, calls, bNrOrHash, overrides, s.backend.RPCEVMTimeout(), s.backend.RPCGasCap())
	return estimate, err
}

func (s *SimulationAPI) Call(ctx context.Context, args export.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *export.StateOverride, blockOverrides *export.BlockOverrides) (result hexutil.Bytes, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_call", s.connectionType, startTime, returnErr)
	/* ---------- fail‑fast limiter ---------- */
	if s.requestLimiter != nil {
		if !s.requestLimiter.TryAcquire(1) {
			returnErr = errors.New("eth_call rejected due to rate limit: server busy")
			return
		}
		defer s.requestLimiter.Release(1)
	}
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
	callResult, err := export.DoCall(ctx, s.backend, args, *blockNrOrHash, overrides, blockOverrides, s.backend.RPCEVMTimeout(), s.backend.RPCGasCap())
	if err != nil {
		return nil, err
	}
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
	MaxConcurrentSimulationCalls int
}

var _ tracers.Backend = (*Backend)(nil)

type Backend struct {
	*eth.EthAPIBackend
	ctxProvider        func(int64) sdk.Context
	txConfigProvider   func(int64) client.TxConfig
	keeper             *keeper.Keeper
	tmClient           rpcclient.Client
	config             *SimulateConfig
	app                *baseapp.BaseApp
	beginBlockKeepers  legacyabci.BeginBlockKeepers
	antehandler        sdk.AnteHandler
	globalBlockCache   BlockCache
	cacheCreationMutex *sync.Mutex
	watermarks         *WatermarkManager
}

func NewBackend(
	ctxProvider func(int64) sdk.Context,
	keeper *keeper.Keeper,
	beginBlockKeepers legacyabci.BeginBlockKeepers,
	txConfigProvider func(int64) client.TxConfig,
	tmClient rpcclient.Client,
	config *SimulateConfig,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
	watermarks *WatermarkManager,
) *Backend {
	return &Backend{
		ctxProvider:        ctxProvider,
		keeper:             keeper,
		beginBlockKeepers:  beginBlockKeepers,
		txConfigProvider:   txConfigProvider,
		tmClient:           tmClient,
		config:             config,
		app:                app,
		antehandler:        antehandler,
		globalBlockCache:   globalBlockCache,
		cacheCreationMutex: cacheCreationMutex,
		watermarks:         watermarks,
	}
}

func (b *Backend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (vm.StateDB, *ethtypes.Header, error) {
	height, isLatestBlock, err := b.getBlockHeight(ctx, blockNrOrHash)
	if err != nil {
		return nil, nil, err
	}
	isWasmdCall, ok := ctx.Value(CtxIsWasmdPrecompileCallKey).(bool)
	sdkCtx := b.ctxProvider(height).WithIsEVM(true).WithEVMEntryViaWasmdPrecompile(ok && isWasmdCall)
	if !isLatestBlock {
		// no need to check version for latest block
		if err := CheckVersion(sdkCtx, b.keeper); err != nil {
			return nil, nil, err
		}
	}
	header := b.getHeader(big.NewInt(height))
	header.BaseFee = b.keeper.GetNextBaseFeePerGas(b.ctxProvider(LatestCtxHeight)).TruncateInt().BigInt()
	return state.NewDBImpl(sdkCtx, b.keeper, true), header, nil
}

func (b *Backend) GetTransaction(ctx context.Context, txHash common.Hash) (found bool, tx *ethtypes.Transaction, blockHash common.Hash, blockNumber uint64, index uint64, err error) {
	sdkCtx := b.ctxProvider(LatestCtxHeight)
	receipt, err := b.keeper.GetReceipt(sdkCtx, txHash)
	if err != nil {
		return false, nil, common.Hash{}, 0, 0, err
	}
	if receipt.BlockNumber > uint64(math.MaxInt64) {
		return false, nil, common.Hash{}, 0, 0, errors.New("block number exceeds int64 max value")
	}

	txHeight := int64(receipt.BlockNumber)
	block, err := blockByNumberRespectingWatermarks(ctx, b.tmClient, b.watermarks, &txHeight, 1)
	if err != nil {
		return false, nil, common.Hash{}, 0, 0, err
	}
	if int(receipt.TransactionIndex) >= len(block.Block.Txs) {
		return false, nil, common.Hash{}, 0, 0, errors.New("transaction index out of range")
	}
	txIndex := hexutil.Uint(receipt.TransactionIndex)
	tmTx := block.Block.Txs[txIndex]
	tx = getEthTxForTxBz(tmTx, b.txConfigProvider(block.Block.Height).TxDecoder())
	blockHash = common.BytesToHash(block.Block.Header.Hash().Bytes())
	return true, tx, blockHash, uint64(txHeight), uint64(txIndex), nil //nolint:gosec
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
	tmBlock, err := blockByNumberRespectingWatermarks(ctx, b.tmClient, b.watermarks, &blockNum, 1)
	if err != nil {
		return nil, nil, err
	}
	blockRes, err := b.tmClient.BlockResults(ctx, &tmBlock.Block.Height)
	if err != nil {
		return nil, nil, err
	}
	TraceTendermintIfApplicable(ctx, "BlockResults", []string{stringifyInt64Ptr(&tmBlock.Block.Height)}, blockRes)
	sdkCtx := b.ctxProvider(LatestCtxHeight)
	var txs []*ethtypes.Transaction
	var metadata []tracersutils.TraceBlockMetadata
	msgs := filterTransactions(b.keeper, b.ctxProvider, b.txConfigProvider, tmBlock, false, false, b.cacheCreationMutex, b.globalBlockCache)
	idxToMsgs := make(map[int]sdk.Msg, len(msgs))
	for _, msg := range msgs {
		idxToMsgs[msg.index] = msg.msg
	}
	for i := range blockRes.TxsResults {
		decoded, err := b.txConfigProvider(blockRes.Height).TxDecoder()(tmBlock.Block.Txs[i])
		if err != nil {
			return nil, nil, err
		}
		isPrioritized := utils.IsTxPrioritized(decoded)
		if isPrioritized {
			continue
		}
		shouldTrace := false
		if msg, ok := idxToMsgs[i]; ok {
			switch m := msg.(type) {
			case *types.MsgEVMTransaction:
				if m.IsAssociateTx() {
					continue
				}
				ethtx, _ := m.AsTransaction()
				if ethtx == nil {
					// AsTransaction may return nil if it fails to unpack the tx data.
					continue
				}
				receipt, err := b.keeper.GetReceipt(sdkCtx, ethtx.Hash())
				if err != nil { //nolint:gosec
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
					typedStateDB := state.GetDBImpl(sd)
					_ = b.app.DeliverTx(typedStateDB.Ctx(), abci.RequestDeliverTxV2{}, decoded, sha256.Sum256(tmBlock.Block.Txs[i]))
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
	tmBlock, err := blockByHashRespectingWatermarks(ctx, b.tmClient, b.watermarks, hash.Bytes(), 1)
	if err != nil {
		return nil, nil, err
	}
	blockNumber := rpc.BlockNumber(tmBlock.Block.Height)
	return b.BlockByNumber(ctx, blockNumber)
}

func (b *Backend) RPCGasCap() uint64 { return b.config.GasCap }

func (b *Backend) RPCEVMTimeout() time.Duration { return b.config.EVMTimeout }

func (b *Backend) chainConfigForHeight(height int64) *params.ChainConfig {
	ctx := b.ctxProvider(height)
	sstore := b.keeper.GetSstoreSetGasEIP2200(ctx)
	return types.DefaultChainConfig().EthereumConfigWithSstore(b.keeper.ChainID(ctx), &sstore)
}

func (b *Backend) ChainConfig() *params.ChainConfig {
	return b.chainConfigForHeight(LatestCtxHeight)
}

func (b *Backend) ChainConfigAtHeight(height int64) *params.ChainConfig {
	return b.chainConfigForHeight(height)
}

func (b *Backend) GetPoolNonce(_ context.Context, addr common.Address) (uint64, error) {
	return state.NewDBImpl(b.ctxProvider(LatestCtxHeight), b.keeper, true).GetNonce(addr), nil
}

func (b *Backend) Engine() consensus.Engine {
	return &Engine{ctxProvider: b.ctxProvider, keeper: b.keeper}
}

func (b *Backend) HeaderByNumber(ctx context.Context, bn rpc.BlockNumber) (*ethtypes.Header, error) {
	height, _, err := b.getBlockHeight(ctx, rpc.BlockNumberOrHashWithNumber(bn))
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
	blockContext, err := b.keeper.GetVMBlockContext(stateDB.(*state.DBImpl).Ctx(), b.keeper.GetGasPool())
	if err != nil {
		return nil, vm.BlockContext{}, nil, emptyRelease, err
	}
	if txIndex > len(txs)-1 {
		return nil, vm.BlockContext{}, nil, emptyRelease, errors.New("transaction not found")
	}
	tx := txs[txIndex]
	sdkTx, err := b.txConfigProvider(block.Number().Int64()).TxDecoder()(tx)
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
		sdkTx, err := b.txConfigProvider(block.Number().Int64()).TxDecoder()(tx)
		if err != nil {
			panic(err)
		}
		if utils.IsTxPrioritized(sdkTx) {
			continue
		}
		_ = b.app.DeliverTx(sdkCtx, abci.RequestDeliverTxV2{Tx: tx}, sdkTx, sha256.Sum256(tx))
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
	tmBlock, err := blockByNumberRespectingWatermarks(ctx, b.tmClient, b.watermarks, &blockNumber, 1)
	if err != nil {
		return sdk.Context{}, nil, fmt.Errorf("cannot find block %d from tendermint", blockNumber)
	}
	res, err := b.tmClient.Validators(ctx, &prevBlockHeight, nil, nil) // todo: load all
	if err != nil {
		return sdk.Context{}, nil, fmt.Errorf("failed to load validators for block %d from tendermint", prevBlockHeight)
	}
	TraceTendermintIfApplicable(ctx, "Validators", []string{stringifyInt64Ptr(&prevBlockHeight)}, res)
	reqBeginBlock := tmBlock.Block.ToReqBeginBlock(res.Validators)
	reqBeginBlock.Simulate = true
	sdkCtx := b.ctxProvider(prevBlockHeight).WithBlockHeight(blockNumber).WithBlockTime(tmBlock.Block.Time)
	legacyabci.BeginBlock(sdkCtx, blockNumber, reqBeginBlock.LastCommitInfo.Votes, tmBlock.Block.Evidence.ToABCI(), b.beginBlockKeepers)
	sdkCtx = sdkCtx.WithNextMs(
		b.ctxProvider(sdkCtx.BlockHeight()).MultiStore(),
		[]string{"oracle", "oracle_mem"},
	)
	return sdkCtx, tmBlock, nil
}

func (b *Backend) GetEVM(_ context.Context, msg *core.Message, stateDB vm.StateDB, h *ethtypes.Header, vmConfig *vm.Config, blockCtx *vm.BlockContext) *vm.EVM {
	txContext := core.NewEVMTxContext(msg)
	if blockCtx == nil {
		blockCtx, _ = b.keeper.GetVMBlockContext(b.ctxProvider(LatestCtxHeight).WithIsEVM(true).WithEVMEntryViaWasmdPrecompile(wasmd.IsWasmdCall(msg.To)), b.keeper.GetGasPool())
	}
	height := h.Number.Int64()
	chainCfg := b.chainConfigForHeight(height)
	evm := vm.NewEVM(*blockCtx, stateDB, chainCfg, *vmConfig, b.keeper.CustomPrecompiles(b.ctxProvider(height)))
	evm.SetTxContext(txContext)
	return evm
}

func (b *Backend) CurrentHeader() *ethtypes.Header {
	header := b.getHeader(big.NewInt(b.ctxProvider(LatestCtxHeight).BlockHeight()))
	header.BaseFee = b.keeper.GetNextBaseFeePerGas(b.ctxProvider(LatestCtxHeight)).TruncateInt().BigInt()
	return header
}

func (b *Backend) SuggestGasTipCap(context.Context) (*big.Int, error) {
	return utils.Big0, nil
}

func (b *Backend) getBlockHeight(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (int64, bool, error) {
	var (
		block         *coretypes.ResultBlock
		err           error
		isLatestBlock bool
	)

	if blockNrOrHash.BlockHash != nil {
		block, err = blockByHashRespectingWatermarks(ctx, b.tmClient, b.watermarks, blockNrOrHash.BlockHash[:], 1)
		if err != nil {
			return 0, false, err
		}
		return block.Block.Height, false, nil
	}

	var blockNumberPtr *int64
	if blockNrOrHash.BlockNumber != nil {
		blockNumberPtr, err = getBlockNumber(ctx, b.tmClient, *blockNrOrHash.BlockNumber)
		if err != nil {
			return 0, false, err
		}
		if blockNumberPtr == nil {
			isLatestBlock = true
		}
	} else {
		isLatestBlock = true
	}
	block, err = blockByNumberRespectingWatermarks(ctx, b.tmClient, b.watermarks, blockNumberPtr, 1)
	if err != nil {
		return 0, false, err
	}
	return block.Block.Height, isLatestBlock, nil
}

func (b *Backend) getHeader(blockNumber *big.Int) *ethtypes.Header {
	zeroExcessBlobGas := uint64(0)
	baseFee := b.keeper.GetNextBaseFeePerGas(b.ctxProvider(blockNumber.Int64() - 1)).TruncateInt().BigInt()
	ctx := b.ctxProvider(blockNumber.Int64())
	if ctx.ChainID() == "pacific-1" && ctx.BlockHeight() < b.keeper.UpgradeKeeper().GetDoneHeight(ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)), "6.2.0") {
		baseFee = nil
	}
	// Get block results to access consensus parameters
	number := blockNumber.Int64()
	block, blockErr := blockByNumberRespectingWatermarks(context.Background(), b.tmClient, b.watermarks, &number, 1)
	var gasLimit uint64
	if blockErr == nil {
		// Try to get consensus parameters from block results
		blockRes, blockResErr := blockResultsWithRetry(context.Background(), b.tmClient, &number)
		if blockResErr == nil && blockRes.ConsensusParamUpdates != nil && blockRes.ConsensusParamUpdates.Block != nil {
			gasLimit = uint64(blockRes.ConsensusParamUpdates.Block.MaxGas) //nolint:gosec
		} else {
			// Fallback to default if block results unavailable
			gasLimit = keeper.DefaultBlockGasLimit
		}
	} else {
		// Fallback to default if block unavailable
		gasLimit = keeper.DefaultBlockGasLimit
	}

	header := &ethtypes.Header{
		Difficulty:    common.Big0,
		Number:        blockNumber,
		BaseFee:       baseFee,
		GasLimit:      gasLimit,
		Time:          toUint64(time.Now().Unix()), //nolint:gosec
		ExcessBlobGas: &zeroExcessBlobGas,
	}

	//TODO: what should happen if an err occurs here?
	if blockErr == nil {
		header.ParentHash = common.BytesToHash(block.BlockID.Hash)
		header.Time = toUint64(block.Block.Time.Unix())
	}
	return header
}

func (b *Backend) GetCustomPrecompiles(h int64) map[common.Address]vm.PrecompiledContract {
	return b.keeper.CustomPrecompiles(b.ctxProvider(h))
}

func (b *Backend) PrepareTx(statedb vm.StateDB, tx *ethtypes.Transaction) error {
	typedStateDB := state.GetDBImpl(statedb)
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
	tb := b.txConfigProvider(ctx.BlockHeight()).NewTxBuilder()
	_ = tb.SetMsgs(msg)
	newCtx, err := b.antehandler(ctx, tb.GetTx(), false)
	if err != nil {
		return fmt.Errorf("transaction failed ante handler due to %s", err)
	}
	typedStateDB.WithCtx(newCtx)
	return nil
}

func (b *Backend) GetBlockContext(ctx context.Context, block *ethtypes.Block, statedb vm.StateDB, backend export.ChainContextBackend) (vm.BlockContext, error) {
	blockCtx, err := b.keeper.GetVMBlockContext(statedb.(*state.DBImpl).Ctx(), b.keeper.GetGasPool())
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
