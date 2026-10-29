package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
)

// genNodeDir writes the files gen-autobahn-config reads for one node, plus
// evm_voter.txt when voter is set.
func genNodeDir(t *testing.T, name string, voter string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	valKey, err := atypes.TestSecretKey(atypes.NodeID(name)).Public().MarshalText()
	require.NoError(t, err)
	nodeKey, err := p2p.NodeSecretKey(ed25519.TestSecretKey([]byte(name))).Public().MarshalText()
	require.NoError(t, err)
	files := map[string]string{
		"validator_pubkey.txt": string(valKey),
		"node_pubkey.txt":      string(nodeKey),
		"autobahn_address.txt": "127.0.0.1:26656",
		"evmrpc_url.txt":       "ws://127.0.0.1:8546",
	}
	if voter != "" {
		files["evm_voter.txt"] = voter + "\n"
	}
	for file, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600))
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
	data, err := os.ReadFile(output) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	var cfg config.AutobahnFileConfig
	require.NoError(t, json.Unmarshal(data, &cfg))
	return cfg, nil
}

func TestGenAutobahnConfigWithEVMGovernance(t *testing.T) {
	voters := []string{"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266", "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"}
	cfg, err := runGenAutobahnConfig(t,
		"--evm-governance-voting-period", "40s",
		"--evm-governance-starting-proposal-id", "500",
		genNodeDir(t, "node0", voters[0]), genNodeDir(t, "node1", voters[1]))
	require.NoError(t, err)

	governance, ok := cfg.EVMGovernance.Get()
	require.True(t, ok)
	require.Equal(t, 40*time.Second, governance.Params.VotingPeriod.Duration())
	require.Equal(t, 40*time.Second, governance.Params.MaxDepositPeriod.Duration())
	require.Equal(t, 20*time.Second, governance.Params.ExpeditedVotingPeriod.Duration())
	require.Equal(t, uint64(500), governance.StartingProposalID)
	for i, v := range cfg.Validators {
		voter, ok := v.EVMVoter.Get()
		require.True(t, ok)
		require.Equal(t, common.HexToAddress(voters[i]), voter)
	}
	require.NoError(t, cfg.Validate())
}

func TestGenAutobahnConfigWithoutEVMGovernance(t *testing.T) {
	cfg, err := runGenAutobahnConfig(t, genNodeDir(t, "node0", "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"))
	require.NoError(t, err)
	require.False(t, cfg.EVMGovernance.IsPresent())
	require.False(t, cfg.Validators[0].EVMVoter.IsPresent(), "evm_voter.txt is ignored without governance")
}

func TestGenAutobahnConfigRejectsBadEVMGovernance(t *testing.T) {
	for name, args := range map[string][]string{
		"missing voter file": {"--evm-governance-voting-period", "40s", genNodeDir(t, "node0", "")},
		"bad voter":          {"--evm-governance-voting-period", "40s", genNodeDir(t, "node0", "not-an-address")},
		"shared voter": {"--evm-governance-voting-period", "40s",
			genNodeDir(t, "node0", "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"),
			genNodeDir(t, "node1", "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")},
		"period below two":      {"--evm-governance-voting-period", "1s", genNodeDir(t, "node0", "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")},
		"start without enabled": {"--evm-governance-starting-proposal-id", "5", genNodeDir(t, "node0", "")},
	} {
		_, err := runGenAutobahnConfig(t, args...)
		require.Error(t, err, name)
	}
}
