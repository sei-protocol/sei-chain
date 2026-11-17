package baseapp

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/snapshots"
	snapshottypes "github.com/sei-protocol/sei-chain/sei-cosmos/snapshots/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func TestSnapshotQuery_EmptyWireResponse(t *testing.T) {
	for _, test := range []struct {
		name             string
		snapshotsEnabled bool
	}{
		{name: "snapshots disabled"},
		{name: "healthy empty store", snapshotsEnabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var app *BaseApp
			if test.snapshotsEnabled {
				app = newSnapshotQueryFixture(t).app
			} else {
				app = newBaseApp(t.Name())
			}

			response, err := app.Query(t.Context(), &abci.RequestQuery{Path: "/app/snapshots", Height: 42})
			require.NoError(t, err)
			require.Zero(t, response.Code)
			require.Equal(t, sdkerrors.RootCodespace, response.Codespace)
			require.EqualValues(t, 42, response.Height)
			require.Equal(t, `{"Snapshots":[]}`, string(response.Value))

			listed, err := app.ListSnapshots(t.Context(), &abci.RequestListSnapshots{})
			require.NoError(t, err)
			require.Equal(t, &abci.ResponseListSnapshots{Snapshots: []*abci.Snapshot{}}, listed)
		})
	}
}

func TestSnapshotQuery_MetadataListingFailure(t *testing.T) {
	fixture := newSnapshotQueryFixture(t)
	fixture.saveCompletedSnapshot(t, 1000)
	// A closed real LevelDB must remain distinguishable from a healthy empty store.
	fixture.closeMetadata(t)

	response, err := fixture.app.Query(t.Context(), &abci.RequestQuery{Path: "/app/snapshots"})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotZero(t, response.Code, "a failed metadata read must not look like healthy emptiness")
	require.NotEqual(t, sdkerrors.ErrPanic.ABCICode(), response.Code, "the query must report the listing error, not recover a panic")
	require.Empty(t, response.Value, "a failed listing must not return a successful snapshot payload")
	require.Contains(t, response.Log, "leveldb: closed", "the real metadata read failure must reach the query response")

	offered, err := fixture.app.ListSnapshots(t.Context(), &abci.RequestListSnapshots{})
	require.NoError(t, err, "the direct ABCI method preserves its existing fallback")
	require.Equal(t, &abci.ResponseListSnapshots{Snapshots: []*abci.Snapshot{}}, offered)
}

func TestSnapshotQuery_CompletedSnapshots(t *testing.T) {
	for _, test := range []struct {
		name            string
		storedHeights   []uint64
		expectedHeights []uint64
	}{
		{name: "single completed snapshot", storedHeights: []uint64{1000}, expectedHeights: []uint64{1000}},
		{name: "newest first", storedHeights: []uint64{1000, 2000}, expectedHeights: []uint64{2000, 1000}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSnapshotQueryFixture(t)
			for _, height := range test.storedHeights {
				fixture.saveCompletedSnapshot(t, height)
			}

			response, err := fixture.app.Query(t.Context(), &abci.RequestQuery{Path: "/app/snapshots"})
			require.NoError(t, err)
			require.Zero(t, response.Code)
			var listed abci.ResponseListSnapshots
			require.NoError(t, json.Unmarshal(response.Value, &listed))
			require.Len(t, listed.Snapshots, len(test.expectedHeights))
			for i, snapshot := range listed.Snapshots {
				require.Equal(t, test.expectedHeights[i], snapshot.Height)
				require.Equal(t, snapshottypes.CurrentFormat, snapshot.Format)
				require.EqualValues(t, 1, snapshot.Chunks)
				require.NotEmpty(t, snapshot.Hash)
				require.NotEmpty(t, snapshot.Metadata)
			}

			offered, err := fixture.app.ListSnapshots(t.Context(), &abci.RequestListSnapshots{})
			require.NoError(t, err)
			require.Equal(t, offered.Snapshots, listed.Snapshots)
		})
	}
}

type snapshotQueryFixture struct {
	app      *BaseApp
	store    *snapshots.Store
	metadata dbm.DB
	closed   bool
}

func newSnapshotQueryFixture(t *testing.T) *snapshotQueryFixture {
	t.Helper()
	dir := t.TempDir()
	metadata, err := dbm.NewGoLevelDB("metadata", dir)
	require.NoError(t, err)
	fixture := &snapshotQueryFixture{metadata: metadata}
	t.Cleanup(func() {
		if !fixture.closed {
			fixture.closeMetadata(t)
		}
	})
	fixture.store, err = snapshots.NewStore(metadata, dir)
	require.NoError(t, err)
	fixture.app = newBaseApp(t.Name(), SetSnapshotStore(fixture.store))
	return fixture
}

func (f *snapshotQueryFixture) saveCompletedSnapshot(t *testing.T, height uint64) {
	t.Helper()
	chunks := make(chan io.ReadCloser, 1)
	chunks <- io.NopCloser(strings.NewReader("snapshot chunk"))
	close(chunks)
	_, err := f.store.Save(height, snapshottypes.CurrentFormat, chunks)
	require.NoError(t, err)
}

func (f *snapshotQueryFixture) closeMetadata(t *testing.T) {
	t.Helper()
	require.NoError(t, f.metadata.Close())
	f.closed = true
}
