package evmrpc

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
)

type SeiBlockAPI struct {
	*BlockAPI
}

func NewSeiBlockAPI(blockAPI *BlockAPI) *SeiBlockAPI {
	return &SeiBlockAPI{blockAPI}
}

func (a *SeiBlockAPI) GetBlockByHash(ctx context.Context, blockHash common.Hash, fullTx bool) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("sei_getBlockByHash", a.connectionType, startTime, returnErr == nil)
	return a.getBlockByHash(ctx, blockHash, fullTx, true)
}

func (a *SeiBlockAPI) GetBlockByNumber(ctx context.Context, number rpc.BlockNumber, fullTx bool) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("sei_getBlockByNumber", a.connectionType, startTime, returnErr == nil)
	return a.getBlockByNumber(ctx, number, fullTx, true)
}
