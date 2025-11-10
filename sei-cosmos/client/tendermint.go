package client

import (
	"context"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

// TendermintRPC defines the interface of a Tendermint RPC client needed for
// queries and transaction handling.
type TendermintRPC interface {
	rpcclient.ABCIClient

	Validators(ctx context.Context, height *int64, page, perPage *int) (*coretypes.ResultValidators, error)
	Status(context.Context) (*coretypes.ResultStatus, error)
	Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error)
	BlockchainInfo(ctx context.Context, minHeight, maxHeight int64) (*coretypes.ResultBlockchainInfo, error)
	Commit(ctx context.Context, height *int64) (*coretypes.ResultCommit, error)
	TxSearch(
		ctx context.Context,
		query string,
		prove bool,
		page, perPage *int,
		orderBy string,
	) (*coretypes.ResultTxSearch, error)
}
