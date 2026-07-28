package config

import (
	"encoding/json"
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
	require.Equal(t, wantRetention, cfg.Retention)
	require.Equal(t, wantGCPeriod, cfg.Litt.GCPeriod)
	require.True(t, cfg.Litt.Fsync)
}
