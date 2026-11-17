package baseapp

import (
	"testing"

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
)

func TestSnapshotQuery_EmptyWireResponse(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "snapshots disabled"
		if enabled {
			name = "healthy empty store"
		}
		t.Run(name, func(t *testing.T) {
			app := newBaseApp(t.Name())
			if enabled {
				app = newSnapshotQueryRepro(t).app
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

func TestSnapshotQuery_MetadataListingError(t *testing.T) {
	fixture := newSnapshotQueryRepro(t)
	fixture.saveCompletedSnapshot(t, 1000)
	fixture.closeMetadata(t)

	response, err := fixture.app.Query(t.Context(), &abci.RequestQuery{Path: "/app/snapshots"})

	fixture.assertReadReached(t, true)
	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotZero(t, response.Code, "a failed metadata read must not look like healthy emptiness")
	require.NotEqual(t, sdkerrors.ErrPanic.ABCICode(), response.Code, "the query must report the listing error, not recover a panic")
	require.Empty(t, response.Value, "a failed listing must not return a successful snapshot payload")
}

func TestListSnapshots_MetadataListingErrorReturnsEmptyOffer(t *testing.T) {
	fixture := newSnapshotQueryRepro(t)
	fixture.saveCompletedSnapshot(t, 1000)
	fixture.closeMetadata(t)

	response, err := fixture.app.ListSnapshots(t.Context(), &abci.RequestListSnapshots{})

	fixture.assertReadReached(t, true)
	require.NoError(t, err, "state sync must retain its existing fallback when local snapshots are unavailable")
	require.Equal(t, &abci.ResponseListSnapshots{Snapshots: []*abci.Snapshot{}}, response)
}

func TestSnapshotQuery_PreservesCompletedSnapshotOrder(t *testing.T) {
	fixture := newSnapshotQueryRepro(t)
	fixture.saveCompletedSnapshot(t, 1000)
	fixture.saveCompletedSnapshot(t, 2000)

	response, err := fixture.app.Query(t.Context(), &abci.RequestQuery{Path: "/app/snapshots"})
	require.NoError(t, err)
	require.Zero(t, response.Code)
	listed := decodeSnapshotQueryRepro(t, response.Value)
	require.Len(t, listed.Snapshots, 2)
	require.EqualValues(t, 2000, listed.Snapshots[0].Height)
	require.EqualValues(t, 1000, listed.Snapshots[1].Height)

	offered, err := fixture.app.ListSnapshots(t.Context(), &abci.RequestListSnapshots{})
	require.NoError(t, err)
	require.Equal(t, offered.Snapshots, listed.Snapshots)
	for _, snapshot := range listed.Snapshots {
		require.EqualValues(t, 1, snapshot.Chunks)
		require.NotEmpty(t, snapshot.Hash)
		require.NotEmpty(t, snapshot.Metadata)
	}
}
