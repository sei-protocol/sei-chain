package evidence

import (
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestPendingEvidenceStopsAtEntryCap(t *testing.T) {
	ctx := t.Context()
	pool := NewPool(dbm.NewMemDB(), nil, nil, nil)
	pv := types.NewMockPV()
	for i := range types.MaxEvidenceEntriesPerBlock + 1 {
		ev, err := types.NewMockDuplicateVoteEvidenceWithValidator(ctx, int64(i+1), time.Unix(0, 0).UTC(), pv, "test_chain")
		require.NoError(t, err)
		require.NoError(t, pool.addPendingEvidence(ctx, ev))
	}

	evs, _ := pool.PendingEvidence(-1)
	require.Len(t, evs, types.MaxEvidenceEntriesPerBlock)
}
