package evmrpc

import (
	"context"
	"math/big"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/bytes"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

type TransactionAPI struct {
	tmClient    rpcclient.Client
	keeper      *keeper.Keeper
	ctxProvider func(int64) sdk.Context
	txConfig    client.TxConfig
}

func NewTransactionAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfig client.TxConfig) *TransactionAPI {
	return &TransactionAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txConfig: txConfig}
}

func (t *TransactionAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error) {
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		// When the transaction doesn't exist, the RPC method should return JSON null
		// as per specification.
		return nil, nil
	}
	// can only get tx from block results, since Tx() endpoint requires tendermint-level tx hash which is not available
	// here (we can potentially store tendermint-level tx hash in receipt but that requires calculating sha256 in the chain
	// critical path, whereas this query endpoint is not on the chain critical path, so we are making the tradeoff to
	// sacrifice perf here.)
	height := int64(receipt.BlockNumber)
	blockRes, err := t.tmClient.BlockResults(ctx, &height)
	if err != nil {
		return nil, err
	}
	block, err := t.tmClient.Block(ctx, &height)
	if err != nil {
		return nil, err
	}
	return encodeReceipt(receipt, block.BlockID.Hash, blockRes.TxsResults[receipt.TransactionIndex])
}

func (t *TransactionAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, index hexutil.Uint) *RPCTransaction {
	blockNumber, err := getBlockNumber(ctx, t.tmClient, blockNr)
	if err != nil {
		return nil
	}
	block, err := t.tmClient.Block(ctx, blockNumber)
	if err != nil {
		return nil
	}
	return t.getTransactionWithBlock(block, index)
}

func (t *TransactionAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, index hexutil.Uint) *RPCTransaction {
	block, err := t.tmClient.BlockByHash(ctx, blockHash[:])
	if err != nil {
		return nil
	}
	return t.getTransactionWithBlock(block, index)
}

func (t *TransactionAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) (*RPCTransaction, error) {
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		return nil, err
	}
	return t.GetTransactionByBlockNumberAndIndex(ctx, rpc.BlockNumber(receipt.BlockNumber), hexutil.Uint(receipt.TransactionIndex)), nil
}

func (t *TransactionAPI) GetTransactionCount(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (*hexutil.Uint64, error) {
	var block *coretypes.ResultBlock
	var err error
	if blockNr, ok := blockNrOrHash.Number(); ok {
		blockNumber, blockNumErr := getBlockNumber(ctx, t.tmClient, blockNr)
		if blockNumErr != nil {
			return nil, blockNumErr
		}
		block, err = t.tmClient.Block(ctx, blockNumber)
	} else {
		block, err = t.tmClient.BlockByHash(ctx, blockNrOrHash.BlockHash[:])
	}
	if err != nil {
		return nil, err
	}
	result := hexutil.Uint64(0)
	for _, tx := range block.Block.Txs {
		if ethtx := getEthTxForTxBz(tx, t.txConfig.TxDecoder()); ethtx != nil {
			receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), ethtx.Hash())
			if err != nil {
				continue
			}
			if common.HexToAddress(receipt.From) == address {
				result++
			}
		}
	}
	return &result, nil
}

func (t *TransactionAPI) getTransactionWithBlock(block *coretypes.ResultBlock, index hexutil.Uint) *RPCTransaction {
	if int(index) >= len(block.Block.Txs) {
		return nil
	}
	ethtx := getEthTxForTxBz(block.Block.Txs[int(index)], t.txConfig.TxDecoder())
	if ethtx == nil {
		return nil
	}
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), ethtx.Hash())
	if err != nil {
		return nil
	}
	res := hydrateTransaction(ethtx, big.NewInt(block.Block.Height), common.HexToHash(string(block.BlockID.Hash)), receipt)
	return &res
}

func getEthTxForTxBz(tx tmtypes.Tx, decoder sdk.TxDecoder) *ethtypes.Transaction {
	decoded, err := decoder(tx)
	if err != nil {
		return nil
	}
	if len(decoded.GetMsgs()) != 1 {
		// not EVM tx since EVM tx will have exactly one msg
		return nil
	}
	evmTx, ok := decoded.GetMsgs()[0].(*types.MsgEVMTransaction)
	if !ok {
		return nil
	}
	ethtx, _ := evmTx.AsTransaction()
	return ethtx
}

func encodeReceipt(receipt *types.Receipt, blockHash bytes.HexBytes, txRes *abci.ExecTxResult) (map[string]interface{}, error) {
	logs := []*ethtypes.Log{}
	for _, e := range txRes.Events {
		if e.Type != types.EventTypeEVMLog {
			continue
		}
		log, err := encodeEventToLog(e)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	fields := map[string]interface{}{
		"blockHash":         common.HexToHash(blockHash.String()),
		"blockNumber":       hexutil.Uint64(receipt.BlockNumber),
		"transactionHash":   common.HexToHash(receipt.TxHashHex),
		"transactionIndex":  hexutil.Uint64(receipt.TransactionIndex),
		"from":              common.HexToAddress(receipt.From),
		"to":                common.HexToAddress(receipt.To),
		"gasUsed":           hexutil.Uint64(receipt.GasUsed),
		"cumulativeGasUsed": hexutil.Uint64(receipt.CumulativeGasUsed),
		"logs":              logs,
		"logsBloom":         ethtypes.Bloom{}, // inapplicable to Sei
		"type":              hexutil.Uint(receipt.TxType),
		"effectiveGasPrice": (*hexutil.Big)(big.NewInt(int64(receipt.EffectiveGasPrice))),
		"status":            hexutil.Uint(receipt.Status),
	}
	if receipt.ContractAddress != "" {
		fields["contractAddress"] = common.HexToAddress(receipt.ContractAddress)
	}
	return fields, nil
}

func encodeEventToLog(e abci.Event) (*ethtypes.Log, error) {
	log := ethtypes.Log{}
	for _, a := range e.Attributes {
		switch string(a.Key) {
		case types.AttributeTypeContractAddress:
			log.Address = common.HexToAddress(string(a.Value))
		case types.AttributeTypeTopics:
			log.Topics = utils.Map(strings.Split(string(a.Value), ","), common.HexToHash)
		case types.AttributeTypeData:
			log.Data = a.Value
		case types.AttributeTypeBlockNumber:
			i, err := strconv.ParseUint(string(a.Value), 10, 64)
			if err != nil {
				return nil, err
			}
			log.BlockNumber = i
		case types.AttributeTypeTxIndex:
			i, err := strconv.ParseUint(string(a.Value), 10, 32)
			if err != nil {
				return nil, err
			}
			log.TxIndex = uint(i)
		case types.AttributeTypeIndex:
			i, err := strconv.ParseUint(string(a.Value), 10, 32)
			if err != nil {
				return nil, err
			}
			log.Index = uint(i)
		case types.AttributeTypeBlockHash:
			log.BlockHash = common.HexToHash(string(a.Value))
		case types.AttributeTypeTxHash:
			log.TxHash = common.HexToHash(string(a.Value))
		case types.AttributeTypeRemoved:
			log.Removed = string(a.Value) == "true"
		}
	}
	return &log, nil
}
