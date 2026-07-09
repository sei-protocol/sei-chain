package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	// set up some defaults
	cfg := DefaultConfig()
	assert.NotNil(t, cfg.P2P)
	assert.NotNil(t, cfg.Mempool)
	assert.NotNil(t, cfg.Consensus)

	// check the root dir stuff...
	cfg.SetRoot("/foo")
	cfg.Genesis = "bar"
	cfg.DBPath = "/opt/data"

	assert.Equal(t, "/foo/bar", cfg.GenesisFile())
	assert.Equal(t, "/opt/data", cfg.DBDir())
}

func TestConfigValidateBasic(t *testing.T) {
	cfg := DefaultConfig()
	assert.NoError(t, cfg.ValidateBasic())

	// tamper with unsafe-propose-timeout-override
	cfg.Consensus.UnsafeProposeTimeoutOverride = -10 * time.Second
	assert.Error(t, cfg.ValidateBasic())
}

func TestTLSConfiguration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SetRoot("/home/user")

	cfg.RPC.TLSCertFile = "file.crt"
	assert.Equal(t, "/home/user/config/file.crt", cfg.RPC.CertFile())
	cfg.RPC.TLSKeyFile = "file.key"
	assert.Equal(t, "/home/user/config/file.key", cfg.RPC.KeyFile())

	cfg.RPC.TLSCertFile = "/abs/path/to/file.crt"
	assert.Equal(t, "/abs/path/to/file.crt", cfg.RPC.CertFile())
	cfg.RPC.TLSKeyFile = "/abs/path/to/file.key"
	assert.Equal(t, "/abs/path/to/file.key", cfg.RPC.KeyFile())
}

func TestBaseConfigValidateBasic(t *testing.T) {
	cfg := TestBaseConfig()
	assert.NoError(t, cfg.ValidateBasic())

	// tamper with log format
	cfg.LogFormat = "invalid"
	assert.Error(t, cfg.ValidateBasic())
}

func TestRPCConfigValidateBasic(t *testing.T) {
	cfg := TestRPCConfig()
	assert.NoError(t, cfg.ValidateBasic())

	fieldsToTest := []string{
		"MaxOpenConnections",
		"MaxSubscriptionClients",
		"MaxSubscriptionsPerClient",
		"TimeoutBroadcastTxCommit",
		"MaxBodyBytes",
		"MaxHeaderBytes",
		"LagThreshold",
		"TimeoutReadHeader",
		"TimeoutWrite",
		"MaxTxSearchResults",
		"MaxEventSearchScan",
	}

	for _, fieldName := range fieldsToTest {
		reflect.ValueOf(cfg).Elem().FieldByName(fieldName).SetInt(-1)
		assert.Error(t, cfg.ValidateBasic())
		reflect.ValueOf(cfg).Elem().FieldByName(fieldName).SetInt(0)
	}

	// Cross-field: timeout-write must be greater than timeout-broadcast-tx-commit when non-zero.
	cfg2 := TestRPCConfig()
	cfg2.TimeoutBroadcastTxCommit = 20 * time.Second
	cfg2.TimeoutWrite = 20 * time.Second
	assert.Error(t, cfg2.ValidateBasic())
	cfg2.TimeoutWrite = 21 * time.Second
	assert.NoError(t, cfg2.ValidateBasic())
	cfg2.TimeoutWrite = 0 // 0 disables; constraint does not apply
	assert.NoError(t, cfg2.ValidateBasic())
}

func TestMempoolConfigValidateBasic(t *testing.T) {
	cfg := TestMempoolConfig()
	assert.NoError(t, cfg.ValidateBasic())

	fieldsToTest := []string{
		"Size",
		"MaxTxsBytes",
		"CacheSize",
		"MaxTxBytes",
	}

	for _, fieldName := range fieldsToTest {
		reflect.ValueOf(cfg).Elem().FieldByName(fieldName).SetInt(-1)
		assert.Error(t, cfg.ValidateBasic())
		reflect.ValueOf(cfg).Elem().FieldByName(fieldName).SetInt(0)
	}
}

func TestStateSyncConfigValidateBasic(t *testing.T) {
	cfg := TestStateSyncConfig()
	require.NoError(t, cfg.ValidateBasic())
}

func TestConsensusConfig_ValidateBasic(t *testing.T) {
	testcases := map[string]struct {
		modify    func(*ConsensusConfig)
		expectErr bool
	}{
		"UnsafeProposeTimeoutOverride":               {func(c *ConsensusConfig) { c.UnsafeProposeTimeoutOverride = time.Second }, false},
		"UnsafeProposeTimeoutOverride negative":      {func(c *ConsensusConfig) { c.UnsafeProposeTimeoutOverride = -1 }, true},
		"UnsafeProposeTimeoutDeltaOverride":          {func(c *ConsensusConfig) { c.UnsafeProposeTimeoutDeltaOverride = time.Second }, false},
		"UnsafeProposeTimeoutDeltaOverride negative": {func(c *ConsensusConfig) { c.UnsafeProposeTimeoutDeltaOverride = -1 }, true},
		"UnsafePrevoteTimeoutOverride":               {func(c *ConsensusConfig) { c.UnsafeVoteTimeoutOverride = time.Second }, false},
		"UnsafePrevoteTimeoutOverride negative":      {func(c *ConsensusConfig) { c.UnsafeVoteTimeoutOverride = -1 }, true},
		"UnsafePrevoteTimeoutDeltaOverride":          {func(c *ConsensusConfig) { c.UnsafeVoteTimeoutDeltaOverride = time.Second }, false},
		"UnsafePrevoteTimeoutDeltaOverride negative": {func(c *ConsensusConfig) { c.UnsafeVoteTimeoutDeltaOverride = -1 }, true},
		"UnsafeCommitTimeoutOverride":                {func(c *ConsensusConfig) { c.UnsafeCommitTimeoutOverride = time.Second }, false},
		"UnsafeCommitTimeoutOverride negative":       {func(c *ConsensusConfig) { c.UnsafeCommitTimeoutOverride = -1 }, true},
		"PeerGossipSleepDuration":                    {func(c *ConsensusConfig) { c.PeerGossipSleepDuration = time.Second }, false},
		"PeerGossipSleepDuration negative":           {func(c *ConsensusConfig) { c.PeerGossipSleepDuration = -1 }, true},
		"PeerQueryMaj23SleepDuration":                {func(c *ConsensusConfig) { c.PeerQueryMaj23SleepDuration = time.Second }, false},
		"PeerQueryMaj23SleepDuration negative":       {func(c *ConsensusConfig) { c.PeerQueryMaj23SleepDuration = -1 }, true},
		"DoubleSignCheckHeight negative":             {func(c *ConsensusConfig) { c.DoubleSignCheckHeight = -1 }, true},
	}
	for desc, tc := range testcases {
		t.Run(desc, func(t *testing.T) {
			cfg := DefaultConsensusConfig()
			tc.modify(cfg)

			err := cfg.ValidateBasic()
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConsensusConfigResolveTimeouts(t *testing.T) {
	overrides := func(enabled bool, bypass *bool) *ConsensusConfig {
		cfg := DefaultConsensusConfig()
		cfg.UnsafeOverridesEnabled = enabled
		cfg.UnsafeProposeTimeoutOverride = 9 * time.Second
		cfg.UnsafeProposeTimeoutDeltaOverride = 8 * time.Second
		cfg.UnsafeVoteTimeoutOverride = 7 * time.Second
		cfg.UnsafeVoteTimeoutDeltaOverride = 6 * time.Second
		cfg.UnsafeCommitTimeoutOverride = 5 * time.Second
		cfg.UnsafeBypassCommitTimeoutOverride = bypass
		return cfg
	}

	onchain := func(bypass bool) types.TimeoutParams {
		return types.TimeoutParams{
			Propose:             2 * time.Second,
			ProposeDelta:        250 * time.Millisecond,
			Vote:                3 * time.Second,
			VoteDelta:           350 * time.Millisecond,
			Commit:              4 * time.Second,
			BypassCommitTimeout: bypass,
		}
	}
	overridden := func(bypass bool) types.TimeoutParams {
		return types.TimeoutParams{
			Propose:             9 * time.Second,
			ProposeDelta:        8 * time.Second,
			Vote:                7 * time.Second,
			VoteDelta:           6 * time.Second,
			Commit:              5 * time.Second,
			BypassCommitTimeout: bypass,
		}
	}

	testCases := []struct {
		name     string
		cfg      *ConsensusConfig
		params   types.TimeoutParams
		expected types.TimeoutParams
	}{
		{"fills defaults", DefaultConsensusConfig(), types.TimeoutParams{}, types.DefaultTimeoutParams()},
		{"disabled ignores overrides for non-legacy params", overrides(false, utils.Alloc(true)), onchain(false), onchain(false)},
		{"disabled preserves legacy override behavior", overrides(false, utils.Alloc(true)), badParams, overridden(true)},
		{"enabled applies overrides", overrides(true, utils.Alloc(false)), onchain(false), overridden(false)},
		{"nil bypass override keeps resolved false", overrides(true, nil), onchain(false), overridden(false)},
		{"nil bypass override keeps resolved true", overrides(true, nil), onchain(true), overridden(true)},
		{"false bypass override clears true", overrides(true, utils.Alloc(false)), onchain(true), overridden(false)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.cfg.ResolveTimeouts(tc.params))
		})
	}
}

func TestInstrumentationConfigValidateBasic(t *testing.T) {
	cfg := TestInstrumentationConfig()
	assert.NoError(t, cfg.ValidateBasic())

	// tamper with maximum open connections
	cfg.MaxOpenConnections = -1
	assert.Error(t, cfg.ValidateBasic())
}

func TestP2PConfigValidateBasic(t *testing.T) {
	cfg := TestP2PConfig()
	assert.NoError(t, cfg.ValidateBasic())

	fieldsToTest := []string{
		"FlushThrottleTimeout",
		"MaxPacketMsgPayloadSize",
		"SendRate",
		"RecvRate",
	}

	for _, fieldName := range fieldsToTest {
		reflect.ValueOf(cfg).Elem().FieldByName(fieldName).SetInt(-1)
		assert.Error(t, cfg.ValidateBasic())
		reflect.ValueOf(cfg).Elem().FieldByName(fieldName).SetInt(0)
	}
}

// --- WalFile legacy fallback tests ---

func TestWalFile_NewDefault_NoLegacy(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConsensusConfig()
	cfg.RootDir = root

	expected := filepath.Join(root, "data", "tendermint", "cs.wal", "wal")
	assert.Equal(t, expected, cfg.WalFile(),
		"new node with new default should use data/tendermint/cs.wal/wal")
}

func TestWalFile_NewDefault_LegacyExists(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, "data", "cs.wal")
	require.NoError(t, os.MkdirAll(legacyDir, 0755))

	cfg := DefaultConsensusConfig()
	cfg.RootDir = root

	expected := filepath.Join(legacyDir, "wal")
	assert.Equal(t, expected, cfg.WalFile(),
		"should fall back to legacy cs.wal when it exists on disk")
}

func TestWalFile_OldDefault_NoLegacy(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConsensusConfig()
	cfg.RootDir = root
	cfg.WalPath = filepath.Join("data", "cs.wal", "wal")

	expected := filepath.Join(root, "data", "tendermint", "cs.wal", "wal")
	assert.Equal(t, expected, cfg.WalFile(),
		"old default in config.toml on a new node should redirect to new path")
}

func TestWalFile_OldDefault_LegacyExists(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, "data", "cs.wal")
	require.NoError(t, os.MkdirAll(legacyDir, 0755))

	cfg := DefaultConsensusConfig()
	cfg.RootDir = root
	cfg.WalPath = filepath.Join("data", "cs.wal", "wal")

	expected := filepath.Join(legacyDir, "wal")
	assert.Equal(t, expected, cfg.WalFile(),
		"old default in config.toml with legacy data should use legacy path")
}

func TestWalFile_CustomPath(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConsensusConfig()
	cfg.RootDir = root
	cfg.WalPath = "/custom/wal/path"

	assert.Equal(t, "/custom/wal/path", cfg.WalFile(),
		"absolute custom path should be returned unchanged")
}

func TestWalFile_CustomRelativePath(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConsensusConfig()
	cfg.RootDir = root
	cfg.WalPath = filepath.Join("data", "mywal", "wal")

	expected := filepath.Join(root, "data", "mywal", "wal")
	assert.Equal(t, expected, cfg.WalFile(),
		"non-default custom relative path should be resolved normally")
}

func TestWalFile_BothExist_LegacyWins(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, "data", "cs.wal")
	require.NoError(t, os.MkdirAll(legacyDir, 0755))
	newDir := filepath.Join(root, "data", "tendermint", "cs.wal")
	require.NoError(t, os.MkdirAll(newDir, 0755))

	cfg := DefaultConsensusConfig()
	cfg.RootDir = root

	expected := filepath.Join(legacyDir, "wal")
	assert.Equal(t, expected, cfg.WalFile(),
		"legacy should win when both locations exist")
}
