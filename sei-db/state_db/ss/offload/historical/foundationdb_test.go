package historical

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFoundationDBConfigDefaultsAndValidate(t *testing.T) {
	cfg := FoundationDBConfig{}
	cfg.ApplyDefaults()
	require.Equal(t, DefaultFoundationDBPrefix, cfg.Prefix)
	require.Equal(t, DefaultFoundationDBAPIVersion, cfg.APIVersion)
	require.Equal(t, DefaultFoundationDBShards, cfg.Shards)
	require.NoError(t, cfg.Validate())

	cfg.APIVersion = 1
	require.ErrorContains(t, cfg.Validate(), "api version")
	cfg.APIVersion = DefaultFoundationDBAPIVersion
	cfg.Shards = -1
	require.ErrorContains(t, cfg.Validate(), "shards")
}

func TestFoundationDBMutationKeyOrdersLatestVersionFirst(t *testing.T) {
	key40 := FoundationDBMutationKey("p", "bank", []byte("k1"), 40, 256)
	key60 := FoundationDBMutationKey("p", "bank", []byte("k1"), 60, 256)
	key80 := FoundationDBMutationKey("p", "bank", []byte("k1"), 80, 256)
	keys := [][]byte{key40, key80, key60}
	sort.Slice(keys, func(i, j int) bool { return string(keys[i]) < string(keys[j]) })
	require.Equal(t, [][]byte{key80, key60, key40}, keys)

	version, ok := FoundationDBVersionFromKey("p", key60)
	require.True(t, ok)
	require.Equal(t, int64(60), version)
	require.NotEqual(t,
		FoundationDBMutationKeyPrefix("p", "bank", []byte("k"), 256),
		FoundationDBMutationKeyPrefix("p", "bank", []byte("k1"), 256),
	)
}

func TestFoundationDBValueFromKeyValue(t *testing.T) {
	key := FoundationDBMutationKey("p", "bank", []byte("k"), 7, 256)
	value, err := FoundationDBValueFromKeyValue("p", key, FoundationDBMutationValue([]byte("value"), false))
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value.Bytes)
	require.Equal(t, int64(7), value.Version)
	value.Bytes[0] = 'V'

	value, err = FoundationDBValueFromKeyValue("p", key, FoundationDBMutationValue([]byte("value"), false))
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value.Bytes)

	_, err = FoundationDBValueFromKeyValue("p", key, FoundationDBMutationValue(nil, true))
	require.ErrorIs(t, err, ErrNotFound)
}
