package p2p

import (
	"context"
	"sync"

	"github.com/ethereum/go-ethereum/common"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// laneSenderStoreCap is how many lane blocks one generation of a laneSenderStore holds. It is
// far above the blocks every lane can have in flight, so an entry outlives the wait for execution.
const laneSenderStoreCap = 4096

// laneSenderWorkers is how many lane blocks recover their senders at once.
const laneSenderWorkers = 2

// laneSenders is the senders of one lane block, aligned with its transactions. done closes once
// senders is set.
type laneSenders struct {
	done    chan struct{}
	senders []utils.Option[common.Address]
}

type laneSenderJob struct {
	entry *laneSenders
	txs   [][]byte
}

// laneSenderStore recovers the senders of lane blocks as they arrive and keeps them, keyed by block
// header hash, until the superblock containing the block takes them.
type laneSenderStore struct {
	mu           sync.Mutex
	cond         *sync.Cond
	fresh, stale map[atypes.BlockHeaderHash]*laneSenders
	queue        []laneSenderJob
}

func newLaneSenderStore() *laneSenderStore {
	s := &laneSenderStore{fresh: map[atypes.BlockHeaderHash]*laneSenders{}}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// add queues the block's transactions for sender recovery. A block already added is ignored.
func (s *laneSenderStore) add(hash atypes.BlockHeaderHash, txs [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.fresh[hash]; ok {
		return
	}
	if _, ok := s.stale[hash]; ok {
		return
	}
	if len(s.fresh) >= laneSenderStoreCap {
		s.stale, s.fresh = s.fresh, make(map[atypes.BlockHeaderHash]*laneSenders, len(s.fresh))
	}
	entry := &laneSenders{done: make(chan struct{})}
	s.fresh[hash] = entry
	s.queue = append(s.queue, laneSenderJob{entry: entry, txs: txs})
	s.cond.Signal()
}

// take removes and returns the entry for the block, if it was added.
func (s *laneSenderStore) take(hash atypes.BlockHeaderHash) (*laneSenders, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, gen := range [...]map[atypes.BlockHeaderHash]*laneSenders{s.fresh, s.stale} {
		if entry, ok := gen[hash]; ok {
			delete(gen, hash)
			return entry, true
		}
	}
	return nil, false
}

// next blocks until a job is queued or ctx is done.
func (s *laneSenderStore) next(ctx context.Context) (laneSenderJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.queue) == 0 {
		if ctx.Err() != nil {
			return laneSenderJob{}, false
		}
		s.cond.Wait()
	}
	job := s.queue[0]
	s.queue[0] = laneSenderJob{}
	s.queue = s.queue[1:]
	return job, true
}

// run recovers queued blocks until ctx is done.
func (s *laneSenderStore) run(ctx context.Context, recoverSenders func(context.Context, [][]byte) []utils.Option[common.Address]) {
	stop := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	defer stop()
	for {
		job, ok := s.next(ctx)
		if !ok {
			return
		}
		job.entry.senders = recoverSenders(ctx, job.txs)
		close(job.entry.done)
	}
}

// superblockSenders returns the recovered senders of every block, concatenated in block order and
// aligned with the superblock's transactions. A block whose senders were not recovered contributes
// None for each transaction, which prepare then recovers itself. It returns nil when no block was
// recovered.
func (r *gigaRouterCommon) superblockSenders(ctx context.Context, blocks []*atypes.GlobalBlock) ([]utils.Option[common.Address], error) {
	store := r.laneSenders.Load()
	if store == nil {
		return nil, nil
	}
	total := 0
	for _, b := range blocks {
		total += len(b.Payload.Txs())
	}
	out := make([]utils.Option[common.Address], 0, total)
	found := false
	for _, b := range blocks {
		txs := len(b.Payload.Txs())
		entry, ok := store.take(b.Header.Hash())
		if ok {
			select {
			case <-entry.done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if !ok || len(entry.senders) != txs {
			out = append(out, make([]utils.Option[common.Address], txs)...)
			continue
		}
		found = true
		out = append(out, entry.senders...)
	}
	if !found {
		return nil, nil
	}
	return out, nil
}
