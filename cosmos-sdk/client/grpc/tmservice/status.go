package tmservice

import (
	"context"

	ctypes "github.com/sei-protocol/sei-chain/tendermint/rpc/coretypes"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/client"
)

func getNodeStatus(ctx context.Context, clientCtx client.Context) (*ctypes.ResultStatus, error) {
	node, err := clientCtx.GetNode()
	if err != nil {
		return &ctypes.ResultStatus{}, err
	}
	return node.Status(ctx)
}
