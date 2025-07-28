package sender

import "github.com/sei-protocol/sei-chain/seiload/types"

type TxSender interface {
	Send(tx *types.LoadTx) error
}
