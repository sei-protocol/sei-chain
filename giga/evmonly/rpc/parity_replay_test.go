package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http/httptest"
	"testing"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/parity"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// replayOCCWorkers runs the replay through the executor's optimistic parallel
// path, as an Autobahn validator does.
const replayOCCWorkers = 4

var errNotReplayed = errors.New("not served by a fixture replay")

// replayBackend serves a fixture's blocks, executed by the EVM-only executor
// from the fixture's prestate, to the EVM-only RPC.
type replayBackend struct {
	chainID     uint64
	chainConfig *params.ChainConfig
	executor    *evmonly.Executor
	store       *evmonly.MemoryStore
	receipts    *evmonly.MemoryReceiptStore
	blocks      []*coretypes.ResultBlock
	byHash      map[common.Hash]*coretypes.ResultBlock
	tip         evmonly.BlockContext
}

// serveFixture replays fixture and returns a Caller for an EVM-only RPC
// server over the result.
func serveFixture(t *testing.T, fixture *parity.Fixture) parity.Caller {
	t.Helper()
	backend := replayFixture(t, fixture)
	handler, err := newHandler(backend, backend.receipts, noRPCLimits)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
		handler.Stop()
	})
	return parity.HTTPCaller(server.Client(), server.URL)
}

// replayFixture executes fixture's blocks in order against its prestate under
// v2's chain configuration and the block contexts v2 executed them under.
func replayFixture(t *testing.T, fixture *parity.Fixture) *replayBackend {
	t.Helper()
	chainID := uint64(fixture.ChainID)
	store := evmonly.NewMemoryStore(prestateReader(fixture.Prestate))
	receipts := evmonly.NewMemoryReceiptStore()
	chainConfig := evmtypes.DefaultChainConfig().EthereumConfig(new(big.Int).SetUint64(chainID))
	executor := evmonly.NewExecutor(evmonly.Config{
		ChainConfig: chainConfig,
		// v2 admitted every fixture transaction; admission pricing is not compared.
		MinGasPrice:          new(big.Int),
		OCCWorkers:           replayOCCWorkers,
		RejectUnappliableTxs: true,
	}, evmonly.WithStore(store, store.EncodeChangeSet), evmonly.WithReceiptStore(receipts))
	t.Cleanup(executor.Close)

	backend := &replayBackend{
		chainID:     chainID,
		chainConfig: chainConfig,
		executor:    executor,
		store:       store,
		receipts:    receipts,
		byHash:      map[common.Hash]*coretypes.ResultBlock{},
	}
	for _, block := range fixture.Blocks {
		blockCtx := replayBlockContext(chainID, block)
		result, err := executor.ExecuteBlock(t.Context(), evmonly.BlockRequest{
			Context: blockCtx,
			Txs:     rawTxs(block),
		})
		require.NoError(t, err, "execute block %d", block.Header.Number)
		result.Release()
		resultBlock := consensusBlock(block)
		backend.blocks = append(backend.blocks, resultBlock)
		backend.byHash[block.Header.Hash] = resultBlock
		backend.tip = blockCtx
	}
	require.NoError(t, executor.AwaitCommits())
	return backend
}

// prestateReader returns the fixture's prestate as executor-readable state.
func prestateReader(prestate map[common.Address]parity.Account) *evmonly.MemoryState {
	state := evmonly.NewMemoryState()
	for address, account := range prestate {
		state.SetBalance(address, account.Balance.ToInt())
		state.SetNonce(address, uint64(account.Nonce))
		if len(account.Code) > 0 {
			state.SetCode(address, account.Code)
		}
		for slot, value := range account.Storage {
			state.SetState(address, slot, value)
		}
	}
	return state
}

func replayBlockContext(chainID uint64, block parity.Block) evmonly.BlockContext {
	return evmonly.BlockContext{
		Number:      uint64(block.Header.Number),
		Time:        uint64(block.Header.Time.Unix()), //nolint:gosec // fixture block times are positive.
		GasLimit:    uint64(block.Execution.GasLimit),
		ChainID:     new(big.Int).SetUint64(chainID),
		BaseFee:     block.Execution.BaseFee.ToInt(),
		BlobBaseFee: block.Execution.BlobBaseFee.ToInt(),
		Coinbase:    block.Execution.Coinbase,
		ParentHash:  block.Header.ParentHash,
		BlockHash:   block.Header.Hash,
		PrevRandao:  block.Execution.PrevRandao,
	}
}

func rawTxs(block parity.Block) [][]byte {
	txs := make([][]byte, len(block.Txs))
	for i, tx := range block.Txs {
		txs[i] = tx
	}
	return txs
}

// consensusBlock returns block as the consensus layer would hand it to the
// EVM-only RPC, carrying v2's reported values in the header fields the RPC
// reads directly.
func consensusBlock(block parity.Block) *coretypes.ResultBlock {
	header := block.Header
	txs := make(tmtypes.Txs, len(block.Txs))
	for i, tx := range block.Txs {
		txs[i] = tmtypes.Tx(tx)
	}
	return &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: header.Hash.Bytes()},
		Block: &tmtypes.Block{
			Header: tmtypes.Header{
				Height:          int64(header.Number), //nolint:gosec // fixture heights fit int64.
				Time:            header.Time,
				LastBlockID:     tmtypes.BlockID{Hash: header.ParentHash.Bytes()},
				AppHash:         header.StateRoot.Bytes(),
				ProposerAddress: header.Miner.Bytes(),
				DataHash:        header.TransactionsRoot.Bytes(),
				LastResultsHash: header.ReceiptsRoot.Bytes(),
			},
			Data:       tmtypes.Data{Txs: txs},
			LastCommit: &tmtypes.Commit{Height: int64(header.Number) - 1}, //nolint:gosec // fixture heights fit int64.
		},
	}
}

func (b *replayBackend) Block(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
	if req.Height == nil {
		return b.blocks[len(b.blocks)-1], nil
	}
	height := int64(*req.Height)
	first, tip := b.blocks[0].Block.Height, b.blocks[len(b.blocks)-1].Block.Height
	switch {
	case height <= 0:
		return nil, coretypes.ErrZeroOrNegativeHeight
	case height > tip:
		return nil, coretypes.ErrHeightExceedsChainHead
	case height < first:
		return nil, coretypes.WrapErrHeightNotAvailable(height, utils.Some(first))
	}
	return b.blocks[height-first], nil
}

func (b *replayBackend) BlockByHash(_ context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
	if block, ok := b.byHash[common.BytesToHash(req.Hash)]; ok {
		return block, nil
	}
	return &coretypes.ResultBlock{}, nil
}

func (*replayBackend) BroadcastTx(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
	return nil, errNotReplayed
}

func (b *replayBackend) EvmBalance(address common.Address) uint256.Int {
	view := b.store.OpenView()
	defer view.Close()
	balance := view.GetBalance(storeAddress(address))
	return *new(uint256.Int).SetBytes(balance[:])
}

func (b *replayBackend) EvmBalanceAt(address common.Address, height int64) (uint256.Int, error) {
	view, err := b.stateAt(height)
	if err != nil {
		return uint256.Int{}, err
	}
	defer view.Close()
	balance := view.GetBalance(storeAddress(address))
	return *new(uint256.Int).SetBytes(balance[:]), nil
}

func (b *replayBackend) EvmBaseFee() (*big.Int, error) {
	return new(big.Int).Set(b.tip.BaseFee), nil
}

func (*replayBackend) EvmBlockContext(*ethtypes.Header) (vm.BlockContext, error) {
	return vm.BlockContext{}, errNotReplayed
}

func (b *replayBackend) EvmBlockNumber() uint64 {
	return b.tip.Number
}

func (*replayBackend) ExecutedBlocks() (utils.AtomicRecv[atypes.ExecutedBlocks], error) {
	return utils.AtomicRecv[atypes.ExecutedBlocks]{}, errNotReplayed
}

func (b *replayBackend) EvmCall(ctx context.Context, msg *core.Message) (*core.ExecutionResult, error) {
	return b.executor.Call(ctx, b.tip, msg)
}

func (*replayBackend) EvmCallAt(context.Context, *core.Message, *ethtypes.Header) (*core.ExecutionResult, error) {
	return nil, errNotReplayed
}

func (*replayBackend) EvmEstimateGasAt(context.Context, *core.Message, uint64, *ethtypes.Header) (uint64, []byte, error) {
	return 0, nil, errNotReplayed
}

func (b *replayBackend) EvmChainConfig() (*params.ChainConfig, error) {
	return b.chainConfig, nil
}

func (b *replayBackend) EvmChainID() uint64 {
	return b.chainID
}

func (b *replayBackend) EvmCode(address common.Address) ([]byte, error) {
	view := b.store.OpenView()
	defer view.Close()
	return append([]byte(nil), view.GetCode(storeAddress(address))...), nil
}

func (b *replayBackend) EvmCodeAt(address common.Address, height int64) ([]byte, error) {
	view, err := b.stateAt(height)
	if err != nil {
		return nil, err
	}
	defer view.Close()
	return append([]byte(nil), view.GetCode(storeAddress(address))...), nil
}

func (b *replayBackend) EvmStorageAt(address common.Address, key common.Hash, height int64) (common.Hash, error) {
	view, err := b.stateAt(height)
	if err != nil {
		return common.Hash{}, err
	}
	defer view.Close()
	return view.GetStorage(storeAddress(address), key), nil
}

func (b *replayBackend) EvmStorage(address common.Address, key common.Hash) (common.Hash, error) {
	view := b.store.OpenView()
	defer view.Close()
	return view.GetStorage(storeAddress(address), key), nil
}

func (b *replayBackend) EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	return b.executor.EstimateGas(ctx, b.tip, msg, gasCap)
}

func (b *replayBackend) EvmGasLimit() (uint64, error) {
	return b.tip.GasLimit, nil
}

func (*replayBackend) EvmMinGasPrice() (*big.Int, error) {
	return new(big.Int), nil
}

func (*replayBackend) EvmProxy(common.Address) utils.Option[*ethrpc.Client] {
	return utils.None[*ethrpc.Client]()
}

func (*replayBackend) EvmProxyEnabled() bool {
	return false
}

func (*replayBackend) EvmStateAt(context.Context, int64) (vm.StateDB, func(), error) {
	return nil, nil, errNotReplayed
}

func (*replayBackend) EvmStateBeforeTx(context.Context, *ethtypes.Header, [][]byte) (vm.StateDB, func(), error) {
	return nil, nil, errNotReplayed
}

func (b *replayBackend) EvmTransactionCount(address common.Address) uint64 {
	view := b.store.OpenView()
	defer view.Close()
	return view.GetNonce(storeAddress(address))
}

func (b *replayBackend) EvmTransactionCountAt(address common.Address, height int64) (uint64, error) {
	view, err := b.stateAt(height)
	if err != nil {
		return 0, err
	}
	defer view.Close()
	return view.GetNonce(storeAddress(address)), nil
}

// stateAt opens the replayed EVM state after the block at height.
func (b *replayBackend) stateAt(height int64) (gigatypes.StateView, error) {
	view, ok := b.store.OpenViewAt(height)
	if !ok {
		return nil, fmt.Errorf("the EVM state after block %d is not held by this node", height)
	}
	return view, nil
}

func storeAddress(address common.Address) gigatypes.Address {
	var stored gigatypes.Address
	copy(stored[:], address[:])
	return stored
}
