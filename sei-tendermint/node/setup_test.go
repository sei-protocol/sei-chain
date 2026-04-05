package node

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func makeTestNodeKey(seed []byte) types.NodeKey {
	return types.NodeKey(ed25519.TestSecretKey(seed))
}

func makeValidator(seed []byte, host string, port uint16) autobahnValidator {
	key := ed25519.TestSecretKey(seed).Public()
	return autobahnValidator{
		PubKey: hex.EncodeToString(key.Bytes()),
		Host:   host,
		Port:   port,
	}
}

func writeAutobahnConfig(t *testing.T, fc *autobahnFileConfig) string {
	t.Helper()
	data, err := json.Marshal(fc)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "autobahn.json")
	require.NoError(t, os.WriteFile(path, data, 0644))
	return path
}

func defaultFileConfig(validators []autobahnValidator) *autobahnFileConfig {
	return &autobahnFileConfig{
		Validators:       validators,
		MaxGasPerBlock:   50_000_000,
		MaxTxsPerBlock:   5_000,
		MaxTxsPerSecond:  0,
		MempoolSize:      5_000,
		BlockInterval:    400 * time.Millisecond,
		AllowEmptyBlocks: false,
		ViewTimeout:      1500 * time.Millisecond,
		DialInterval:     10 * time.Second,
	}
}

func TestBuildGigaConfig_Disabled(t *testing.T) {
	nodeKey := makeTestNodeKey([]byte("test-node-key"))
	result, err := buildGigaConfig("", nodeKey, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result, "should return nil when config file is empty")
}

func TestBuildGigaConfig_EnabledWithValidators(t *testing.T) {
	v1 := makeValidator([]byte("node1-seed"), "localhost", 26660)
	v2 := makeValidator([]byte("node2-seed"), "peer1.example.com", 26661)
	v3 := makeValidator([]byte("node3-seed"), "peer2.example.com", 26662)

	fc := &autobahnFileConfig{
		Validators:         []autobahnValidator{v1, v2, v3},
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
	cfgFile := writeAutobahnConfig(t, fc)

	nodeKey := makeTestNodeKey([]byte("node1-seed"))
	genDoc := &types.GenesisDoc{ChainID: "test-chain", InitialHeight: 1}

	result, err := buildGigaConfig(cfgFile, nodeKey, nil, genDoc)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.ValidatorAddrs, 3)
	assert.Equal(t, 5*time.Second, result.DialInterval)

	// Consensus config.
	require.NotNil(t, result.Consensus)
	assert.Equal(t, 3*time.Second, result.Consensus.ViewTimeout(atypes.View{}))
	persistDir, ok := result.Consensus.PersistentStateDir.Get()
	require.True(t, ok)
	assert.Equal(t, "/tmp/autobahn-state", persistDir)

	// Producer config.
	require.NotNil(t, result.Producer)
	assert.Equal(t, uint64(50_000_000), result.Producer.MaxGasPerBlock)
	assert.Equal(t, uint64(5_000), result.Producer.MaxTxsPerBlock)
	maxTps, ok := result.Producer.MaxTxsPerSecond.Get()
	require.True(t, ok)
	assert.Equal(t, uint64(1_000), maxTps)
	assert.Equal(t, uint64(20_000), result.Producer.MempoolSize)
	assert.Equal(t, 200*time.Millisecond, result.Producer.BlockInterval)
	assert.True(t, result.Producer.AllowEmptyBlocks)

	assert.Equal(t, genDoc, result.GenDoc)
}

func TestBuildGigaConfig_ZeroMaxTxsPerSecond(t *testing.T) {
	v1 := makeValidator([]byte("node-seed"), "localhost", 26660)
	fc := defaultFileConfig([]autobahnValidator{v1})
	cfgFile := writeAutobahnConfig(t, fc)
	nodeKey := makeTestNodeKey([]byte("node-seed"))

	result, err := buildGigaConfig(cfgFile, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
	require.NoError(t, err)
	assert.False(t, result.Producer.MaxTxsPerSecond.IsPresent())
}

func TestBuildGigaConfig_EmptyPersistentStateDir(t *testing.T) {
	v1 := makeValidator([]byte("node-seed"), "localhost", 26660)
	fc := defaultFileConfig([]autobahnValidator{v1})
	cfgFile := writeAutobahnConfig(t, fc)
	nodeKey := makeTestNodeKey([]byte("node-seed"))

	result, err := buildGigaConfig(cfgFile, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
	require.NoError(t, err)
	assert.False(t, result.Consensus.PersistentStateDir.IsPresent())
}

func TestBuildGigaConfig_InvalidConfigFile(t *testing.T) {
	nodeKey := makeTestNodeKey([]byte("node-seed"))

	t.Run("missing file", func(t *testing.T) {
		_, err := buildGigaConfig("/nonexistent/autobahn.json", nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
		assert.Error(t, err)
	})

	t.Run("invalid json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.json")
		require.NoError(t, os.WriteFile(path, []byte("not json"), 0644))
		_, err := buildGigaConfig(path, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
		assert.Error(t, err)
	})

	t.Run("empty validators", func(t *testing.T) {
		fc := defaultFileConfig([]autobahnValidator{})
		cfgFile := writeAutobahnConfig(t, fc)
		_, err := buildGigaConfig(cfgFile, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "validators must not be empty")
	})

	t.Run("zero max_gas_per_block", func(t *testing.T) {
		v := makeValidator([]byte("node-seed"), "localhost", 26660)
		fc := defaultFileConfig([]autobahnValidator{v})
		fc.MaxGasPerBlock = 0
		cfgFile := writeAutobahnConfig(t, fc)
		_, err := buildGigaConfig(cfgFile, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max_gas_per_block")
	})

	t.Run("invalid pubkey hex", func(t *testing.T) {
		fc := defaultFileConfig([]autobahnValidator{{PubKey: "not_hex", Host: "localhost", Port: 26660}})
		data, _ := json.Marshal(fc)
		path := filepath.Join(t.TempDir(), "autobahn.json")
		require.NoError(t, os.WriteFile(path, data, 0644))
		_, err := buildGigaConfig(path, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
		assert.Error(t, err)
	})
}

func TestBuildGigaConfig_DuplicateValidator(t *testing.T) {
	v1 := makeValidator([]byte("node-seed"), "localhost", 26660)
	v1dup := makeValidator([]byte("node-seed"), "localhost", 26661)
	fc := defaultFileConfig([]autobahnValidator{v1, v1dup})
	data, _ := json.Marshal(fc)
	path := filepath.Join(t.TempDir(), "autobahn.json")
	os.WriteFile(path, data, 0644)
	nodeKey := makeTestNodeKey([]byte("node-seed"))

	_, err := buildGigaConfig(path, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestBuildGigaConfig_SelfNotInValidators(t *testing.T) {
	v1 := makeValidator([]byte("other-node-seed"), "localhost", 26660)
	cfgFile := writeAutobahnConfig(t, defaultFileConfig([]autobahnValidator{v1}))
	nodeKey := makeTestNodeKey([]byte("my-node-seed"))

	_, err := buildGigaConfig(cfgFile, nodeKey, nil, &types.GenesisDoc{InitialHeight: 1})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "own public key")
}
