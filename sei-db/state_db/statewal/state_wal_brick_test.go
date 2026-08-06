package statewal

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

// errInjected is the sentinel failure returned by fakeWAL to simulate a fatal underlying-WAL error.
var errInjected = errors.New("injected failure")

// fakeWAL is a seiwal.WAL whose methods return an injected error when the corresponding field is set,
// letting tests drive each of the wrapper's fatal error paths. Errors are set after construction so the
// initial Bounds call in newStateWAL succeeds.
type fakeWAL struct {
	appendErr   error
	flushErr    error
	boundsErr   error
	pruneErr    error
	iteratorErr error

	// The indices successfully appended, so tests can assert that a failed append persisted nothing.
	appended []uint64
}

var _ seiwal.WAL[[]*proto.NamedChangeSet] = (*fakeWAL)(nil)

func (f *fakeWAL) Append(index uint64, data []*proto.NamedChangeSet) error {
	if f.appendErr != nil {
		return f.appendErr
	}
	f.appended = append(f.appended, index)
	return nil
}

func (f *fakeWAL) Flush() error {
	return f.flushErr
}

func (f *fakeWAL) Bounds() (bool, uint64, uint64, error) {
	if f.boundsErr != nil {
		return false, 0, 0, f.boundsErr
	}
	return false, 0, 0, nil
}

func (f *fakeWAL) PruneBefore(lowestIndexToKeep uint64) error {
	return f.pruneErr
}

func (f *fakeWAL) Iterator(startIndex uint64, endIndex uint64) (seiwal.Iterator[[]*proto.NamedChangeSet], error) {
	if f.iteratorErr != nil {
		return nil, f.iteratorErr
	}
	return nil, nil
}

func (f *fakeWAL) Close() error {
	return nil
}

func newFakeStateWAL(t *testing.T, f *fakeWAL) StateWAL {
	t.Helper()
	w, err := newStateWAL(f)
	require.NoError(t, err)
	return w
}

// requireBricked asserts that every operation fails with the fatal error the WAL was bricked by.
func requireBricked(t *testing.T, w StateWAL) {
	t.Helper()
	require.ErrorIs(t, w.Write(999, nil), errInjected)
	require.ErrorIs(t, w.SignalEndOfBlock(), errInjected)
	require.ErrorIs(t, w.Flush(), errInjected)
	_, _, _, err := w.GetStoredRange()
	require.ErrorIs(t, err, errInjected)
	require.ErrorIs(t, w.Prune(1), errInjected)
	_, err = w.Iterator(0, 0)
	require.ErrorIs(t, err, errInjected)
}

// TestFatalErrorsBrickWAL verifies that a fatal error from any underlying-WAL operation permanently
// bricks the wrapper, so every subsequent operation fails fast rather than limping onward.
func TestFatalErrorsBrickWAL(t *testing.T) {
	t.Run("append", func(t *testing.T) {
		f := &fakeWAL{}
		w := newFakeStateWAL(t, f)
		f.appendErr = errInjected
		require.NoError(t, w.Write(1, nil))
		require.ErrorIs(t, w.SignalEndOfBlock(), errInjected)
		requireBricked(t, w)
	})

	t.Run("flush", func(t *testing.T) {
		f := &fakeWAL{}
		w := newFakeStateWAL(t, f)
		f.flushErr = errInjected
		require.ErrorIs(t, w.Flush(), errInjected)
		requireBricked(t, w)
	})

	t.Run("bounds", func(t *testing.T) {
		f := &fakeWAL{}
		w := newFakeStateWAL(t, f)
		f.boundsErr = errInjected
		_, _, _, err := w.GetStoredRange()
		require.ErrorIs(t, err, errInjected)
		requireBricked(t, w)
	})

	t.Run("prune", func(t *testing.T) {
		f := &fakeWAL{}
		w := newFakeStateWAL(t, f)
		f.pruneErr = errInjected
		require.ErrorIs(t, w.Prune(1), errInjected)
		requireBricked(t, w)
	})

	t.Run("iterator", func(t *testing.T) {
		f := &fakeWAL{}
		w := newFakeStateWAL(t, f)
		f.iteratorErr = errInjected
		_, err := w.Iterator(0, 0)
		require.ErrorIs(t, err, errInjected)
		requireBricked(t, w)
	})
}

// TestAppendFailureDoesNotSilentlyAdvance verifies the Bugbot scenario: when the append for a block fails,
// the block is not silently finalized. The WAL is bricked, so a write to the next block is rejected rather
// than skipping the lost block.
func TestAppendFailureDoesNotSilentlyAdvance(t *testing.T) {
	f := &fakeWAL{appendErr: errInjected}
	w := newFakeStateWAL(t, f)

	require.NoError(t, w.Write(1, nil))
	require.ErrorIs(t, w.SignalEndOfBlock(), errInjected)

	require.ErrorIs(t, w.Write(2, nil), errInjected)
	require.Empty(t, f.appended)
}

// TestCallerViolationsDoNotBrick verifies that a caller-contract rejection (an out-of-order write) leaves
// the WAL fully usable, since it corrupts no state.
func TestCallerViolationsDoNotBrick(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	writeBlock(t, w, 5)
	require.Error(t, w.Write(4, nil)) // block numbers may not decrease

	// The WAL is not bricked: a subsequent valid write still succeeds and is durable.
	writeBlock(t, w, 6)
	require.NoError(t, w.Flush())
	ok, _, end, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(6), end)
}
