package core

import (
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

func TestABCIInfoWithoutApplication(t *testing.T) {
	env := &Environment{}
	_, err := env.ABCIInfo(t.Context())
	require.ErrorIs(t, err, ErrABCIUnavailable)
}

func TestCheckTxWithoutApplication(t *testing.T) {
	env := &Environment{}
	_, err := env.CheckTx(t.Context(), &coretypes.RequestCheckTx{Tx: []byte{1}})
	require.ErrorIs(t, err, ErrABCIUnavailable)
}

func TestABCIQueryWithoutApplication(t *testing.T) {
	env := &Environment{}
	_, err := env.ABCIQuery(t.Context(), &coretypes.RequestABCIQuery{})
	require.ErrorIs(t, err, ErrABCIUnavailable)
}

func TestStatusWithoutApplication(t *testing.T) {
	env := newStatusEnv(t, statusEnvCase{initialHeight: 1})
	env.App = utils.None[*proxy.Proxy]()
	result, err := env.Status(t.Context())
	require.ErrorIs(t, err, ErrABCIUnavailable)
	require.Nil(t, result)
}

func TestABCIWithApplication(t *testing.T) {
	env := &Environment{App: utils.Some(proxy.New(abci.BaseApplication{}))}
	info, err := env.ABCIInfo(t.Context())
	require.NoError(t, err)
	require.NotNil(t, info)
	checked, err := env.CheckTx(t.Context(), &coretypes.RequestCheckTx{Tx: []byte{1}})
	require.NoError(t, err)
	require.NotNil(t, checked)
	queried, err := env.ABCIQuery(t.Context(), &coretypes.RequestABCIQuery{})
	require.NoError(t, err)
	require.NotNil(t, queried)
}
