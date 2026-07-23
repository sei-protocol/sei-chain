package walsim

import (
	"context"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
)

// RecordGenerator asynchronously produces fixed-size opaque records and feeds them into a channel.
// Each record is a zero-copy sub-slice of a pre-generated, immutable CannedRandom buffer, so the
// generator never runs math/rand or allocates payload bytes on the hot path. The generator stops
// when the context is cancelled.
//
// The CannedRandom buffer is never mutated, so the sub-slices remain valid indefinitely; this makes
// it safe for the WAL to retain a record slice and serialize it asynchronously.
type RecordGenerator struct {
	ctx        context.Context
	rand       *crand.CannedRandom
	recordSize int
	recordChan chan []byte
}

// NewRecordGenerator creates a RecordGenerator and immediately starts its background goroutine. rand
// must not be shared with any other goroutine (this generator owns it).
func NewRecordGenerator(
	ctx context.Context,
	rng *crand.CannedRandom,
	recordSize int,
	queueSize int,
) *RecordGenerator {
	g := &RecordGenerator{
		ctx:        ctx,
		rand:       rng,
		recordSize: recordSize,
		recordChan: make(chan []byte, queueSize),
	}
	go g.mainLoop()
	return g
}

func (g *RecordGenerator) mainLoop() {
	for {
		record := g.rand.Bytes(g.recordSize)
		select {
		case <-g.ctx.Done():
			return
		case g.recordChan <- record:
		}
	}
}
