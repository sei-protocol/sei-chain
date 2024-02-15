package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

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
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/lib/ethapi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

type SimulationAPI struct {
	backend *Backend
}

func NewSimulationAPI(
	ctxProvider func(int64) sdk.Context,
	keeper *keeper.Keeper,
	txDecoder sdk.TxDecoder,
	tmClient rpcclient.Client,
	config *SimulateConfig,
) *SimulationAPI {
	return &SimulationAPI{
		backend: NewBackend(ctxProvider, keeper, txDecoder, tmClient, config),
	}
}

type AccessListResult struct {
	Accesslist *ethtypes.AccessList `json:"accessList"`
	Error      string               `json:"error,omitempty"`
	GasUsed    hexutil.Uint64       `json:"gasUsed"`
}

func (s *SimulationAPI) CreateAccessList(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash) (result *AccessListResult, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_createAccessList", startTime, returnErr == nil)
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
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
	defer recordMetrics("eth_estimateGas", startTime, returnErr == nil)
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
	estimate, err := ethapi.DoEstimateGas(ctx, s.backend, args, bNrOrHash, overrides, s.backend.RPCGasCap())
	return estimate, err
}

func (s *SimulationAPI) Call(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *ethapi.StateOverride, blockOverrides *ethapi.BlockOverrides) (result hexutil.Bytes, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_call", startTime, returnErr == nil)
	if blockNrOrHash == nil {
		latest := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
		blockNrOrHash = &latest
	}
	callResult, err := ethapi.DoCall(ctx, s.backend, args, *blockNrOrHash, overrides, blockOverrides, s.backend.RPCEVMTimeout(), s.backend.RPCGasCap())
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
	GasCap     uint64
	EVMTimeout time.Duration
}

var _ tracers.Backend = (*Backend)(nil)

type Backend struct {
	*eth.EthAPIBackend
	ctxProvider func(int64) sdk.Context
	txDecoder   sdk.TxDecoder
	keeper      *keeper.Keeper
	tmClient    rpcclient.Client
	config      *SimulateConfig
}

func NewBackend(ctxProvider func(int64) sdk.Context, keeper *keeper.Keeper, txDecoder sdk.TxDecoder, tmClient rpcclient.Client, config *SimulateConfig) *Backend {
	return &Backend{ctxProvider: ctxProvider, keeper: keeper, txDecoder: txDecoder, tmClient: tmClient, config: config}
}

func (b *Backend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (vm.StateDB, *ethtypes.Header, error) {
	height, err := b.getBlockHeight(ctx, blockNrOrHash)
	if err != nil {
		return nil, nil, err
	}
	sdkCtx := b.ctxProvider(height)
	if err := CheckVersion(sdkCtx, b.keeper); err != nil {
		return nil, nil, err
	}
	return state.NewDBImpl(b.ctxProvider(height), b.keeper, true), b.getHeader(big.NewInt(height)), nil
}

func (b *Backend) GetTransaction(ctx context.Context, txHash common.Hash) (tx *ethtypes.Transaction, blockHash common.Hash, blockNumber uint64, index uint64, err error) {
	sdkCtx := b.ctxProvider(LatestCtxHeight)
	receipt, err := b.keeper.GetReceipt(sdkCtx, txHash)
	if err != nil {
		return nil, common.Hash{}, 0, 0, err
	}
	txHeight := int64(receipt.BlockNumber)
	block, err := b.tmClient.Block(ctx, &txHeight)
	if err != nil {
		return nil, common.Hash{}, 0, 0, err
	}
	txIndex := hexutil.Uint(receipt.TransactionIndex)
	tmTx := block.Block.Txs[int(index)]
	tx = getEthTxForTxBz(tmTx, b.txDecoder)
	blockHash = common.BytesToHash(block.Block.Header.Hash().Bytes())
	return tx, blockHash, uint64(txHeight), uint64(txIndex), nil
}

func (b *Backend) ChainDb() ethdb.Database {
	panic("implement me")
}

func (b Backend) BlockByNumber(ctx context.Context, bn rpc.BlockNumber) (*ethtypes.Block, error) {
	bnn := bn.Int64()
	tmBlock, err := b.tmClient.Block(ctx, &bnn)
	if err != nil {
		return nil, err
	}
	blockRes, err := b.tmClient.BlockResults(ctx, &tmBlock.Block.Height)
	if err != nil {
		return nil, err
	}
	var txs []*ethtypes.Transaction
	for i := range blockRes.TxsResults {
		decoded, err := b.txDecoder(tmBlock.Block.Txs[i])
		if err != nil {
			return nil, err
		}
		for _, msg := range decoded.GetMsgs() {
			switch m := msg.(type) {
			case *types.MsgEVMTransaction:
				if m.IsAssociateTx() {
					continue
				}
				ethtx, _ := m.AsTransaction()
				txs = append(txs, ethtx)
			}
		}
	}
	header := b.getHeader(big.NewInt(bn.Int64()))
	block := &ethtypes.Block{
		Header_: header,
		Txs:     txs,
	}
	return block, nil
}

func (b Backend) BlockByHash(ctx context.Context, hash common.Hash) (*ethtypes.Block, error) {
	tmBlock, err := b.tmClient.BlockByHash(ctx, hash.Bytes())
	if err != nil {
		return nil, err
	}
	if tmBlock.Block == nil {
		panic("tmBlock.Block is nil")
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

func (b *Backend) StateAtTransaction(ctx context.Context, block *ethtypes.Block, txIndex int, reexec uint64) (*core.Message, vm.BlockContext, vm.StateDB, tracers.StateReleaseFunc, error) {
	emptyRelease := func() {}
	// Short circuit if it's genesis block.
	if block.Number().Int64() == 0 {
		return nil, vm.BlockContext{}, nil, emptyRelease, errors.New("no transaction in genesis")
	}
	// get the parent block using block.parentHash
	prevBlockHeight := block.Number().Int64() - 1
	// Get statedb of parent block from the store
	statedb := state.NewDBImpl(b.ctxProvider(prevBlockHeight), b.keeper, true)
	if txIndex == 0 && len(block.Transactions()) == 0 {
		return nil, vm.BlockContext{}, statedb, emptyRelease, nil
	}
	// Recompute transactions up to the target index. (only doing EVM at the moment, but should do both EVM + Cosmos)
	signer := ethtypes.MakeSigner(b.ChainConfig(), block.Number(), block.Time())
	for idx, tx := range block.Transactions() {
		msg, _ := core.TransactionToMessage(tx, signer, block.BaseFee())
		txContext := core.NewEVMTxContext(msg)
		blockContext, err := b.keeper.GetVMBlockContext(b.ctxProvider(prevBlockHeight), core.GasPool(b.RPCGasCap()))
		if err != nil {
			return nil, vm.BlockContext{}, nil, nil, err
		}
		if idx == txIndex {
			return msg, *blockContext, statedb, emptyRelease, nil
		}
		// Not yet the searched for transaction, execute on top of the current state
		vmenv := vm.NewEVM(*blockContext, txContext, statedb, b.ChainConfig(), vm.Config{})
		statedb.SetTxContext(tx.Hash(), idx)
		if _, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(tx.Gas())); err != nil {
			return nil, vm.BlockContext{}, nil, nil, fmt.Errorf("transaction %#x failed: %v", tx.Hash(), err)
		}
		// Ensure any modifications are committed to the state
		// Only delete empty objects if EIP158/161 (a.k.a Spurious Dragon) is in effect
		statedb.Finalise(vmenv.ChainConfig().IsEIP158(block.Number()))
	}
	return nil, vm.BlockContext{}, nil, nil, fmt.Errorf("transaction index %d out of range for block %#x", txIndex, block.Hash())
}

func (b *Backend) GetEVM(_ context.Context, msg *core.Message, stateDB vm.StateDB, _ *ethtypes.Header, vmConfig *vm.Config, _ *vm.BlockContext) *vm.EVM {
	txContext := core.NewEVMTxContext(msg)
	context, _ := b.keeper.GetVMBlockContext(b.ctxProvider(LatestCtxHeight), core.GasPool(b.RPCGasCap()))
	evm := vm.NewEVM(*context, txContext, stateDB, b.ChainConfig(), *vmConfig)
	if dbImpl, ok := stateDB.(*state.DBImpl); ok {
		dbImpl.SetEVM(evm)
	}
	return evm
}

func (b *Backend) CurrentHeader() *ethtypes.Header {
	return b.getHeader(big.NewInt(b.ctxProvider(LatestCtxHeight).BlockHeight()))
}

func (b *Backend) SuggestGasTipCap(context.Context) (*big.Int, error) {
	return big.NewInt(0), nil
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
		block, err = b.tmClient.Block(ctx, blockNumber)
	} else {
		block, err = b.tmClient.BlockByHash(ctx, blockNrOrHash.BlockHash[:])
	}
	if err != nil {
		return 0, err
	}
	return block.Block.Height, nil
}

func (b *Backend) getHeader(blockNumber *big.Int) *ethtypes.Header {
	return &ethtypes.Header{
		Difficulty: common.Big0,
		Number:     blockNumber,
		BaseFee:    b.keeper.GetBaseFeePerGas(b.ctxProvider(LatestCtxHeight)).BigInt(),
		GasLimit:   b.config.GasCap,
		Time:       uint64(time.Now().Unix()),
	}
}

type Engine struct {
	*ethash.Ethash
	ctxProvider func(int64) sdk.Context
	keeper      *keeper.Keeper
}

func (e *Engine) Author(*ethtypes.Header) (common.Address, error) {
	return e.keeper.GetFeeCollectorAddress(e.ctxProvider(LatestCtxHeight))
}
