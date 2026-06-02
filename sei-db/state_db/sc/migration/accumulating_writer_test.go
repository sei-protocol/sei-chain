package migration

import (
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// recordedWrite captures a single call made to the wrapped leaf DBWriter.
type recordedWrite struct {
	changesets        []*proto.NamedChangeSet
	firstBatchInBlock bool
}

// recordingWriter is a leaf DBWriter that records every call and can be
// configured to return an error.
type recordingWriter struct {
	calls []recordedWrite
	err   error
}

func (w *recordingWriter) write(changesets []*proto.NamedChangeSet, firstBatchInBlock bool) error {
	w.calls = append(w.calls, recordedWrite{changesets: changesets, firstBatchInBlock: firstBatchInBlock})
	return w.err
}

func TestAccumulatingWriter_ApplyDoesNotWriteUntilFlush(t *testing.T) {
	leaf := &recordingWriter{}
	a := newAccumulatingWriter(leaf.write)

	require.NoError(t, a.Apply([]*proto.NamedChangeSet{namedCS("evm", kv("k", "v"))}, true))
	require.Empty(t, leaf.calls, "Apply must not touch the wrapped writer before Flush")

	require.NoError(t, a.Flush())
	require.Len(t, leaf.calls, 1)
	require.Equal(t, []*proto.NamedChangeSet{namedCS("evm", kv("k", "v"))}, leaf.calls[0].changesets)
}

func TestAccumulatingWriter_CoalescesMultipleAppliesInOrder(t *testing.T) {
	leaf := &recordingWriter{}
	a := newAccumulatingWriter(leaf.write)

	require.NoError(t, a.Apply([]*proto.NamedChangeSet{namedCS("evm", kv("a", "1"))}, true))
	require.NoError(t, a.Apply([]*proto.NamedChangeSet{namedCS("bank", kv("b", "2")), namedCS("evm", kv("c", "3"))}, false))
	require.NoError(t, a.Flush())

	require.Len(t, leaf.calls, 1, "all accumulated applies must collapse into one downstream call")
	require.Equal(t, []*proto.NamedChangeSet{
		namedCS("evm", kv("a", "1")),
		namedCS("bank", kv("b", "2")),
		namedCS("evm", kv("c", "3")),
	}, leaf.calls[0].changesets, "changesets must be forwarded in accumulation order")
}

func TestAccumulatingWriter_EmptyApplyStillForwardsOnFlush(t *testing.T) {
	leaf := &recordingWriter{}
	a := newAccumulatingWriter(leaf.write)

	require.NoError(t, a.Apply(nil, false))
	require.NoError(t, a.Flush())

	require.Len(t, leaf.calls, 1, "an apply with no changesets must still forward once on Flush")
	require.Empty(t, leaf.calls[0].changesets)
}

func TestAccumulatingWriter_FlushWithoutApplyIsNoOp(t *testing.T) {
	leaf := &recordingWriter{}
	a := newAccumulatingWriter(leaf.write)

	require.NoError(t, a.Flush())
	require.Empty(t, leaf.calls, "Flush with nothing accumulated must not touch the wrapped writer")
}

func TestAccumulatingWriter_ResetsBetweenFlushes(t *testing.T) {
	leaf := &recordingWriter{}
	a := newAccumulatingWriter(leaf.write)

	require.NoError(t, a.Apply([]*proto.NamedChangeSet{namedCS("evm", kv("a", "1"))}, true))
	require.NoError(t, a.Flush())

	// A no-op Flush in between must not re-emit the first batch.
	require.NoError(t, a.Flush())
	require.NoError(t, a.Apply([]*proto.NamedChangeSet{namedCS("evm", kv("b", "2"))}, false))
	require.NoError(t, a.Flush())

	require.Len(t, leaf.calls, 2)
	require.Equal(t, []*proto.NamedChangeSet{namedCS("evm", kv("a", "1"))}, leaf.calls[0].changesets)
	require.Equal(t, []*proto.NamedChangeSet{namedCS("evm", kv("b", "2"))}, leaf.calls[1].changesets)
}

func TestAccumulatingWriter_FlushPropagatesDownstreamError(t *testing.T) {
	wantErr := errors.New("downstream boom")
	leaf := &recordingWriter{err: wantErr}
	a := newAccumulatingWriter(leaf.write)

	require.NoError(t, a.Apply([]*proto.NamedChangeSet{namedCS("evm", kv("k", "v"))}, true))
	require.ErrorIs(t, a.Flush(), wantErr)

	// Buffer is cleared even on error; a subsequent Flush is a clean no-op.
	require.NoError(t, a.Flush())
	require.Len(t, leaf.calls, 1, "failed batch must not be retried by a later Flush")
}
