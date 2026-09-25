package config

import (
	"encoding/json"
	"math"
	"net/url"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
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
