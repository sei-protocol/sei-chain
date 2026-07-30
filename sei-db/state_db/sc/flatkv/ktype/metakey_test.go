package ktype

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsMetaKey(t *testing.T) {
	require.True(t, IsMetaKey(MetaVersionKey))
	require.True(t, IsMetaKey(MetaLtHashKey))
	require.True(t, IsMetaKey([]byte("_meta/future")))
	require.False(t, IsMetaKey([]byte{0x00}))
	addr := Address{0x01}
	require.False(t, IsMetaKey(addr[:]))
	require.False(t, IsMetaKey(StorageKey(Address{0x01}, Slot{0x02})))
}

func TestModuleMetaKeyRoundTrip(t *testing.T) {
	for _, module := range []string{"evm", "gov", "bank", "x"} {
		hashKey := ModuleLtHashKey(module)
		statsKey := ModuleStatsKey(module)
		require.True(t, IsMetaKey(hashKey))
		require.True(t, IsMetaKey(statsKey))

		gotHash, ok := ParseModuleLtHashKey(hashKey)
		require.True(t, ok)
		require.Equal(t, module, gotHash)

		gotStats, ok := ParseModuleStatsKey(statsKey)
		require.True(t, ok)
		require.Equal(t, module, gotStats)

		// The two key spaces must not alias: a hash key is never parsed as a
		// stats key and vice versa, even though they share the "_meta/x:" prefix.
		_, ok = ParseModuleStatsKey(hashKey)
		require.False(t, ok, "hash key must not parse as stats key")
		_, ok = ParseModuleLtHashKey(statsKey)
		require.False(t, ok, "stats key must not parse as hash key")
	}
}

func TestParseModuleMetaKeyRejectsMalformed(t *testing.T) {
	for _, key := range [][]byte{
		[]byte("_meta/version"),           // fixed per-DB key
		[]byte("_meta/x:/hash"),           // empty module name
		[]byte("_meta/x:evm"),             // missing suffix
		[]byte("_meta/x:evm/other"),       // wrong suffix
		StorageKey(Address{0x01}, Slot{}), // user data
	} {
		_, ok := ParseModuleLtHashKey(key)
		require.False(t, ok, "hash parse should reject %q", key)
		_, ok = ParseModuleStatsKey(key)
		require.False(t, ok, "stats parse should reject %q", key)
	}
	// Empty-module stats key is also rejected.
	_, ok := ParseModuleStatsKey([]byte("_meta/x:/stats"))
	require.False(t, ok)
}
