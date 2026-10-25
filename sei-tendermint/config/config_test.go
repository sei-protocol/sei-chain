package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	rpctypes "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/jsonrpc/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestDefaultConfig(t *testing.T) {
	// set up some defaults
	cfg := DefaultConfig()
	assert.NotNil(t, cfg.P2P)
	assert.NotNil(t, cfg.Mempool)
	assert.NotNil(t, cfg.Consensus)
	assert.False(t, cfg.FastCheckTx)

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

	// tamper with create-empty-blocks-interval
	cfg.Consensus.CreateEmptyBlocksInterval = -10 * time.Second
	assert.Error(t, cfg.ValidateBasic())
}

// Asserts Config.ValidateBasic routes the [p2p] section, not merely that the
// section's own checks work.
func TestConfigValidateBasicRoutesP2P(t *testing.T) {
	cfg := DefaultConfig()
	require.NoError(t, cfg.ValidateBasic())

	cfg.P2P.AcceptInterval = -1
	require.Error(t, cfg.ValidateBasic())
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
		"MaxSearchScanBudget",
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

	cfg3 := TestRPCConfig()
	cfg3.RateLimitingEnabled = true
	cfg3.IPRateLimitBurst = rpctypes.RequestBatchSizeLimit - 1
	assert.Error(t, cfg3.ValidateBasic())
	cfg3.IPRateLimitBurst = rpctypes.RequestBatchSizeLimit
	assert.NoError(t, cfg3.ValidateBasic())
	cfg3.IPRateLimitBurst = 0
	assert.NoError(t, cfg3.ValidateBasic())
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
		"PeerGossipSleepDuration":              {func(c *ConsensusConfig) { c.PeerGossipSleepDuration = time.Second }, false},
		"PeerGossipSleepDuration negative":     {func(c *ConsensusConfig) { c.PeerGossipSleepDuration = -1 }, true},
		"PeerQueryMaj23SleepDuration":          {func(c *ConsensusConfig) { c.PeerQueryMaj23SleepDuration = time.Second }, false},
		"PeerQueryMaj23SleepDuration negative": {func(c *ConsensusConfig) { c.PeerQueryMaj23SleepDuration = -1 }, true},
		"DoubleSignCheckHeight negative":       {func(c *ConsensusConfig) { c.DoubleSignCheckHeight = -1 }, true},
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
	testCases := []struct {
		name     string
		cfg      *ConsensusConfig
		params   types.TimeoutParams
		expected types.TimeoutParams
	}{
		{"fills defaults", DefaultConsensusConfig(), types.TimeoutParams{}, types.DefaultTimeoutParams()},
		{"keeps chain params", DefaultConsensusConfig(), onchain(false), onchain(false)},
		{"keeps chain bypass", DefaultConsensusConfig(), onchain(true), onchain(true)},
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
		"DialInterval",
		"AcceptInterval",
	}

	for _, fieldName := range fieldsToTest {
		reflect.ValueOf(cfg).Elem().FieldByName(fieldName).SetInt(-1)
		assert.Error(t, cfg.ValidateBasic())
		reflect.ValueOf(cfg).Elem().FieldByName(fieldName).SetInt(0)
	}
}

// Pins the accept-interval default exactly, so changing it is deliberate and
// visible in the diff.
func TestP2PConfigAcceptInterval(t *testing.T) {
	cfg := DefaultP2PConfig()
	require.NoError(t, cfg.ValidateBasic())

	require.Equal(t, 10*time.Millisecond, cfg.AcceptInterval)

	// A zero interval is the documented escape hatch for disabling the limiter
	// outright, and must stay valid rather than becoming a zero rate.
	cfg.AcceptInterval = 0
	require.NoError(t, cfg.ValidateBasic())
	require.Equal(t, rate.Inf, rate.Every(cfg.AcceptInterval))
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

func TestRPCRateLimitKeysKebabCase(t *testing.T) {
	const body = `
[rpc]
ip-rate-limit-rps = 42.5
ip-rate-limit-burst = 50
rate-limiting-enabled = true
trusted-proxy-cidrs = ["10.0.0.0/8"]
`
	conf, err := unmarshalConfigTOML(t, body)
	require.NoError(t, err)
	require.Equal(t, 42.5, conf.RPC.IPRateLimitRPS)
	require.Equal(t, 50, conf.RPC.IPRateLimitBurst)
	require.True(t, conf.RPC.RateLimitingEnabled)
	require.Equal(t, []string{"10.0.0.0/8"}, conf.RPC.TrustedProxyCIDRs)
}
