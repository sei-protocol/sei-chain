package blockstore

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// TestWaitForBlockRecordServesTheStoredBytesOnlyAfterFlush verifies that a written block's record
// reaches WaitForBlockRecord, byte for byte, only once Flush has made it durable.
func TestWaitForBlockRecordServesTheStoredBytesOnlyAfterFlush(t *testing.T) {
	store, batches := newWaitTestStore(t)
	block := types.NewRoadBlock(batches[0].qc, batches[0].blocks)
	require.NoError(t, store.WriteBlock(block))

	_, err := store.WaitForBlockRecord(canceledContext(t), batches[0].first)
	require.ErrorIs(t, err, context.Canceled)

	require.NoError(t, store.Flush())
	got, err := store.WaitForBlockRecord(canceledContext(t), batches[0].first)
	require.NoError(t, err)
	require.Equal(t, encodeBlock(block).Value, got)
}

// TestWaitForAppQCRecordServesTheStoredBytesOnlyAfterFlush verifies that a written AppQC's record
// reaches WaitForAppQCRecord, byte for byte, only once a Flush after it has made it durable.
func TestWaitForAppQCRecordServesTheStoredBytesOnlyAfterFlush(t *testing.T) {
	store, batches := newWaitTestStore(t)
	_, keys := buildCommittee()
	writeBatch(t, store, batches[0])
	require.NoError(t, store.Flush())
	appQC := appQCForBatch(utils.TestRng(), keys, batches[0])
	require.NoError(t, store.WriteAppProposal(appQC.Proposal()))
	require.NoError(t, store.WriteAppQC(appQC))

	_, err := store.WaitForAppQCRecord(canceledContext(t), batches[0].first)
	require.ErrorIs(t, err, context.Canceled)

	require.NoError(t, store.Flush())
	got, err := store.WaitForAppQCRecord(canceledContext(t), batches[0].first)
	require.NoError(t, err)
	require.Equal(t, types.AppQCConv.Marshal(appQC), got)
}

// TestWaitForAppQCRecordWaitsForFlush verifies that a reader waiting on an AppQC record wakes up
// once a Flush makes it durable.
func TestWaitForAppQCRecordWaitsForFlush(t *testing.T) {
	store, batches := newWaitTestStore(t)
	_, keys := buildCommittee()
	appQC := appQCForBatch(utils.TestRng(), keys, batches[0])

	var got []byte
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			record, err := store.WaitForAppQCRecord(ctx, batches[0].first)
			got = record
			return err
		})
		if err := store.WriteBlock(types.NewRoadBlock(batches[0].qc, batches[0].blocks)); err != nil {
			return err
		}
		if err := store.WriteAppProposal(appQC.Proposal()); err != nil {
			return err
		}
		if err := store.WriteAppQC(appQC); err != nil {
			return err
		}
		return store.Flush()
	}))
	require.Equal(t, types.AppQCConv.Marshal(appQC), got)
}

// TestWaitForRecordsBelowFirstArePruned verifies that the record accessors refuse a road below the
// retention floor.
func TestWaitForRecordsBelowFirstArePruned(t *testing.T) {
	store, batches := newWaitTestStore(t)
	_, keys := buildCommittee()
	for _, b := range batches {
		writeBatch(t, store, b)
	}
	writeAppData(t, store, utils.TestRng(), keys, batches)
	require.NoError(t, store.Flush())
	require.NoError(t, store.PruneBefore(batches[1].first))

	_, err := store.WaitForBlockRecord(t.Context(), batches[0].first)
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = store.WaitForAppQCRecord(t.Context(), batches[0].first)
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = store.WaitForBlockRecord(t.Context(), batches[1].first)
	require.NoError(t, err)
	_, err = store.WaitForAppQCRecord(t.Context(), batches[1].first)
	require.NoError(t, err)
}

// TestWaitForRecordsAfterReopen verifies that a store reopened over existing data serves every
// stored record at once.
func TestWaitForRecordsAfterReopen(t *testing.T) {
	cfg := storeConfig(t, t.TempDir())
	first, err := New(cfg)
	require.NoError(t, err)
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	for _, b := range batches {
		writeBatch(t, first, b)
	}
	writeAppData(t, first, utils.TestRng(), keys, batches)
	require.NoError(t, first.Flush())
	require.NoError(t, first.Close())

	reopened, err := New(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	last := batches[len(batches)-1].first
	_, err = reopened.WaitForBlockRecord(canceledContext(t), last)
	require.NoError(t, err)
	_, err = reopened.WaitForAppQCRecord(canceledContext(t), last)
	require.NoError(t, err)
}
