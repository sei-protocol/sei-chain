package mvcc

import (
	"context"
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
