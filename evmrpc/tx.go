package evmrpc

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync"
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
	"github.com/sei-protocol/sei-chain/evmrpc/rpcutils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

var ErrPanicTx = errors.New("transaction is panic tx")

const (
	UnconfirmedTxQueryMaxPage = 20
	UnconfirmedTxQueryPerPage = 30
)

type TransactionAPI struct {
	tmClient           rpcclient.Client
	keeper             *keeper.Keeper
	ctxProvider        func(int64) sdk.Context
	txConfigProvider   func(int64) client.TxConfig
	homeDir            string
	connectionType     ConnectionType
	includeSynthetic   bool
	watermarks         *WatermarkManager
	globalBlockCache   BlockCache
	cacheCreationMutex *sync.Mutex
}

type SeiTransactionAPI struct {
	*TransactionAPI
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error)
}

func NewTransactionAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	homeDir string,
	connectionType ConnectionType,
	watermarks *WatermarkManager,
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
) *TransactionAPI {
	return &TransactionAPI{
		tmClient:           tmClient,
		keeper:             k,
		ctxProvider:        ctxProvider,
		txConfigProvider:   txConfigProvider,
		homeDir:            homeDir,
		connectionType:     connectionType,
		watermarks:         watermarks,
		globalBlockCache:   globalBlockCache,
		cacheCreationMutex: cacheCreationMutex,
	}
}

func NewSeiTransactionAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	homeDir string,
	connectionType ConnectionType,
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error),
	watermarks *WatermarkManager,
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
) *SeiTransactionAPI {
	baseAPI := NewTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, connectionType, watermarks, globalBlockCache, cacheCreationMutex)
	baseAPI.includeSynthetic = true
	return &SeiTransactionAPI{TransactionAPI: baseAPI, isPanicTx: isPanicTx}
}

func (t *SeiTransactionAPI) GetTransactionReceiptExcludeTraceFail(ctx context.Context, hash common.Hash) (map[string]interface{}, error) {
	return getTransactionReceipt(ctx, t.TransactionAPI, hash, true, t.isPanicTx, true)
}

func (t *TransactionAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error) {
	return getTransactionReceipt(ctx, t, hash, false, nil, t.includeSynthetic)
}

func getTransactionReceipt(
	ctx context.Context,
	t *TransactionAPI,
	hash common.Hash,
	excludePanicTxs bool,
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error),
	includeSynthetic bool,
) (map[string]interface{}, error) {
	startTime := time.Now()
	var returnErr error
	defer recordMetricsWithError("eth_getTransactionReceipt", t.connectionType, startTime, returnErr)

	sdkctx := t.ctxProvider(LatestCtxHeight)
	if excludePanicTxs {
		isPanic, err := isPanicTx(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("failed to check if tx is panic tx: %w", err)
		}
		if isPanic {
			return nil, ErrPanicTx
		}
	}

	receipt, err := t.keeper.GetReceipt(sdkctx, hash)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}

	// Patch failed + zero gas txs
	if receipt.Status == 0 && receipt.GasUsed == 0 {
		height := int64(receipt.BlockNumber)
		block, err := blockByNumberRespectingWatermarks(ctx, t.tmClient, t.watermarks, &height, 1)
		if err != nil {
			return nil, err
		}

		for _, tx := range block.Block.Txs {
			etx := getEthTxForTxBz(tx, t.txConfigProvider(block.Block.Height).TxDecoder())
			if etx != nil && etx.Hash() == hash {
				from, err := rpcutils.RecoverEVMSender(etx, height, block.Block.Time.Unix())
				if err != nil {
					return nil, err
				}
				receipt.From = from.Hex()
				if etx.To() != nil {
					receipt.To = etx.To().Hex()
					receipt.ContractAddress = ""
				} else {
					receipt.To = ""
					receipt.ContractAddress = crypto.CreateAddress(from, etx.Nonce()).Hex()
				}
				receipt.TxType = uint32(etx.Type())
				receipt.Status = uint32(ethtypes.ReceiptStatusFailed)
				receipt.GasUsed = 0
				break
			}
		}
	}

	height := int64(receipt.BlockNumber)
	block, err := blockByNumberRespectingWatermarks(ctx, t.tmClient, t.watermarks, &height, 1)
	if err != nil {
		return nil, err
	}
	return encodeReceipt(t.ctxProvider, t.txConfigProvider, receipt, t.keeper, block, includeSynthetic, t.globalBlockCache, t.cacheCreationMutex)
}

func (t *TransactionAPI) GetVMError(hash common.Hash) (string, error) {
	startTime := time.Now()
	var returnErr error
	defer recordMetricsWithError("eth_getVMError", t.connectionType, startTime, returnErr)
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		return "", err
	}
	return receipt.VmError, nil
}

func (t *TransactionAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, txIndex hexutil.Uint) (*export.RPCTransaction, error) {
	startTime := time.Now()
	var returnErr error
	defer recordMetricsWithError("eth_getTransactionByBlockNumberAndIndex", t.connectionType, startTime, returnErr)

	idx, err := txIndexToUint32(txIndex)
	if err != nil {
		return nil, err
	}
	return t.getTransactionByBlockNumberAndIndex(ctx, blockNr, idx)
}

func (t *TransactionAPI) getTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, txIndex uint32) (*export.RPCTransaction, error) {
	blockNumber, err := getBlockNumber(ctx, t.tmClient, blockNr)
	if err != nil {
		return nil, err
	}
	block, err := blockByNumberRespectingWatermarks(ctx, t.tmClient, t.watermarks, blockNumber, 1)
	if err != nil {
		return nil, err
	}
	return t.getTransactionWithBlock(block, txIndex, t.includeSynthetic)
}

func (t *TransactionAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, txIndex hexutil.Uint) (*export.RPCTransaction, error) {
	startTime := time.Now()
	var returnErr error
	defer recordMetricsWithError("eth_getTransactionByBlockHashAndIndex", t.connectionType, startTime, returnErr)
	block, err := blockByHashRespectingWatermarks(ctx, t.tmClient, t.watermarks, blockHash[:], 1)
	if err != nil {
		return nil, err
	}
	idx, err := txIndexToUint32(txIndex)
	if err != nil {
		return nil, err
	}
	return t.getTransactionWithBlock(block, idx, t.includeSynthetic)
}

func (t *TransactionAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) (*export.RPCTransaction, error) {
	startTime := time.Now()
	var returnErr error
	defer recordMetricsWithError("eth_getTransactionByHash", t.connectionType, startTime, returnErr)
	sdkCtx := t.ctxProvider(LatestCtxHeight)

	// Mempool scan
	for page := 1; page <= UnconfirmedTxQueryMaxPage; page++ {
		res, err := t.tmClient.UnconfirmedTxs(ctx, &page, nil)
		if err != nil || len(res.Txs) == 0 {
			break
		}
		for _, tx := range res.Txs {
			etx := getEthTxForTxBz(tx, t.txConfigProvider(LatestCtxHeight).TxDecoder())
			if etx != nil && etx.Hash() == hash {
				from, err := rpcutils.RecoverEVMSenderWithContext(sdkCtx, etx)
				if err != nil {
					sdkCtx.Logger().Error("failed to recover sender", "err", err, "tx", etx.Hash().Hex())
					return nil, err
				}
				v, r, s := etx.RawSignatureValues()
				return &export.RPCTransaction{
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
				}, nil
			}
		}
	}

	// From committed blocks
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), hash)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	blockNumber := int64(receipt.BlockNumber)
	block, err := blockByNumberRespectingWatermarks(ctx, t.tmClient, t.watermarks, &blockNumber, 1)
	if err != nil {
		return nil, err
	}
	filteredMsgs := t.getFilteredMsgs(block)
	txIndex, found, ethtx, _ := GetEvmTxIndex(t.ctxProvider(LatestCtxHeight), block, filteredMsgs, receipt.TransactionIndex, t.keeper)
	if !found {
		return nil, nil
	}
	if ethtx == nil {
		return nil, errors.New("transaction is not an EVM transaction and thus cannot be represented")
	}
	return t.encodeRPCTransaction(ethtx, block, uint32(txIndex))
}
