package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func requireCommitQCEqual(t *testing.T, want, got *types.CommitQC) {
	t.Helper()
	require.True(t, proto.Equal(types.CommitQCConv.Encode(want), types.CommitQCConv.Encode(got)))
}
