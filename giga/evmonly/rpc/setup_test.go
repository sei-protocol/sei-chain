package rpc

import (
	"context"
	"math/big"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// testRequestBodyBytes is the request size limit the handler runs under in tests. Unlike the other
// limits it cannot be disabled.
var testRequestBodyBytes = gigaconfig.DefaultConfig.RPC.MaxRequestBodyBytes

// noRPCLimits disables every limit the handler can run without.
var noRPCLimits = gigaconfig.RPCConfig{MaxRequestBodyBytes: testRequestBodyBytes}

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
	code             func(common.Address) ([]byte, error)
	estimateGas      func(context.Context, *core.Message, uint64) (uint64, []byte, error)
	gasLimit         func() (uint64, error)
	minGasPrice      func() (*big.Int, error)
	proxy            utils.Option[*ethrpc.Client]
	proxyCalls       int
	storage          func(common.Address, common.Hash) (common.Hash, error)
	executedBlocks   func() (utils.AtomicRecv[atypes.ExecutedBlocks], error)
	transactionCount func(common.Address) uint64

	balanceAt          func(common.Address, int64) (uint256.Int, error)
	codeAt             func(common.Address, int64) ([]byte, error)
	storageAt          func(common.Address, common.Hash, int64) (common.Hash, error)
	transactionCountAt func(common.Address, int64) (uint64, error)

	// gotOverrides is the overrides the last call or estimate ran with.
	gotOverrides evmonly.CallOverrides

	callAt                func(context.Context, *core.Message, *ethtypes.Header) (*core.ExecutionResult, error)
	estimateGasAt         func(context.Context, *core.Message, uint64, *ethtypes.Header) (uint64, []byte, error)
	estimateGasAfterCalls func(context.Context, []*core.Message, *core.Message, uint64) (uint64, []byte, error)
	createAccessList      func(context.Context, *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error)

	blockContext  func(*ethtypes.Header) (vm.BlockContext, error)
	stateAt       func(context.Context, int64) (vm.StateDB, func(), error)
	stateBeforeTx func(context.Context, *ethtypes.Header, [][]byte) (vm.StateDB, func(), error)
}

func (b *testBackend) EvmBlockContext(header *ethtypes.Header) (vm.BlockContext, error) {
	return b.blockContext(header)
}

func (b *testBackend) EvmStateAt(ctx context.Context, height int64) (vm.StateDB, func(), error) {
	return b.stateAt(ctx, height)
}

func (b *testBackend) EvmStateBeforeTx(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.StateDB, func(), error) {
	return b.stateBeforeTx(ctx, header, txs)
}

func (b *testBackend) ExecutedBlocks() (utils.AtomicRecv[atypes.ExecutedBlocks], error) {
	return b.executedBlocks()
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

func (b *testBackend) EvmBalanceAt(address common.Address, height int64) (uint256.Int, error) {
	return b.balanceAt(address, height)
}

func (b *testBackend) EvmCodeAt(address common.Address, height int64) ([]byte, error) {
	return b.codeAt(address, height)
}

func (b *testBackend) EvmStorageAt(address common.Address, key common.Hash, height int64) (common.Hash, error) {
	return b.storageAt(address, key, height)
}

func (b *testBackend) EvmTransactionCountAt(address common.Address, height int64) (uint64, error) {
	return b.transactionCountAt(address, height)
}

func (b *testBackend) EvmBaseFee() (*big.Int, error) {
	return b.baseFee()
}

func (b *testBackend) EvmBlockNumber() uint64 {
	return b.blockNumber()
}

func (b *testBackend) EvmCall(ctx context.Context, msg *core.Message, overrides evmonly.CallOverrides) (*core.ExecutionResult, error) {
	b.gotOverrides = overrides
	return b.call(ctx, msg)
}

func (b *testBackend) EvmCallAt(ctx context.Context, msg *core.Message, header *ethtypes.Header, overrides evmonly.CallOverrides) (*core.ExecutionResult, error) {
	b.gotOverrides = overrides
	return b.callAt(ctx, msg, header)
}

func (b *testBackend) EvmEstimateGasAt(ctx context.Context, msg *core.Message, gasCap uint64, header *ethtypes.Header, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	b.gotOverrides = overrides
	return b.estimateGasAt(ctx, msg, gasCap, header)
}

func (b *testBackend) EvmChainConfig() (*params.ChainConfig, error) {
	return b.chainConfig()
}

func (b *testBackend) EvmChainID() uint64 {
	return b.chainID()
}

func (b *testBackend) EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	b.gotOverrides = overrides
	return b.estimateGas(ctx, msg, gasCap)
}

func (b *testBackend) EvmEstimateGasAfterCalls(ctx context.Context, calls []*core.Message, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	return b.estimateGasAfterCalls(ctx, calls, msg, gasCap)
}

func (b *testBackend) EvmCreateAccessList(ctx context.Context, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error) {
	return b.createAccessList(ctx, msg)
}

func (b *testBackend) EvmCode(address common.Address) ([]byte, error) {
	return b.code(address)
}

func (b *testBackend) EvmGasLimit() (uint64, error) {
	return b.gasLimit()
}

func (b *testBackend) EvmMinGasPrice() (*big.Int, error) {
	return b.minGasPrice()
}

func (b *testBackend) EvmProxy(common.Address) utils.Option[*ethrpc.Client] {
	b.proxyCalls++
	return b.proxy
}

func (b *testBackend) EvmProxyEnabled() bool {
	return b.proxy.IsPresent()
}

func (b *testBackend) EvmStorage(address common.Address, key common.Hash) (common.Hash, error) {
	return b.storage(address, key)
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

// stubBlockStatsStore overrides GetBlockStats to answer ErrNotFound for each height in
// holeHeights, regardless of the wrapped store's real data.
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

// hashOnlyReceiptStore answers ErrBlockStatsNotSupported and ErrRangeQueryNotSupported on an
// otherwise real store, so tests can exercise the by-hash recompute path pebble takes.
type hashOnlyReceiptStore struct {
	receipt.ReceiptStore
}

func (hashOnlyReceiptStore) GetBlockStats(sdk.Context, uint64) (receipt.BlockStats, error) {
	return receipt.BlockStats{}, receipt.ErrBlockStatsNotSupported
}

func (hashOnlyReceiptStore) IterateReceipts(uint64) (receipt.ReceiptIterator, error) {
	return nil, receipt.ErrRangeQueryNotSupported
}

// stubIteratingReceiptStore overrides IterateReceipts on an otherwise real store, so tests can
// exercise the iterator path without a real block-ordered backend.
type stubIteratingReceiptStore struct {
	receipt.ReceiptStore
	iterate func(startBlock uint64) (receipt.ReceiptIterator, error)
}

func (s stubIteratingReceiptStore) IterateReceipts(startBlock uint64) (receipt.ReceiptIterator, error) {
	return s.iterate(startBlock)
}

// fakeReceiptEntry is one row a fakeReceiptIterator replays.
type fakeReceiptEntry struct {
	blockNumber uint64
	txHash      common.Hash
	receipt     *evmtypes.Receipt
}

// fakeReceiptIterator replays a fixed list of entries as a receipt.ReceiptIterator.
type fakeReceiptIterator struct {
	entries []fakeReceiptEntry
	i       int
}

func (it *fakeReceiptIterator) Next() (bool, error) {
	it.i++
	return it.i <= len(it.entries), nil
}

func (it *fakeReceiptIterator) BlockNumber() uint64 {
	return it.entries[it.i-1].blockNumber
}

func (it *fakeReceiptIterator) TxHash() common.Hash {
	return it.entries[it.i-1].txHash
}

func (it *fakeReceiptIterator) Receipt() (*evmtypes.Receipt, error) {
	return it.entries[it.i-1].receipt, nil
}

func (it *fakeReceiptIterator) Close() error {
	return nil
}
