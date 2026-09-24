package giga

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Where each store is put is decided once, from what the stores hold, so every rule the open follows is
// visible here without opening anything.
func TestPlanRecovery(t *testing.T) {
	snapshots := []uint64{0, 4, 8}
	for _, tc := range []struct {
		name       string
		survey     recoverySurvey
		rollbackTo uint64
		depth      uint64
		want       recoveryPlan
	}{
		{
			name: "a plain open puts every store on the WAL's head",
			survey: recoverySurvey{
				walStored:     true,
				walFirst:      1,
				walLast:       10,
				vaultRecorded: true,
				vaultHead:     10,
				scSnapshots:   snapshots,
			},
			want: recoveryPlan{head: 10, scTarget: 10, ssTarget: 10},
		},
		{
			name: "an empty WAL moves nothing",
			survey: recoverySurvey{
				vaultRecorded: true,
				vaultHead:     10,
				scSnapshots:   snapshots,
			},
			want: recoveryPlan{},
		},
		{
			name: "a rollback puts every store on its target",
			survey: recoverySurvey{
				walStored:     true,
				walFirst:      1,
				walLast:       10,
				vaultRecorded: true,
				vaultHead:     10,
				scSnapshots:   snapshots,
			},
			rollbackTo: 6,
			want:       recoveryPlan{head: 6, scTarget: 6, ssTarget: 6, rollback: true},
		},
		{
			name: "a vault ahead of the head keeps its hashes and moves nothing",
			survey: recoverySurvey{
				walStored:     true,
				walFirst:      1,
				walLast:       10,
				vaultRecorded: true,
				vaultHead:     12,
				scSnapshots:   snapshots,
			},
			want: recoveryPlan{head: 10, scTarget: 10, ssTarget: 10},
		},
		{
			name: "a vault behind the head puts SC alone on the vault's newest block",
			survey: recoverySurvey{
				walStored:     true,
				walFirst:      1,
				walLast:       10,
				vaultRecorded: true,
				vaultHead:     7,
				scSnapshots:   snapshots,
			},
			want: recoveryPlan{head: 10, scTarget: 7, ssTarget: 10},
		},
		{
			name: "a vault behind a rollback target is caught up the same way",
			survey: recoverySurvey{
				walStored:     true,
				walFirst:      1,
				walLast:       10,
				vaultRecorded: true,
				vaultHead:     3,
				scSnapshots:   snapshots,
			},
			rollbackTo: 6,
			want:       recoveryPlan{head: 6, scTarget: 3, ssTarget: 6, rollback: true},
		},
		{
			name: "a vault holding only block 0 is caught up from 1",
			survey: recoverySurvey{
				walStored:     true,
				walFirst:      1,
				walLast:       10,
				vaultRecorded: true,
				vaultHead:     0,
				scSnapshots:   snapshots,
			},
			want: recoveryPlan{head: 10, scTarget: 1, ssTarget: 10},
		},
		{
			name: "an empty vault puts SC the configured depth below the head",
			survey: recoverySurvey{
				walStored:   true,
				walFirst:    1,
				walLast:     10,
				scSnapshots: snapshots,
			},
			depth: 3,
			want:  recoveryPlan{head: 10, scTarget: 7, ssTarget: 10},
		},
		{
			name: "an empty vault with no depth configured moves nothing",
			survey: recoverySurvey{
				walStored:   true,
				walFirst:    1,
				walLast:     10,
				scSnapshots: snapshots,
			},
			want: recoveryPlan{head: 10, scTarget: 10, ssTarget: 10},
		},
		{
			name: "an empty vault's rewind is shortened to the lowest snapshot the WAL replays from",
			survey: recoverySurvey{
				walStored:   true,
				walFirst:    5,
				walLast:     10,
				scSnapshots: snapshots,
			},
			depth: 1000,
			want:  recoveryPlan{head: 10, scTarget: 4, ssTarget: 10, emptyVaultRefillShortened: true},
		},
		{
			name: "an empty vault with no replayable snapshot records only the loaded block",
			survey: recoverySurvey{
				walStored: true,
				walFirst:  9,
				walLast:   10,
				scSnapshots: []uint64{0,
					4},
			},
			depth: 3,
			want:  recoveryPlan{head: 10, scTarget: 10, ssTarget: 10, emptyVaultRefillUnreachable: true},
		},
		{
			name: "a rollback that empties the WAL leaves nothing to refill the vault from",
			survey: recoverySurvey{
				walStored:   true,
				walFirst:    5,
				walLast:     10,
				scSnapshots: snapshots,
			},
			rollbackTo: 4,
			depth:      3,
			want:       recoveryPlan{head: 4, scTarget: 4, ssTarget: 4, rollback: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := planRecovery(tc.survey, tc.rollbackTo, tc.depth)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// A rollback above the WAL's head is a target no replay reaches, refused before anything is planned.
func TestPlanRecoveryRefusesARollbackAboveTheWAL(t *testing.T) {
	_, err := planRecovery(recoverySurvey{walStored: true, walFirst: 1, walLast: 3}, 5, 0)
	require.ErrorContains(t, err, "the state WAL ends at 3")
}
