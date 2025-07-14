package evmrpc

import (
	"context"
	"math/big"
	"strconv"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/lib/ethapi"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
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

func NewTxPoolAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfigProvider func(int64) client.TxConfig, txPoolConfig *TxPoolConfig, connectionType ConnectionType) *TxPoolAPI {
	return &TxPoolAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txConfigProvider: txConfigProvider, txPoolConfig: txPoolConfig, connectionType: connectionType}
}

// For now, we put all unconfirmed txs in pending and none in queued
func (t *TxPoolAPI) Content(ctx context.Context) (result map[string]map[string]map[string]*ethapi.RPCTransaction, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("sei_content", t.connectionType, startTime, returnErr == nil)
	content := map[string]map[string]map[string]*ethapi.RPCTransaction{
		"pending": make(map[string]map[string]*ethapi.RPCTransaction),
		"queued":  make(map[string]map[string]*ethapi.RPCTransaction),
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
		ethTx := getEthTxForTxBz(tx, t.txConfigProvider(LatestCtxHeight).TxDecoder())
		if ethTx == nil { // not an evm tx
			continue
		}
		fromAddr, err := ethtypes.Sender(signer, ethTx)
		if err != nil {
			return nil, err
		}

		nonce := ethTx.Nonce()
		chainConfig := types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(sdkCtx))
		res := ethapi.NewRPCPendingTransaction(ethTx, nil, chainConfig)
		nonceStr := strconv.FormatUint(nonce, 10)
		if content["pending"][fromAddr.String()] == nil {
			content["pending"][fromAddr.String()] = map[string]*ethapi.RPCTransaction{
				nonceStr: res,
			}
		} else {
			content["pending"][fromAddr.String()][nonceStr] = res
		}
	}
	return content, nil
}
