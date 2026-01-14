package consensus

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-stream/config"
	"github.com/sei-protocol/sei-stream/pkg/grpcutils"
	"github.com/sei-protocol/sei-stream/pkg/service"
	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

// Client is a StreamAPIClient wrapper capable of sending consensus state updates.
type client struct {
	protocol.StreamAPIClient
	cfg   *config.PeerConfig
	state *State
}

// Sends a consensus message to the peer whenever atomic watch is updated.
func sendUpdates[T interface {
	comparable
	types.ConsensusReq
}](
	ctx context.Context,
	c *client,
	w utils.AtomicRecv[utils.Option[T]],
) error {
	msgType := fmt.Sprintf("%T", utils.Zero[T]())
	o := c.state.metrics.RPCClientLatency(msgType)
	return c.cfg.Retry(ctx, msgType, func(ctx context.Context) error {
		// We maintain a long lived RPC stream and retransmit the latest message on reconnect.
		var last utils.Option[T]
		stream, err := c.Consensus(ctx)
		if err != nil {
			return fmt.Errorf("p.client.Consensus(): %w", err)
		}
		for {
			if last, err = w.Wait(ctx, func(m utils.Option[T]) bool { return m != last }); err != nil {
				return err
			}
			last, ok := last.Get()
			if !ok {
				continue
			}
			t0 := time.Now()
			if err := stream.Send(types.ConsensusReqConv.Encode(last)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
			// We will have at most 1 inflight consensus message of each type.
			if _, err := stream.Recv(); err != nil {
				return fmt.Errorf("stream.Recv(): %w", err)
			}
			o.Observe(time.Since(t0).Seconds())
		}
	})
}

// sendPings periodically sends Ping messages.
func (c *client) sendPings(ctx context.Context) error {
	msgType := "Ping"
	o := c.state.metrics.RPCClientLatency(msgType)
	return c.cfg.Retry(ctx, msgType, func(ctx context.Context) error {
		stream, err := c.Ping(ctx)
		if err != nil {
			return fmt.Errorf("p.client.Ping(): %w", err)
		}
		for {
			if err := utils.Sleep(ctx, 10*time.Second); err != nil {
				return err
			}
			t0 := time.Now()
			if err := stream.Send(&protocol.PingReq{}); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
			if _, err := stream.Recv(); err != nil {
				return fmt.Errorf("stream.Recv(): %w", err)
			}
			o.Observe(time.Since(t0).Seconds())
		}
	})
}

// Run sends newest consensus messages to the peer.
func (c *client) Run(ctx context.Context) error {
	return service.Run(ctx, func(ctx context.Context, s service.Scope) error {
		// Send updates about new consensus messages.
		s.Spawn(func() error { return sendUpdates(ctx, c, c.state.myProposal.Subscribe()) })
		s.Spawn(func() error { return sendUpdates(ctx, c, c.state.myPrepareVote.Subscribe()) })
		s.Spawn(func() error { return sendUpdates(ctx, c, c.state.myCommitVote.Subscribe()) })
		s.Spawn(func() error { return sendUpdates(ctx, c, c.state.myTimeoutVote.Subscribe()) })
		s.Spawn(func() error { return sendUpdates(ctx, c, c.state.myTimeoutQC.Subscribe()) })
		s.Spawn(func() error { return c.state.avail.RunClient(ctx, c.cfg) })
		return c.sendPings(ctx)
	})
}

// RunClientPool runs a pool of RPC clients for consensus state.
// NOTE: the traffic received on the consensus TCP connection is low,
// except for spikes when the server is the leader (and sends proposals).
//
// With default settings, TCP is aggresively decreasing window size
// of low traffic connections, which causes increased latency of proposal RPCs.
// To mitigate that, set:
//
//	sysctl -w net.ipv4.tcp_slow_start_after_idle=0
//
// Unfortunately this is a system-level setting, not a per-connection one,
// so it will affect all TCP connections on the system.
func (s *State) RunClientPool(ctx context.Context, cfgs []*config.PeerConfig) error {
	return utils.IgnoreCancel(service.Run(ctx, func(ctx context.Context, scope service.Scope) error {
		for _, cfg := range cfgs {
			conn, err := grpcutils.NewClient(cfg.Address)
			if err != nil {
				return fmt.Errorf("grpc.Dial(%q): %w", cfg.Address, err)
			}
			c := &client{
				cfg:             cfg,
				state:           s,
				StreamAPIClient: protocol.NewStreamAPIClient(conn),
			}
			scope.SpawnNamed(cfg.Address, func() error { return c.Run(ctx) })
		}
		return nil
	}))
}
