package rpc

import (
	"context"

	"golang.org/x/sync/semaphore"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
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
	return api.backend.InsertTx(ctx, append(abevm.RawTx(nil), input...))
}
