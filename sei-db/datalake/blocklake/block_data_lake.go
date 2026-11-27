// Package blocklake copies the Autobahn block store into a data lake.
package blocklake

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-db/datalake"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/cdkdatalake"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

var logger = seilog.NewLogger("db", "datalake", "blocklake")

// recordKind is a kind of block store record, copied into a data lake store of its own keyed by road height.
type recordKind struct {
	// The name of the kind's store in the bucket.
	storeName string

	// Returns the bytes the block store holds for the record of a road, once they are durable.
	waitForRecord func(store *blockstore.Store, ctx context.Context, road types.RoadHeight) ([]byte, error)
}

// recordKinds are the kinds a BlockDataLake copies.
var recordKinds = []recordKind{
	// A block record carries the FullCommitQC that commits it.
	{storeName: "blocks", waitForRecord: (*blockstore.Store).WaitForBlockRecord},
	// An AppQC carries the AppProposal it certifies.
	{storeName: "app_qcs", waitForRecord: (*blockstore.Store).WaitForAppQCRecord},
}

// BlockDataLake copies every block and AppQC record a block store makes durable into a data lake, byte for byte, in
// one store per record kind keyed by road height. Copying runs in the background and never holds up the block store.
type BlockDataLake struct {
	// One per record kind.
	mirrors []*mirror

	// Stops the mirrors.
	cancel context.CancelFunc

	// Tracks the mirror goroutines.
	running sync.WaitGroup
}

// DefaultDataLakeConfig returns the configuration of the stores a BlockDataLake keeps in the bucket bucketURL names.
func DefaultDataLakeConfig(bucketURL string) cdkdatalake.DataLakeConfig {
	config := cdkdatalake.DefaultDataLakeConfig()
	config.BucketURL = bucketURL
	// Each queued put holds a whole record, so the queues are sized for memory rather than throughput.
	config.RequestBufferSize = 64
	config.WorkBufferSize = 64
	return config
}

// New opens a store per record kind in the bucket config names, and starts copying into each the records the block
// store makes durable. A store resumes at the road after its highest key; an empty one starts at the lowest road the
// block store retains.
func New(ctx context.Context, config cdkdatalake.DataLakeConfig, store *blockstore.Store) (*BlockDataLake, error) {
	mirrors, err := openMirrors(ctx, config, store)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	lake := &BlockDataLake{mirrors: mirrors, cancel: cancel}
	for _, m := range mirrors {
		lake.running.Go(func() { m.run(runCtx) })
	}
	return lake, nil
}

// openMirrors opens the store of every record kind. A failed call closes the stores it opened.
func openMirrors(ctx context.Context, config cdkdatalake.DataLakeConfig, store *blockstore.Store) ([]*mirror, error) {
	mirrors := make([]*mirror, 0, len(recordKinds))
	for _, kind := range recordKinds {
		lake, err := cdkdatalake.NewCDKDataLake(ctx, kind.storeName, false, config)
		if err != nil {
			err = fmt.Errorf("open store %q: %w", kind.storeName, err)
			return nil, errors.Join(err, closeMirrors(mirrors))
		}
		mirrors = append(mirrors, &mirror{kind: kind, store: store, lake: lake})
	}
	return mirrors, nil
}

// Close stops copying, then makes every accepted write crash durable and closes the stores.
func (l *BlockDataLake) Close() error {
	l.cancel()
	l.running.Wait()
	return closeMirrors(l.mirrors)
}

// closeMirrors closes the store of every mirror.
func closeMirrors(mirrors []*mirror) error {
	var err error
	for _, m := range mirrors {
		if closeErr := m.lake.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close store %q: %w", m.kind.storeName, closeErr))
		}
	}
	return err
}

// mirror copies the records of one kind from the block store into the kind's store.
type mirror struct {
	kind  recordKind
	store *blockstore.Store
	lake  datalake.DataLake
}

// run copies records until ctx ends or a copy fails. A failure stops this kind's copying only.
func (m *mirror) run(ctx context.Context) {
	err := m.copyRecords(ctx)
	if ctx.Err() == nil {
		logger.Error("stopped copying block store records to the data lake", "store", m.kind.storeName, "err", err)
	}
}

// copyRecords copies each durable record in road order, from resumeRoad() on, until ctx ends or a copy fails.
func (m *mirror) copyRecords(ctx context.Context) error {
	road, err := m.resumeRoad(ctx)
	if err != nil {
		return fmt.Errorf("find the road to resume at: %w", err)
	}
	logger.Info("copying block store records to the data lake", "store", m.kind.storeName, "road", road)
	for ; ; road++ {
		record, err := m.kind.waitForRecord(m.store, ctx, road)
		if errors.Is(err, types.ErrPruned) {
			return fmt.Errorf("road %d was pruned from the block store before it was copied: %w", road, err)
		}
		if err != nil {
			return fmt.Errorf("read road %d: %w", road, err)
		}
		if err := m.lake.Put(ctx, uint64(road), record); err != nil {
			return fmt.Errorf("put road %d: %w", road, err)
		}
	}
}

// resumeRoad returns the road to copy first: the one after the store's highest key, or, for an empty store, the
// lowest road the block store retains once it holds a durable record of the kind.
func (m *mirror) resumeRoad(ctx context.Context) (types.RoadHeight, error) {
	if _, ok := m.lake.GetFirstKey(); ok {
		return types.RoadHeight(m.lake.GetDurableWatermark()), nil
	}
	for {
		// First is 0 until the block store holds a record, so a store starting above road 0 ends this wait in
		// ErrPruned, as pruning past first does.
		first := m.store.First()
		_, err := m.kind.waitForRecord(m.store, ctx, first)
		if !errors.Is(err, types.ErrPruned) {
			return first, err
		}
	}
}
