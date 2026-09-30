package composite

import (
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// endRecordingImporter is an importer whose Close returns closeErr, and which records whether Close or
// Abort ended it.
type endRecordingImporter struct {
	closeErr error
	endedBy  string
}

func (e *endRecordingImporter) AddModule(string) error { return nil }

func (e *endRecordingImporter) AddNode(*types.SnapshotNode) {}

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
