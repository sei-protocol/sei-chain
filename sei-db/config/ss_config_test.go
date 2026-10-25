package config

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
)

func TestAlignSSSnapshotWithSC(t *testing.T) {
	scConfig := DefaultStateCommitConfig()
	ssConfig := DefaultStateStoreConfig()
	ssConfig.SnapshotEnable = true

	scConfig.MemIAVLConfig.SnapshotInterval = 123
	scConfig.MemIAVLConfig.SnapshotKeepRecent = 4
	scConfig.MemIAVLConfig.SnapshotMinTimeInterval = 17

	AlignSSSnapshotWithSC(scConfig, &ssConfig)

	require.Equal(t, int64(123), ssConfig.SnapshotInterval)
	require.Equal(t, 4, ssConfig.SnapshotKeepRecent)
	require.Equal(t, 17*time.Second, ssConfig.SnapshotMinTimeInterval)
}

func TestAlignSSSnapshotWithSCHealsZeroToSCDefaults(t *testing.T) {
	scConfig := DefaultStateCommitConfig()
	ssConfig := DefaultStateStoreConfig()
	ssConfig.SnapshotEnable = true

	scConfig.MemIAVLConfig.SnapshotInterval = 0
	scConfig.MemIAVLConfig.SnapshotKeepRecent = 0

	AlignSSSnapshotWithSC(scConfig, &ssConfig)

	require.Equal(t, int64(memiavl.DefaultSnapshotInterval), ssConfig.SnapshotInterval)
	require.Equal(t, memiavl.DefaultSnapshotKeepRecent, ssConfig.SnapshotKeepRecent)
	require.Equal(
		t,
		time.Duration(memiavl.DefaultSnapshotMinTimeInterval)*time.Second,
		ssConfig.SnapshotMinTimeInterval,
	)
}

func TestDefaultStateStoreConfigDisablesSnapshots(t *testing.T) {
	require.False(t, DefaultStateStoreConfig().SnapshotEnable,
		"snapshots require an explicit ss-snapshot-enable opt-in")
}

// A zero cadence is what the snapshot manager reads as "do not run", so the
// off switch has to zero it rather than mirror SC's.
func TestAlignSSSnapshotWithSCZeroesCadenceWhenDisabled(t *testing.T) {
	scConfig := DefaultStateCommitConfig()
	scConfig.MemIAVLConfig.SnapshotInterval = 123
	scConfig.MemIAVLConfig.SnapshotKeepRecent = 4
	scConfig.MemIAVLConfig.SnapshotMinTimeInterval = 17

	ssConfig := DefaultStateStoreConfig()
	ssConfig.SnapshotEnable = false

	AlignSSSnapshotWithSC(scConfig, &ssConfig)

	require.Zero(t, ssConfig.SnapshotInterval)
	require.Zero(t, ssConfig.SnapshotKeepRecent)
	require.Zero(t, ssConfig.SnapshotMinTimeInterval)
}

// FlatKV and SS both mirror memIAVL's cadence, and they must resolve it
// identically or the two backends drift onto different snapshot heights.
func TestAlignSSSnapshotMatchesEffectiveMemIAVLCadence(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		interval, keepRecent uint32
		minTime              uint32
		wantInterval         int64
		wantMinTime          time.Duration
	}{
		{
			name: "explicit", interval: 500, keepRecent: 3, minTime: 45,
			wantInterval: 500, wantMinTime: 45 * time.Second,
		},
		{
			name: "zero heals to default", interval: 0, keepRecent: 0,
			wantInterval: memiavl.DefaultSnapshotInterval,
			wantMinTime:  time.Duration(memiavl.DefaultSnapshotMinTimeInterval) * time.Second,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scConfig := DefaultStateCommitConfig()
			scConfig.MemIAVLConfig.SnapshotInterval = tc.interval
			scConfig.MemIAVLConfig.SnapshotKeepRecent = tc.keepRecent
			scConfig.MemIAVLConfig.SnapshotMinTimeInterval = tc.minTime

			ssConfig := DefaultStateStoreConfig()
			ssConfig.SnapshotEnable = true
			AlignSSSnapshotWithSC(scConfig, &ssConfig)

			wantInterval, wantKeepRecent := EffectiveMemIAVLSnapshotCadence(scConfig.MemIAVLConfig)
			require.Equal(t, int64(wantInterval), ssConfig.SnapshotInterval)
			require.Equal(t, int(wantKeepRecent), ssConfig.SnapshotKeepRecent)
			require.Equal(t, tc.wantInterval, ssConfig.SnapshotInterval)
			require.Equal(t, tc.wantMinTime, ssConfig.SnapshotMinTimeInterval)
		})
	}
}
