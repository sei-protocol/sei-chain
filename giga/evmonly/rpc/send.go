package rpc

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/semaphore"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// limitExceededError reports a request exceeding an RPC resource limit.
type limitExceededError struct{}

func (limitExceededError) ErrorCode() int { return -32005 }
func (limitExceededError) Error() string  { return "server busy" }

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

// SendRawTransaction submits a signed raw Ethereum transaction to Autobahn and
// returns its Ethereum transaction hash.
func (api *sendAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	if !api.requestLimiter.TryAcquire(1) {
		return common.Hash{}, limitExceededError{}
	}
	defer api.requestLimiter.Release(1)
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	hash := tx.Hash()

	if client, ok := api.shardProxy(tx).Get(); ok {
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

// broadcast submits input to the local mempool, returning a CheckTx rejection as an error.
func (api *sendAPI) broadcast(ctx context.Context, input hexutil.Bytes) error {
	result, err := api.backend.BroadcastTx(ctx, &coretypes.RequestBroadcastTx{
		Tx: append(tmtypes.Tx(nil), input...),
	})
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("missing broadcast response")
	}
	if result.Code != abci.CodeTypeOK {
		message := result.Log
		if message == "" {
			message = fmt.Sprintf("transaction rejected with code %d", result.Code)
		}
		return errors.New(message)
	}
	return nil
}

// shardProxy returns the RPC client of the validator owning tx's sender shard,
// or None when the transaction is handled locally. Sender recovery is skipped
// entirely when the backend has no proxies, since CheckTx recovers it anyway. A
// sender recovered for a transaction handled locally is handed to the backend,
// so CheckTx need not recover it again.
func (api *sendAPI) shardProxy(tx *ethtypes.Transaction) utils.Option[*ethrpc.Client] {
	if !api.backend.EvmProxyEnabled() {
		return utils.None[*ethrpc.Client]()
	}
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		return utils.None[*ethrpc.Client]()
	}
	proxy := api.backend.EvmProxy(sender)
	if !proxy.IsPresent() {
		api.backend.RememberEvmSender(tx.Hash(), sender)
	}
	return proxy
}
