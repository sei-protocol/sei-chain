package migration

import (
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// writeMigrationMeta commits the given migration version and/or boundary
// to the store's MigrationStore, mimicking the records the
// MigrationManager persists during a migration.
func writeMigrationMeta(t *testing.T, s flatkv.Store, version *uint64, boundaryBytes []byte) {
	t.Helper()
	var pairs []*proto.KVPair
	if version != nil {
		versionBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(versionBytes, *version)
		pairs = append(pairs, &proto.KVPair{Key: []byte(MigrationVersionKey), Value: versionBytes})
	}
	if boundaryBytes != nil {
		pairs = append(pairs, &proto.KVPair{Key: []byte(MigrationBoundaryKey), Value: boundaryBytes})
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{{
		Name:      MigrationStore,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}}))
	_, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
}

func uintPtr(v uint64) *uint64 { return &v }

func TestDeriveWriteMode(t *testing.T) {
	inFlightBoundary := NewMigrationBoundary("evm", []byte{0x01})
	inFlight := inFlightBoundary.Serialize()
	notStarted := MigrationBoundaryNotStarted.Serialize()

	cases := []struct {
		name     string
		version  *uint64
		boundary []byte
		want     types.WriteMode
		wantErr  bool
	}{
		{name: "fresh store", want: types.MemiavlOnly},
		{name: "v0 in-flight", boundary: inFlight, want: types.MigrateEVM},
		{name: "v0 persisted NotStarted boundary", boundary: notStarted, want: types.MemiavlOnly},
		{name: "v1 steady", version: uintPtr(Version1_MigrateEVM), want: types.EVMMigrated},
		{name: "v1 in-flight", version: uintPtr(Version1_MigrateEVM), boundary: inFlight, want: types.MigrateAllButBank},
		{name: "v2 steady", version: uintPtr(Version2_MigrateAllButBank), want: types.AllMigratedButBank},
		{name: "v2 in-flight", version: uintPtr(Version2_MigrateAllButBank), boundary: inFlight, want: types.MigrateBank},
		{name: "v3 steady", version: uintPtr(Version3_FlatKVOnly), want: types.FlatKVOnly},
		{name: "v3 in-flight is corrupt", version: uintPtr(Version3_FlatKVOnly), boundary: inFlight, wantErr: true},
		{name: "unknown version", version: uintPtr(4), wantErr: true},
		{name: "corrupt boundary bytes", boundary: []byte{0xFF, 0xFF}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewTestFlatKVCommitStore(t, t.TempDir())
			if tc.version != nil || tc.boundary != nil {
				writeMigrationMeta(t, s, tc.version, tc.boundary)
			}
			got, err := DeriveWriteMode(s)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestDeriveWriteModeNilStore(t *testing.T) {
	_, err := DeriveWriteMode(nil)
	require.Error(t, err)
}

func TestIsModeComplete(t *testing.T) {
	steadyModes := []types.WriteMode{
		types.MemiavlOnly, types.EVMMigrated, types.AllMigratedButBank, types.FlatKVOnly,
	}
	for _, mode := range steadyModes {
		// Steady-state modes are complete without consulting the DB; a nil
		// store proves no read happens.
		complete, err := IsModeComplete(nil, mode)
		require.NoError(t, err)
		require.True(t, complete, "steady-state mode %q must always be complete", mode)
	}

	cases := []struct {
		name    string
		mode    types.WriteMode
		version *uint64
		want    bool
	}{
		{name: "MigrateEVM no version key", mode: types.MigrateEVM, want: false},
		{name: "MigrateEVM at target", mode: types.MigrateEVM, version: uintPtr(Version1_MigrateEVM), want: true},
		{name: "MigrateEVM past target", mode: types.MigrateEVM, version: uintPtr(Version2_MigrateAllButBank), want: true},
		{name: "MigrateAllButBank below target", mode: types.MigrateAllButBank, version: uintPtr(Version1_MigrateEVM), want: false},
		{name: "MigrateAllButBank at target", mode: types.MigrateAllButBank, version: uintPtr(Version2_MigrateAllButBank), want: true},
		{name: "MigrateBank below target", mode: types.MigrateBank, version: uintPtr(Version2_MigrateAllButBank), want: false},
		{name: "MigrateBank at target", mode: types.MigrateBank, version: uintPtr(Version3_FlatKVOnly), want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewTestFlatKVCommitStore(t, t.TempDir())
			if tc.version != nil {
				writeMigrationMeta(t, s, tc.version, nil)
			}
			complete, err := IsModeComplete(s, tc.mode)
			require.NoError(t, err)
			require.Equal(t, tc.want, complete)
		})
	}
}

func TestIsModeCompleteNilStoreForMigrationMode(t *testing.T) {
	_, err := IsModeComplete(nil, types.MigrateEVM)
	require.Error(t, err)
}
