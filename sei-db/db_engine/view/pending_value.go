package view

import (
	"context"
	"fmt"
)

// pendingValue is a staged value whose bytes are not yet known: the fold of one batch's changes onto
// the value its key already held.
type pendingValue struct {
	// The key whose value this fold produces.
	key string

	// Closed once value and err are final.
	done chan struct{}

	// What the fold produced, nil for a delete. Illegal to read before the done chan is closed.
	value []byte

	// The failure that stopped the fold, if it failed. Illegal to read before the done chan is closed.
	err error
}

// newPendingValue returns an unresolved staged value for the given key.
func newPendingValue(key string) *pendingValue {
	return &pendingValue{key: key, done: make(chan struct{})}
}

// inject records what the fold produced and releases every observer. Called exactly once.
func (p *pendingValue) inject(value []byte, err error) {
	p.value = value
	p.err = err
	// Closing publishes both fields and wakes every observer at once, so none of them has to pass the
	// value to the next, and a second inject panics here rather than queueing a second answer.
	close(p.done)
}

// await blocks until this value is resolved and reports what it resolved to. A nil value means the
// key was deleted. Must be called with no shard lock held, since the fold needs that lock to publish.
//
// ctx is cancelled when the manager shuts down, and shutdownError then names the cause.
func (p *pendingValue) await(ctx context.Context, shutdownError func() error) ([]byte, error) {
	// Not threading.InterruptiblePull: it reports a closed channel as an error, and a close is how a
	// resolved value is published here.
	select {
	case <-p.done:
		if p.err != nil {
			return nil, fmt.Errorf("staged value failed to resolve: %w", p.err)
		}
		return p.value, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("view manager shut down while awaiting a staged value: %w", shutdownError())
	}
}

// stagedFold is one fold's two halves: where it gets the value it folds onto, and where it puts the
// result. The key is on result.
type stagedFold struct {
	// Where the prior value comes from.
	prior priorValueSource

	// The handle every observer of this key waits on, and where the fold injects its result.
	result *pendingValue
}

// Which of the three places a staged fold's prior value comes from.
type priorValueLocation int

const (
	// The shard's versioned data holds it; priorValueSource.value is it.
	priorValueInVersionedData priorValueLocation = 1
	// An earlier staged fold on the same key has yet to produce it; priorValueSource.pending is
	// that fold.
	priorValueInEarlierFold priorValueLocation = 2
	// Nothing the shard holds has it, so it comes from the read cache.
	priorValueInReadCache priorValueLocation = 3
)

// priorValueSource is where a staged fold gets the prior value it folds onto.
type priorValueSource struct {
	// Which of the three places the prior value comes from.
	location priorValueLocation

	// The prior value. Meaningful exactly while location is priorValueInVersionedData, where a nil
	// value is a tombstone rather than an absence.
	value []byte

	// The earlier fold to await. Non-nil exactly while location is priorValueInEarlierFold.
	pending *pendingValue
}
