package parquet_v2

import (
	"context"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
)

// txHashIndexPruner runs periodic pruning on a TxHashIndex, aligned with
// parquet file pruning. Mirrors the package-private pruner in the v1 receipt
// package; duplicated here because Go doesn't share unexported symbols across
// sibling packages.
type txHashIndexPruner struct {
	index       receipt.TxHashIndex
	interval    time.Duration
	stopCh      chan struct{}
	wg          sync.WaitGroup
	latestBlock func() int64
	keepRecent  int64
}

func newTxHashIndexPruner(index receipt.TxHashIndex, keepRecent, pruneIntervalSec int64, latestBlock func() int64) *txHashIndexPruner {
	return &txHashIndexPruner{
		index:       index,
		interval:    time.Duration(pruneIntervalSec) * time.Second,
		stopCh:      make(chan struct{}),
		latestBlock: latestBlock,
		keepRecent:  keepRecent,
	}
}

func (p *txHashIndexPruner) Start() {
	if p.keepRecent <= 0 || p.interval <= 0 {
		return
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			latest := p.latestBlock()
			pruneBefore := latest - p.keepRecent
			if pruneBefore > 0 {
				if err := p.index.PruneBefore(context.Background(), uint64(pruneBefore)); err != nil {
					logger.Error("failed to prune tx hash index", "err", err)
				}
			}
			select {
			case <-p.stopCh:
				return
			case <-ticker.C:
			}
		}
	}()
}

func (p *txHashIndexPruner) Stop() {
	close(p.stopCh)
	p.wg.Wait()
}
