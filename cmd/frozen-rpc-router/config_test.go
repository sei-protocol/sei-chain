package main

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseConfig(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--listen-address", "0.0.0.0:9000",
		"--live-node", "localhost:8545",
		"--frozen-node", "200=localhost:8547",
		"--frozen-node", "100=localhost:8546",
	}, io.Discard)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:9000", cfg.listenAddress)
	require.Equal(t, "localhost:8545", cfg.liveNode)

	nodes, err := parseFrozenNodes(cfg.frozenNodes)
	require.NoError(t, err)
	require.Equal(t, []frozenNodeConfig{
		{freezeHeight: 200, address: "localhost:8547"},
		{freezeHeight: 100, address: "localhost:8546"},
	}, nodes)
}

func TestParseConfigRejectsMissingLiveNode(t *testing.T) {
	_, err := parseConfig(nil, io.Discard)
	require.EqualError(t, err, "--live-node is required")
}

func TestParseFrozenNodesRejectsInvalidPairs(t *testing.T) {
	for _, value := range []string{"100", "=localhost:8545", "0=localhost:8545", "abc=localhost:8545", "9223372036854775808=localhost:8545", "100="} {
		t.Run(value, func(t *testing.T) {
			_, err := parseFrozenNodes(frozenNodeFlags{value})
			require.Error(t, err)
		})
	}
}
