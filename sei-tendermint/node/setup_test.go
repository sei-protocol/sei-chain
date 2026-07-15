package node

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func makeTestNodeKey(seed []byte) types.NodeKey {
	return types.NodeKey(ed25519.TestSecretKey(seed))
}

func makeTestValidatorKey(seed []byte) atypes.SecretKey {
	return atypes.SecretKeyFromED25519(ed25519.TestSecretKey(seed))
}

func makeValidator(valSeed, nodeSeed []byte, address string) config.AutobahnValidator {
	valKey := atypes.SecretKeyFromED25519(ed25519.TestSecretKey(valSeed))
	nodeKey := p2p.NodeSecretKey(ed25519.TestSecretKey(nodeSeed))
	hp, err := tcp.ParseHostPort(address)
	if err != nil {
		panic(err)
	}
	return config.AutobahnValidator{
		ValidatorKey: valKey.Public(),
		NodeKey:      nodeKey.Public(),
		Address:      hp,
		EVMRPC:       config.URL{URL: utils.OrPanic1(url.Parse("http://" + address))},
	}
}

func writeAutobahnConfig(t *testing.T, fc *config.AutobahnFileConfig) string {
	t.Helper()
	data, err := json.Marshal(fc)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "autobahn.json")
	require.NoError(t, os.WriteFile(path, data, 0644))
	return path
}

func defaultFileConfig(t testing.TB, validators []config.AutobahnValidator) *config.AutobahnFileConfig {
	return &config.AutobahnFileConfig{
		Validators:         validators,
		MaxTxsPerBlock:     5_000,
		MaxTxsPerSecond:    utils.None[uint64](),
		AllowEmptyBlocks:   false,
		BlockInterval:      utils.Duration(400 * time.Millisecond),
		ViewTimeout:        utils.Duration(1500 * time.Millisecond),
		PersistentStateDir: utils.Some(t.TempDir()),
		DialInterval:       utils.Duration(10 * time.Second),
	}
}

// testGenesisMaxGas is the gas limit baked into the test genesis doc.
const testGenesisMaxGas int64 = 50_000_000

func makeTestGigaDeps() (*proxy.Proxy, *types.GenesisDoc) {
	app := kvstore.NewProxy()
	genDoc := &types.GenesisDoc{
		ChainID: "test-chain",
		// Nontrivial InitialHeight so any future code that assumes the
		// genesis height is 1 surfaces here.
		InitialHeight: 100,
		ConsensusParams: &types.ConsensusParams{
			Block: types.BlockParams{MaxGas: testGenesisMaxGas},
		},
	}
	return app, genDoc
}

func TestBuildGigaConfig_NonePersistentStateDir(t *testing.T) {
	v1 := makeValidator([]byte("val-seed"), []byte("node-seed"), "localhost:26660")
	fc := defaultFileConfig(t, []config.AutobahnValidator{v1})
	fc.PersistentStateDir = utils.None[string]()
	cfgFile := writeAutobahnConfig(t, fc)
	nodeKey := makeTestNodeKey([]byte("node-seed"))
	valKey := makeTestValidatorKey([]byte("val-seed"))
	txMempool, genDoc := makeTestGigaDeps()

	result, err := buildValidatorGigaConfig(cfgFile, nodeKey, valKey, txMempool, genDoc)
	require.NoError(t, err)
	assert.False(t, result.PersistentStateDir.IsPresent())
}

func TestBuildGigaConfig_BlockDBOverrides(t *testing.T) {
	v1 := makeValidator([]byte("val-seed"), []byte("node-seed"), "localhost:26660")
	fc := defaultFileConfig(t, []config.AutobahnValidator{v1})
	fc.BlockDB = utils.Some(config.AutobahnBlockDBConfig{
		Retention: utils.Some(utils.Duration(30 * time.Second)),
		GCPeriod:  utils.Some(utils.Duration(5 * time.Second)),
		Fsync:     utils.Some(false),
	})
	cfgFile := writeAutobahnConfig(t, fc)
	nodeKey := makeTestNodeKey([]byte("node-seed"))
	valKey := makeTestValidatorKey([]byte("val-seed"))
	txMempool, genDoc := makeTestGigaDeps()

	result, err := buildValidatorGigaConfig(cfgFile, nodeKey, valKey, txMempool, genDoc)
	require.NoError(t, err)
	assert.Equal(t, utils.Some(30*time.Second), result.BlockDB.Retention)
	assert.Equal(t, utils.Some(5*time.Second), result.BlockDB.GCPeriod)
	assert.Equal(t, utils.Some(false), result.BlockDB.Fsync)
}

func TestBuildGigaConfig_BlockDBOmittedKeepsZeroOverrides(t *testing.T) {
	v1 := makeValidator([]byte("val-seed"), []byte("node-seed"), "localhost:26660")
	fc := defaultFileConfig(t, []config.AutobahnValidator{v1})
	cfgFile := writeAutobahnConfig(t, fc)
	nodeKey := makeTestNodeKey([]byte("node-seed"))
	valKey := makeTestValidatorKey([]byte("val-seed"))
	txMempool, genDoc := makeTestGigaDeps()

	result, err := buildValidatorGigaConfig(cfgFile, nodeKey, valKey, txMempool, genDoc)
	require.NoError(t, err)
	assert.False(t, result.BlockDB.Retention.IsPresent())
	assert.False(t, result.BlockDB.GCPeriod.IsPresent())
	assert.False(t, result.BlockDB.Fsync.IsPresent())
}

func TestBuildGigaConfig_EmptyPathErrors(t *testing.T) {
	nodeKey := makeTestNodeKey([]byte("test-node-key"))
	valKey := makeTestValidatorKey([]byte("val-seed"))
	txMempool, genDoc := makeTestGigaDeps()
	_, err := buildValidatorGigaConfig("", nodeKey, valKey, txMempool, genDoc)
	assert.Error(t, err, "empty path should error")
}

func TestBuildGigaConfig_EnabledWithValidators(t *testing.T) {
	// val1 uses same seed as node1 for simplicity; val2/val3 have separate seeds.
	v1 := makeValidator([]byte("val1-seed"), []byte("node1-seed"), "localhost:26660")
	v2 := makeValidator([]byte("val2-seed"), []byte("node2-seed"), "peer1.example.com:26661")
	v3 := makeValidator([]byte("val3-seed"), []byte("node3-seed"), "peer2.example.com:26662")

	fc := &config.AutobahnFileConfig{
		Validators:         []config.AutobahnValidator{v1, v2, v3},
		MaxTxsPerBlock:     5_000,
		MaxTxsPerSecond:    utils.Some(uint64(1_000)),
		AllowEmptyBlocks:   true,
		BlockInterval:      utils.Duration(200 * time.Millisecond),
		ViewTimeout:        utils.Duration(3 * time.Second),
		PersistentStateDir: utils.Some("/tmp/autobahn-state"),
		DialInterval:       utils.Duration(5 * time.Second),
	}
	cfgFile := writeAutobahnConfig(t, fc)

	nodeKey := makeTestNodeKey([]byte("node1-seed"))
	valKey := makeTestValidatorKey([]byte("val1-seed"))
	txMempool, genDoc := makeTestGigaDeps()

	result, err := buildValidatorGigaConfig(cfgFile, nodeKey, valKey, txMempool, genDoc)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.ValidatorAddrs, 3)
	assert.Equal(t, 5*time.Second, result.DialInterval)

	assert.Equal(t, 3*time.Second, result.ViewTimeout(atypes.View{}))
	assert.Equal(t, utils.Some("/tmp/autobahn-state"), result.PersistentStateDir)

	// Verify the validator key is derived from the validator-key seed, not the node key.
	expectedValPub := makeTestValidatorKey([]byte("val1-seed")).Public()
	assert.Equal(t, expectedValPub, result.ValidatorKey.Public())

	// Producer config.
	require.NotNil(t, result.Producer)
	assert.Equal(t, uint64(testGenesisMaxGas), result.Producer.MaxGasEstimatedPerBlock)
	assert.Equal(t, uint64(5_000), result.Producer.MaxTxsPerBlock)
	maxTps, ok := result.Producer.MaxTxsPerSecond.Get()
	require.True(t, ok)
	assert.Equal(t, uint64(1_000), maxTps)
	assert.True(t, result.Producer.AllowEmptyBlocks)
	assert.Equal(t, 200*time.Millisecond, result.Producer.BlockInterval)

	assert.Equal(t, genDoc, result.GenDoc)
}

func TestBuildGigaConfig_NoneMaxTxsPerSecond(t *testing.T) {
	v1 := makeValidator([]byte("val-seed"), []byte("node-seed"), "localhost:26660")
	fc := defaultFileConfig(t, []config.AutobahnValidator{v1})
	cfgFile := writeAutobahnConfig(t, fc)
	nodeKey := makeTestNodeKey([]byte("node-seed"))
	valKey := makeTestValidatorKey([]byte("val-seed"))
	txMempool, genDoc := makeTestGigaDeps()

	result, err := buildValidatorGigaConfig(cfgFile, nodeKey, valKey, txMempool, genDoc)
	require.NoError(t, err)
	require.NotNil(t, result.Producer)
	assert.False(t, result.Producer.MaxTxsPerSecond.IsPresent())
}

func TestBuildGigaConfig_InvalidConfigFile(t *testing.T) {
	nodeKey := makeTestNodeKey([]byte("node-seed"))
	valKey := makeTestValidatorKey([]byte("val-seed"))
	txMempool, genDoc := makeTestGigaDeps()

	t.Run("missing file", func(t *testing.T) {
		_, err := buildValidatorGigaConfig("/nonexistent/autobahn.json", nodeKey, valKey, txMempool, genDoc)
		assert.Error(t, err)
	})

	t.Run("invalid json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.json")
		require.NoError(t, os.WriteFile(path, []byte("not json"), 0644))
		_, err := buildValidatorGigaConfig(path, nodeKey, valKey, txMempool, genDoc)
		assert.Error(t, err)
	})

	t.Run("empty validators", func(t *testing.T) {
		fc := defaultFileConfig(t, []config.AutobahnValidator{})
		cfgFile := writeAutobahnConfig(t, fc)
		_, err := buildValidatorGigaConfig(cfgFile, nodeKey, valKey, txMempool, genDoc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "validators must not be empty")
	})
}

// TestBuildGigaConfig_GenesisMaxGas covers the genesis-sourced
// Producer.MaxGasPerBlock validation.
func TestBuildGigaConfig_GenesisMaxGas(t *testing.T) {
	nodeKey := makeTestNodeKey([]byte("node-seed"))
	valKey := makeTestValidatorKey([]byte("val-seed"))
	v := makeValidator([]byte("val-seed"), []byte("node-seed"), "localhost:26660")
	cfgFile := writeAutobahnConfig(t, defaultFileConfig(t, []config.AutobahnValidator{v}))

	t.Run("nil ConsensusParams", func(t *testing.T) {
		txMempool, genDoc := makeTestGigaDeps()
		genDoc.ConsensusParams = nil
		_, err := buildValidatorGigaConfig(cfgFile, nodeKey, valKey, txMempool, genDoc)
		assert.ErrorIs(t, err, ErrGenesisMaxGasInvalid)
	})

	t.Run("zero MaxGas", func(t *testing.T) {
		txMempool, genDoc := makeTestGigaDeps()
		genDoc.ConsensusParams.Block.MaxGas = 0
		_, err := buildValidatorGigaConfig(cfgFile, nodeKey, valKey, txMempool, genDoc)
		assert.ErrorIs(t, err, ErrGenesisMaxGasInvalid)
	})

	t.Run("negative MaxGas", func(t *testing.T) {
		txMempool, genDoc := makeTestGigaDeps()
		genDoc.ConsensusParams.Block.MaxGas = -1
		_, err := buildValidatorGigaConfig(cfgFile, nodeKey, valKey, txMempool, genDoc)
		assert.ErrorIs(t, err, ErrGenesisMaxGasInvalid)
	})
}

func TestBuildGigaConfig_DuplicateValidatorKey(t *testing.T) {
	v1 := makeValidator([]byte("val-seed"), []byte("node1"), "localhost:26660")
	v1dup := makeValidator([]byte("val-seed"), []byte("node2"), "localhost:26661")
	fc := defaultFileConfig(t, []config.AutobahnValidator{v1, v1dup})
	data, _ := json.Marshal(fc)
	path := filepath.Join(t.TempDir(), "autobahn.json")
	os.WriteFile(path, data, 0644)
	nodeKey := makeTestNodeKey([]byte("node1"))
	valKey := makeTestValidatorKey([]byte("val-seed"))
	txMempool, genDoc := makeTestGigaDeps()

	_, err := buildValidatorGigaConfig(path, nodeKey, valKey, txMempool, genDoc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate validator key")
}

func TestBuildGigaConfig_DuplicateNodeKey(t *testing.T) {
	v1 := makeValidator([]byte("val1"), []byte("same-node"), "localhost:26660")
	v2 := makeValidator([]byte("val2"), []byte("same-node"), "localhost:26661")
	fc := defaultFileConfig(t, []config.AutobahnValidator{v1, v2})
	data, _ := json.Marshal(fc)
	path := filepath.Join(t.TempDir(), "autobahn.json")
	os.WriteFile(path, data, 0644)
	nodeKey := makeTestNodeKey([]byte("same-node"))
	valKey := makeTestValidatorKey([]byte("val1"))
	txMempool, genDoc := makeTestGigaDeps()

	_, err := buildValidatorGigaConfig(path, nodeKey, valKey, txMempool, genDoc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate node key")
}

func TestBuildGigaConfig_SelfNotInValidators(t *testing.T) {
	v1 := makeValidator([]byte("other-val"), []byte("other-node"), "localhost:26660")
	cfgFile := writeAutobahnConfig(t, defaultFileConfig(t, []config.AutobahnValidator{v1}))
	nodeKey := makeTestNodeKey([]byte("my-node"))
	valKey := makeTestValidatorKey([]byte("my-val"))
	txMempool, genDoc := makeTestGigaDeps()

	_, err := buildValidatorGigaConfig(cfgFile, nodeKey, valKey, txMempool, genDoc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validator key not found")
}

func TestBuildGigaConfig_NodeKeyMismatch(t *testing.T) {
	// Validator entry has the right val key but wrong node key.
	v1 := makeValidator([]byte("my-val"), []byte("wrong-node"), "localhost:26660")
	cfgFile := writeAutobahnConfig(t, defaultFileConfig(t, []config.AutobahnValidator{v1}))
	nodeKey := makeTestNodeKey([]byte("my-node"))
	valKey := makeTestValidatorKey([]byte("my-val"))
	txMempool, genDoc := makeTestGigaDeps()

	_, err := buildValidatorGigaConfig(cfgFile, nodeKey, valKey, txMempool, genDoc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "node key mismatch")
}

func TestMakeCloser_NoErrorsReturnsNil(t *testing.T) {
	cl := makeCloser([]closer{
		func() error { return nil },
		func() error { return nil },
	})
	require.NoError(t, cl())
}

func TestPreparePersistentStateDir_EmptyStringIsNone(t *testing.T) {
	cfg := &p2p.GigaRouterCommonConfig{
		PersistentStateDir: utils.Some(""),
	}
	require.NoError(t, preparePersistentStateDir(t.TempDir(), cfg))
	_, ok := cfg.PersistentStateDir.Get()
	require.False(t, ok, "Some(\"\") must be cleared to None for in-memory mode")
}
