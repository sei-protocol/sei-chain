package evmrpc

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/lib/ethapi"
)

type SeiTransactionAPI struct {
	*TransactionAPI
}

func NewSeiTransactionAPI(t *TransactionAPI) *SeiTransactionAPI {
	return &SeiTransactionAPI{t}
}

func (t *SeiTransactionAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) (result *ethapi.RPCTransaction, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("sei_getTransactionByHash", t.connectionType, startTime, returnErr == nil)
	return t.getTransactionByHash(ctx, hash, true)
}

func (t *SeiTransactionAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("sei_getTransactionReceipt", t.connectionType, startTime, returnErr == nil)
	return t.getTransactionReceipt(ctx, hash)
}
