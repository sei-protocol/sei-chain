package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func makeTestNodeKey(seed []byte) types.NodeKey {
	return types.NodeKey(ed25519.TestSecretKey(seed))
}

func makeTestValidatorKey(seed []byte) crypto.PrivKey {
	return ed25519.TestSecretKey(seed)
}

func makeValidator(valSeed, nodeSeed []byte, host string, port uint16) autobahnValidator {
	valKey := atypes.SecretKeyFromED25519(ed25519.TestSecretKey(valSeed))
	nodeKey := p2p.NodeSecretKey(ed25519.TestSecretKey(nodeSeed))
	return autobahnValidator{
		ValidatorKey: valKey.Public(),
		NodeKey:      nodeKey.Public(),
		Host:         host,
		Port:         port,
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
		Validators:         validators,
		MaxGasPerBlock:     50_000_000,
		MaxTxsPerBlock:     5_000,
		MaxTxsPerSecond:    utils.None[uint64](),
		MempoolSize:        5_000,
		BlockInterval:      400 * time.Millisecond,
		AllowEmptyBlocks:   false,
		ViewTimeout:        1500 * time.Millisecond,
		PersistentStateDir: utils.None[string](),
		DialInterval:       10 * time.Second,
	}
}

func TestBuildGigaConfig_Disabled(t *testing.T) {
	nodeKey := makeTestNodeKey([]byte("test-node-key"))
	result, err := buildGigaConfig("", nodeKey, utils.None[crypto.PrivKey](), nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result, "should return nil when config file is empty")
}

func TestBuildGigaConfig_EnabledWithValidators(t *testing.T) {
	// val1 uses same seed as node1 for simplicity; val2/val3 have separate seeds.
	v1 := makeValidator([]byte("val1-seed"), []byte("node1-seed"), "localhost", 26660)
	v2 := makeValidator([]byte("val2-seed"), []byte("node2-seed"), "peer1.example.com", 26661)
	v3 := makeValidator([]byte("val3-seed"), []byte("node3-seed"), "peer2.example.com", 26662)

	fc := &autobahnFileConfig{
		Validators:         []autobahnValidator{v1, v2, v3},
		MaxGasPerBlock:     50_000_000,
		MaxTxsPerBlock:     5_000,
		MaxTxsPerSecond:    utils.Some(uint64(1_000)),
		MempoolSize:        20_000,
		BlockInterval:      200 * time.Millisecond,
		AllowEmptyBlocks:   true,
		ViewTimeout:        3 * time.Second,
		PersistentStateDir: utils.Some("/tmp/autobahn-state"),
		DialInterval:       5 * time.Second,
	}
	cfgFile := writeAutobahnConfig(t, fc)

	nodeKey := makeTestNodeKey([]byte("node1-seed"))
	valKey := utils.Some(makeTestValidatorKey([]byte("val1-seed")))
	genDoc := &types.GenesisDoc{ChainID: "test-chain", InitialHeight: 1}

	result, err := buildGigaConfig(cfgFile, nodeKey, valKey, nil, genDoc)
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

	// Verify the consensus key is derived from the validator key, not the node key.
	expectedValPub := atypes.SecretKeyFromED25519(makeTestValidatorKey([]byte("val1-seed"))).Public()
	assert.Equal(t, expectedValPub, result.Consensus.Key.Public())

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

func TestBuildGigaConfig_NoneMaxTxsPerSecond(t *testing.T) {
	v1 := makeValidator([]byte("val-seed"), []byte("node-seed"), "localhost", 26660)
	fc := defaultFileConfig([]autobahnValidator{v1})
	cfgFile := writeAutobahnConfig(t, fc)
	nodeKey := makeTestNodeKey([]byte("node-seed"))
	valKey := utils.Some(makeTestValidatorKey([]byte("val-seed")))

	result, err := buildGigaConfig(cfgFile, nodeKey, valKey, nil, &types.GenesisDoc{InitialHeight: 1})
	require.NoError(t, err)
	assert.False(t, result.Producer.MaxTxsPerSecond.IsPresent())
}

func TestBuildGigaConfig_NonePersistentStateDir(t *testing.T) {
	v1 := makeValidator([]byte("val-seed"), []byte("node-seed"), "localhost", 26660)
	fc := defaultFileConfig([]autobahnValidator{v1})
	cfgFile := writeAutobahnConfig(t, fc)
	nodeKey := makeTestNodeKey([]byte("node-seed"))
	valKey := utils.Some(makeTestValidatorKey([]byte("val-seed")))

	result, err := buildGigaConfig(cfgFile, nodeKey, valKey, nil, &types.GenesisDoc{InitialHeight: 1})
	require.NoError(t, err)
	assert.False(t, result.Consensus.PersistentStateDir.IsPresent())
}

func TestBuildGigaConfig_InvalidConfigFile(t *testing.T) {
	nodeKey := makeTestNodeKey([]byte("node-seed"))
	valKey := utils.Some(makeTestValidatorKey([]byte("val-seed")))

	t.Run("missing file", func(t *testing.T) {
		_, err := buildGigaConfig("/nonexistent/autobahn.json", nodeKey, valKey, nil, &types.GenesisDoc{InitialHeight: 1})
		assert.Error(t, err)
	})

	t.Run("invalid json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.json")
		require.NoError(t, os.WriteFile(path, []byte("not json"), 0644))
		_, err := buildGigaConfig(path, nodeKey, valKey, nil, &types.GenesisDoc{InitialHeight: 1})
		assert.Error(t, err)
	})

	t.Run("empty validators", func(t *testing.T) {
		fc := defaultFileConfig([]autobahnValidator{})
		cfgFile := writeAutobahnConfig(t, fc)
		_, err := buildGigaConfig(cfgFile, nodeKey, valKey, nil, &types.GenesisDoc{InitialHeight: 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "validators must not be empty")
	})

	t.Run("zero max_gas_per_block", func(t *testing.T) {
		v := makeValidator([]byte("val-seed"), []byte("node-seed"), "localhost", 26660)
		fc := defaultFileConfig([]autobahnValidator{v})
		fc.MaxGasPerBlock = 0
		cfgFile := writeAutobahnConfig(t, fc)
		_, err := buildGigaConfig(cfgFile, nodeKey, valKey, nil, &types.GenesisDoc{InitialHeight: 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max_gas_per_block")
	})
}

func TestBuildGigaConfig_NoValidatorKey(t *testing.T) {
	v1 := makeValidator([]byte("val-seed"), []byte("node-seed"), "localhost", 26660)
	cfgFile := writeAutobahnConfig(t, defaultFileConfig([]autobahnValidator{v1}))
	nodeKey := makeTestNodeKey([]byte("node-seed"))

	_, err := buildGigaConfig(cfgFile, nodeKey, utils.None[crypto.PrivKey](), nil, &types.GenesisDoc{InitialHeight: 1})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "requires a local validator key")
}

func TestBuildGigaConfig_DuplicateValidatorKey(t *testing.T) {
	v1 := makeValidator([]byte("val-seed"), []byte("node1"), "localhost", 26660)
	v1dup := makeValidator([]byte("val-seed"), []byte("node2"), "localhost", 26661)
	fc := defaultFileConfig([]autobahnValidator{v1, v1dup})
	data, _ := json.Marshal(fc)
	path := filepath.Join(t.TempDir(), "autobahn.json")
	os.WriteFile(path, data, 0644)
	nodeKey := makeTestNodeKey([]byte("node1"))
	valKey := utils.Some(makeTestValidatorKey([]byte("val-seed")))

	_, err := buildGigaConfig(path, nodeKey, valKey, nil, &types.GenesisDoc{InitialHeight: 1})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate validator key")
}

func TestBuildGigaConfig_DuplicateNodeKey(t *testing.T) {
	v1 := makeValidator([]byte("val1"), []byte("same-node"), "localhost", 26660)
	v2 := makeValidator([]byte("val2"), []byte("same-node"), "localhost", 26661)
	fc := defaultFileConfig([]autobahnValidator{v1, v2})
	data, _ := json.Marshal(fc)
	path := filepath.Join(t.TempDir(), "autobahn.json")
	os.WriteFile(path, data, 0644)
	nodeKey := makeTestNodeKey([]byte("same-node"))
	valKey := utils.Some(makeTestValidatorKey([]byte("val1")))

	_, err := buildGigaConfig(path, nodeKey, valKey, nil, &types.GenesisDoc{InitialHeight: 1})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate node key")
}

func TestBuildGigaConfig_SelfNotInValidators(t *testing.T) {
	v1 := makeValidator([]byte("other-val"), []byte("other-node"), "localhost", 26660)
	cfgFile := writeAutobahnConfig(t, defaultFileConfig([]autobahnValidator{v1}))
	nodeKey := makeTestNodeKey([]byte("my-node"))
	valKey := utils.Some(makeTestValidatorKey([]byte("my-val")))

	_, err := buildGigaConfig(cfgFile, nodeKey, valKey, nil, &types.GenesisDoc{InitialHeight: 1})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validator key not found")
}

func TestBuildGigaConfig_NodeKeyMismatch(t *testing.T) {
	// Validator entry has the right val key but wrong node key.
	v1 := makeValidator([]byte("my-val"), []byte("wrong-node"), "localhost", 26660)
	cfgFile := writeAutobahnConfig(t, defaultFileConfig([]autobahnValidator{v1}))
	nodeKey := makeTestNodeKey([]byte("my-node"))
	valKey := utils.Some(makeTestValidatorKey([]byte("my-val")))

	_, err := buildGigaConfig(cfgFile, nodeKey, valKey, nil, &types.GenesisDoc{InitialHeight: 1})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "node key mismatch")
}
