package statesync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	tmjson "github.com/sei-protocol/sei-chain/sei-tendermint/libs/json"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// trustPeriodQueryTimeout bounds how long one RPC server may take to report the trust period.
const trustPeriodQueryTimeout = 10 * time.Second

var errTrustPeriodNotSet = errors.New("the chain sets no state sync trust period")

// trustPeriodClient answers ABCI queries against an RPC server's application state.
type trustPeriodClient interface {
	ABCIQueryWithOptions(ctx context.Context, path string, data tmbytes.HexBytes,
		opts rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error)
	Remote() string
}

// rpcTrustPeriod returns the trust period to verify light blocks from servers with: the
// lowest one any of them reports the chain sets, or config.DefaultStateSyncTrustPeriod
// when none reports one.
func rpcTrustPeriod(ctx context.Context, servers []string) (time.Duration, error) {
	clients := make([]trustPeriodClient, 0, len(servers))
	for _, server := range servers {
		client, err := rpcClient(server)
		if err != nil {
			return 0, fmt.Errorf("failed to set up RPC client: %w", err)
		}
		clients = append(clients, client)
	}
	return lowestTrustPeriod(ctx, clients), nil
}

// lowestTrustPeriod returns the lowest trust period any of clients reports the chain
// sets, or config.DefaultStateSyncTrustPeriod when none reports one.
func lowestTrustPeriod(ctx context.Context, clients []trustPeriodClient) time.Duration {
	lowest := utils.None[time.Duration]()
	for _, client := range clients {
		trustPeriod, err := queryTrustPeriod(ctx, client)
		if err != nil {
			logger.Info("RPC server reported no state sync trust period", "server", client.Remote(), "err", err)
			continue
		}
		logger.Info("RPC server reported a state sync trust period", "server", client.Remote(), "trustPeriod", trustPeriod)
		// The lowest wins, so one honest server keeps the others from lengthening it.
		if current, ok := lowest.Get(); !ok || trustPeriod < current {
			lowest = utils.Some(trustPeriod)
		}
	}
	trustPeriod, ok := lowest.Get()
	if !ok {
		logger.Info("no RPC server reported a state sync trust period; using the default",
			"trustPeriod", config.DefaultStateSyncTrustPeriod)
		return config.DefaultStateSyncTrustPeriod
	}
	logger.Info("using the lowest state sync trust period the RPC servers reported", "trustPeriod", trustPeriod)
	return trustPeriod
}

// queryTrustPeriod returns the trust period held in client's application state.
func queryTrustPeriod(ctx context.Context, client trustPeriodClient) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, trustPeriodQueryTimeout)
	defer cancel()
	res, err := client.ABCIQueryWithOptions(ctx, config.StateSyncTrustPeriodQueryPath,
		[]byte(config.StateSyncTrustPeriodQueryKey), rpcclient.DefaultABCIQueryOptions)
	if err != nil {
		return 0, err
	}
	if !res.Response.IsOK() {
		return 0, fmt.Errorf("query failed with code %d: %s", res.Response.Code, res.Response.Log)
	}
	if len(res.Response.Value) == 0 {
		return 0, errTrustPeriodNotSet
	}
	var trustPeriod time.Duration
	if err := tmjson.Unmarshal(res.Response.Value, &trustPeriod); err != nil {
		return 0, fmt.Errorf("decoding trust period %q: %w", res.Response.Value, err)
	}
	if trustPeriod <= 0 {
		return 0, fmt.Errorf("trust period %s is not positive", trustPeriod)
	}
	return trustPeriod, nil
}
