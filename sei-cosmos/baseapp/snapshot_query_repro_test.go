package baseapp

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/snapshots"
	snapshottypes "github.com/sei-protocol/sei-chain/sei-cosmos/snapshots/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	dbm "github.com/tendermint/tm-db"
)

func TestSnapshotQueryRepro_HealthyEmptyStore(t *testing.T) {
	fixture := newSnapshotQueryRepro(t)

	response, err := fixture.app.Query(t.Context(), &abci.RequestQuery{Path: "/app/snapshots"})
	if err != nil || response.Code != 0 {
		t.Fatalf("healthy empty query: response=%+v error=%v", response, err)
	}
	listed := decodeSnapshotQueryRepro(t, response.Value)
	if listed.Snapshots == nil || len(listed.Snapshots) != 0 {
		t.Fatalf("healthy empty query returned snapshots=%+v", listed.Snapshots)
	}
	fixture.assertReadReached(t, false)
	t.Log("healthy metadata store returned Code=0 and an empty snapshot list")
}

func TestSnapshotQueryRepro_HealthyCompletedStore(t *testing.T) {
	fixture := newSnapshotQueryRepro(t)
	fixture.saveCompletedSnapshot(t, 1000)

	response, err := fixture.app.Query(t.Context(), &abci.RequestQuery{Path: "/app/snapshots"})
	if err != nil || response.Code != 0 {
		t.Fatalf("healthy completed query: response=%+v error=%v", response, err)
	}
	listed := decodeSnapshotQueryRepro(t, response.Value)
	if len(listed.Snapshots) != 1 || listed.Snapshots[0].Height != 1000 || listed.Snapshots[0].Chunks != 1 {
		t.Fatalf("healthy query did not return the completed snapshot: %+v", listed.Snapshots)
	}
	fixture.assertReadReached(t, false)
	t.Log("healthy metadata store returned the completed snapshot at height 1000")
}

// A metadata read failure must remain distinguishable from a healthy empty store.
// Closing the real LevelDB makes its iterator report the real driver error.
func TestSnapshotQueryRepro_MetadataReadFailureMustNotBecomeEmptySuccess(t *testing.T) {
	fixture := newSnapshotQueryRepro(t)
	fixture.saveCompletedSnapshot(t, 1000)
	fixture.closeMetadata(t)

	response, queryErr := fixture.app.Query(t.Context(), &abci.RequestQuery{Path: "/app/snapshots"})
	fixture.assertReadReached(t, true)
	if queryErr == nil && response.Code == 0 {
		listed := decodeSnapshotQueryRepro(t, response.Value)
		t.Fatalf("metadata read failed with %v, but /app/snapshots returned Code=0, query error=nil, snapshots=%d; the storage failure became an empty success", fixture.observed.lastIteratorError, len(listed.Snapshots))
	}
}

type snapshotQueryReproFixture struct {
	app      *BaseApp
	store    *snapshots.Store
	metadata dbm.DB
	observed *snapshotQueryObservedDB
	closed   bool
}

func newSnapshotQueryRepro(t *testing.T) *snapshotQueryReproFixture {
	t.Helper()
	dir := t.TempDir()
	metadata, err := dbm.NewGoLevelDB("metadata", dir)
	if err != nil {
		t.Fatal(err)
	}
	observed := &snapshotQueryObservedDB{DB: metadata}
	store, err := snapshots.NewStore(observed, dir)
	if err != nil {
		_ = metadata.Close()
		t.Fatal(err)
	}
	fixture := &snapshotQueryReproFixture{
		app:      newBaseApp(t.Name(), SetSnapshotStore(store)),
		store:    store,
		metadata: metadata,
		observed: observed,
	}
	t.Cleanup(func() {
		if !fixture.closed {
			fixture.closeMetadata(t)
		}
	})
	return fixture
}

func (f *snapshotQueryReproFixture) saveCompletedSnapshot(t *testing.T, height uint64) {
	t.Helper()
	chunks := make(chan io.ReadCloser, 1)
	chunks <- io.NopCloser(strings.NewReader("diagnostic snapshot chunk"))
	close(chunks)
	if _, err := f.store.Save(height, snapshottypes.CurrentFormat, chunks); err != nil {
		t.Fatalf("saving completed snapshot: %v", err)
	}
}

func (f *snapshotQueryReproFixture) closeMetadata(t *testing.T) {
	t.Helper()
	if err := f.metadata.Close(); err != nil {
		t.Fatalf("closing metadata database: %v", err)
	}
	f.closed = true
}

func (f *snapshotQueryReproFixture) assertReadReached(t *testing.T, wantFailure bool) {
	t.Helper()
	if f.observed.reverseReads != 1 {
		t.Fatalf("query reached metadata ReverseIterator %d times; want 1", f.observed.reverseReads)
	}
	if (f.observed.lastIteratorError != nil) != wantFailure {
		t.Fatalf("metadata iterator error=%v; want failure=%v", f.observed.lastIteratorError, wantFailure)
	}
}

func decodeSnapshotQueryRepro(t *testing.T, value []byte) abci.ResponseListSnapshots {
	t.Helper()
	var listed abci.ResponseListSnapshots
	if err := json.Unmarshal(value, &listed); err != nil {
		t.Fatalf("decoding actual query value %q: %v", value, err)
	}
	return listed
}

// snapshotQueryObservedDB records the real database read without changing it.
type snapshotQueryObservedDB struct {
	dbm.DB
	reverseReads      int
	lastIteratorError error
}

func (d *snapshotQueryObservedDB) ReverseIterator(start, end []byte) (dbm.Iterator, error) {
	d.reverseReads++
	iterator, err := d.DB.ReverseIterator(start, end)
	if err != nil {
		d.lastIteratorError = err
		return nil, err
	}
	return &snapshotQueryObservedIterator{Iterator: iterator, observed: d}, nil
}

type snapshotQueryObservedIterator struct {
	dbm.Iterator
	observed *snapshotQueryObservedDB
}

func (i *snapshotQueryObservedIterator) Error() error {
	err := i.Iterator.Error()
	i.observed.lastIteratorError = err
	return err
}
