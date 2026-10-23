package config

import (
	"encoding/json"
	"math"
	"net/url"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
)

func TestURLJSONReencode(t *testing.T) {
	want := URL{URL: &url.URL{
		Scheme:   "wss",
		Host:     "example.com:8546",
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

func TestURLUnmarshalAcceptsWebSocket(t *testing.T) {
	for _, raw := range []string{"ws://example.com:8546", "wss://example.com:8546"} {
		var got URL
		require.NoError(t, json.Unmarshal([]byte(`"`+raw+`"`), &got))
		require.NotNil(t, got.URL)
		require.Equal(t, raw, got.String())
	}
}

func TestURLUnmarshalRejectsNonWebSocket(t *testing.T) {
	for _, raw := range []string{`"http://example.com:8545"`, `"https://example.com:8545"`, `"file://localhost/rpc"`} {
		var got URL
		require.Error(t, json.Unmarshal([]byte(raw), &got))
	}
}

func validAutobahnFileConfig() AutobahnFileConfig {
	return AutobahnFileConfig{
		Validators: []AutobahnValidator{{
			EVMRPC: URL{URL: &url.URL{Scheme: "ws", Host: "localhost:8546"}},
		}},
		MaxTxsPerBlock: 1,
		BlockInterval:  utils.Duration(time.Second),
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

func governedAutobahnFileConfig() AutobahnFileConfig {
	fc := validAutobahnFileConfig()
	second := fc.Validators[0]
	fc.Validators[0].EVMVoter = utils.Some(common.HexToAddress("0x01"))
	second.EVMVoter = utils.Some(common.HexToAddress("0x02"))
	fc.Validators = append(fc.Validators, second)
	fc.EVMGovernance = utils.Some(EVMGovernance{Params: gov.DefaultParams(), StartingProposalID: 7})
	return fc
}

func TestAutobahnFileConfig_ValidateEVMGovernance(t *testing.T) {
	valid := governedAutobahnFileConfig()
	require.NoError(t, valid.Validate())
	for name, edit := range map[string]func(*AutobahnFileConfig){
		"voter without governance": func(fc *AutobahnFileConfig) { fc.EVMGovernance = utils.None[EVMGovernance]() },
		"missing voter":            func(fc *AutobahnFileConfig) { fc.Validators[1].EVMVoter = utils.None[common.Address]() },
		"zero voter":               func(fc *AutobahnFileConfig) { fc.Validators[1].EVMVoter = utils.Some(common.Address{}) },
		"shared voter":             func(fc *AutobahnFileConfig) { fc.Validators[1].EVMVoter = fc.Validators[0].EVMVoter },
		"invalid params": func(fc *AutobahnFileConfig) {
			g, _ := fc.EVMGovernance.Get()
			g.Params.VotingPeriod = 0
			fc.EVMGovernance = utils.Some(g)
		},
	} {
		fc := governedAutobahnFileConfig()
		edit(&fc)
		require.Error(t, fc.Validate(), name)
	}
}

func TestAutobahnFileConfig_EVMGovernanceJSON(t *testing.T) {
	plain, err := json.Marshal(validAutobahnFileConfig())
	require.NoError(t, err)
	require.NotContains(t, string(plain), "evm_governance")
	require.NotContains(t, string(plain), "evm_voter")

	want := governedAutobahnFileConfig()
	encoded, err := json.Marshal(want)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"starting_proposal_id":7`)
	require.Contains(t, string(encoded), `"voting_period":172800`)
	var got AutobahnFileConfig
	require.NoError(t, json.Unmarshal(encoded, &got))
	require.NoError(t, got.Validate())
	require.Equal(t, want.EVMGovernance, got.EVMGovernance)
	require.Equal(t, want.Validators[1].EVMVoter, got.Validators[1].EVMVoter)
}
