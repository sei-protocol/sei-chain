package mvcc

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const ctxIterStore = "store1"

func TestIteratorWithCancelledContextAbortsSkip(t *testing.T) {
	db := newTestDB(t, true)
	require.True(t, db.descending)

	applyVersion(t, db, ctxIterStore, 10, []byte("aaa"), []byte("visible"))
	for i := 0; i < 1000; i++ {
		applyVersion(t, db, ctxIterStore, 1000, []byte(fmt.Sprintf("zzz%04d", i)), []byte("new"))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	itr, err := db.IteratorWithContext(ctx, ctxIterStore, 10, nil, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, itr)

	itr, err = db.ReverseIteratorWithContext(ctx, ctxIterStore, 10, nil, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, itr)
}

func TestIteratorNextPanicsAfterCancel(t *testing.T) {
	db := newTestDB(t, true)

	applyVersion(t, db, ctxIterStore, 1, []byte("a"), []byte("va"))
	applyVersion(t, db, ctxIterStore, 1, []byte("b"), []byte("vb"))

	ctx, cancel := context.WithCancel(context.Background())
	itr, err := db.IteratorWithContext(ctx, ctxIterStore, 1, nil, nil)
	require.NoError(t, err)
	defer func() { _ = itr.Close() }()
	require.True(t, itr.Valid())

	cancel()
	require.Panics(t, func() { itr.Next() })
}

func TestAscendingIteratorWithCancelledContextAbortsSkip(t *testing.T) {
	db := newAscendingIterTestDB(t)

	applyVersion(t, db, ctxIterStore, 10, []byte("aaa"), []byte("visible"))
	for i := 0; i < 1000; i++ {
		applyVersion(t, db, ctxIterStore, 1000, []byte(fmt.Sprintf("zzz%04d", i)), []byte("new"))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	itr, err := db.ReverseIteratorWithContext(ctx, ctxIterStore, 10, nil, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, itr)
}

func TestFinishMVCCIteratorReturnsOnlyCancelErrors(t *testing.T) {
	pebbleErr := errors.New("pebble: seek failed")
	readErr := &finishIterStub{err: pebbleErr}
	got, err := finishMVCCIterator(readErr)
	require.NoError(t, err)
	require.Equal(t, readErr, got)
	require.False(t, readErr.closed)

	canceled := &finishIterStub{err: context.Canceled}
	got, err = finishMVCCIterator(canceled)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
	require.True(t, canceled.closed)

	deadline := &finishIterStub{err: context.DeadlineExceeded}
	got, err = finishMVCCIterator(deadline)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, got)
	require.True(t, deadline.closed)
}

type finishIterStub struct {
	err    error
	closed bool
}

func (s *finishIterStub) Domain() ([]byte, []byte) { return nil, nil }
func (s *finishIterStub) Valid() bool              { return false }
func (s *finishIterStub) Next()                    {}
func (s *finishIterStub) Key() []byte              { return nil }
func (s *finishIterStub) Value() []byte            { return nil }
func (s *finishIterStub) Error() error             { return s.err }
func (s *finishIterStub) Close() error             { s.closed = true; return nil }
