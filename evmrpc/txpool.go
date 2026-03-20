package evmrpc

import (
	"context"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/export"
	"github.com/sei-protocol/sei-chain/evmrpc/rpcutils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type TxPoolAPI struct {
	tmClient         rpcclient.Client
	keeper           *keeper.Keeper
	ctxProvider      func(int64) sdk.Context
	txConfigProvider func(int64) client.TxConfig
	txPoolConfig     *TxPoolConfig
	connectionType   ConnectionType
}

type TxPoolConfig struct {
	maxNumTxs int
}

// NewTxPoolConfig creates a new TxPoolConfig primarily for tests.
func NewTxPoolConfig(maxNumTxs int) *TxPoolConfig {
	return &TxPoolConfig{maxNumTxs: maxNumTxs}
}

func NewTxPoolAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfigProvider func(int64) client.TxConfig, txPoolConfig *TxPoolConfig, connectionType ConnectionType) *TxPoolAPI {
	return &TxPoolAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txConfigProvider: txConfigProvider, txPoolConfig: txPoolConfig, connectionType: connectionType}
}

// Content returns the content of the txpool.
// for now, we put all unconfirmed txs in pending and none in queued.
func (t *TxPoolAPI) Content(ctx context.Context) (result map[string]map[string]map[string]*export.RPCTransaction, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("sei_content", t.connectionType, startTime, returnErr)
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
	for _, tx := range resUnconfirmedTxs.Txs {
		ethTx := getEthTxForTxBz(tx, t.txConfigProvider(LatestCtxHeight).TxDecoder())
		if ethTx == nil { // not an evm tx
			continue
		}
		fromAddr, err := rpcutils.RecoverEVMSenderWithContext(sdkCtx, ethTx)
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
