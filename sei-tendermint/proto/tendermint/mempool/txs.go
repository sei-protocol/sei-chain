package mempool

import "errors"

func (txs *Txs) Validate() error {
	protoTxs := txs.GetTxs()
	if len(protoTxs) == 0 {
		return errors.New("empty txs received from peer")
	}
	if len(protoTxs) > 1 {
		return errors.New("right now we only allow 1 tx per envelope")
	}
	return nil
}
