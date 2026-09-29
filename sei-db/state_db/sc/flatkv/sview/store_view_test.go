package sview

import (
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/stretchr/testify/require"
)

// bricksOnRelease builds a store view over stubs that all fail to release, and returns the stubs so a
// test can count the attempts.
func bricksOnRelease(t *testing.T, version int64) (*StoreView, map[string]*fakeView) {
	t.Helper()
	stubs := make(map[string]*fakeView, len(dataDBDirs))
	for _, name := range dataDBDirs {
		stubs[name] = &fakeView{name: name, releaseErr: errors.New("view manager is bricked")}
	}
	blockView, err := storeViewOver(version,
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
	account := typedFakeView[vtype.AccountData]{&fakeView{name: accountDBDir}}
	code := typedFakeView[vtype.CodeData]{&fakeView{name: codeDBDir}}
	storage := typedFakeView[vtype.StorageData]{&fakeView{name: storageDBDir}}
	misc := typedFakeView[vtype.MiscData]{&fakeView{name: miscDBDir}}

	_, err := NewStoreView(1, nil, code, storage, misc)
	require.ErrorContains(t, err, "account view is nil")

	_, err = NewStoreView(1, account, nil, storage, misc)
	require.ErrorContains(t, err, "code view is nil")

	_, err = NewStoreView(1, account, code, nil, misc)
	require.ErrorContains(t, err, "storage view is nil")

	_, err = NewStoreView(1, account, code, storage, nil)
	require.ErrorContains(t, err, "misc view is nil")
}

// A view manager error is unrecoverable, so reserve reports the first one and stops rather than
// unwinding. What it still holds does not matter: the node cannot continue.
func TestStoreViewReserveStopsAtFirstFailure(t *testing.T) {
	bad := &fakeView{name: codeDBDir, reserveErr: errors.New("manager is bricked")}
	rest := &fakeView{name: storageDBDir}

	blockView, err := storeViewOver(1, &fakeView{name: accountDBDir}, bad, rest, &fakeView{name: miscDBDir})
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

	var attempted int64
	for _, stub := range stubs {
		attempted += stub.releases.Load()
	}
	require.Equal(t, int64(1), attempted, "release must stop at the first failure rather than attempt the rest")
}
