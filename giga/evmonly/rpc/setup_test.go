package rpc

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

type testBackend struct {
	balance          func(common.Address) uint256.Int
	baseFee          func() (*big.Int, error)
	block            func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error)
	blockNumber      func() uint64
	broadcast        func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error)
	call             func(context.Context, *core.Message) (*core.ExecutionResult, error)
	chainConfig      func() (*params.ChainConfig, error)
	chainID          func() uint64
	proxy            utils.Option[*ethrpc.Client]
	proxyCalls       int
	transactionCount func(common.Address) uint64
}

func (b *testBackend) BroadcastTx(ctx context.Context, req *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
	return b.broadcast(ctx, req)
}

func (b *testBackend) Block(ctx context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
	return b.block(ctx, req)
}

func (b *testBackend) EvmProxy(common.Address) utils.Option[*ethrpc.Client] {
	b.proxyCalls++
	return b.proxy
}

func (b *testBackend) EvmBalance(address common.Address) uint256.Int {
	return b.balance(address)
}

func (b *testBackend) EvmBaseFee() (*big.Int, error) {
	return b.baseFee()
}

func (b *testBackend) EvmProxyEnabled() bool {
	return b.proxy.IsPresent()
}

func (b *testBackend) EvmBlockNumber() uint64 {
	return b.blockNumber()
}

func (b *testBackend) EvmCall(ctx context.Context, msg *core.Message) (*core.ExecutionResult, error) {
	return b.call(ctx, msg)
}

func (b *testBackend) EvmChainConfig() (*params.ChainConfig, error) {
	return b.chainConfig()
}

func (b *testBackend) EvmChainID() uint64 {
	return b.chainID()
}

func (b *testBackend) EvmTransactionCount(address common.Address) uint64 {
	return b.transactionCount(address)
}
