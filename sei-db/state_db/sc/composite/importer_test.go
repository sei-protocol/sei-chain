package composite

import (
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// endRecordingImporter is an importer whose AddNode returns addNodeErr and whose Close returns closeErr, and
// which records whether Close or Abort ended it.
type endRecordingImporter struct {
	addNodeErr error
	closeErr   error
	endedBy    string
}

func (e *endRecordingImporter) AddModule(string) error { return nil }

func (e *endRecordingImporter) AddNode(*types.SnapshotNode) error { return e.addNodeErr }

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
