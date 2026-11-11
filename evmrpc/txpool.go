package evmrpc

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/export"
	"github.com/sei-protocol/sei-chain/evmrpc/rpcutils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// txPoolPageSize is the number of mempool txs requested per page, the most the
// Tendermint RPC returns in one page outside unsafe mode.
const txPoolPageSize = 100

type TxPoolAPI struct {
	tmClient         client.LocalClient
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

func NewTxPoolAPI(tmClient client.LocalClient, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfigProvider func(int64) client.TxConfig, txPoolConfig *TxPoolConfig, connectionType ConnectionType) *TxPoolAPI {
	return &TxPoolAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txConfigProvider: txConfigProvider, txPoolConfig: txPoolConfig, connectionType: connectionType}
}

// Content returns the content of the txpool.
// for now, we put all unconfirmed txs in pending and none in queued.
func (t *TxPoolAPI) Content(ctx context.Context) (result map[string]map[string]map[string]*export.RPCTransaction, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "sei_content", t.connectionType, startTime, returnErr, recover())
	}()
	content := map[string]map[string]map[string]*export.RPCTransaction{
		"pending": make(map[string]map[string]*export.RPCTransaction),
		"queued":  make(map[string]map[string]*export.RPCTransaction),
	}

	txs, err := t.loadUnconfirmedTxs(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx := t.ctxProvider(LatestCtxHeight)
	decoder := t.txConfigProvider(LatestCtxHeight).TxDecoder()
	chainConfig := types.DefaultChainConfig().EthereumConfig(t.keeper.ChainID(sdkCtx))
	for _, tx := range txs {
		ethTx := getEthTxForTxBz(tx, decoder)
		if ethTx == nil { // not an evm tx
			continue
		}
		fromAddr, err := rpcutils.RecoverEVMSenderWithContext(sdkCtx, ethTx)
		if err != nil {
			logger.Warn("txpool_content: skipping tx whose sender cannot be recovered", "hash", ethTx.Hash().Hex(), "err", err)
			continue
		}
		nonce := ethTx.Nonce()
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

// loadUnconfirmedTxs returns up to maxNumTxs distinct mempool txs, Cosmos txs
// included, in the order the mempool lists them. A block landing between pages
// can shift or remove txs, so the result is a best-effort view of the pool.
func (t *TxPoolAPI) loadUnconfirmedTxs(ctx context.Context) (tmtypes.Txs, error) {
	limit := t.txPoolConfig.maxNumTxs
	if limit <= 0 {
		return nil, nil
	}
	perPage := min(limit, txPoolPageSize)
	var txs tmtypes.Txs
	seen := make(map[tmtypes.TxHash]struct{})
	listed := 0
	for page := 1; len(txs) < limit; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		res, err := t.tmClient.UnconfirmedTxs(ctx, &page, &perPage)
		if page > 1 && errors.Is(err, coretypes.ErrPageOutOfRange) {
			// The pool shrank below this page since the previous one was read.
			break
		}
		if err != nil {
			return nil, err
		}
		for _, tx := range res.Txs {
			if len(txs) == limit {
				break
			}
			// A tx moved across a page boundary between reads is listed twice.
			hash := tx.Hash()
			if _, ok := seen[hash]; ok {
				continue
			}
			seen[hash] = struct{}{}
			txs = append(txs, tx)
		}
		listed += len(res.Txs)
		if len(res.Txs) == 0 || listed >= res.Total {
			break
		}
	}
	return txs, nil
}
