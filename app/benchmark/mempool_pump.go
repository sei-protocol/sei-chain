package benchmark

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// StartMempoolPump reads encoded txs from the benchmark proposal channel and submits
// them to the mempool via the in-process Tendermint local client (no HTTP EVM RPC).
func StartMempoolPump(ctx context.Context, m *Manager, lc *local.Local) {
	if m == nil || lc == nil {
		return
	}
	logger.Info("benchmark: starting mempool pump (BroadcastTxAsync)")
	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("benchmark: mempool pump stopped")
				return
			case batch, ok := <-m.ProposalChannel():
				if !ok {
					return
				}
				for _, txb := range batch {
					if _, err := lc.BroadcastTxAsync(ctx, types.Tx(txb)); err != nil {
						logger.Error("benchmark: BroadcastTxAsync failed", "error", err)
					}
				}
			}
		}
	}()
}
