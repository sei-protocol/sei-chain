package state

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/stretchr/testify/require"
)

func TestAddLog(t *testing.T) {
	k, _, ctx := keeper.MockEVMKeeper()
	statedb := NewStateDBImpl(ctx, k)

	logs, err := statedb.GetLogs()
	require.Nil(t, err)
	require.Empty(t, logs)

	log1 := ethtypes.Log{Address: common.BytesToAddress([]byte{1}), Topics: []common.Hash{}, Data: []byte{}}
	statedb.AddLog(&log1)
	require.Nil(t, statedb.err)
	logs, err = statedb.GetLogs()
	require.Nil(t, err)
	require.Equal(t, 1, len(logs))
	require.Equal(t, log1, *logs[0])

	log2 := ethtypes.Log{Address: common.BytesToAddress([]byte{2}), Topics: []common.Hash{}, Data: []byte{3}}
	statedb.AddLog(&log2)
	require.Nil(t, statedb.err)
	logs, err = statedb.GetLogs()
	require.Nil(t, err)
	require.Equal(t, 2, len(logs))
	require.Equal(t, log1, *logs[0])
	require.Equal(t, log2, *logs[1])
}
