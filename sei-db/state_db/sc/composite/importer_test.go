package composite

import (
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// endRecordingImporter is a configurable importer that records how it ended and whether it received
// permission to publish an empty import.
type endRecordingImporter struct {
	addModuleErr error
	addNodeErr   error
	closeErr     error
	allowEmpty   bool
	endedBy      string
}

func (e *endRecordingImporter) AddModule(string) error { return e.addModuleErr }

func (e *endRecordingImporter) AddNode(*types.SnapshotNode) error { return e.addNodeErr }

func (e *endRecordingImporter) AllowEmpty() {
	e.allowEmpty = true
}

func (e *endRecordingImporter) Close() error {
	e.endedBy = "close"
	return e.closeErr
}

func (e *endRecordingImporter) Abort(error) error {
	e.endedBy = "abort"
	return nil
}

// TestSnapshotImporterCloseDiscardsFlatKVWhenCosmosFails pins that Close publishes flatkv only when the
// cosmos import succeeded, and that Abort discards both.
func TestSnapshotImporterCloseDiscardsFlatKVWhenCosmosFails(t *testing.T) {
	t.Run("cosmos succeeds", func(t *testing.T) {
		cosmos, flatkv := &endRecordingImporter{}, &endRecordingImporter{}
		require.NoError(t, NewImporter(cosmos, flatkv, nil).Close())
		require.Equal(t, "close", cosmos.endedBy)
		require.Equal(t, "close", flatkv.endedBy)
	})
	t.Run("cosmos fails", func(t *testing.T) {
		cosmos := &endRecordingImporter{closeErr: errors.New("cosmos import failed")}
		flatkv := &endRecordingImporter{}
		require.ErrorContains(t, NewImporter(cosmos, flatkv, nil).Close(), "cosmos import failed")
		require.Equal(t, "abort", flatkv.endedBy, "a failed cosmos import must not publish flatkv")
	})
	t.Run("abort", func(t *testing.T) {
		cosmos, flatkv := &endRecordingImporter{}, &endRecordingImporter{}
		require.NoError(t, NewImporter(cosmos, flatkv, nil).Abort(errors.New("restore failed")))
		require.Equal(t, "abort", cosmos.endedBy)
		require.Equal(t, "abort", flatkv.endedBy)
	})
}

// TestSnapshotImporterAddNodeReturnsBackendError pins that AddNode returns the error of the backend that the
// current section routes to.
func TestSnapshotImporterAddNodeReturnsBackendError(t *testing.T) {
	cosmos := &endRecordingImporter{addNodeErr: errors.New("cosmos rejected node")}
	flatkv := &endRecordingImporter{addNodeErr: errors.New("flatkv rejected node")}
	imp := NewImporter(cosmos, flatkv, nil)
	node := &types.SnapshotNode{Key: []byte("k"), Value: []byte("v")}

	require.NoError(t, imp.AddModule("bank"))
	require.ErrorContains(t, imp.AddNode(node), "cosmos rejected node")
	require.NoError(t, imp.AddModule(keys.FlatKVStoreKey))
	require.ErrorContains(t, imp.AddNode(node), "flatkv rejected node")
}

func TestSnapshotImporterAllowsEmptyCosmosAfterFlatKVSection(t *testing.T) {
	t.Run("flatkv section accepted", func(t *testing.T) {
		cosmos, flatkv := &endRecordingImporter{}, &endRecordingImporter{}
		require.NoError(t, NewImporter(cosmos, flatkv, nil).AddModule(keys.FlatKVStoreKey))
		require.True(t, cosmos.allowEmpty)
	})

	t.Run("flatkv section rejected", func(t *testing.T) {
		cosmos := &endRecordingImporter{}
		flatkv := &endRecordingImporter{addModuleErr: errors.New("flatkv rejected module")}
		require.ErrorContains(t, NewImporter(cosmos, flatkv, nil).AddModule(keys.FlatKVStoreKey), "flatkv rejected module")
		require.False(t, cosmos.allowEmpty)
	})

	t.Run("cosmos section accepted", func(t *testing.T) {
		cosmos, flatkv := &endRecordingImporter{}, &endRecordingImporter{}
		require.NoError(t, NewImporter(cosmos, flatkv, nil).AddModule(keys.BankStoreKey))
		require.False(t, cosmos.allowEmpty)
	})
}
