package tmservice

import (
	"context"

	ctypes "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
)

func getNodeStatus(ctx context.Context, clientCtx client.Context) (*ctypes.ResultStatus, error) {
	node, err := clientCtx.GetNode()
	if err != nil {
		return &ctypes.ResultStatus{}, err
	}
	return node.Status(ctx)
}
