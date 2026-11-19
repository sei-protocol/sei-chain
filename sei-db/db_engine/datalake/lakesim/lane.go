package lakesim

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/datalake"
)

// lane writes one value per block into its own data lake, and prunes blocks that fall out of the retention window.
// Its goroutine owns every field but blocks, done and failure.
type lane struct {
	// The lane's store name.
	name string

	// The lane's data lake.
	lake datalake.DataLake

	// The source of the lane's values.
	rand *crand.CannedRandom

	// The size of each value.
	valueSize int

	// The number of most recent blocks to keep. 0 keeps every block.
	retainedBlocks uint64

	// The number of blocks beyond retainedBlocks the lane may hold while their deletes are pending.
	maxDeletionBacklog uint64

	// How often the lane reads its data lake's watermarks.
	pollInterval time.Duration

	// Receives a value for each block the lane is to write. Received from only while the lane accepts blocks.
	blocks chan struct{}

	// Closed when the lane's goroutine exits.
	done chan struct{}

	// The error that stopped the lane, or nil. Set before done is closed, and read only after.
	failure error

	// One above the highest key the lane has put.
	nextKey uint64

	// The data lake's durable watermark when last read.
	durableWatermark uint64

	// The data lake's deletion watermark when last read.
	deletionWatermark uint64

	// When each accepted put at or above durableWatermark was accepted, in key order.
	acceptTimes *structures.Deque[time.Time]

	// The lane's metric attributes.
	attributes metric.MeasurementOption

	// The benchmark's instruments.
	metrics *LakesimMetrics
}

// newLane opens the data lake of lane index, resuming after the highest key it holds, and starts the lane's
// goroutine, which runs until ctx ends or the lane fails.
func newLane(
	ctx context.Context,
	// The lane's position in config.Lanes.
	index int,
	config *LakesimConfig,
	// The source of the lane's values, owned by the lane.
	rand *crand.CannedRandom,
	metrics *LakesimMetrics,
) (*lane, error) {
	name := fmt.Sprintf("lane%d", index)
	lake, err := datalake.NewCDKDataLake(ctx, name, false, config.DataLakeConfig())
	if err != nil {
		return nil, fmt.Errorf("open data lake of %s: %w", name, err)
	}
	durableWatermark := lake.GetDurableWatermark()
	l := &lane{
		name:               name,
		lake:               lake,
		rand:               rand,
		valueSize:          config.Lanes[index].ValueSize,
		retainedBlocks:     config.RetainedBlocks,
		maxDeletionBacklog: config.MaxDeletionBacklog,
		pollInterval:       time.Duration(config.WatermarkPollIntervalMs) * time.Millisecond,
		blocks:             make(chan struct{}),
		done:               make(chan struct{}),
		nextKey:            durableWatermark,
		durableWatermark:   durableWatermark,
		deletionWatermark:  lake.GetDeletionWatermark(),
		acceptTimes:        structures.NewDeque[time.Time](),
		attributes:         LaneAttributes(name),
		metrics:            metrics,
	}
	go l.run(ctx)
	return l, nil
}

// WriteBlock hands the lane its next block, waiting while the lane does not accept blocks. It returns false if ctx
// ended or the lane stopped first.
func (l *lane) WriteBlock(ctx context.Context) bool {
	select {
	case l.blocks <- struct{}{}:
		return true
	case <-l.done:
		return false
	case <-ctx.Done():
		return false
	}
}

// Wait waits for the lane's goroutine to exit, and returns the error that stopped the lane, or nil.
func (l *lane) Wait() error {
	<-l.done
	return l.failure
}

// Name returns the lane's store name.
func (l *lane) Name() string {
	return l.name
}

// NextKey returns the key of the lane's next put. It may be called only before the lane's first block.
func (l *lane) NextKey() uint64 {
	return l.nextKey
}

// run writes blocks and reads the data lake's watermarks until ctx ends or the lane fails, then makes every accepted
// write and delete durable and closes the data lake.
func (l *lane) run(ctx context.Context) {
	defer close(l.done)
	err := l.writeBlocks(ctx)
	if err != nil {
		err = fmt.Errorf("lane %s: %w", l.name, err)
	}
	if shutdownErr := l.shutdown(); shutdownErr != nil {
		err = errors.Join(err, fmt.Errorf("lane %s: %w", l.name, shutdownErr))
	}
	l.failure = err
}

// writeBlocks writes each block it receives and reads the data lake's watermarks on each tick, until ctx ends or a
// write fails. It receives no block while the deletion backlog is full.
func (l *lane) writeBlocks(ctx context.Context) error {
	ticker := time.NewTicker(l.pollInterval)
	defer ticker.Stop()
	for {
		blocks := l.blocks
		if l.deletionBacklogFull() {
			blocks = nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			l.readWatermarks()
		case <-blocks:
			if err := l.writeBlock(); err != nil {
				return err
			}
		}
	}
}

// deletionBacklogFull reports whether the blocks that may still exist in the bucket, as of the last deletion
// watermark read, exceed the retention window by the allowed backlog.
func (l *lane) deletionBacklogFull() bool {
	if l.retainedBlocks == 0 {
		return false
	}
	return l.nextKey-l.deletionWatermark > l.retainedBlocks+l.maxDeletionBacklog
}

// writeBlock puts the block's value, then prunes the block that falls out of the retention window.
func (l *lane) writeBlock() error {
	value := l.rand.Bytes(l.valueSize)
	start := time.Now()
	// A put that is queued completes whatever its context does, so it gets none to end.
	if err := l.lake.Put(context.Background(), l.nextKey, value); err != nil {
		return fmt.Errorf("put block: %w", err)
	}
	accepted := time.Now()
	l.metrics.ReportPut(l.attributes, l.valueSize, accepted.Sub(start))
	l.acceptTimes.PushBack(accepted)
	l.nextKey++

	if l.retainedBlocks > 0 && l.nextKey > l.retainedBlocks {
		if err := l.lake.PruneBelow(context.Background(), l.nextKey-l.retainedBlocks); err != nil {
			return fmt.Errorf("prune: %w", err)
		}
	}
	return nil
}

// readWatermarks reads the data lake's watermarks, records the durable latency of each put the durable watermark
// passed since the last read, and reports the lane's gauges.
func (l *lane) readWatermarks() {
	durableWatermark := l.lake.GetDurableWatermark()
	now := time.Now()
	for ; l.durableWatermark < durableWatermark && !l.acceptTimes.IsEmpty(); l.durableWatermark++ {
		l.metrics.ReportDurable(l.attributes, now.Sub(l.acceptTimes.PopFront()))
	}
	l.durableWatermark = durableWatermark
	l.deletionWatermark = l.lake.GetDeletionWatermark()
	l.metrics.ReportWatermarks(l.attributes, l.nextKey, l.durableWatermark, l.deletionWatermark, l.valueSize)
}

// shutdown makes every accepted write and delete durable, then closes the data lake.
func (l *lane) shutdown() error {
	var err error
	if flushErr := l.lake.Flush(context.Background()); flushErr != nil {
		err = fmt.Errorf("flush: %w", flushErr)
	}
	if closeErr := l.lake.Close(); closeErr != nil {
		err = errors.Join(err, fmt.Errorf("close: %w", closeErr))
	}
	return err
}
