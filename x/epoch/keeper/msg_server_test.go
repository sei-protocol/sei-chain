package keeper_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/epoch/keeper"
	"github.com/stretchr/testify/require"
)

func TestSetupMsgServer(t *testing.T) {
	k, _ := keepertest.EpochKeeper(t)
	msg := keeper.NewMsgServerImpl(*k)
	require.NotNil(t, msg)
}
