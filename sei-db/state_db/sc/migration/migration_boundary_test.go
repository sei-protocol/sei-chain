package migration

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrationBoundaryNotStarted(t *testing.T) {
	mb := MigrationBoundaryNotStarted
	require.False(t, mb.IsMigrated("bank", []byte("a")))
	require.False(t, mb.IsMigrated("staking", []byte("z")))
	require.False(t, mb.IsMigrated("", nil))
}

func TestMigrationBoundaryComplete(t *testing.T) {
	mb := MigrationBoundaryComplete
	require.True(t, mb.IsMigrated("bank", []byte("a")))
	require.True(t, mb.IsMigrated("staking", []byte("z")))
	require.True(t, mb.IsMigrated("", nil))
}

func TestMigrationBoundaryInProgress(t *testing.T) {
	mb := NewMigrationBoundary("mymod", []byte("m"))

	require.True(t, mb.IsMigrated("mymod", []byte("a")), "key before boundary should be migrated")
	require.True(t, mb.IsMigrated("mymod", []byte("m")), "key equal to boundary should be migrated")

	require.False(t, mb.IsMigrated("mymod", []byte("n")), "key after boundary should not be migrated")
	require.False(t, mb.IsMigrated("mymod", []byte("z")), "key well after boundary should not be migrated")
}

func TestMigrationBoundaryEmptyKey(t *testing.T) {
	mb := NewMigrationBoundary("mod", []byte{})
	require.True(t, mb.IsMigrated("mod", []byte{}))
	require.False(t, mb.IsMigrated("mod", []byte{0x00}))
	require.False(t, mb.IsMigrated("mod", []byte("a")))
}

func TestMigrationBoundaryNilKey(t *testing.T) {
	mb := NewMigrationBoundary("mod", nil)
	require.True(t, mb.IsMigrated("mod", nil))
	require.True(t, mb.IsMigrated("mod", []byte{}))
	require.False(t, mb.IsMigrated("mod", []byte{0x00}))
}

func TestMigrationBoundaryBinaryKeys(t *testing.T) {
	mb := NewMigrationBoundary("mod", []byte{0x80})
	require.True(t, mb.IsMigrated("mod", []byte{0x00}))
	require.True(t, mb.IsMigrated("mod", []byte{0x7F}))
	require.True(t, mb.IsMigrated("mod", []byte{0x80}))
	require.False(t, mb.IsMigrated("mod", []byte{0x81}))
	require.False(t, mb.IsMigrated("mod", []byte{0xFF}))
}

func TestMigrationBoundarySharedPrefix(t *testing.T) {
	mb := NewMigrationBoundary("mod", []byte("abc"))
	require.True(t, mb.IsMigrated("mod", []byte("abc")), "exact match")
	require.True(t, mb.IsMigrated("mod", []byte("abb")), "last byte less")
	require.True(t, mb.IsMigrated("mod", []byte("ab")), "prefix is shorter, so less")
	require.False(t, mb.IsMigrated("mod", []byte("abd")), "last byte greater")
	require.False(t, mb.IsMigrated("mod", []byte("abcd")), "key is longer extension")
}

func TestMigrationBoundaryModuleOrdering(t *testing.T) {
	mb := NewMigrationBoundary("gov", []byte("proposal_42"))

	require.True(t, mb.IsMigrated("bank", []byte("anything")),
		"module before boundary module is fully migrated")
	require.True(t, mb.IsMigrated("auth", nil),
		"earlier module with nil key is migrated")
	require.True(t, mb.IsMigrated("gov", []byte("proposal_42")),
		"boundary module at boundary key is migrated")
	require.True(t, mb.IsMigrated("gov", []byte("proposal_41")),
		"boundary module with earlier key is migrated")

	require.False(t, mb.IsMigrated("gov", []byte("proposal_43")),
		"boundary module with later key is not migrated")
	require.False(t, mb.IsMigrated("staking", []byte("a")),
		"module after boundary module is not migrated")
	require.False(t, mb.IsMigrated("zzz", nil),
		"module well after boundary module is not migrated")
}

func TestMigrationBoundaryEmptyModuleName(t *testing.T) {
	mb := NewMigrationBoundary("", []byte("key"))

	require.True(t, mb.IsMigrated("", []byte("key")), "same empty module, exact key")
	require.True(t, mb.IsMigrated("", []byte("abc")), "same empty module, earlier key")
	require.False(t, mb.IsMigrated("", []byte("zzz")), "same empty module, later key")
	require.False(t, mb.IsMigrated("anymod", []byte("a")),
		"any non-empty module is after empty module")
}

func TestMigrationBoundaryEmptyModuleNameAndKey(t *testing.T) {
	mb := NewMigrationBoundary("", nil)

	require.True(t, mb.IsMigrated("", nil), "empty module nil key")
	require.True(t, mb.IsMigrated("", []byte{}), "empty module empty key")
	require.False(t, mb.IsMigrated("", []byte{0x00}), "empty module, any non-empty key is after")
	require.False(t, mb.IsMigrated("a", nil), "non-empty module is after empty module")
}

func TestMigrationBoundaryInvalidStatusPanics(t *testing.T) {
	mb := MigrationBoundary{status: MigrationStatus(99)}
	require.Panics(t, func() {
		mb.IsMigrated("mod", []byte("x"))
	})
}

// --- Serialization tests ---

func TestSerializeNotStarted(t *testing.T) {
	mb := MigrationBoundaryNotStarted
	data := mb.Serialize()
	require.Equal(t, []byte{byte(MigrationNotStarted)}, data)

	got, err := DeserializeMigrationBoundary(data)
	require.NoError(t, err)
	require.Equal(t, MigrationNotStarted, got.Status())
	require.Empty(t, got.ModuleName())
	require.Nil(t, got.Key())
}

func TestSerializeComplete(t *testing.T) {
	mb := MigrationBoundaryComplete
	data := mb.Serialize()
	require.Equal(t, []byte{byte(MigrationComplete)}, data)

	got, err := DeserializeMigrationBoundary(data)
	require.NoError(t, err)
	require.Equal(t, MigrationComplete, got.Status())
	require.Empty(t, got.ModuleName())
	require.Nil(t, got.Key())
}

func TestSerializeInProgress(t *testing.T) {
	mb := NewMigrationBoundary("bank", []byte("hello"))
	data := mb.Serialize()

	expected := []byte{byte(MigrationInProgress), 0x00, 0x00, 0x00, 0x04}
	expected = append(expected, []byte("bank")...)
	expected = append(expected, []byte("hello")...)
	require.Equal(t, expected, data)

	got, err := DeserializeMigrationBoundary(data)
	require.NoError(t, err)
	require.Equal(t, MigrationInProgress, got.Status())
	require.Equal(t, "bank", got.ModuleName())
	require.Equal(t, []byte("hello"), got.Key())
}

func TestSerializeInProgressEmptyKey(t *testing.T) {
	mb := NewMigrationBoundary("mod", []byte{})
	data := mb.Serialize()

	expected := []byte{byte(MigrationInProgress), 0x00, 0x00, 0x00, 0x03}
	expected = append(expected, []byte("mod")...)
	require.Equal(t, expected, data)

	got, err := DeserializeMigrationBoundary(data)
	require.NoError(t, err)
	require.Equal(t, MigrationInProgress, got.Status())
	require.Equal(t, "mod", got.ModuleName())
	require.Empty(t, got.Key())
}

func TestSerializeInProgressEmptyModuleName(t *testing.T) {
	mb := NewMigrationBoundary("", []byte("key"))
	data := mb.Serialize()

	expected := []byte{byte(MigrationInProgress), 0x00, 0x00, 0x00, 0x00}
	expected = append(expected, []byte("key")...)
	require.Equal(t, expected, data)

	got, err := DeserializeMigrationBoundary(data)
	require.NoError(t, err)
	require.Equal(t, MigrationInProgress, got.Status())
	require.Empty(t, got.ModuleName())
	require.Equal(t, []byte("key"), got.Key())
}

func TestSerializeInProgressEmptyBoth(t *testing.T) {
	mb := NewMigrationBoundary("", []byte{})
	data := mb.Serialize()
	require.Equal(t, []byte{byte(MigrationInProgress), 0x00, 0x00, 0x00, 0x00}, data)

	got, err := DeserializeMigrationBoundary(data)
	require.NoError(t, err)
	require.Equal(t, MigrationInProgress, got.Status())
	require.Empty(t, got.ModuleName())
	require.Empty(t, got.Key())
}

func TestSerializeRoundTripPreservesBehavior(t *testing.T) {
	original := NewMigrationBoundary("gov", []byte("m"))
	data := original.Serialize()
	restored, err := DeserializeMigrationBoundary(data)
	require.NoError(t, err)

	cases := []struct {
		mod string
		key []byte
	}{
		{"bank", []byte("a")},
		{"gov", []byte("a")},
		{"gov", []byte("m")},
		{"gov", []byte("n")},
		{"staking", []byte("z")},
	}
	for _, tc := range cases {
		require.Equal(t, original.IsMigrated(tc.mod, tc.key), restored.IsMigrated(tc.mod, tc.key),
			"IsMigrated mismatch for module %q key %q", tc.mod, tc.key)
	}
}

func TestSerializeKeyIsACopy(t *testing.T) {
	mb := NewMigrationBoundary("mod", []byte("abc"))
	data := mb.Serialize()
	got, err := DeserializeMigrationBoundary(data)
	require.NoError(t, err)

	got.Key()[0] = 'z'
	nameLen := int(binary.BigEndian.Uint32(data[1:5]))
	require.Equal(t, byte('a'), data[5+nameLen], "mutating deserialized key must not affect serialized data")
}

func TestDeserializeEmptyData(t *testing.T) {
	_, err := DeserializeMigrationBoundary([]byte{})
	require.Error(t, err)
}

func TestDeserializeNilData(t *testing.T) {
	_, err := DeserializeMigrationBoundary(nil)
	require.Error(t, err)
}

func TestDeserializeInvalidStatus(t *testing.T) {
	_, err := DeserializeMigrationBoundary([]byte{99})
	require.Error(t, err)
}

func TestDeserializeNotStartedWithTrailingData(t *testing.T) {
	_, err := DeserializeMigrationBoundary([]byte{byte(MigrationNotStarted), 0xFF})
	require.Error(t, err)
}

func TestDeserializeCompleteWithTrailingData(t *testing.T) {
	_, err := DeserializeMigrationBoundary([]byte{byte(MigrationComplete), 0xFF})
	require.Error(t, err)
}

func TestDeserializeInProgressTooShortForLength(t *testing.T) {
	_, err := DeserializeMigrationBoundary([]byte{byte(MigrationInProgress)})
	require.Error(t, err, "only status byte, missing length bytes")

	_, err = DeserializeMigrationBoundary([]byte{byte(MigrationInProgress), 0x00, 0x00})
	require.Error(t, err, "only 2 of 4 length bytes")

	_, err = DeserializeMigrationBoundary([]byte{byte(MigrationInProgress), 0x00, 0x00, 0x00})
	require.Error(t, err, "only 3 of 4 length bytes")
}

func TestDeserializeInProgressTruncatedModuleName(t *testing.T) {
	data := []byte{byte(MigrationInProgress), 0x00, 0x00, 0x00, 0x05, 'a', 'b'}
	_, err := DeserializeMigrationBoundary(data)
	require.Error(t, err, "declared name length 5 but only 2 bytes available")
}

func TestString(t *testing.T) {
	notStarted := MigrationBoundaryNotStarted
	require.Equal(t,
		"MigrationBoundary{status=not_started}",
		notStarted.String())

	complete := MigrationBoundaryComplete
	require.Equal(t,
		"MigrationBoundary{status=complete}",
		complete.String())

	inProgress := NewMigrationBoundary("bank", []byte("hello"))
	require.Equal(t,
		"MigrationBoundary{status=in_progress, module=bank, key=68656c6c6f}",
		inProgress.String())

	nilKey := NewMigrationBoundary("gov", nil)
	require.Equal(t,
		"MigrationBoundary{status=in_progress, module=gov, key=}",
		nilKey.String())

	binaryKey := NewMigrationBoundary("staking", []byte{0x00, 0xff, 0x80})
	require.Equal(t,
		"MigrationBoundary{status=in_progress, module=staking, key=00ff80}",
		binaryKey.String())
}

func TestSerializeInvalidStatusPanics(t *testing.T) {
	mb := MigrationBoundary{status: MigrationStatus(99)}
	require.Panics(t, func() {
		mb.Serialize()
	})
}
