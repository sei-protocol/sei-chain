package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
)

func writeGenAutobahnNodeDir(t *testing.T, i int, voter string) string {
	t.Helper()
	dir := t.TempDir()
	valKey, err := atypes.SecretKeyFromED25519(ed25519.TestSecretKey(fmt.Appendf(nil, "validator-%d", i))).Public().MarshalText()
	require.NoError(t, err)
	nodeKey, err := p2p.NodeSecretKey(ed25519.TestSecretKey(fmt.Appendf(nil, "node-%d", i))).Public().MarshalText()
	require.NoError(t, err)
	files := map[string]string{
		"validator_pubkey.txt": string(valKey),
		"node_pubkey.txt":      string(nodeKey),
		"autobahn_address.txt": fmt.Sprintf("localhost:%d", 26660+i),
		"evmrpc_url.txt":       fmt.Sprintf("http://localhost:%d", 8545+i),
	}
	if voter != "" {
		files["evm_voter.txt"] = voter + "\n"
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
	}
	return dir
}

func runGenAutobahnConfig(t *testing.T, args ...string) (config.AutobahnFileConfig, error) {
	t.Helper()
	output := filepath.Join(t.TempDir(), "autobahn.json")
	cmd := MakeGenAutobahnConfigCommand()
	cmd.SetArgs(append([]string{"--output", output}, args...))
	if err := cmd.Execute(); err != nil {
		return config.AutobahnFileConfig{}, err
	}
	data, err := os.ReadFile(output) //nolint:gosec // G304: test output path.
	require.NoError(t, err)
	var fc config.AutobahnFileConfig
	require.NoError(t, json.Unmarshal(data, &fc))
	require.NoError(t, fc.Validate())
	return fc, nil
}

func TestGenAutobahnConfigWithoutGovernance(t *testing.T) {
	fc, err := runGenAutobahnConfig(t,
		writeGenAutobahnNodeDir(t, 0, "0x00000000000000000000000000000000000000a1"),
		writeGenAutobahnNodeDir(t, 1, ""),
	)
	require.NoError(t, err)
	require.False(t, fc.EVMGovernance.IsPresent())
	for _, v := range fc.Validators {
		require.False(t, v.EVMVoter.IsPresent())
	}
}

func TestGenAutobahnConfigWithGovernance(t *testing.T) {
	voters := []string{
		"0x00000000000000000000000000000000000000a1",
		"0x00000000000000000000000000000000000000B2",
	}
	fc, err := runGenAutobahnConfig(t, "--evm-governance-voting-period", "90s",
		writeGenAutobahnNodeDir(t, 0, voters[0]),
		writeGenAutobahnNodeDir(t, 1, voters[1]),
	)
	require.NoError(t, err)
	params, ok := fc.EVMGovernance.Get()
	require.True(t, ok)
	want := gov.DefaultParams()
	want.VotingPeriod = 90
	require.Equal(t, want, params)
	require.Len(t, fc.Validators, len(voters))
	for i, v := range fc.Validators {
		voter, ok := v.EVMVoter.Get()
		require.True(t, ok)
		require.Equal(t, common.HexToAddress(voters[i]), voter)
	}
}

func TestGenAutobahnConfigRejectsBadGovernanceInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		period string
		voters []string
	}{
		{"missing_voter", "90s", []string{"0x00000000000000000000000000000000000000a1", ""}},
		{"malformed_voter", "90s", []string{"0x00000000000000000000000000000000000000a1", "0x1234"}},
		{"zero_voter", "90s", []string{"0x00000000000000000000000000000000000000a1", "0x0000000000000000000000000000000000000000"}},
		{"shared_voter", "90s", []string{"0x00000000000000000000000000000000000000a1", "0x00000000000000000000000000000000000000A1"}},
		{"fractional_period", "1500ms", []string{"0x00000000000000000000000000000000000000a1", "0x00000000000000000000000000000000000000b2"}},
		{"negative_period", "-1s", []string{"0x00000000000000000000000000000000000000a1", "0x00000000000000000000000000000000000000b2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"--evm-governance-voting-period", tc.period}
			for i, voter := range tc.voters {
				args = append(args, writeGenAutobahnNodeDir(t, i, voter))
			}
			_, err := runGenAutobahnConfig(t, args...)
			require.Error(t, err)
		})
	}
}

func TestBuildGenEVMGovernance(t *testing.T) {
	disabled, err := buildGenEVMGovernance(0)
	require.NoError(t, err)
	require.False(t, disabled.IsPresent())
	enabled, err := buildGenEVMGovernance(time.Hour)
	require.NoError(t, err)
	params, ok := enabled.Get()
	require.True(t, ok)
	require.Equal(t, uint64(3600), params.VotingPeriod)
}
