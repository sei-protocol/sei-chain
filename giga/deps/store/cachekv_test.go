package store_test

import (
	"errors"
	"fmt"
	"testing"

	gigastore "github.com/sei-protocol/sei-chain/giga/deps/store"
	"github.com/stretchr/testify/require"
)

func TestIteratorPanicsWithSentinel(t *testing.T) {
	s := gigastore.NewStore(nil, nil, 0)

	require.PanicsWithValue(t, gigastore.ErrIteratorUnsupported, func() {
		_ = s.Iterator(nil, nil)
	})
	require.PanicsWithValue(t, gigastore.ErrIteratorUnsupported, func() {
		_ = s.ReverseIterator(nil, nil)
	})
}

// TestIteratorPanicDetectableViaErrorsIs mirrors the recover net in app.go: the
// recovered panic value must be detectable as the sentinel via errors.Is, even
// once wrapped, so the giga executor can fall back to v2 instead of diverging.
func TestIteratorPanicDetectableViaErrorsIs(t *testing.T) {
	s := gigastore.NewStore(nil, nil, 0)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = s.Iterator(nil, nil)
	}()

	err, ok := recovered.(error)
	require.True(t, ok)
	require.True(t, errors.Is(err, gigastore.ErrIteratorUnsupported))
	require.True(t, errors.Is(fmt.Errorf("wrapped: %w", err), gigastore.ErrIteratorUnsupported))
}
