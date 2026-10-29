package statesync

import (
	"context"
	"errors"
	"testing"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	tmjson "github.com/sei-protocol/sei-chain/sei-tendermint/libs/json"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// fakeTrustPeriodClient answers the trust period query with response, or fails with err.
type fakeTrustPeriodClient struct {
	remote   string
	response abci.ResponseQuery
	err      error
}

func (c fakeTrustPeriodClient) ABCIQueryWithOptions(
	_ context.Context, path string, data tmbytes.HexBytes, _ rpcclient.ABCIQueryOptions,
) (*coretypes.ResultABCIQuery, error) {
	if c.err != nil {
		return nil, c.err
	}
	if path != config.StateSyncTrustPeriodQueryPath || string(data) != config.StateSyncTrustPeriodQueryKey {
		return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Code: 1, Log: "unexpected query"}}, nil
	}
	return &coretypes.ResultABCIQuery{Response: c.response}, nil
}

func (c fakeTrustPeriodClient) Remote() string { return c.remote }

// reporting returns a client whose chain sets trustPeriod, encoded as the params store holds it.
func reporting(remote string, trustPeriod time.Duration) fakeTrustPeriodClient {
	value := utils.OrPanic1(tmjson.Marshal(trustPeriod))
	return fakeTrustPeriodClient{remote: remote, response: abci.ResponseQuery{Value: value}}
}

func TestLowestTrustPeriod(t *testing.T) {
	testcases := map[string]struct {
		clients []trustPeriodClient
		want    time.Duration
	}{
		"the lowest period any server reports": {
			clients: []trustPeriodClient{reporting("long", 1000*time.Hour), reporting("short", 48*time.Hour)},
			want:    48 * time.Hour,
		},
		"a server that cannot be reached is skipped": {
			clients: []trustPeriodClient{
				fakeTrustPeriodClient{remote: "down", err: errors.New("connection refused")},
				reporting("up", 48*time.Hour),
			},
			want: 48 * time.Hour,
		},
		"a server whose chain sets none is skipped": {
			clients: []trustPeriodClient{fakeTrustPeriodClient{remote: "unset"}, reporting("set", 48*time.Hour)},
			want:    48 * time.Hour,
		},
		"a failed query is skipped": {
			clients: []trustPeriodClient{
				fakeTrustPeriodClient{remote: "failing", response: abci.ResponseQuery{Code: 1, Log: "no such store"}},
				reporting("set", 48*time.Hour),
			},
			want: 48 * time.Hour,
		},
		"a value that is not a duration is skipped": {
			clients: []trustPeriodClient{
				fakeTrustPeriodClient{remote: "garbled", response: abci.ResponseQuery{Value: []byte("not a duration")}},
				reporting("set", 48*time.Hour),
			},
			want: 48 * time.Hour,
		},
		"a period that is not positive is skipped": {
			clients: []trustPeriodClient{reporting("zero", 0), reporting("set", 48*time.Hour)},
			want:    48 * time.Hour,
		},
		"the default when no server reports one": {
			clients: []trustPeriodClient{fakeTrustPeriodClient{remote: "unset"}},
			want:    config.DefaultStateSyncTrustPeriod,
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, lowestTrustPeriod(t.Context(), tc.clients))
		})
	}
}
