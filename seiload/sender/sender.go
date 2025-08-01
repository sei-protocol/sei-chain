package sender

import (
	"context"
	"github.com/sei-protocol/sei-chain/seiload/types"
)

type TxSender interface {
	Send(ctx context.Context, tx *types.LoadTx) error
}
