package mock

import (
	"testing"

	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
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
