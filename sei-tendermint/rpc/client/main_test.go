package client_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	rpctest "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/test"
)

func NodeSuite(ctx context.Context, t *testing.T) (service.Service, *config.Config) {
	t.Helper()

	return nodeSuiteWithMode(ctx, t, config.ModeValidator)
}

func FullNodeSuite(ctx context.Context, t *testing.T) (service.Service, *config.Config) {
	t.Helper()

	return nodeSuiteWithMode(ctx, t, config.ModeFull)
}

func nodeSuiteWithMode(ctx context.Context, t *testing.T, mode string) (service.Service, *config.Config) {
	t.Helper()

	ctx, cancel := context.WithCancel(ctx)

	conf, err := rpctest.CreateConfig(t, t.Name())
	require.NoError(t, err)
	conf.Mode = mode

	app := kvstore.NewApplication()

	// start a tendermint node in the background to test against.
	node, closer, err := rpctest.StartTendermint(ctx, conf, app, rpctest.SuppressStdout)
	require.NoError(t, err)
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, closer(ctx))
		assert.NoError(t, app.Close())
		node.Wait()
	})
	return node, conf
}
