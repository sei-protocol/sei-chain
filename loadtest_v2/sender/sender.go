package sender

import "github.com/sei-protocol/sei-chain/loadtest_v2/types"

type TxSender interface {
	Send(tx *types.LoadTx) error
}
