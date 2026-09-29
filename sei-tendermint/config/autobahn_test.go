package config

import (
	"encoding/json"
	"math"
	"net/url"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestURLJSONReencode(t *testing.T) {
	want := URL{URL: &url.URL{
		Scheme:   "https",
		Host:     "example.com:8545",
		Path:     "/rpc",
		RawQuery: "foo=bar&baz=qux",
	}}

	encoded, err := json.Marshal(want)
	require.NoError(t, err)

	var got URL
	require.NoError(t, json.Unmarshal(encoded, &got))
	require.NotNil(t, got.URL)
	require.Equal(t, want.String(), got.String())
}

func TestURLUnmarshalRejectsNonHTTP(t *testing.T) {
	var got URL
	require.Error(t, json.Unmarshal([]byte(`"ws://example.com:8545"`), &got))
}

func validAutobahnFileConfig() AutobahnFileConfig {
	return AutobahnFileConfig{
		Validators: []AutobahnValidator{{
			EVMRPC: URL{URL: &url.URL{Scheme: "http", Host: "localhost:8545"}},
		}},
		MaxTxsPerBlock: 1,
		BlockInterval:  utils.Duration(time.Second),
		ViewTimeout:    utils.Duration(time.Second),
		DialInterval:   utils.Duration(time.Second),

		PersistentStateDir: "autobahn",
	}
}

func TestAutobahnFileConfig_ValidateMaxConcurrentCheckTx(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value utils.Option[uint64]
		ok    bool
	}{
		{"absent", utils.None[uint64](), true},
		{"one", utils.Some[uint64](1), true},
		{"max_int32", utils.Some[uint64](math.MaxInt32), true},
		{"zero", utils.Some[uint64](0), false},
		{"over_max_int32", utils.Some[uint64](math.MaxInt32 + 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := validAutobahnFileConfig()
			require.NoError(t, fc.Validate())
			fc.MaxConcurrentCheckTx = tc.value
			if tc.ok {
				require.NoError(t, fc.Validate())
			} else {
				require.Error(t, fc.Validate())
			}
		})
	}
}

func TestAutobahnBlockDBConfig_LittBlockConfig(t *testing.T) {
	dir := t.TempDir()
	const (
		wantRetention = 2 * time.Hour
		wantGCPeriod  = 3 * time.Second
	)
	cfg, err := (AutobahnBlockDBConfig{
		Retention: utils.Some(utils.Duration(wantRetention)),
		GCPeriod:  utils.Some(utils.Duration(wantGCPeriod)),
	}).LittBlockConfig(dir)
	require.NoError(t, err)
	require.Equal(t, wantRetention, cfg.RetentionTime)
	require.Equal(t, wantGCPeriod, cfg.Litt.GCPeriod)
	require.True(t, cfg.Litt.Fsync)
}

func TestAutobahnFileConfig_ValidateEVMGovernance(t *testing.T) {
	voterA := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	voterB := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	badParams := gov.DefaultParams()
	badParams.VotingPeriod = 0
	for _, tc := range []struct {
		name       string
		governance utils.Option[gov.Params]
		voters     []utils.Option[common.Address]
		ok         bool
	}{
		{"disabled", utils.None[gov.Params](), []utils.Option[common.Address]{utils.None[common.Address](), utils.None[common.Address]()}, true},
		{"disabled_with_voter", utils.None[gov.Params](), []utils.Option[common.Address]{utils.Some(voterA), utils.None[common.Address]()}, false},
		{"enabled", utils.Some(gov.DefaultParams()), []utils.Option[common.Address]{utils.Some(voterA), utils.Some(voterB)}, true},
		{"enabled_missing_voter", utils.Some(gov.DefaultParams()), []utils.Option[common.Address]{utils.Some(voterA), utils.None[common.Address]()}, false},
		{"enabled_zero_voter", utils.Some(gov.DefaultParams()), []utils.Option[common.Address]{utils.Some(voterA), utils.Some(common.Address{})}, false},
		{"enabled_shared_voter", utils.Some(gov.DefaultParams()), []utils.Option[common.Address]{utils.Some(voterA), utils.Some(voterA)}, false},
		{"enabled_invalid_params", utils.Some(badParams), []utils.Option[common.Address]{utils.Some(voterA), utils.Some(voterB)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := validAutobahnFileConfig()
			validator := fc.Validators[0]
			fc.Validators = nil
			for _, voter := range tc.voters {
				validator.EVMVoter = voter
				fc.Validators = append(fc.Validators, validator)
			}
			fc.EVMGovernance = tc.governance
			if tc.ok {
				require.NoError(t, fc.Validate())
			} else {
				require.Error(t, fc.Validate())
			}
		})
	}
}

func TestAutobahnFileConfig_EVMGovernanceJSONRoundTrip(t *testing.T) {
	fc := validAutobahnFileConfig()
	fc.Validators[0].EVMVoter = utils.Some(common.HexToAddress("0x00000000000000000000000000000000000000a1"))
	params := gov.DefaultParams()
	params.VotingPeriod = 42
	fc.EVMGovernance = utils.Some(params)
	encoded, err := json.Marshal(fc)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"evm_voter":"0x00000000000000000000000000000000000000a1"`)
	require.Contains(t, string(encoded), `"quorum":"0.334000000000000000"`)
	var got AutobahnFileConfig
	require.NoError(t, json.Unmarshal(encoded, &got))
	require.NoError(t, got.Validate())
	require.Equal(t, fc.Validators[0].EVMVoter, got.Validators[0].EVMVoter)
	require.Equal(t, fc.EVMGovernance, got.EVMGovernance)

	fc = validAutobahnFileConfig()
	encoded, err = json.Marshal(fc)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "evm_voter")
	require.NotContains(t, string(encoded), "evm_governance")
}
