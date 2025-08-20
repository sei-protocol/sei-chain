package sender

import "seiload/types"

type TxSender interface {
	Send(tx *types.LoadTx) error
}
