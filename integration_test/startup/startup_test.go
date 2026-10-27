// Package startup is the seitest migration of startup_test.yaml: it brings up
// an in-process 4-validator network and asserts the same two facts the YAML
// checked over seid/jq — validator count and reported network — through the
// Go SDK instead.
//
// Env:
//   - SEI_SEITEST_RUN_INTEGRATION=1 to run. Otherwise the test skips, so
//     `go test ./...` stays cheap while seitest is still under construction.
package startup

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/integration_test/seitest"
)

func TestStartup(t *testing.T) {
	if os.Getenv("SEI_SEITEST_RUN_INTEGRATION") == "" {
		t.Skip("set SEI_SEITEST_RUN_INTEGRATION=1 to run")
	}

	validators := seitest.NewValidators(t, 4)
	val1 := validators[0]

	require.Len(t, val1.Tendermint().ValidatorSet(), 4)
	require.Equal(t, val1.ChainID(), val1.Tendermint().NodeInfo().Network)
}
