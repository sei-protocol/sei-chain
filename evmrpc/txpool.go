package evmrpc

import (
	"context"
	"math/big"
	"strconv"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type TxPoolAPI struct {
	tmClient       rpcclient.Client
	keeper         *keeper.Keeper
	ctxProvider    func(int64) sdk.Context
	txDecoder      sdk.TxDecoder
	txPoolConfig   *TxPoolConfig
	connectionType ConnectionType
}

type TxPoolConfig struct {
	maxNumTxs int
}

func NewTxPoolAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder, txPoolConfig *TxPoolConfig, connectionType ConnectionType) *TxPoolAPI {
	return &TxPoolAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder, txPoolConfig: txPoolConfig, connectionType: connectionType}
}

// For now, we put all unconfirmed txs in pending and none in queued
func (t *TxPoolAPI) Content(ctx context.Context) (result map[string]map[string]map[string]*export.RPCTransaction, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("sei_content", t.connectionType, startTime, returnErr == nil)
	content := map[string]map[string]map[string]*export.RPCTransaction{
		"pending": make(map[string]map[string]*export.RPCTransaction),
		"queued":  make(map[string]map[string]*export.RPCTransaction),
	}

	total := t.txPoolConfig.maxNumTxs
	resUnconfirmedTxs, err := t.tmClient.UnconfirmedTxs(ctx, nil, &total)
	if err != nil {
		return nil, err
	}

	sdkCtx := t.ctxProvider(LatestCtxHeight)
	signer := ethtypes.MakeSigner(
		types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(sdkCtx)),
		big.NewInt(sdkCtx.BlockHeight()),
		uint64(sdkCtx.BlockTime().Unix()),
	)

	for _, tx := range resUnconfirmedTxs.Txs {
		ethTx := getEthTxForTxBz(tx, t.txDecoder)
		if ethTx == nil { // not an evm tx
			continue
		}
		fromAddr, err := ethtypes.Sender(signer, ethTx)
		if err != nil {
			return nil, err
		}

		nonce := ethTx.Nonce()
		chainConfig := types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(sdkCtx))
		res := export.NewRPCPendingTransaction(ethTx, nil, chainConfig)
		nonceStr := strconv.FormatUint(nonce, 10)
		if content["pending"][fromAddr.String()] == nil {
			content["pending"][fromAddr.String()] = map[string]*export.RPCTransaction{
				nonceStr: res,
			}
		} else {
			content["pending"][fromAddr.String()][nonceStr] = res
		}
	}
	return content, nil
}
