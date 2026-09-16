package rpc

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

type testBackend struct {
	broadcast        func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error)
	block            func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error)
	balance          func(common.Address) uint256.Int
	proxy            utils.Option[*ethrpc.Client]
	proxyCalls       int
	transactionCount func(common.Address) uint64
	blockNumber      func() uint64
	chainID          func() uint64
}

func (b *testBackend) BroadcastTx(ctx context.Context, req *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
	return b.broadcast(ctx, req)
}

func (b *testBackend) Block(ctx context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
	return b.block(ctx, req)
}

func (b *testBackend) EvmBalance(address common.Address) uint256.Int {
	return b.balance(address)
}

func (b *testBackend) EvmProxy(common.Address) utils.Option[*ethrpc.Client] {
	b.proxyCalls++
	return b.proxy
}

func (b *testBackend) EvmProxyEnabled() bool {
	return b.proxy.IsPresent()
}

func (b *testBackend) EvmTransactionCount(address common.Address) uint64 {
	return b.transactionCount(address)
}

func (b *testBackend) EvmBlockNumber() uint64 {
	return b.blockNumber()
}

func (b *testBackend) EvmChainID() uint64 {
	return b.chainID()
}
