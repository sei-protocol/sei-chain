package threading

import "sync/atomic"

var _ Pool = (*shardedPool)(nil)

// shardedPool is a set of fixed pools that Submit spreads tasks across in turn, so concurrent
// submitters contend on one of several queues rather than all on one.
type shardedPool struct {
	shards []Pool
	next   atomic.Uint64
}

// NewShardedFixedPool creates a pool of workers split into queues of about workersPerQueue workers
// each, the total queue capacity split between them.
func NewShardedFixedPool(
	// The name of the work pool. Used for metrics.
	name string,
	// The number of workers to create.
	workers int,
	// The total size of the work queues. Once the queue a task lands on is full, Submit blocks.
	queueSize int,
	// The number of workers sharing each queue.
	workersPerQueue int,
) Pool {
	workers = max(workers, 1)
	queues := max(workers/max(workersPerQueue, 1), 1)
	p := &shardedPool{shards: make([]Pool, queues)}
	for i := range p.shards {
		shardWorkers := workers / queues
		if i < workers%queues {
			shardWorkers++
		}
		p.shards[i] = NewFixedPool(name, shardWorkers, max(queueSize/queues, 1))
	}
	return p
}

func (p *shardedPool) Submit(task func()) {
	if task == nil {
		return
	}
	p.shards[p.next.Add(1)%uint64(len(p.shards))].Submit(task)
}

func (p *shardedPool) Close() {
	for _, shard := range p.shards {
		shard.Close()
	}
}
