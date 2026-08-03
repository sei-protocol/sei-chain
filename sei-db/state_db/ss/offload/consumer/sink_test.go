package consumer

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestDecodeEntry(t *testing.T) {
	entry := &proto.ChangelogEntry{Version: 7}
	payload, err := entry.Marshal()
	require.NoError(t, err)
	got, err := DecodeEntry(payload)
	require.NoError(t, err)
	require.Equal(t, int64(7), got.Version)
}
