package evmrpc

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/lib/ethapi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

type SimulationAPI struct {
	ctxProvider func(int64) sdk.Context
	keeper      *keeper.Keeper
	tmClient    rpcclient.Client
	config      *SimulateConfig
}

func NewSimulationAPI(
	ctxProvider func(int64) sdk.Context,
	keeper *keeper.Keeper,
	tmClient rpcclient.Client,
	config *SimulateConfig,
) *SimulationAPI {
	return &SimulationAPI{
		ctxProvider: ctxProvider,
		keeper:      keeper,
		tmClient:    tmClient,
		config:      config,
	}
}

type accessListResult struct {
	Accesslist *ethtypes.AccessList `json:"accessList"`
	Error      string               `json:"error,omitempty"`
	GasUsed    hexutil.Uint64       `json:"gasUsed"`
}

func (s *SimulationAPI) CreateAccessList(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash) (*accessListResult, error) {
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
	acl, gasUsed, vmerr, err := ethapi.AccessList(ctx, &Backend{
		ctxProvider: s.ctxProvider,
		keeper:      s.keeper,
		tmClient:    s.tmClient,
		config:      s.config,
	}, bNrOrHash, args)
	if err != nil {
		return nil, err
	}
	result := &accessListResult{Accesslist: &acl, GasUsed: hexutil.Uint64(gasUsed)}
	if vmerr != nil {
		result.Error = vmerr.Error()
	}
	return result, nil
}

type SimulateConfig struct {
	GasCap uint64
}

type Backend struct {
	*eth.EthAPIBackend
	ctxProvider func(int64) sdk.Context
	keeper      *keeper.Keeper
	tmClient    rpcclient.Client
	config      *SimulateConfig
}

func NewBackend(ctxProvider func(int64) sdk.Context, keeper *keeper.Keeper, tmClient rpcclient.Client, config *SimulateConfig) *Backend {
	return &Backend{ctxProvider: ctxProvider, keeper: keeper, tmClient: tmClient, config: config}
}

func (b *Backend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (vm.StateDB, *ethtypes.Header, error) {
	var block *coretypes.ResultBlock
	var err error
	if blockNr, ok := blockNrOrHash.Number(); ok {
		blockNumber, blockNumErr := getBlockNumber(ctx, b.tmClient, blockNr)
		if blockNumErr != nil {
			return nil, nil, blockNumErr
		}
		block, err = b.tmClient.Block(ctx, blockNumber)
	} else {
		block, err = b.tmClient.BlockByHash(ctx, blockNrOrHash.BlockHash[:])
	}
	if err != nil {
		return nil, nil, err
	}
	return state.NewDBImpl(b.ctxProvider(block.Block.Height), b.keeper), b.getHeader(), nil
}

func (b *Backend) RPCGasCap() uint64 { return b.config.GasCap }

func (b *Backend) ChainConfig() *params.ChainConfig {
	return b.keeper.GetChainConfig(b.ctxProvider(LatestCtxHeight)).EthereumConfig(b.keeper.ChainID())
}

func (b *Backend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return state.NewDBImpl(b.ctxProvider(LatestCtxHeight), b.keeper).GetNonce(addr), nil
}

func (b *Backend) Engine() consensus.Engine {
	return &Engine{ctxProvider: b.ctxProvider, keeper: b.keeper}
}

func (b *Backend) HeaderByNumber(context.Context, rpc.BlockNumber) (*ethtypes.Header, error) {
	return b.getHeader(), nil
}

func (b *Backend) GetEVM(ctx context.Context, msg *core.Message, state vm.StateDB, header *ethtypes.Header, vmConfig *vm.Config, _ *vm.BlockContext) (*vm.EVM, func() error) {
	if vmConfig == nil {
		panic("vmConfig should not be nil")
	}
	txContext := core.NewEVMTxContext(msg)
	context, err := b.keeper.GetVMBlockContext(b.ctxProvider(LatestCtxHeight), core.GasPool(b.RPCGasCap()))
	if err != nil {
		panic(err)
	}
	return vm.NewEVM(*context, txContext, state, b.ChainConfig(), *vmConfig), state.Error
}

func (b *Backend) getHeader() *ethtypes.Header {
	return &ethtypes.Header{}
}

type Engine struct {
	*ethash.Ethash
	ctxProvider func(int64) sdk.Context
	keeper      *keeper.Keeper
}

func (e *Engine) Author(*ethtypes.Header) (common.Address, error) {
	return e.keeper.GetFeeCollectorAddress(e.ctxProvider(LatestCtxHeight))
}
