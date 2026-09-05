package flatkv

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests use the fakeView stub and the fakeViews helper defined in snapshot_writer_test.go, and
// requireBalanced from store_view_test.go.

// An atomic store view with nothing in it would force every later call to answer "no view", which is
// the case the constructor exists to rule out.
func TestNewAtomicStoreViewRequiresAView(t *testing.T) {
	_, err := newAtomicStoreView(nil)
	require.ErrorContains(t, err, "initial view is nil")
}

// A failed set must leave the installed view exactly as it was. Handing the old view back before the
// new one is secured would strand readers on a view at zero reservations, which can no longer be
// reserved and so can no longer be read.
func TestAtomicStoreViewSetKeepsInstalledViewWhenReserveFails(t *testing.T) {
	installed, installedStubs := fakeViews(t, 1)
	asv, err := newAtomicStoreView(installed)
	require.NoError(t, err)

	bad, err := newStoreView(2,
		&fakeView{name: accountDBDir},
		&fakeView{name: codeDBDir, reserveErr: errors.New("manager is bricked")},
		&fakeView{name: storageDBDir},
		&fakeView{name: miscDBDir})
	require.NoError(t, err)

	require.ErrorContains(t, asv.set(bad), "manager is bricked")

	blockView, err := asv.get()
	require.NoError(t, err, "the installed view must still be readable after a failed set")
	require.Equal(t, int64(1), blockView.blockHeight, "the installed view must not have been displaced")
	require.NoError(t, blockView.release())

	require.NoError(t, asv.Close())
	requireBalanced(t, installedStubs)
}

// Readers must never see the chain move backwards, so the installed height only ever advances. Going
// backwards means building a new atomic store view, which is what the startup-lifecycle seals do.
func TestAtomicStoreViewRefusesToGoBackwards(t *testing.T) {
	installed, _ := fakeViews(t, 5)
	asv, err := newAtomicStoreView(installed)
	require.NoError(t, err)

	for _, height := range []int64{4, 5} {
		earlier, stubs := fakeViews(t, height)
		require.ErrorContains(t, asv.set(earlier), "view height must advance",
			"height %d is not above the installed height 5", height)
		for name, stub := range stubs {
			require.Zero(t, stub.reserves.Load(), "%s: a refused view must not be reserved", name)
		}
	}

	later, laterStubs := fakeViews(t, 6)
	require.NoError(t, asv.set(later))

	require.NoError(t, asv.Close())
	requireBalanced(t, laterStubs)
}

// Close is the terminal release: it hands back the reservation the atomic store view owns, which is
// what lets the view managers underneath it shut down. Nothing may be handed out afterwards.
func TestAtomicStoreViewIsUnusableAfterClose(t *testing.T) {
	installed, stubs := fakeViews(t, 1)
	asv, err := newAtomicStoreView(installed)
	require.NoError(t, err)

	require.NoError(t, asv.Close())
	require.NoError(t, asv.Close(), "Close must be idempotent")

	_, err = asv.get()
	require.ErrorContains(t, err, "closed")

	later, _ := fakeViews(t, 2)
	require.ErrorContains(t, asv.set(later), "closed")

	requireBalanced(t, stubs)
	for name, stub := range stubs {
		require.Equal(t, int64(1), stub.releases.Load(),
			"%s: a second Close must not hand the reservation back twice", name)
	}
}

// The execution thread advances the view while other threads read it. A reader is not promised any
// particular height, but it is promised that what it is handed is reserved and readable — so the
// reservation has to be taken while the view is still pinned, not after it has been handed over.
func TestAtomicStoreViewServesReadersWhileAdvancing(t *testing.T) {
	const blocks = 200
	const readers = 8

	initial, initialStubs := fakeViews(t, 1)
	asv, err := newAtomicStoreView(initial)
	require.NoError(t, err)

	// Built up front so the writer goroutine does nothing but install them.
	blockViews := make([]*storeView, 0, blocks)
	allStubs := []map[string]*fakeView{initialStubs}
	for height := int64(2); height <= blocks; height++ {
		blockView, stubs := fakeViews(t, height)
		blockViews = append(blockViews, blockView)
		allStubs = append(allStubs, stubs)
	}

	failures := make(chan error, readers+1)
	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stop)
		for _, blockView := range blockViews {
			if err := asv.set(blockView); err != nil {
				failures <- fmt.Errorf("set height %d: %w", blockView.blockHeight, err)
				return
			}
		}
	}()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				blockView, err := asv.get()
				if err != nil {
					failures <- fmt.Errorf("get: %w", err)
					return
				}
				if blockView.blockHeight < 1 {
					failures <- fmt.Errorf("reader was handed height %d", blockView.blockHeight)
					return
				}
				if err := blockView.release(); err != nil {
					failures <- fmt.Errorf("release height %d: %w", blockView.blockHeight, err)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}

	require.NoError(t, asv.Close())
	for _, stubs := range allStubs {
		requireBalanced(t, stubs)
	}
}
