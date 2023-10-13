package evmrpc

import (
	"context"
	"math/big"
	"strconv"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/bytes"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type TransactionAPI struct {
	tmClient    rpcclient.Client
	keeper      *keeper.Keeper
	ctxProvider func() sdk.Context
	txDecoder   sdk.TxDecoder
}

func NewTransactionAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func() sdk.Context, txDecoder sdk.TxDecoder) *TransactionAPI {
	return &TransactionAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder}
}

func (t *TransactionAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error) {
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(), hash)
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
