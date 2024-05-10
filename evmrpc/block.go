package evmrpc

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/lib/ethapi"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

type BlockAPI struct {
	tmClient       rpcclient.Client
	keeper         *keeper.Keeper
	ctxProvider    func(int64) sdk.Context
	txConfig       client.TxConfig
	connectionType ConnectionType
}

func NewBlockAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfig client.TxConfig, connectionType ConnectionType) *BlockAPI {
	return &BlockAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txConfig: txConfig, connectionType: connectionType}
}

func (a *BlockAPI) GetBlockTransactionCountByNumber(ctx context.Context, number rpc.BlockNumber) (result *hexutil.Uint, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getBlockTransactionCountByNumber", a.connectionType, startTime, returnErr == nil)
	numberPtr, err := getBlockNumber(ctx, a.tmClient, number)
	if err != nil {
		return nil, err
	}
	block, err := blockByNumberWithRetry(ctx, a.tmClient, numberPtr, 1)
	if err != nil {
		return nil, err
	}
	return a.getEvmTxCount(block.Block.Txs), nil
}

func (a *BlockAPI) GetBlockTransactionCountByHash(ctx context.Context, blockHash common.Hash) (result *hexutil.Uint, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getBlockTransactionCountByHash", a.connectionType, startTime, returnErr == nil)
	block, err := blockByHashWithRetry(ctx, a.tmClient, blockHash[:], 1)
	if err != nil {
		return nil, err
	}
	return a.getEvmTxCount(block.Block.Txs), nil
}

func (a *BlockAPI) GetBlockByHash(ctx context.Context, blockHash common.Hash, fullTx bool) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getBlockByHash", a.connectionType, startTime, returnErr == nil)
	block, err := blockByHashWithRetry(ctx, a.tmClient, blockHash[:], 1)
	if err != nil {
		return nil, err
	}
	blockRes, err := blockResultsWithRetry(ctx, a.tmClient, &block.Block.Height)
	if err != nil {
		return nil, err
	}
	return EncodeTmBlock(a.ctxProvider(LatestCtxHeight), block, blockRes, a.keeper, a.txConfig.TxDecoder(), fullTx)
}

func (a *BlockAPI) GetBlockByNumber(ctx context.Context, number rpc.BlockNumber, fullTx bool) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getBlockByNumber", a.connectionType, startTime, returnErr == nil)
	numberPtr, err := getBlockNumber(ctx, a.tmClient, number)
	if err != nil {
		return nil, err
	}
	block, err := blockByNumberWithRetry(ctx, a.tmClient, numberPtr, 1)
	if err != nil {
		return nil, err
	}
	blockRes, err := blockResultsWithRetry(ctx, a.tmClient, &block.Block.Height)
	if err != nil {
		return nil, err
	}
	return EncodeTmBlock(a.ctxProvider(LatestCtxHeight), block, blockRes, a.keeper, a.txConfig.TxDecoder(), fullTx)
}

func (a *BlockAPI) GetBlockReceipts(ctx context.Context, number rpc.BlockNumber) (result []map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getBlockReceipts", a.connectionType, startTime, returnErr == nil)
	// Get height from params
	heightPtr, err := getBlockNumber(ctx, a.tmClient, number)
	if err != nil {
		return nil, err
	}
	// Get the block by height
	block, err := blockByNumberWithRetry(ctx, a.tmClient, heightPtr, 1)
	if err != nil {
		return nil, err
	}
	// Get all tx hashes for the block
	height := LatestCtxHeight
	if heightPtr != nil {
		height = *heightPtr
	}
	txHashes := a.keeper.GetTxHashesOnHeight(a.ctxProvider(height), height)
	// Get tx receipts for all hashes in parallel
	wg := sync.WaitGroup{}
	mtx := sync.Mutex{}
	allReceipts := make([]map[string]interface{}, len(txHashes))
	for i, hash := range txHashes {
		wg.Add(1)
		go func(i int, hash common.Hash) {
			defer wg.Done()
			receipt, err := a.keeper.GetReceipt(a.ctxProvider(height), hash)
			if err != nil {
				// When the transaction doesn't exist, skip it
				if !strings.Contains(err.Error(), "not found") {
					mtx.Lock()
					returnErr = err
					mtx.Unlock()
				}
			} else {
				encodedReceipt, err := encodeReceipt(receipt, a.txConfig.TxDecoder(), block)
				if err != nil {
					mtx.Lock()
					returnErr = err
					mtx.Unlock()
				}
				allReceipts[i] = encodedReceipt
			}
		}(i, hash)
	}
	wg.Wait()
	if returnErr != nil {
		return nil, returnErr
	}
	return allReceipts, nil
}

func EncodeTmBlock(
	ctx sdk.Context,
	block *coretypes.ResultBlock,
	blockRes *coretypes.ResultBlockResults,
	k *keeper.Keeper,
	txDecoder sdk.TxDecoder,
	fullTx bool,
) (map[string]interface{}, error) {
	number := big.NewInt(block.Block.Height)
	blockhash := common.HexToHash(block.BlockID.Hash.String())
	blockTime := block.Block.Time
	lastHash := common.HexToHash(block.Block.LastBlockID.Hash.String())
	appHash := common.HexToHash(block.Block.AppHash.String())
	txHash := common.HexToHash(block.Block.DataHash.String())
	resultHash := common.HexToHash(block.Block.LastResultsHash.String())
	miner := common.HexToAddress(block.Block.ProposerAddress.String())
	baseFeePerGas := k.GetBaseFeePerGas(ctx).TruncateInt().BigInt()
	gasLimit, gasWanted := int64(0), int64(0)
	chainConfig := types.DefaultChainConfig().EthereumConfig(k.ChainID(ctx))
	transactions := []interface{}{}
	for i, txRes := range blockRes.TxsResults {
		gasLimit += txRes.GasWanted
		gasWanted += txRes.GasUsed
		decoded, err := txDecoder(block.Block.Txs[i])
		if err != nil {
			return nil, errors.New("failed to decode transaction")
		}
		for _, msg := range decoded.GetMsgs() {
			switch m := msg.(type) {
			case *types.MsgEVMTransaction:
				if m.IsAssociateTx() {
					continue
				}
				ethtx, _ := m.AsTransaction()
				hash := ethtx.Hash()
				if !fullTx {
					transactions = append(transactions, hash)
				} else {
					receipt, err := k.GetReceipt(ctx, hash)
					if err != nil {
						continue
					}
					newTx := ethapi.NewRPCTransaction(ethtx, blockhash, number.Uint64(), uint64(blockTime.Second()), uint64(receipt.TransactionIndex), baseFeePerGas, chainConfig)
					transactions = append(transactions, newTx)
				}
			}
		}
	}
	if len(transactions) == 0 {
		txHash = ethtypes.EmptyTxsHash
	}
	result := map[string]interface{}{
		"number":           (*hexutil.Big)(number),
		"hash":             blockhash,
		"parentHash":       lastHash,
		"nonce":            ethtypes.BlockNonce{},   // inapplicable to Sei
		"mixHash":          common.Hash{},           // inapplicable to Sei
		"sha3Uncles":       ethtypes.EmptyUncleHash, // inapplicable to Sei
		"logsBloom":        k.GetBlockBloom(ctx, block.Block.Height),
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
		"baseFeePerGas":    (*hexutil.Big)(baseFeePerGas),
	}
	if fullTx {
		result["totalDifficulty"] = (*hexutil.Big)(big.NewInt(0)) // inapplicable to Sei
	}
	return result, nil
}

func FullBloom() ethtypes.Bloom {
	bz := []byte{}
	for i := 0; i < ethtypes.BloomByteLength; i++ {
		bz = append(bz, 255)
	}
	return ethtypes.BytesToBloom(bz)
}

// filters out non-evm txs
func (a *BlockAPI) getEvmTxCount(txs tmtypes.Txs) *hexutil.Uint {
	cnt := 0
	// Only count eth txs
	for _, tx := range txs {
		ethtx := getEthTxForTxBz(tx, a.txConfig.TxDecoder())
		if ethtx != nil {
			cnt += 1
		}

	}
	cntHex := hexutil.Uint(cnt)
	return &cntHex
}
