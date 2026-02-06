package mock

import (
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/stretchr/testify/require"
)

// SetupApp returns an application as well as a clean-up function
// to be used to quickly setup a test case with an app
func SetupApp(t *testing.T) abci.Application {
	logger, err := log.NewDefaultLogger(
		log.LogFormatText,
		"info",
	)
	require.NoError(t, err)
	rootDir := t.TempDir()

	app, err := NewApp(rootDir, logger)
	require.NoError(t, err)
	return app
}
