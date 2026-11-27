package evidence

import (
	"math"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// poolWithPendingEvidence returns a pool holding n mock pending evidence entries.
func poolWithPendingEvidence(t *testing.T, n int) *Pool {
	t.Helper()
	ctx := t.Context()
	pool := NewPool(dbm.NewMemDB(), nil, nil, nil)
	pv := types.NewMockPV()
	for i := range n {
		ev, err := types.NewMockDuplicateVoteEvidenceWithValidator(ctx, int64(i+1), time.Unix(0, 0).UTC(), pv, "test_chain")
		require.NoError(t, err)
		require.NoError(t, pool.addPendingEvidence(ctx, ev))
	}
	return pool
}

func TestPendingEvidenceStopsAtEntryCap(t *testing.T) {
	pool := poolWithPendingEvidence(t, types.MaxEvidenceEntriesPerBlock+1)

	evs, _ := pool.PendingEvidence(-1)
	require.Len(t, evs, types.MaxEvidenceEntriesPerBlock)
}

func TestPendingEvidenceStopsAtByteCap(t *testing.T) {
	pool := poolWithPendingEvidence(t, types.MaxEvidenceEntriesPerBlock)

	evs, size := pool.PendingEvidence(math.MaxInt64)
	require.True(t, size <= types.MaxEvidenceBytesPerBlock)
	require.True(t, len(evs) < types.MaxEvidenceEntriesPerBlock)

	pb := make([]tmproto.Evidence, len(evs))
	for i, ev := range evs {
		p, err := types.EvidenceToProto(ev)
		require.NoError(t, err)
		pb[i] = *p
	}
	require.True(t, (&tmproto.EvidenceList{Evidence: pb}).Size() <= types.MaxEvidenceBytesPerBlock)
}
