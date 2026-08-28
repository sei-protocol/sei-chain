package main

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseConfig(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--listen-address", "0.0.0.0:9000",
		"--live-node", "localhost:8545",
		"--frozen-node", "200=localhost:8547",
		"--frozen-node", "100=localhost:8546",
		"--max-block-reference-depth", "32",
		"--batch-request-limit", "50",
		"--write-timeout", "45s",
	}, io.Discard)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:9000", cfg.listenAddress)
	require.Equal(t, "localhost:8545", cfg.liveNode)
	require.Equal(t, 32, cfg.maxBlockReferenceDepth)
	require.Equal(t, 50, cfg.batchRequestLimit)
	require.Equal(t, 45*time.Second, cfg.writeTimeout)

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

func TestParseConfigRejectsNonPositiveBlockReferenceDepth(t *testing.T) {
	_, err := parseConfig([]string{"--live-node", "localhost:8545", "--max-block-reference-depth", "0"}, io.Discard)
	require.EqualError(t, err, "--max-block-reference-depth must be positive")
}

func TestParseConfigUsesResourceLimitDefaults(t *testing.T) {
	cfg, err := parseConfig([]string{"--live-node", "localhost:8545"}, io.Discard)
	require.NoError(t, err)
	require.Equal(t, defaultBatchRequestLimit, cfg.batchRequestLimit)
	require.Equal(t, 30*time.Second, cfg.writeTimeout)
}

func TestParseConfigRejectsNonPositiveResourceLimits(t *testing.T) {
	testCases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "batch request limit",
			args:    []string{"--batch-request-limit", "0"},
			wantErr: "--batch-request-limit must be positive",
		},
		{
			name:    "write timeout",
			args:    []string{"--write-timeout", "0s"},
			wantErr: "--write-timeout must be positive",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			args := append([]string{"--live-node", "localhost:8545"}, testCase.args...)
			_, err := parseConfig(args, io.Discard)
			require.EqualError(t, err, testCase.wantErr)
		})
	}
}

func TestParseFrozenNodesRejectsInvalidPairs(t *testing.T) {
	for _, value := range []string{"100", "=localhost:8545", "0=localhost:8545", "abc=localhost:8545", "9223372036854775808=localhost:8545", "100="} {
		t.Run(value, func(t *testing.T) {
			_, err := parseFrozenNodes(frozenNodeFlags{value})
			require.Error(t, err)
		})
	}
}
