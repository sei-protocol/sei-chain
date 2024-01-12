package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
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

const UnconfirmedTxQueryMaxPage = 20
const UnconfirmedTxQueryPerPage = 30

type TransactionAPI struct {
	tmClient    rpcclient.Client
	keeper      *keeper.Keeper
	ctxProvider func(int64) sdk.Context
	txConfig    client.TxConfig
	homeDir     string
}

func NewTransactionAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfig client.TxConfig, homeDir string) *TransactionAPI {
	return &TransactionAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txConfig: txConfig, homeDir: homeDir}
}

func (t *TransactionAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error) {
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			// When the transaction doesn't exist, the RPC method should return JSON null
			// as per specification.
			return nil, nil
		}
		return nil, err
	}
	// can only get tx from block results, since Tx() endpoint requires tendermint-level tx hash which is not available
	// here (we can potentially store tendermint-level tx hash in receipt but that requires calculating sha256 in the chain
	// critical path, whereas this query endpoint is not on the chain critical path, so we are making the tradeoff to
	// sacrifice perf here.)
	height := int64(receipt.BlockNumber)
	blockRes, err := blockResultsWithRetry(ctx, t.tmClient, &height)
	if err != nil {
		return nil, err
	}
	block, err := blockWithRetry(ctx, t.tmClient, &height)
	if err != nil {
		return nil, err
	}
	return encodeReceipt(receipt, block.BlockID.Hash, blockRes.TxsResults[receipt.TransactionIndex])
}

func (t *TransactionAPI) GetVMError(hash common.Hash) (string, error) {
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		return "", err
	}
	return receipt.VmError, nil
}

func (t *TransactionAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, index hexutil.Uint) *RPCTransaction {
	blockNumber, err := getBlockNumber(ctx, t.tmClient, blockNr)
	if err != nil {
		return nil
	}
	block, err := blockWithRetry(ctx, t.tmClient, blockNumber)
	if err != nil {
		return nil
	}
	return t.getTransactionWithBlock(block, index)
}

func (t *TransactionAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, index hexutil.Uint) *RPCTransaction {
	block, err := blockByHashWithRetry(ctx, t.tmClient, blockHash[:])
	if err != nil {
		return nil
	}
	return t.getTransactionWithBlock(block, index)
}

func (t *TransactionAPI) GetPendingNonces(ctx context.Context, addr common.Address) (string, error) {
	sdkCtx := t.ctxProvider(LatestCtxHeight)
	nonces := []int{}
	perPage := UnconfirmedTxQueryPerPage
	for page := 1; page <= UnconfirmedTxQueryMaxPage; page++ {
		res, err := t.tmClient.UnconfirmedTxs(ctx, &page, &perPage)
		if err != nil {
			return "", err
		}
		if len(res.Txs) == 0 {
			break
		}
		for _, tx := range res.Txs {
			etx := getEthTxForTxBz(tx, t.txConfig.TxDecoder())
			if etx != nil {
				signer := ethtypes.MakeSigner(
					t.keeper.GetChainConfig(sdkCtx).EthereumConfig(t.keeper.ChainID(sdkCtx)),
					big.NewInt(sdkCtx.BlockHeight()),
					uint64(sdkCtx.BlockTime().Unix()),
				)
				from, _ := ethtypes.Sender(signer, etx)
				if from == addr {
					nonces = append(nonces, int(etx.Nonce()))
				}
			}
		}
		if page*perPage >= res.Total {
			break
		}
	}
	sort.Ints(nonces)
	return strings.Join(utils.Map(nonces, func(i int) string { return fmt.Sprintf("%d", i) }), ","), nil
}

func (t *TransactionAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) (*RPCTransaction, error) {
	sdkCtx := t.ctxProvider(LatestCtxHeight)
	// first try get from mempool
	for page := 1; page <= UnconfirmedTxQueryMaxPage; page++ {
		res, err := t.tmClient.UnconfirmedTxs(ctx, &page, nil)
		if err != nil || len(res.Txs) == 0 {
			break
		}
		for _, tx := range res.Txs {
			etx := getEthTxForTxBz(tx, t.txConfig.TxDecoder())
			if etx != nil && etx.Hash() == hash {
				signer := ethtypes.MakeSigner(
					t.keeper.GetChainConfig(sdkCtx).EthereumConfig(t.keeper.ChainID(sdkCtx)),
					big.NewInt(sdkCtx.BlockHeight()),
					uint64(sdkCtx.BlockTime().Unix()),
				)
				from, _ := ethtypes.Sender(signer, etx)
				v, r, s := etx.RawSignatureValues()
				res := RPCTransaction{
					Type:     hexutil.Uint64(etx.Type()),
					From:     from,
					Gas:      hexutil.Uint64(etx.Gas()),
					GasPrice: (*hexutil.Big)(etx.GasPrice()),
					Hash:     etx.Hash(),
					Input:    hexutil.Bytes(etx.Data()),
					Nonce:    hexutil.Uint64(etx.Nonce()),
					To:       etx.To(),
					Value:    (*hexutil.Big)(etx.Value()),
					V:        (*hexutil.Big)(v),
					R:        (*hexutil.Big)(r),
					S:        (*hexutil.Big)(s),
				}
				return &res, nil
			}
		}
	}

	// then try get from committed
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	return t.GetTransactionByBlockNumberAndIndex(ctx, rpc.BlockNumber(receipt.BlockNumber), hexutil.Uint(receipt.TransactionIndex)), nil
}

func (t *TransactionAPI) GetTransactionErrorByHash(_ context.Context, hash common.Hash) (string, error) {
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return "", nil
		}
		return "", err
	}
	return receipt.VmError, nil
}

func (t *TransactionAPI) GetTransactionCount(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (*hexutil.Uint64, error) {
	sdkCtx := t.ctxProvider(LatestCtxHeight)

	var pending bool
	if blockNrOrHash.BlockHash == nil && *blockNrOrHash.BlockNumber == rpc.PendingBlockNumber {
		pending = true
	}

	blkNr, err := GetBlockNumberByNrOrHash(ctx, t.tmClient, blockNrOrHash)
	if err != nil {
		return nil, err
	}

	if blkNr != nil {
		sdkCtx = t.ctxProvider(*blkNr)
	}

	nonce := t.keeper.CalculateNextNonce(sdkCtx, address, pending)
	return (*hexutil.Uint64)(&nonce), nil
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
	res := hydrateTransaction(ethtx, big.NewInt(block.Block.Height), common.HexToHash(block.BlockID.Hash.String()), receipt)
	return &res
}

func (t *TransactionAPI) Sign(addr common.Address, data hexutil.Bytes) (hexutil.Bytes, error) {
	kb, err := getTestKeyring(t.homeDir)
	if err != nil {
		return nil, err
	}
	for taddr, privKey := range getAddressPrivKeyMap(kb) {
		if taddr != addr.Hex() {
			continue
		}
		dataHash := accounts.TextHash(data)
		return crypto.Sign(dataHash, privKey)
	}
	return nil, errors.New("address does not have hosted key")
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
	if !ok || evmTx.IsAssociateTx() {
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

var ErrInvalidEventAttribute = errors.New("invalid event attribute")

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
		default:
			return nil, ErrInvalidEventAttribute
		}

	}
	return &log, nil
}
