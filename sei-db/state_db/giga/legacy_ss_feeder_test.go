package giga

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// fakeLegacySSStore records the blocks a feeder hands it, and fails one on demand. Only the feeder's goroutine
// touches it until the feeder is closed.
type fakeLegacySSStore struct {
	// The versions handed over, in order.
	versions []int64
	// The changesets handed over, in order.
	changesets [][]*proto.NamedChangeSet
	// The version CommitBlock fails at, or 0 for none.
	failAt int64
}

func (s *fakeLegacySSStore) CommitBlock(version int64, changesets []*proto.NamedChangeSet) error {
	if version == s.failAt {
		return errors.New("store is bricked")
	}
	s.versions = append(s.versions, version)
	s.changesets = append(s.changesets, changesets)
	return nil
}

// SS replays nothing from the commit path on its own, so a block delivered out of order, or not at all, is history
// served wrong until the next restart rebuilds it.
func TestLegacySSFeederHandsEveryBlockOverInOrder(t *testing.T) {
	store := &fakeLegacySSStore{}
	feeder := newLegacySSFeeder(store)
	rng := rand.New(rand.NewSource(1))

	var want [][]*proto.NamedChangeSet
	// More blocks than the queue holds, so some feeds wait on the goroutine.
	for block := int64(1); block <= 3*legacySSFeederQueueDepth; block++ {
		shape := changeSetShape{accounts: 4, slots: 8, misc: 2, addressPool: 8, slotPool: 2}
		changes := randomStateChangeSet(rng, shape)
		want = append(want, encodeLegacyChangeSets(changes))
		require.NoError(t, feeder.Feed(block, changes))
	}
	require.NoError(t, feeder.Close())

	require.Len(t, store.versions, 3*legacySSFeederQueueDepth)
	for i, version := range store.versions {
		require.Equal(t, int64(i)+1, version)
	}
	require.Equal(t, want, store.changesets)
}

// A block SS failed to take leaves SS behind for good, so every later commit has to report it rather than only the
// one that happened to be in flight.
func TestLegacySSFeederReportsAFailureToEveryLaterFeed(t *testing.T) {
	store := &fakeLegacySSStore{failAt: 2}
	feeder := newLegacySSFeeder(store)

	require.NoError(t, feeder.Feed(1, &gigatypes.StateChangeSet{}))
	require.NoError(t, feeder.Feed(2, &gigatypes.StateChangeSet{}))
	<-feeder.failed

	require.ErrorContains(t, feeder.Feed(3, &gigatypes.StateChangeSet{}), "commit block 2")
	require.ErrorContains(t, feeder.Feed(4, &gigatypes.StateChangeSet{}), "commit block 2")
	require.ErrorContains(t, feeder.Close(), "commit block 2")
	require.Equal(t, []int64{1}, store.versions)
}
