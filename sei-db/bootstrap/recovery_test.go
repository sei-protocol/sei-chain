package bootstrap

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPlanRecovery pins the target and the rollback set for every shape of disagreement the
// stores can open at. The expectations are written out rather than derived, so a change to how
// the target is chosen shows up as a diff here.
func TestPlanRecovery(t *testing.T) {
	tests := []struct {
		name    string
		heights StoreHeights
		want    recoveryPlan
	}{
		{
			name:    "fresh node",
			heights: StoreHeights{ReceiptEnabled: true},
			want:    recoveryPlan{target: 0},
		},
		{
			name: "every store agrees",
			heights: StoreHeights{
				Block: 100, SC: 100, SS: 100, StateWAL: 100, Receipt: 100, ReceiptEnabled: true,
			},
			want: recoveryPlan{target: 100},
		},
		{
			name: "block ledger ahead of a settled state, which is the normal case",
			heights: StoreHeights{
				Block: 105, SC: 100, SS: 100, StateWAL: 100, Receipt: 100, ReceiptEnabled: true,
			},
			want: recoveryPlan{target: 100},
		},
		{
			name: "SC ahead of SS rolls SC back",
			heights: StoreHeights{
				Block: 105, SC: 100, SS: 99, StateWAL: 100, Receipt: 99, ReceiptEnabled: true,
			},
			want: recoveryPlan{target: 99, rollbackSC: true},
		},
		{
			name: "SS ahead of SC rolls SS back",
			heights: StoreHeights{
				Block: 105, SC: 99, SS: 100, StateWAL: 99, Receipt: 99, ReceiptEnabled: true,
			},
			want: recoveryPlan{target: 99, rollbackSS: true},
		},
		{
			name: "receipts ahead of state roll receipts back",
			heights: StoreHeights{
				Block: 105, SC: 100, SS: 100, StateWAL: 100, Receipt: 101, ReceiptEnabled: true,
			},
			want: recoveryPlan{target: 100, rollbackReceipt: true},
		},
		{
			name: "receipts behind state roll both halves of state back",
			heights: StoreHeights{
				Block: 105, SC: 100, SS: 100, StateWAL: 100, Receipt: 98, ReceiptEnabled: true,
			},
			want: recoveryPlan{target: 98, rollbackSC: true, rollbackSS: true},
		},
		{
			name: "receipts behind an already split state settle on the lowest of the three",
			heights: StoreHeights{
				Block: 105, SC: 100, SS: 99, StateWAL: 100, Receipt: 97, ReceiptEnabled: true,
			},
			want: recoveryPlan{target: 97, rollbackSC: true, rollbackSS: true},
		},
		{
			name: "receipts between the two state halves are still above the target",
			heights: StoreHeights{
				Block: 105, SC: 100, SS: 98, StateWAL: 100, Receipt: 99, ReceiptEnabled: true,
			},
			want: recoveryPlan{target: 98, rollbackSC: true, rollbackReceipt: true},
		},
		{
			name: "disabled receipts are ignored however far behind their old height is",
			heights: StoreHeights{
				Block: 105, SC: 100, SS: 100, StateWAL: 100, Receipt: 2, ReceiptEnabled: false,
			},
			want: recoveryPlan{target: 100},
		},
		{
			name: "a state WAL below the target is left to SC to replay over",
			heights: StoreHeights{
				Block: 105, SC: 100, SS: 100, StateWAL: 40, Receipt: 100, ReceiptEnabled: true,
			},
			want: recoveryPlan{target: 100},
		},
		{
			name: "a state WAL above the target is truncated by SC's own rollback",
			heights: StoreHeights{
				Block: 105, SC: 100, SS: 98, StateWAL: 100, Receipt: 98, ReceiptEnabled: true,
			},
			want: recoveryPlan{target: 98, rollbackSC: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := planRecovery(test.heights)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

// TestPlanRecoveryRefusals covers the disagreements no rollback closes, which have to stop the
// boot rather than resolve to a plan.
func TestPlanRecoveryRefusals(t *testing.T) {
	tests := []struct {
		name    string
		heights StoreHeights
		wantErr string
	}{
		{
			name:    "state above the block ledger",
			heights: StoreHeights{Block: 99, SC: 100, SS: 100, StateWAL: 100},
			wantErr: "the state commit is at height 100 but the block ledger only reaches 99",
		},
		{
			name:    "EVM state store above the block ledger",
			heights: StoreHeights{Block: 100, SC: 100, SS: 101, StateWAL: 100},
			wantErr: "the EVM state store is at height 101 but the block ledger only reaches 100",
		},
		{
			name:    "state WAL above the block ledger",
			heights: StoreHeights{Block: 100, SC: 100, SS: 100, StateWAL: 101},
			wantErr: "the state WAL is at height 101 but the block ledger only reaches 100",
		},
		{
			name: "receipts above the block ledger",
			heights: StoreHeights{
				Block: 100, SC: 100, SS: 100, StateWAL: 100, Receipt: 101, ReceiptEnabled: true,
			},
			wantErr: "the receipt store is at height 101 but the block ledger only reaches 100",
		},
		{
			name: "receipts above the block ledger are ignored when receipts are off",
			heights: StoreHeights{
				Block: 100, SC: 100, SS: 100, StateWAL: 100, Receipt: 101, ReceiptEnabled: false,
			},
			wantErr: "", // no refusal: the height belongs to a store that is not open
		},
		{
			name:    "an empty store below a populated one would roll back to genesis",
			heights: StoreHeights{Block: 100, SC: 100, SS: 0, StateWAL: 100},
			wantErr: "rolling back to height 0 discards state rather than recovering it",
		},
		{
			name: "empty receipts below populated state would roll back to genesis",
			heights: StoreHeights{
				Block: 100, SC: 100, SS: 100, StateWAL: 100, Receipt: 0, ReceiptEnabled: true,
			},
			wantErr: "rolling back to height 0 discards state rather than recovering it",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := planRecovery(test.heights)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

// rollbackRecorder is a store that can rewind, and records where it was asked to.
type rollbackRecorder struct {
	target int64
	calls  int
	err    error
}

func (r *rollbackRecorder) Rollback(target int64) error {
	r.target = target
	r.calls++
	return r.err
}

// noRollback is a store with no rewind, standing in for the EVM state store and the receipt
// store as they are today.
type noRollback struct{}

func TestRollbackTo(t *testing.T) {
	t.Run("rewinds a store that can", func(t *testing.T) {
		store := &rollbackRecorder{}
		require.NoError(t, rollbackTo("state commit", store, 98))
		require.Equal(t, int64(98), store.target)
		require.Equal(t, 1, store.calls)
	})

	t.Run("reports a store that cannot", func(t *testing.T) {
		err := rollbackTo("EVM state store", &noRollback{}, 98)
		require.ErrorIs(t, err, ErrRollbackUnsupported)
		require.ErrorContains(t, err, "EVM state store")
		require.ErrorContains(t, err, "state sync")
	})

	t.Run("wraps a failed rewind with the store and target", func(t *testing.T) {
		wanted := errors.New("no snapshot at or below target")
		err := rollbackTo("state commit", &rollbackRecorder{err: wanted}, 98)
		require.ErrorIs(t, err, wanted)
		require.ErrorContains(t, err, "roll state commit back to height 98")
	})
}

func TestAsHeight(t *testing.T) {
	require.Equal(t, uint64(0), asHeight(-1))
	require.Equal(t, uint64(0), asHeight(0))
	require.Equal(t, uint64(7), asHeight(7))
}
