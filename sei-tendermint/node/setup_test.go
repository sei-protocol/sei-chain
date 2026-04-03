package node

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func makeTestNodeKey(seed []byte) types.NodeKey {
	return types.NodeKey(ed25519.TestSecretKey(seed))
}

func makeTestValidatorEntry(seed []byte, hostport string) string {
	key := ed25519.TestSecretKey(seed).Public()
	return hex.EncodeToString(key.Bytes()) + "@" + hostport
}

func TestBuildGigaConfig_Disabled(t *testing.T) {
	cfg := config.DefaultAutobahnConfig()
	nodeKey := makeTestNodeKey([]byte("test-node-key"))

	result, err := buildGigaConfig(cfg, nodeKey, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result, "should return nil when autobahn is disabled")
}

func TestBuildGigaConfig_NilConfig(t *testing.T) {
	nodeKey := makeTestNodeKey([]byte("test-node-key"))

	result, err := buildGigaConfig(nil, nodeKey, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestBuildGigaConfig_EnabledWithValidators(t *testing.T) {
	v1 := makeTestValidatorEntry([]byte("node1-seed"), "localhost:26660")
	v2 := makeTestValidatorEntry([]byte("node2-seed"), "peer1.example.com:26661")
	v3 := makeTestValidatorEntry([]byte("node3-seed"), "peer2.example.com:26662")

	nodeKey := makeTestNodeKey([]byte("node1-seed"))
	genDoc := &types.GenesisDoc{
		ChainID:       "test-chain",
		InitialHeight: 1,
	}

	cfg := &config.AutobahnConfig{
		Enable:             true,
		Validators:         v1 + "," + v2 + "," + v3,
		MaxGasPerBlock:     50_000_000,
		MaxTxsPerBlock:     5_000,
		MaxTxsPerSecond:    1_000,
		MempoolSize:        20_000,
		BlockInterval:      200 * time.Millisecond,
		AllowEmptyBlocks:   true,
		ViewTimeout:        3 * time.Second,
		PersistentStateDir: "/tmp/autobahn-state",
		DialInterval:       5 * time.Second,
	}

	result, err := buildGigaConfig(cfg, nodeKey, nil, genDoc)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify ValidatorAddrs has all 3 entries.
	assert.Len(t, result.ValidatorAddrs, 3, "all validators should be in ValidatorAddrs")

	// Verify DialInterval propagated.
	assert.Equal(t, 5*time.Second, result.DialInterval)

	// Verify ConsensusConfig fields.
	require.NotNil(t, result.Consensus)
	assert.Equal(t, 3*time.Second, result.Consensus.ViewTimeout(atypes.View{}))
	persistDir, ok := result.Consensus.PersistentStateDir.Get()
	require.True(t, ok, "PersistentStateDir should be set")
	assert.Equal(t, "/tmp/autobahn-state", persistDir)

	// Verify ProducerConfig fields.
	require.NotNil(t, result.Producer)
	assert.Equal(t, uint64(50_000_000), result.Producer.MaxGasPerBlock)
	assert.Equal(t, uint64(5_000), result.Producer.MaxTxsPerBlock)
	maxTps, ok := result.Producer.MaxTxsPerSecond.Get()
	require.True(t, ok, "MaxTxsPerSecond should be set when > 0")
	assert.Equal(t, uint64(1_000), maxTps)
	assert.Equal(t, uint64(20_000), result.Producer.MempoolSize)
	assert.Equal(t, 200*time.Millisecond, result.Producer.BlockInterval)
	assert.True(t, result.Producer.AllowEmptyBlocks)

	// Verify App and GenDoc are passed through.
	assert.Equal(t, genDoc, result.GenDoc)
}

func TestBuildGigaConfig_ZeroMaxTxsPerSecond(t *testing.T) {
	v1 := makeTestValidatorEntry([]byte("node-seed"), "localhost:26660")
	nodeKey := makeTestNodeKey([]byte("node-seed"))

	cfg := &config.AutobahnConfig{
		Enable:          true,
		Validators:      v1,
		MaxGasPerBlock:  30_000_000,
		MaxTxsPerBlock:  10_000,
		MaxTxsPerSecond: 0,
		MempoolSize:     10_000,
		BlockInterval:   400 * time.Millisecond,
		ViewTimeout:     5 * time.Second,
		DialInterval:    10 * time.Second,
	}

	result, err := buildGigaConfig(cfg, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Producer.MaxTxsPerSecond.IsPresent(), "MaxTxsPerSecond should be None when 0")
}

func TestBuildGigaConfig_EmptyPersistentStateDir(t *testing.T) {
	v1 := makeTestValidatorEntry([]byte("node-seed"), "localhost:26660")
	nodeKey := makeTestNodeKey([]byte("node-seed"))

	cfg := &config.AutobahnConfig{
		Enable:             true,
		Validators:         v1,
		MaxGasPerBlock:     30_000_000,
		MaxTxsPerBlock:     10_000,
		MempoolSize:        10_000,
		BlockInterval:      400 * time.Millisecond,
		ViewTimeout:        5 * time.Second,
		PersistentStateDir: "",
		DialInterval:       10 * time.Second,
	}

	result, err := buildGigaConfig(cfg, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Consensus.PersistentStateDir.IsPresent(), "PersistentStateDir should be None when empty")
}

func TestBuildGigaConfig_InvalidValidatorFormat(t *testing.T) {
	nodeKey := makeTestNodeKey([]byte("node-seed"))

	tests := []struct {
		name       string
		validators string
	}{
		{"missing @", "abcdef0123456789"},
		{"invalid hex", "notahex@localhost:26660"},
		{"missing port", "aabb@localhost"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.AutobahnConfig{
				Enable:         true,
				Validators:     tc.validators,
				MaxGasPerBlock: 30_000_000,
				MaxTxsPerBlock: 10_000,
				MempoolSize:    10_000,
				BlockInterval:  400 * time.Millisecond,
				ViewTimeout:    5 * time.Second,
				DialInterval:   10 * time.Second,
			}
			_, err := buildGigaConfig(cfg, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
			assert.Error(t, err, "should fail for invalid validators format: %s", tc.name)
		})
	}
}

func TestBuildGigaConfig_EnabledNoValidators(t *testing.T) {
	nodeKey := makeTestNodeKey([]byte("node-seed"))
	cfg := &config.AutobahnConfig{
		Enable:     true,
		Validators: "",
	}
	_, err := buildGigaConfig(cfg, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
	assert.Error(t, err, "should fail when enabled but no validators")
}

func TestBuildGigaConfig_DuplicateValidator(t *testing.T) {
	v1 := makeTestValidatorEntry([]byte("node-seed"), "localhost:26660")
	v1dup := makeTestValidatorEntry([]byte("node-seed"), "localhost:26661")
	nodeKey := makeTestNodeKey([]byte("node-seed"))

	cfg := &config.AutobahnConfig{
		Enable:         true,
		Validators:     v1 + "," + v1dup,
		MaxGasPerBlock: 30_000_000,
		MaxTxsPerBlock: 10_000,
		MempoolSize:    10_000,
		BlockInterval:  400 * time.Millisecond,
		ViewTimeout:    5 * time.Second,
		DialInterval:   10 * time.Second,
	}
	_, err := buildGigaConfig(cfg, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
	assert.Error(t, err, "should fail when duplicate pubkey in validators")
	assert.Contains(t, err.Error(), "duplicate")
}

func TestBuildGigaConfig_SelfNotInValidators(t *testing.T) {
	// Only include a different node in the validator set.
	v1 := makeTestValidatorEntry([]byte("other-node-seed"), "localhost:26660")
	nodeKey := makeTestNodeKey([]byte("my-node-seed"))

	cfg := &config.AutobahnConfig{
		Enable:         true,
		Validators:     v1,
		MaxGasPerBlock: 30_000_000,
		MaxTxsPerBlock: 10_000,
		MempoolSize:    10_000,
		BlockInterval:  400 * time.Millisecond,
		ViewTimeout:    5 * time.Second,
		DialInterval:   10 * time.Second,
	}
	_, err := buildGigaConfig(cfg, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
	assert.Error(t, err, "should fail when node's own key is not in validators")
	assert.Contains(t, err.Error(), "own public key")
}
