package evmrpc

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

var ErrPanicTx = errors.New("transaction is panic tx")

const UnconfirmedTxQueryMaxPage = 20
const UnconfirmedTxQueryPerPage = 30

type TransactionAPI struct {
	tmClient         rpcclient.Client
	keeper           *keeper.Keeper
	ctxProvider      func(int64) sdk.Context
	txConfigProvider func(int64) client.TxConfig
	homeDir          string
	connectionType   ConnectionType
	includeSynthetic bool
}

type SeiTransactionAPI struct {
	*TransactionAPI
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error)
}

func NewTransactionAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfigProvider func(int64) client.TxConfig, homeDir string, connectionType ConnectionType) *TransactionAPI {
	return &TransactionAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txConfigProvider: txConfigProvider, homeDir: homeDir, connectionType: connectionType}
}

func NewSeiTransactionAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	homeDir string,
	connectionType ConnectionType,
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error),
) *SeiTransactionAPI {
	baseAPI := NewTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, connectionType)
	baseAPI.includeSynthetic = true
	return &SeiTransactionAPI{TransactionAPI: baseAPI, isPanicTx: isPanicTx}
}

func (t *SeiTransactionAPI) GetTransactionReceiptExcludeTraceFail(ctx context.Context, hash common.Hash) (result map[string]interface{}, returnErr error) {
	sdkCtx := t.ctxProvider(LatestCtxHeight)
	signer := ethtypes.MakeSigner(
		types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(sdkCtx)),
		big.NewInt(sdkCtx.BlockHeight()),
		uint64(sdkCtx.BlockTime().Unix()), //nolint:gosec
	)
	return getTransactionReceipt(ctx, t.TransactionAPI, hash, true, t.isPanicTx, true, signer)
}

func (t *TransactionAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (result map[string]interface{}, returnErr error) {
	sdkCtx := t.ctxProvider(LatestCtxHeight)
	signer := ethtypes.MakeSigner(
		types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(sdkCtx)),
		big.NewInt(sdkCtx.BlockHeight()),
		uint64(sdkCtx.BlockTime().Unix()), //nolint:gosec
	)
	return getTransactionReceipt(ctx, t, hash, false, nil, t.includeSynthetic, signer)
}

func getTransactionReceipt(
	ctx context.Context,
	t *TransactionAPI,
	hash common.Hash,
	excludePanicTxs bool,
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error),
	includeSynthetic bool,
	signer ethtypes.Signer,
) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_getTransactionReceipt", t.connectionType, startTime, returnErr)
	sdkctx := t.ctxProvider(LatestCtxHeight)

	if excludePanicTxs {
		isPanicTx, err := isPanicTx(ctx, hash)
		if isPanicTx {
			return nil, ErrPanicTx
		}
		if err != nil {
			return nil, fmt.Errorf("failed to check if tx is panic tx: %w", err)
		}
	}

	receipt, err := t.keeper.GetReceipt(sdkctx, hash)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			// When the transaction doesn't exist, the RPC method should return JSON null
			// as per specification.
			return nil, nil
		}
		return nil, err
	}
	// Fill in the receipt if the transaction has failed and used 0 gas
	// This case is for when a tx fails before it makes it to the VM
	if receipt.Status == 0 && receipt.GasUsed == 0 {
		// Get the block
		height := int64(receipt.BlockNumber) //nolint:gosec
		block, err := blockByNumberWithRetry(ctx, t.tmClient, &height, 1)
		if err != nil {
			return nil, err
		}

		// Find the transaction in the block
		for _, tx := range block.Block.Txs {
			etx := getEthTxForTxBz(tx, t.txConfigProvider(block.Block.Height).TxDecoder())
			if etx != nil && etx.Hash() == hash {
				// Get the signer
				signer := ethtypes.MakeSigner(
					types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(sdkctx)),
					big.NewInt(height),
					uint64(block.Block.Time.Unix()), //nolint:gosec
				)
				from, _ := ethtypes.Sender(signer, etx)

				// Update receipt with correct information
				receipt.From = from.Hex()
				if etx.To() != nil {
					receipt.To = etx.To().Hex()
					receipt.ContractAddress = ""
				} else {
					receipt.To = ""
					// For contract creation transactions, calculate the contract address
					receipt.ContractAddress = crypto.CreateAddress(from, etx.Nonce()).Hex()
				}
				receipt.TxType = uint32(etx.Type())
				receipt.Status = uint32(ethtypes.ReceiptStatusFailed)
				receipt.GasUsed = 0
				break
			}
		}
	}
	height := int64(receipt.BlockNumber) //nolint:gosec
	block, err := blockByNumberWithRetry(ctx, t.tmClient, &height, 1)
	if err != nil {
		return nil, err
	}
	return encodeReceipt(t.ctxProvider, t.txConfigProvider, receipt, t.keeper, block, includeSynthetic, signer)
}

func (t *TransactionAPI) GetVMError(hash common.Hash) (result string, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_getVMError", t.connectionType, startTime, returnErr)
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		return "", err
	}
	return receipt.VmError, nil
}

func (t *TransactionAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, txIndex hexutil.Uint) (result *export.RPCTransaction, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_getTransactionByBlockNumberAndIndex", t.connectionType, startTime, returnErr)

	var idx uint32
	idx, returnErr = txIndexToUint32(txIndex)
	if returnErr != nil {
		return nil, returnErr
	}
	return t.getTransactionByBlockNumberAndIndex(ctx, blockNr, idx)
}

func (t *TransactionAPI) getTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, txIndex uint32) (result *export.RPCTransaction, returnErr error) {
	blockNumber, err := getBlockNumber(ctx, t.tmClient, blockNr)
	if err != nil {
		return nil, err
	}
	block, err := blockByNumberWithRetry(ctx, t.tmClient, blockNumber, 1)
	if err != nil {
		return nil, err
	}
	return t.getTransactionWithBlock(block, txIndex, t.includeSynthetic)
}

func (t *TransactionAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, txIndex hexutil.Uint) (result *export.RPCTransaction, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_getTransactionByBlockHashAndIndex", t.connectionType, startTime, returnErr)
	block, err := blockByHash(ctx, t.tmClient, blockHash[:])
	if err != nil {
		return nil, err
	}
	var idx uint32
	idx, returnErr = txIndexToUint32(txIndex)
	if returnErr != nil {
		return nil, returnErr
	}
	return t.getTransactionWithBlock(block, idx, t.includeSynthetic)
}

func (t *TransactionAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) (result *export.RPCTransaction, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_getTransactionByHash", t.connectionType, startTime, returnErr)
	sdkCtx := t.ctxProvider(LatestCtxHeight)
	// first try get from mempool
	for page := 1; page <= UnconfirmedTxQueryMaxPage; page++ {
		res, err := t.tmClient.UnconfirmedTxs(ctx, &page, nil)
		if err != nil || len(res.Txs) == 0 {
			break
		}
		for _, tx := range res.Txs {
			etx := getEthTxForTxBz(tx, t.txConfigProvider(LatestCtxHeight).TxDecoder())
			if etx != nil && etx.Hash() == hash {
				signer := ethtypes.MakeSigner(
					types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(sdkCtx)),
					big.NewInt(sdkCtx.BlockHeight()),
					uint64(sdkCtx.BlockTime().Unix()), //nolint:gosec
				)
				from, _ := ethtypes.Sender(signer, etx)
				v, r, s := etx.RawSignatureValues()
				res := export.RPCTransaction{
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
	blockNumber := int64(receipt.BlockNumber) //nolint:gosec
	block, err := blockByNumberWithRetry(ctx, t.tmClient, &blockNumber, 1)
	if err != nil {
		return nil, err
	}
	filteredMsgs := t.getFilteredMsgs(block)
	txIndex, found, ethtx, _ := GetEvmTxIndex(t.ctxProvider(LatestCtxHeight), block, filteredMsgs, receipt.TransactionIndex, t.keeper)
	if !found {
		return nil, nil
	}
	if ethtx == nil {
		return nil, errors.New("transaction is not an EVM transaction and thus cannot be represented in _getTransaction* endpoints")
	}
	return t.encodeRPCTransaction(ethtx, block, uint32(txIndex)) //nolint:gosec
}

func (t *TransactionAPI) GetTransactionErrorByHash(_ context.Context, hash common.Hash) (result string, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_getTransactionErrorByHash", t.connectionType, startTime, returnErr)
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
	defer recordMetricsWithError("eth_getTransactionCount", t.connectionType, startTime, returnErr)
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

func (t *TransactionAPI) getTransactionWithBlock(block *coretypes.ResultBlock, txIndex uint32, includeSynthetic bool) (*export.RPCTransaction, error) {
	ctx := t.ctxProvider(block.Block.Height)
	signer := ethtypes.MakeSigner(
		types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(ctx)),
		big.NewInt(ctx.BlockHeight()),
		uint64(ctx.BlockTime().Unix()), //nolint:gosec
	)
	msgs := filterTransactions(t.keeper, t.ctxProvider, t.txConfigProvider, block, signer, includeSynthetic, false)
	if txIndex >= uint32(len(msgs)) { //nolint:gosec
		return nil, errors.New("transaction index out of range")
	}
	msg := msgs[txIndex]
	evmTx, ok := msg.msg.(*types.MsgEVMTransaction)
	if !ok {
		return nil, errors.New("transaction is not an EVM transaction and thus cannot be represented in _getTransaction* endpoints")
	}
	ethtx, _ := evmTx.AsTransaction()

	return t.encodeRPCTransaction(ethtx, block, txIndex)
}

func (t *TransactionAPI) encodeRPCTransaction(ethtx *ethtypes.Transaction, block *coretypes.ResultBlock, txIndex uint32) (*export.RPCTransaction, error) {
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), ethtx.Hash())
	if err != nil {
		return nil, err
	}
	height := int64(receipt.BlockNumber) // nolint:gosec
	var baseFeePerGas *big.Int
	if block.Block.Height > 1 {
		baseFeePerGas = t.keeper.GetNextBaseFeePerGas(t.ctxProvider(height - 1)).TruncateInt().BigInt()
	} else {
		baseFeePerGas = types.DefaultMinFeePerGas.TruncateInt().BigInt()
	}
	chainConfig := types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(t.ctxProvider(height)))
	blockHash := common.HexToHash(block.BlockID.Hash.String())
	blockNumber := uint64(block.Block.Height) //nolint:gosec
	blockTime := block.Block.Time
	res := export.NewRPCTransaction(ethtx, blockHash, blockNumber, uint64(blockTime.Second()), uint64(txIndex), baseFeePerGas, chainConfig) //nolint:gosec
	return res, nil
}

func (t *TransactionAPI) Sign(addr common.Address, data hexutil.Bytes) (result hexutil.Bytes, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_sign", t.connectionType, startTime, returnErr)
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

func (t *TransactionAPI) getFilteredMsgs(block *coretypes.ResultBlock) []indexedMsg {
	ctx := t.ctxProvider(block.Block.Height)
	signer := ethtypes.MakeSigner(
		types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(ctx)),
		big.NewInt(ctx.BlockHeight()),
		uint64(ctx.BlockTime().Unix()), //nolint:gosec
	)
	return filterTransactions(t.keeper, t.ctxProvider, t.txConfigProvider, block, signer, t.includeSynthetic, false)
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

// receipt.TransactionIndex represents the index of the transaction among ALL transactions in the block.
// This function returns the index if irrelevant transactions are excluded.
// Specifically, if includeSynthetic is false, all Cosmos transactions are excluded. If includeSynthetic is true,
// Cosmos transactions without a receipt (i.e. Cosmos transactions that don't touch CW20/721/1155) are excluded.
// It also returns the log index offset, which always includes all logs of relevant transactions, regardless of
// whether logs themselves are synthetic or not.
func GetEvmTxIndex(ctx sdk.Context, block *coretypes.ResultBlock, msgs []indexedMsg, txIndex uint32, k *keeper.Keeper) (index int, found bool, etx *ethtypes.Transaction, logIndexOffset int) {
	var evmTxIndex, logIndex int
	for _, msg := range msgs {
		var txHash common.Hash
		switch m := msg.msg.(type) {
		case *types.MsgEVMTransaction:
			etx, _ = m.AsTransaction()
			txHash = etx.Hash()
		case *wasmtypes.MsgExecuteContract:
			etx = nil
			txHash = common.Hash(sha256.Sum256(block.Block.Txs[msg.index]))
		}
		receipt, err := k.GetReceipt(ctx, txHash)
		if err != nil {
			continue
		}
		if msg.index == int(txIndex) {
			return evmTxIndex, true, etx, logIndex
		}

		evmTxIndex++
		logIndex += len(receipt.Logs)
	}
	return -1, false, nil, -1
}

func encodeReceipt(ctxProvider func(int64) sdk.Context, txConfigProvider func(int64) client.TxConfig, receipt *types.Receipt, k *keeper.Keeper, block *coretypes.ResultBlock, includeSynthetic bool, signer ethtypes.Signer) (map[string]interface{}, error) {
	blockHash := block.BlockID.Hash
	bh := common.HexToHash(blockHash.String())
	ctx := ctxProvider(block.Block.Height)
	msgs := filterTransactions(k, ctxProvider, txConfigProvider, block, signer, includeSynthetic, false)
	evmTxIndex, foundTx, etx, logIndexOffset := GetEvmTxIndex(ctx, block, msgs, receipt.TransactionIndex, k)
	// convert tx index including cosmos txs to tx index excluding cosmos txs
	if !foundTx {
		return nil, errors.New("failed to find transaction in block")
	}
	receipt.TransactionIndex = uint32(evmTxIndex)              //nolint:gosec
	logs := keeper.GetLogsForTx(receipt, uint(logIndexOffset)) //nolint:gosec
	for _, log := range logs {
		log.BlockHash = bh
	}
	bloom := ethtypes.Bloom{}
	bloom.SetBytes(receipt.LogsBloom)
	fields := map[string]interface{}{
		"blockHash":         bh,
		"blockNumber":       hexutil.Uint64(receipt.BlockNumber),
		"transactionHash":   common.HexToHash(receipt.TxHashHex),
		"transactionIndex":  hexutil.Uint64(evmTxIndex), //nolint:gosec
		"from":              common.HexToAddress(receipt.From),
		"gasUsed":           hexutil.Uint64(receipt.GasUsed),
		"cumulativeGasUsed": hexutil.Uint64(receipt.CumulativeGasUsed),
		"logs":              logs,
		"logsBloom":         bloom,
		"type":              hexutil.Uint(receipt.TxType),
		"effectiveGasPrice": (*hexutil.Big)(big.NewInt(int64(receipt.EffectiveGasPrice))), // nolint:gosec
		"status":            hexutil.Uint(receipt.Status),
	}
	if etx != nil && receipt.From == "" {
		from, err := ethtypes.Sender(signer, etx)
		if err == nil {
			fields["from"] = from
		}
		to := etx.To()
		if to != nil {
			fields["to"] = *to
		}
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

func txIndexToUint32(txIndex hexutil.Uint) (uint32, error) {
	if txIndex > math.MaxUint32 {
		return 0, errors.New("invalid tx index")
	}
	return uint32(txIndex), nil //nolint:gosec
}
