package evmrpc

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
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

func (t *TransactionAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getTransactionReceipt", startTime, returnErr == nil)
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			// When the transaction doesn't exist, the RPC method should return JSON null
			// as per specification.
			return nil, nil
		}
		return nil, err
	}
	height := int64(receipt.BlockNumber)
	block, err := blockByNumberWithRetry(ctx, t.tmClient, &height, 1)
	if err != nil {
		return nil, err
	}
	return encodeReceipt(receipt, t.txConfig.TxDecoder(), block)
}

func (t *TransactionAPI) GetVMError(hash common.Hash) (result string, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getVMError", startTime, true)
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		return "", err
	}
	return receipt.VmError, nil
}

func (t *TransactionAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, index hexutil.Uint) (result *RPCTransaction, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getTransactionByBlockNumberAndIndex", startTime, returnErr == nil)
	blockNumber, err := getBlockNumber(ctx, t.tmClient, blockNr)
	if err != nil {
		return nil, err
	}
	block, err := blockByNumberWithRetry(ctx, t.tmClient, blockNumber, 1)
	if err != nil {
		return nil, err
	}
	return t.getTransactionWithBlock(block, index)
}

func (t *TransactionAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, index hexutil.Uint) (result *RPCTransaction, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getTransactionByBlockHashAndIndex", startTime, returnErr == nil)
	block, err := blockByHash(ctx, t.tmClient, blockHash[:])
	if err != nil {
		return nil, err
	}
	return t.getTransactionWithBlock(block, index)
}

func (t *TransactionAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) (result *RPCTransaction, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getTransactionByHash", startTime, returnErr == nil)
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
					types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(sdkCtx)),
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
	return t.GetTransactionByBlockNumberAndIndex(ctx, rpc.BlockNumber(receipt.BlockNumber), hexutil.Uint(receipt.TransactionIndex))
}

func (t *TransactionAPI) GetTransactionErrorByHash(_ context.Context, hash common.Hash) (result string, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getTransactionErrorByHash", startTime, returnErr == nil)
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return "", nil
		}
		return "", err
	}
	return receipt.VmError, nil
}

func (t *TransactionAPI) GetTransactionCount(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (result *hexutil.Uint64, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_getTransactionCount", startTime, returnErr == nil)
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
		if err := CheckVersion(sdkCtx, t.keeper); err != nil {
			return nil, err
		}
	}

	nonce := t.keeper.CalculateNextNonce(sdkCtx, address, pending)
	return (*hexutil.Uint64)(&nonce), nil
}

func (t *TransactionAPI) getTransactionWithBlock(block *coretypes.ResultBlock, index hexutil.Uint) (*RPCTransaction, error) {
	if int(index) >= len(block.Block.Txs) {
		return nil, nil
	}
	ethtx := getEthTxForTxBz(block.Block.Txs[int(index)], t.txConfig.TxDecoder())
	if ethtx == nil {
		return nil, nil
	}
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), ethtx.Hash())
	if err != nil {
		return nil, err
	}
	res := hydrateTransaction(ethtx, big.NewInt(block.Block.Height), common.HexToHash(block.BlockID.Hash.String()), receipt)
	return &res, nil
}

func (t *TransactionAPI) Sign(addr common.Address, data hexutil.Bytes) (result hexutil.Bytes, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_sign", startTime, returnErr == nil)
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

// Gets the EVM tx index based on the tx index (typically from receipt.TransactionIndex
// Essentially loops through and calculates the index if we ignore cosmos txs
func GetEvmTxIndex(txs tmtypes.Txs, txIndex uint32, decoder sdk.TxDecoder) (index int, found bool) {
	evmTxIndex := 0
	foundTx := false
	for i, tx := range txs {
		etx := getEthTxForTxBz(tx, decoder)
		if etx == nil { // cosmos tx, skip
			continue
		}
		if i == int(txIndex) {
			foundTx = true
			break
		}
		evmTxIndex++
	}
	if !foundTx {
		return -1, false
	} else {
		return evmTxIndex, true

	}
}

func encodeReceipt(receipt *types.Receipt, decoder sdk.TxDecoder, block *coretypes.ResultBlock) (map[string]interface{}, error) {
	blockHash := block.BlockID.Hash
	bh := common.HexToHash(blockHash.String())
	logs := keeper.GetLogsForTx(receipt)
	for _, log := range logs {
		log.BlockHash = bh
	}
	evmTxIndex, foundTx := GetEvmTxIndex(block.Block.Txs, receipt.TransactionIndex, decoder)
	// convert tx index including cosmos txs to tx index excluding cosmos txs
	if !foundTx {
		return nil, errors.New("failed to find transaction in block")
	}
	bloom := ethtypes.Bloom{}
	bloom.SetBytes(receipt.LogsBloom)

	fields := map[string]interface{}{
		"blockHash":         bh,
		"blockNumber":       hexutil.Uint64(receipt.BlockNumber),
		"transactionHash":   common.HexToHash(receipt.TxHashHex),
		"transactionIndex":  hexutil.Uint64(evmTxIndex),
		"from":              common.HexToAddress(receipt.From),
		"gasUsed":           hexutil.Uint64(receipt.GasUsed),
		"cumulativeGasUsed": hexutil.Uint64(receipt.CumulativeGasUsed),
		"logs":              logs,
		"logsBloom":         bloom,
		"type":              hexutil.Uint(receipt.TxType),
		"effectiveGasPrice": (*hexutil.Big)(big.NewInt(int64(receipt.EffectiveGasPrice))),
		"status":            hexutil.Uint(receipt.Status),
	}
	if receipt.ContractAddress != "" && receipt.To == "" {
		fields["contractAddress"] = common.HexToAddress(receipt.ContractAddress)
	} else {
		fields["contractAddress"] = nil
	}
	if receipt.To != "" {
		fields["to"] = common.HexToAddress(receipt.To)
	}
	return fields, nil
}
