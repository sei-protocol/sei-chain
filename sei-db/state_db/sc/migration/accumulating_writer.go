package migration

import "github.com/sei-protocol/sei-chain/sei-db/proto"

// accumulatingWriter wraps a leaf DBWriter, buffering the change sets from one
// or more Apply calls and forwarding them to the wrapped writer as a single
// ApplyChangeSets batch when Flush is called.
//
// It exists to coalesce the fan-out a single dispatch can produce: when several
// routes (e.g. a direct flatKV route and MigrationManager.newDBWriter) target
// the same backend, they share one accumulatingWriter so the backend observes
// exactly one ApplyChangeSets per dispatch instead of one per route.
//
// The wrapped writer is a leaf backend writer (buildFlatKVWriter /
// buildMemIAVLWriter) that ignores firstBatchInBlock, so Apply ignores it too;
// any migration-boundary semantics have already been consumed upstream by the
// MigrationManager before its physical change set reaches this writer.
//
// Not safe for concurrent use; callers must serialize Apply/Flush (the router
// tree is serialized by threadSafeRouter in migration modes).
type accumulatingWriter struct {
	wrapped DBWriter

	// buffered holds the change sets accumulated since the last Flush, in the
	// order they were supplied to Apply.
	buffered []*proto.NamedChangeSet

	// pending is true once at least one Apply call has occurred since the last
	// Flush, even if that call carried no change sets. This lets an empty apply
	// still be forwarded on Flush, preserving the "every dispatch forwards once"
	// behavior the wrapped leaf writer saw before coalescing.
	pending bool
}

// newAccumulatingWriter wraps writer in an accumulator.
func newAccumulatingWriter(writer DBWriter) *accumulatingWriter {
	return &accumulatingWriter{wrapped: writer}
}

// Apply buffers changesets for a later Flush. It never writes to the wrapped
// writer and so always returns nil; downstream write errors surface from Flush
// instead. Its signature matches DBWriter so it can be used as a route writer.
func (a *accumulatingWriter) Apply(changesets []*proto.NamedChangeSet, _ bool) error {
	a.buffered = append(a.buffered, changesets...)
	a.pending = true
	return nil
}

// Flush forwards all change sets buffered since the last Flush to the wrapped
// writer as a single ApplyChangeSets call, then resets the buffer. If no Apply
// call has occurred since the last Flush, Flush is a no-op.
//
// The buffer is reset before the downstream call so the writer is left in a
// clean state regardless of the outcome; per the Router contract an
// ApplyChangeSets error is fatal and must not be retried.
func (a *accumulatingWriter) Flush() error {
	if !a.pending {
		return nil
	}
	changesets := a.buffered
	a.buffered = nil
	a.pending = false
	return a.wrapped(changesets, false)
}
