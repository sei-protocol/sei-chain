package flatkv

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/stretchr/testify/require"
)

func TestIsMetaKey(t *testing.T) {
	require.True(t, isMetaKey(metaVersionKey))
	require.True(t, isMetaKey(metaLtHashKey))
	require.True(t, isMetaKey([]byte("_meta/future")))
	require.False(t, isMetaKey([]byte{0x00}))
	addr := ktype.Address{0x01}
	require.False(t, isMetaKey(addr[:]))
	require.False(t, isMetaKey(ktype.StorageKey(ktype.Address{0x01}, ktype.Slot{0x02})))
}
