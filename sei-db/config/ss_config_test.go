package config

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
)

func TestAlignSSSnapshotWithSC(t *testing.T) {
	scConfig := DefaultStateCommitConfig()
	ssConfig := DefaultStateStoreConfig()

	scConfig.MemIAVLConfig.SnapshotInterval = 123
	scConfig.MemIAVLConfig.SnapshotKeepRecent = 4

	AlignSSSnapshotWithSC(scConfig, &ssConfig)

	require.Equal(t, int64(123), ssConfig.SnapshotInterval)
	require.Equal(t, 4, ssConfig.SnapshotKeepRecent)
}

func TestAlignSSSnapshotWithSCHealsZeroToSCDefaults(t *testing.T) {
	scConfig := DefaultStateCommitConfig()
	ssConfig := DefaultStateStoreConfig()

	scConfig.MemIAVLConfig.SnapshotInterval = 0
	scConfig.MemIAVLConfig.SnapshotKeepRecent = 0

	AlignSSSnapshotWithSC(scConfig, &ssConfig)

	require.Equal(t, int64(memiavl.DefaultSnapshotInterval), ssConfig.SnapshotInterval)
	require.Equal(t, memiavl.DefaultSnapshotKeepRecent, ssConfig.SnapshotKeepRecent)
}

func TestDefaultStateStoreConfigEnablesSnapshots(t *testing.T) {
	require.True(t, DefaultStateStoreConfig().SnapshotEnable,
		"snapshots default on; the off switch is ss-snapshot-enable")
}

// A zero cadence is what the snapshot manager reads as "do not run", so the
// off switch has to zero it rather than mirror SC's.
func TestAlignSSSnapshotWithSCZeroesCadenceWhenDisabled(t *testing.T) {
	scConfig := DefaultStateCommitConfig()
	scConfig.MemIAVLConfig.SnapshotInterval = 123
	scConfig.MemIAVLConfig.SnapshotKeepRecent = 4

	ssConfig := DefaultStateStoreConfig()
	ssConfig.SnapshotEnable = false

	AlignSSSnapshotWithSC(scConfig, &ssConfig)

	require.Zero(t, ssConfig.SnapshotInterval)
	require.Zero(t, ssConfig.SnapshotKeepRecent)
}

// FlatKV and SS both mirror memIAVL's cadence, and they must resolve it
// identically or the two backends drift onto different snapshot heights.
func TestAlignSSSnapshotMatchesEffectiveMemIAVLCadence(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		interval, keepRecent uint32
		wantInterval         int64
	}{
		{name: "explicit", interval: 500, keepRecent: 3, wantInterval: 500},
		{name: "zero heals to default", interval: 0, keepRecent: 0, wantInterval: memiavl.DefaultSnapshotInterval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scConfig := DefaultStateCommitConfig()
			scConfig.MemIAVLConfig.SnapshotInterval = tc.interval
			scConfig.MemIAVLConfig.SnapshotKeepRecent = tc.keepRecent

			ssConfig := DefaultStateStoreConfig()
			AlignSSSnapshotWithSC(scConfig, &ssConfig)

			wantInterval, wantKeepRecent := EffectiveMemIAVLSnapshotCadence(scConfig.MemIAVLConfig)
			require.Equal(t, int64(wantInterval), ssConfig.SnapshotInterval)
			require.Equal(t, int(wantKeepRecent), ssConfig.SnapshotKeepRecent)
			require.Equal(t, tc.wantInterval, ssConfig.SnapshotInterval)
		})
	}
}
