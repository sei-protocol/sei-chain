package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"golang.org/x/sync/semaphore"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/rpcerr"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type sendAPI struct {
	backend        Backend
	requestLimiter *semaphore.Weighted
}

func newSendAPI(backend Backend, maxConcurrentSendCalls int) *sendAPI {
	return &sendAPI{
		backend:        backend,
		requestLimiter: semaphore.NewWeighted(int64(maxConcurrentSendCalls)),
	}
}

var (
	// errUnprotectedTx rejects a transaction that is not replay-protected (EIP-155).
	errUnprotectedTx = errors.New("only replay-protected (EIP-155) transactions allowed over RPC")
	// errInvalidChainID rejects a transaction signed for another chain.
	errInvalidChainID = errors.New("invalid chain-id")
	// errSendBusy refuses a transaction while every send slot is taken.
	errSendBusy = errors.New(rpcerr.MsgServerBusy)
)

// SendRawTransaction submits a signed raw Ethereum transaction to Autobahn and
// returns its Ethereum transaction hash.
func (api *sendAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	if !api.requestLimiter.TryAcquire(1) {
		return common.Hash{}, rpcerr.LimitExceeded(errSendBusy)
	}
	defer api.requestLimiter.Release(1)
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	chainID := new(big.Int).SetUint64(api.backend.EvmChainID())
	if err := checkChainID(tx, chainID); err != nil {
		return common.Hash{}, err
	}
	hash := tx.Hash()

	if client, ok := api.shardProxy(tx, chainID).Get(); ok {
		if err := client.CallContext(ctx, &hash, "eth_sendRawTransaction", input); err != nil {
			return hash, err
		}
		return hash, nil
	}

	if err := api.broadcast(ctx, input); err != nil {
		api.backend.ForgetEvmSender(hash)
		return hash, err
	}
	return hash, nil
}

// checkChainID returns a transaction rejected error unless tx is replay-protected and signed for
// chainID.
func checkChainID(tx *ethtypes.Transaction, chainID *big.Int) error {
	if !tx.Protected() {
		return rpcerr.TransactionRejected(errUnprotectedTx)
	}
	if tx.ChainId().Sign() == 0 || tx.ChainId().Cmp(chainID) != 0 {
		return rpcerr.TransactionRejected(errInvalidChainID)
	}
	return nil
}

// broadcast submits input to the local mempool, returning its refusal of the transaction as the
// JSON-RPC error a client receives.
func (api *sendAPI) broadcast(ctx context.Context, input hexutil.Bytes) error {
	result, err := api.backend.BroadcastTx(ctx, &coretypes.RequestBroadcastTx{
		Tx: append(tmtypes.Tx(nil), input...),
	})
	if err != nil {
		return mempoolError(err)
	}
	if result == nil {
		return errors.New("missing broadcast response")
	}
	if result.Code != abci.CodeTypeOK {
		message := result.Log
		if message == "" {
			message = fmt.Sprintf("transaction rejected with code %d", result.Code)
		}
		return rpcerr.TransactionRejected(errors.New(message))
	}
	return nil
}

// mempoolError returns the JSON-RPC error a client receives for err, the local mempool's refusal of
// a transaction: a transaction rejected error when the mempool refused the transaction itself, and
// a resource unavailable error while the validator is not producing. Any other failure, a deadline
// included, is returned as it is.
func mempoolError(err error) error {
	switch {
	case errors.Is(err, abevm.ErrTxAlreadyKnown),
		errors.Is(err, abevm.ErrTxBadNonce),
		errors.Is(err, abevm.ErrTxTooLarge):
		return rpcerr.TransactionRejected(err)
	case errors.Is(err, abevm.ErrNotProducing):
		return rpcerr.ResourceUnavailable(err)
	}
	return err
}

// shardProxy returns the RPC client of the validator owning tx's sender shard,
// or None when the transaction is handled locally. Sender recovery is skipped
// entirely when the backend has no proxies, since CheckTx recovers it anyway. A
// sender recovered for a transaction handled locally is handed to the backend,
// so CheckTx need not recover it again.
func (api *sendAPI) shardProxy(tx *ethtypes.Transaction, chainID *big.Int) utils.Option[*ethrpc.Client] {
	if !api.backend.EvmProxyEnabled() {
		return utils.None[*ethrpc.Client]()
	}
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(chainID), tx)
	if err != nil {
		return utils.None[*ethrpc.Client]()
	}
	proxy := api.backend.EvmProxy(sender)
	if !proxy.IsPresent() {
		api.backend.RememberEvmSender(tx.Hash(), sender)
	}
	return proxy
}
