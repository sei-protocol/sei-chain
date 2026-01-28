package autobahn

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/tendermint/tendermint/internal/autobahn/types"
	"github.com/tendermint/tendermint/libs/utils"
)

// PeerConfig stores the configuration of a peer.
type PeerConfig struct {
	Name       types.NodeID                  `json:"name"`
	Key        utils.Option[types.PublicKey] `json:"key,omitzero"`
	Address    string                        `json:"address"`
	Delay      utils.Option[utils.Duration]  `json:"delay,omitzero"`
	RetryDelay utils.Option[utils.Duration]  `json:"retry_delay,omitzero"`
}

// GetKey returns the key of the peer.
func (c *PeerConfig) GetKey() types.PublicKey {
	if key, ok := c.Key.Get(); ok {
		return key
	}
	// If the key is not set, we use a temporary fallback
	// derived from the peer's name.
	return types.TestSecretKey(c.Name).Public()
}

// getRetryDelay returns the delay between reconnect attempts.
func (c *PeerConfig) getRetryDelay() time.Duration {
	return c.RetryDelay.Or(utils.Duration(10 * time.Second)).Duration()
}

// Retry retries a sending function `f` until success (with constant backoff on error).
// This function is expected to send a streaming RPC request to the peer.
func (c *PeerConfig) Retry(ctx context.Context, name string, f func(ctx context.Context) error) error {
	retryDelay := c.getRetryDelay()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Artificial delay simulating real world network delay.
		if d, ok := c.Delay.Get(); ok {
			if err := utils.Sleep(ctx, d.Duration()); err != nil {
				return err
			}
		}
		// Create a separate context for each attempt:
		// Cancelling immediately prevents the gRPC streams created by f from leaking.
		fctx, cancel := context.WithCancel(ctx)
		err := f(fctx)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Info().Err(err).Msg(name)
		// Since there is no timeout on sending the message and
		// RPC client internally retries in case of connection problems
		// (aka transparent retry),
		// there is no need for us to aggresively retry here.
		// The most likely reason for the failure is that the peer is not reachable,
		// in which case we should wait a while before retrying.
		if err := utils.Sleep(ctx, retryDelay); err != nil {
			return err
		}
	}
}

// Config is the config of the stream component.
type Config struct {
	// ViewTimeout is the timeout for a view in the consensus protocol.
	ViewTimeout utils.Option[utils.Duration] `json:"view_timeout,omitzero"`
	// Peers is a temporary configuration to be removed in the future.
	Peers []*PeerConfig `json:"peers"`
	// KeyPath is where the signing key is stored (default = config path).
	KeyPath string `json:"key_path,omitempty"`
	// Stream node prunes blocks after PruneAfter time since execution.
	// If PruneAfter is not set, the node will not prune blocks and memory usage will grow indefinitely.
	PruneAfter utils.Option[utils.Duration] `json:"prune_after,omitzero"`
	// Rate of mock execution.
	MockExecutorTxsPerSecond uint64 `json:"mock_executor_txs_per_second,omitzero"`
	// Cap on the total txs per second that consensus is allowed to process.
	TxsPerSecondCap utils.Option[uint64] `json:"txs_per_second_cap,omitzero"`
	// Offline nodes ratio is the ratio of offline nodes in the committee.
	// Used for simulation purposes. Value should be in range [0, 1/3).
	OfflineNodesRatio utils.Option[float64] `json:"offline_nodes_ratio,omitzero"`
}

// GetViewTimeout returns the ViewTimeout.
// Returns the default value if not set.
func (c *Config) GetViewTimeout() time.Duration {
	return time.Duration(c.ViewTimeout.Or(utils.Duration(2 * time.Second)))
}

// Committee Returns the consensus committee specified in the config.
func (c *Config) Committee() (*types.Committee, error) {
	var replicas []types.PublicKey
	for _, p := range c.Peers {
		replicas = append(replicas, p.GetKey())
	}
	return types.NewRoundRobinElection(replicas)
}

// IsOnline checks if the consensus node is supposed to be online.
func (c *Config) IsOnline(myKey types.PublicKey) (bool, error) {
	committee, err := c.Committee()
	if err != nil {
		return false, err
	}
	// If the node is not in the committee, it just doesn't participate in consensus.
	if !committee.Replicas().Has(myKey) {
		return false, nil
	}
	// If the offline nodes ratio is not set, all committee nodes are online.
	rate, ok := c.OfflineNodesRatio.Get()
	if !ok {
		return true, nil
	}
	// Cap the number of offline nodes by the number of faulty nodes that the consensus can tolerate.
	offlineNodes := min(committee.Faulty(), int(float64(committee.Replicas().Len())*rate))
	for i := range offlineNodes {
		if committee.Replicas().At(i) == myKey {
			return false, nil
		}
	}
	return true, nil
}
