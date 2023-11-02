package query

import (
	"context"

	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	"github.com/sei-protocol/sei-chain/tools/tx-scanner/client"
)

// GetLatestBlock query the latest block data
func GetLatestBlock() (*tmservice.GetLatestBlockResponse, error) {
	request := &tmservice.GetLatestBlockRequest{}
	return client.GetTmServiceClient().GetLatestBlock(context.Background(), request)
}

// GetBlockByHeight query the block data at height
func GetBlockByHeight(height int64) (*tmservice.GetBlockByHeightResponse, error) {
	request := &tmservice.GetBlockByHeightRequest{
		Height: height,
	}
	return client.GetTmServiceClient().GetBlockByHeight(context.Background(), request)
}
