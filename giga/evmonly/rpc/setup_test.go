package rpc

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type testBackend struct {
	balance          func(common.Address) uint256.Int
	baseFee          func() (*big.Int, error)
	block            func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error)
	blockByHash      func(context.Context, *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error)
	blockNumber      func() uint64
	broadcast        func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error)
	call             func(context.Context, *core.Message) (*core.ExecutionResult, error)
	chainConfig      func() (*params.ChainConfig, error)
	chainID          func() uint64
	gasLimit         func() (uint64, error)
	minGasPrice      func() (*big.Int, error)
	proxy            utils.Option[*ethrpc.Client]
	transactionCount func(common.Address) uint64
}

func (b *testBackend) BroadcastTx(ctx context.Context, req *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
	return b.broadcast(ctx, req)
}

func (b *testBackend) Block(ctx context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
	return b.block(ctx, req)
}

func (b *testBackend) BlockByHash(ctx context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
	return b.blockByHash(ctx, req)
}

func (b *testBackend) EvmBalance(address common.Address) uint256.Int {
	return b.balance(address)
}

func (b *testBackend) EvmBaseFee() (*big.Int, error) {
	return b.baseFee()
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

func (b *testBackend) EvmGasLimit() (uint64, error) {
	return b.gasLimit()
}

func (b *testBackend) EvmMinGasPrice() (*big.Int, error) {
	return b.minGasPrice()
}

func (b *testBackend) EvmProxy(common.Address) utils.Option[*ethrpc.Client] {
	return b.proxy
}

func (b *testBackend) EvmTransactionCount(address common.Address) uint64 {
	return b.transactionCount(address)
}

// stubReceiptStore overrides GetReceipt on an otherwise real store so tests can
// inject a store error or a nil receipt without ErrNotFound.
type stubReceiptStore struct {
	receipt.ReceiptStore
	get func(sdk.Context, common.Hash) (*evmtypes.Receipt, error)
}

func (s stubReceiptStore) GetReceipt(ctx sdk.Context, hash common.Hash) (*evmtypes.Receipt, error) {
	return s.get(ctx, hash)
}

// stubBlockStatsStore overrides GetBlockStats on an otherwise real store, answering a genuine
// ErrNotFound for each height in holeHeights regardless of what the wrapped store holds for it.
// Real pruning can only remove a leading prefix, so this is the only way to construct an interior
// or trailing ErrNotFound hole to test walkFeeHistoryRange's restart-on-hole logic directly.
type stubBlockStatsStore struct {
	receipt.ReceiptStore
	holeHeights map[uint64]bool
}

func (s stubBlockStatsStore) GetBlockStats(ctx sdk.Context, blockNumber uint64) (receipt.BlockStats, error) {
	if s.holeHeights[blockNumber] {
		return receipt.BlockStats{}, receipt.ErrNotFound
	}
	return s.ReceiptStore.GetBlockStats(ctx, blockNumber)
}
