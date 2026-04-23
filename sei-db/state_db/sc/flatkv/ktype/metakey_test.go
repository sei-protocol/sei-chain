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
