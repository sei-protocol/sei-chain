package mock

import (
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
)

// SetupApp returns an application as well as a clean-up function
// to be used to quickly setup a test case with an app
func SetupApp(t *testing.T) abci.Application {
	rootDir := t.TempDir()
	app, err := NewApp(rootDir)
	require.NoError(t, err)
	return app
}
