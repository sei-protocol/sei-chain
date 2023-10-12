package evmrpc

import (
	"context"
	"errors"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

type BlockAPI struct {
	tmClient    rpcclient.Client
	keeper      *keeper.Keeper
	ctxProvider func() sdk.Context
	txDecoder   sdk.TxDecoder
}

func NewBlockAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func() sdk.Context, txDecoder sdk.TxDecoder) *BlockAPI {
	return &BlockAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder}
}

func (a *BlockAPI) GetBlockTransactionCountByNumber(ctx context.Context, number rpc.BlockNumber) *hexutil.Uint {
	numberPtr, err := a.getBlockNumber(ctx, number)
	if err != nil {
		return nil
	}
	block, err := a.tmClient.Block(ctx, numberPtr)
	if err != nil {
		return nil
	}
	cnt := hexutil.Uint(len(block.Block.Txs))
	return &cnt
}

func (a *BlockAPI) GetBlockTransactionCountByHash(ctx context.Context, blockHash common.Hash) *hexutil.Uint {
	block, err := a.tmClient.BlockByHash(ctx, blockHash[:])
	if err != nil {
		return nil
	}
	cnt := hexutil.Uint(len(block.Block.Txs))
	return &cnt
}

func (a *BlockAPI) GetBlockByHash(ctx context.Context, blockHash common.Hash, fullTx bool) (map[string]interface{}, error) {
	block, err := a.tmClient.BlockByHash(ctx, blockHash[:])
	if err != nil {
		return nil, err
	}
	blockRes, err := a.tmClient.BlockResults(ctx, &block.Block.Height)
	if err != nil {
		return nil, err
	}
	return encodeTmBlock(a.ctxProvider(), block, blockRes, a.keeper, a.txDecoder, fullTx)
}

func (a *BlockAPI) GetBlockByNumber(ctx context.Context, number rpc.BlockNumber, fullTx bool) (map[string]interface{}, error) {
	numberPtr, err := a.getBlockNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	block, err := a.tmClient.Block(ctx, numberPtr)
	if err != nil {
		return nil, err
	}
	blockRes, err := a.tmClient.BlockResults(ctx, &block.Block.Height)
	if err != nil {
		return nil, err
	}
	return encodeTmBlock(a.ctxProvider(), block, blockRes, a.keeper, a.txDecoder, fullTx)
}

func (a *BlockAPI) getBlockNumber(ctx context.Context, number rpc.BlockNumber) (*int64, error) {
	var numberPtr *int64
	switch number {
	case rpc.SafeBlockNumber, rpc.FinalizedBlockNumber, rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		numberPtr = nil // requesting Block with nil means the latest block
	case rpc.EarliestBlockNumber:
		genesisRes, err := a.tmClient.Genesis(ctx)
		if err != nil {
			return nil, err
		}
		numberPtr = &genesisRes.Genesis.InitialHeight
	default:
		numberI64 := number.Int64()
		numberPtr = &numberI64
	}
	return numberPtr, nil
}

func encodeTmBlock(
	ctx sdk.Context,
	block *coretypes.ResultBlock,
	blockRes *coretypes.ResultBlockResults,
	k *keeper.Keeper,
	txDecoder sdk.TxDecoder,
	fullTx bool,
) (map[string]interface{}, error) {
	number := big.NewInt(block.Block.Height)
	blockhash := common.HexToHash(string(block.BlockID.Hash))
	lastHash := common.HexToHash(string(block.Block.LastBlockID.Hash))
	appHash := common.HexToHash(string(block.Block.AppHash))
	txHash := common.HexToHash(string(block.Block.DataHash))
	resultHash := common.HexToHash(string(block.Block.LastResultsHash))
	miner := common.HexToAddress(string(block.Block.ProposerAddress))
	gasLimit, gasWanted := int64(0), int64(0)
	transactions := []interface{}{}
	for _, txRes := range blockRes.TxsResults {
		gasLimit += txRes.GasWanted
		gasWanted += txRes.GasUsed
		decoded, err := txDecoder(txRes.Data)
		if err != nil {
			return nil, errors.New("failed to decode transaction")
		}
		if len(decoded.GetMsgs()) != 1 {
			// EVM message must have exactly one message
			continue
		}
		evmTx, ok := decoded.GetMsgs()[0].(*types.MsgEVMTransaction)
		if !ok {
			continue
		}
		ethtx, _ := evmTx.AsTransaction()
		hash := ethtx.Hash()
		if !fullTx {
			transactions = append(transactions, hash)
		} else {
			receipt, err := k.GetReceipt(ctx, hash)
			if err != nil {
				continue
			}
			transactions = append(transactions, hydrateTransaction(ethtx, number, blockhash, receipt))
		}
	}
	result := map[string]interface{}{
		"number":           (*hexutil.Big)(number),
		"hash":             blockhash,
		"parentHash":       lastHash,
		"nonce":            ethtypes.BlockNonce{}, // inapplicable to Sei
		"mixHash":          common.Hash{},         // inapplicable to Sei
		"sha3Uncles":       common.Hash{},         // inapplicable to Sei
		"logsBloom":        ethtypes.Bloom{},      // inapplicable to Sei
		"stateRoot":        appHash,
		"miner":            miner,
		"difficulty":       (*hexutil.Big)(big.NewInt(0)), // inapplicable to Sei
		"extraData":        hexutil.Bytes{},               // inapplicable to Sei
		"gasLimit":         hexutil.Uint64(gasLimit),
		"gasUsed":          hexutil.Uint64(gasWanted),
		"timestamp":        hexutil.Uint64(block.Block.Time.Unix()),
		"transactionsRoot": txHash,
		"receiptsRoot":     resultHash,
		"size":             hexutil.Uint64(block.Block.Size()),
		"uncles":           []common.Hash{}, // inapplicable to Sei
		"transactions":     transactions,
	}
	return result, nil
}
