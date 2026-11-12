package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestAutobahnParamsValidate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		timeout  time.Duration
		interval time.Duration
		valid    bool
	}{
		{"unlimited", time.Second, 0, true},
		{"zero timeout", 0, 0, false},
		{"negative timeout", -time.Second, 0, false},
		{"negative interval", time.Second, -time.Nanosecond, false},
		{"interval below timeout", time.Second, 500 * time.Millisecond, true},
		{"interval equals timeout", time.Second, time.Second, false},
		{"interval above timeout", time.Second, 2 * time.Second, false},
		{"subsecond timeout", 500 * time.Millisecond, 250 * time.Millisecond, true},
		{"just short enough", time.Second, time.Second - time.Nanosecond, true},
		{"just too long", time.Second, time.Second + time.Nanosecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := AutobahnParams{ViewTimeout: tc.timeout, MinRoadBlockInverval: tc.interval}
			doc := &GenesisDoc{ChainID: "test-chain", Autobahn: &params}
			if !tc.valid {
				require.Error(t, params.Validate())
				require.Error(t, doc.ValidateAndComplete())
				return
			}
			require.NoError(t, params.Validate())
			require.NoError(t, doc.ValidateAndComplete())
			data, err := json.Marshal(doc)
			require.NoError(t, err)
			decoded, err := GenesisDocFromJSON(data)
			require.NoError(t, err)
			require.Equal(t, params, *decoded.Autobahn)
		})
	}
}

func TestAutobahnParamsIntervalJSON(t *testing.T) {
	raw := []byte(`{"chain_id":"test-chain","autobahn":{"view_timeout":"1500000000","min_road_block_inverval":"500000000"}}`)
	doc, err := GenesisDocFromJSON(raw)
	require.NoError(t, err)
	require.Equal(t, 500*time.Millisecond, doc.Autobahn.MinRoadBlockInverval)

	raw = []byte(`{"chain_id":"test-chain","autobahn":{"view_timeout":"1500000000"}}`)
	doc, err = GenesisDocFromJSON(raw)
	require.NoError(t, err)
	require.Equal(t, time.Duration(0), doc.Autobahn.MinRoadBlockInverval)
	require.NoError(t, DefaultAutobahnParams().Validate())
}
