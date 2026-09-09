package sview

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var _ view.View = (*stubView)(nil)

// stubView is a view whose Release outcome the test chooses. Only Name, Reserve and Release are
// implemented; every other method panics, so a use this stub was not written for is loud rather than
// silently wrong.
type stubView struct {
	// Reported by Name.
	name string

	// Returned by every Release call.
	releaseErr error

	// Counts Release calls.
	releaseCalls int
}

func (s *stubView) Name() string {
	return s.name
}

func (s *stubView) Release() error {
	s.releaseCalls++
	return s.releaseErr
}

func (s *stubView) Get(key []byte, updateLru bool) ([]byte, bool, error) {
	panic("stubView: unexpected Get")
}

func (s *stubView) BatchGet(keys [][]byte) (map[string][]byte, error) {
	panic("stubView: unexpected BatchGet")
}

func (s *stubView) GetDiff() (map[string][]byte, error) {
	panic("stubView: unexpected GetDiff")
}

func (s *stubView) Reserve() error {
	return nil
}

func (s *stubView) Finalize(writes []*proto.KVPair) error {
	panic("stubView: unexpected Finalize")
}

func (s *stubView) AwaitFlush(ctx context.Context) error {
	panic("stubView: unexpected AwaitFlush")
}

// bricksOnRelease builds a store view over stubs that all fail to release, and returns the stubs so a
// test can count the attempts.
func bricksOnRelease(t *testing.T, version int64) (*StoreView, map[string]*stubView) {
	t.Helper()
	stubs := make(map[string]*stubView, len(dataDBDirs))
	for _, name := range dataDBDirs {
		stubs[name] = &stubView{name: name, releaseErr: errors.New("view manager is bricked")}
	}
	blockView, err := NewStoreView(version,
		stubs[accountDBDir], stubs[codeDBDir], stubs[storageDBDir], stubs[miscDBDir])
	require.NoError(t, err)
	return blockView, stubs
}

// requireBalanced asserts every reservation taken on these views was released. A reservation left
// held stalls its store's flushes forever, and one released twice bricks its manager.
func requireBalanced(t *testing.T, stubs map[string]*fakeView) {
	t.Helper()
	for name, stub := range stubs {
		require.Equal(t, stub.reserves.Load(), stub.releases.Load(),
			"%s: took %d reservations and released %d",
			name, stub.reserves.Load(), stub.releases.Load())
	}
}

// A nil view would surface much later as a panic inside whichever operation happened to walk it, so
// construction rejects one and names the store it was missing.
func TestNewStoreViewRejectsNilViews(t *testing.T) {
	present := func() (view.View, view.View, view.View, view.View) {
		return &fakeView{name: accountDBDir}, &fakeView{name: codeDBDir},
			&fakeView{name: storageDBDir}, &fakeView{name: miscDBDir}
	}

	account, code, storage, misc := present()
	_, err := NewStoreView(1, nil, code, storage, misc)
	require.ErrorContains(t, err, "account view is nil")

	account, code, storage, misc = present()
	_, err = NewStoreView(1, account, nil, storage, misc)
	require.ErrorContains(t, err, "code view is nil")

	account, code, storage, misc = present()
	_, err = NewStoreView(1, account, code, nil, misc)
	require.ErrorContains(t, err, "storage view is nil")

	account, code, storage, misc = present()
	_, err = NewStoreView(1, account, code, storage, nil)
	require.ErrorContains(t, err, "misc view is nil")
}

// A view manager error is unrecoverable, so reserve reports the first one and stops rather than
// unwinding. What it still holds does not matter: the node cannot continue.
func TestStoreViewReserveStopsAtFirstFailure(t *testing.T) {
	bad := &fakeView{name: codeDBDir, reserveErr: errors.New("manager is bricked")}
	rest := &fakeView{name: storageDBDir}

	blockView, err := NewStoreView(1, &fakeView{name: accountDBDir}, bad, rest, &fakeView{name: miscDBDir})
	require.NoError(t, err)

	err = blockView.Reserve()
	require.Error(t, err)
	require.ErrorContains(t, err, "manager is bricked")
	require.ErrorContains(t, err, "reserve code view at height 1", "the error must name the store that failed")

	require.Zero(t, rest.reserves.Load(), "reserve must stop at the failure rather than carry on")
}

// Same contract on the way back: the first failure is reported and the walk stops.
func TestStoreViewReleaseStopsAtFirstFailure(t *testing.T) {
	blockView, stubs := bricksOnRelease(t, 1)

	err := blockView.Release()
	require.Error(t, err, "a failed release must be returned, not swallowed")
	require.ErrorContains(t, err, "view manager is bricked")

	attempted := 0
	for _, stub := range stubs {
		attempted += stub.releaseCalls
	}
	require.Equal(t, 1, attempted, "release must stop at the first failure rather than attempt the rest")
}
