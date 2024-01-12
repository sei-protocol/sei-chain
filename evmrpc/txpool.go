package evmrpc

import (
	"context"
	"math/big"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type TxPoolAPI struct {
	tmClient     rpcclient.Client
	keeper       *keeper.Keeper
	ctxProvider  func(int64) sdk.Context
	txDecoder    sdk.TxDecoder
	txPoolConfig *TxPoolConfig
}

type TxPoolConfig struct {
	maxNumTxs int
}

func NewTxPoolAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder, txPoolConfig *TxPoolConfig) *TxPoolAPI {
	return &TxPoolAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder, txPoolConfig: txPoolConfig}
}

// For now, we put all unconfirmed txs in pending and none in queued
func (t *TxPoolAPI) Content(ctx context.Context) map[string]map[string]map[string]*RPCTransaction {
	content := map[string]map[string]map[string]*RPCTransaction{
		"pending": make(map[string]map[string]*RPCTransaction),
		"queued":  make(map[string]map[string]*RPCTransaction),
	}

	total := t.txPoolConfig.maxNumTxs
	resUnconfirmedTxs, err := t.tmClient.UnconfirmedTxs(ctx, nil, &total)
	if err != nil {
		return nil
	}

	sdkCtx := t.ctxProvider(LatestCtxHeight)
	signer := ethtypes.MakeSigner(
		t.keeper.GetChainConfig(sdkCtx).EthereumConfig(t.keeper.ChainID(sdkCtx)),
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
			return nil
		}

		nonce := ethTx.Nonce()
		res := hydratePendingTransaction(ethTx)
		nonceStr := strconv.FormatUint(nonce, 10)
		if content["pending"][fromAddr.String()] == nil {
			content["pending"][fromAddr.String()] = map[string]*RPCTransaction{
				nonceStr: &res,
			}
		} else {
			content["pending"][fromAddr.String()][nonceStr] = &res
		}
	}
	return content
}
