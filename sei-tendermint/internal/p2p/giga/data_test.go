package giga

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type testNode struct {
	data      *data.State
	consensus *consensus.State
	service   *Service
}

func defaultViewTimeout(view types.View) time.Duration { return time.Hour }

func newTestNode(committee *types.Committee, cfg *consensus.Config) *testNode {
	dataState := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())
	consensusState, err := consensus.NewState(cfg, dataState)
	if err != nil {
		panic(fmt.Sprintf("consensus.NewState(): %v", err))
	}
	return &testNode{
		data:      dataState,
		consensus: consensusState,
		service:   NewService(consensusState),
	}
}

func (n *testNode) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return n.data.Run(ctx) })
		s.Spawn(func() error { return n.consensus.Run(ctx) })
		s.Spawn(func() error { return n.service.Run(ctx) })
		return nil
	})
}

type testEnv struct {
	committee *types.Committee
	nodes     map[types.PublicKey]*testNode
}

func newTestEnv(committee *types.Committee) *testEnv {
	return &testEnv{committee, map[types.PublicKey]*testNode{}}
}

// Call AddNode BEFORE Run.
func (e *testEnv) AddNode(key types.SecretKey) *testNode {
	n := newTestNode(e.committee, &consensus.Config{
		Key: key,
		ViewTimeout: func(view types.View) time.Duration {
			if _, ok := e.nodes[e.committee.Leader(view)]; ok {
				return time.Hour
			}
			return 0
		},
	})
	e.nodes[key.Public()] = n
	return n
}

func (e *testEnv) Run(ctx context.Context) error {
	return utils.IgnoreAfterCancel(ctx, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, x := range e.nodes {
			s.SpawnNamed("node", func() error { return x.Run(ctx) })
			for _, y := range e.nodes {
				xConn, yConn := conn.NewTestConn()
				server := rpc.NewServer[API]()
				client := rpc.NewClient[API]()
				s.SpawnNamed("mux server", func() error { return server.Run(ctx, xConn) })
				s.SpawnNamed("mux client", func() error { return client.Run(ctx, yConn) })
				s.SpawnNamed("RunServer", func() error { return x.service.RunServer(ctx, server) })
				s.SpawnNamed("RunClient", func() error { return y.service.RunClient(ctx, client) })
			}
		}
		return nil
	}))
}

func TestDataClientServer(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 2)
	env := newTestEnv(committee)
	server := env.AddNode(keys[0])
	client := env.AddNode(keys[1])
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })

		t.Logf("push data")
		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := data.TestCommitQC(rng, committee, keys, prev)
			if err := server.data.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("serverState.PushQC(): %w", err)
			}
			prev = utils.Some(qc.QC())
		}
		t.Logf("wait for replication")
		for n := range server.data.NextBlock() {
			want, err := server.data.GlobalBlock(ctx, n)
			if err != nil {
				return fmt.Errorf("serverState.FinalBlock(): %w", err)
			}
			got, err := client.data.GlobalBlock(ctx, n)
			if err != nil {
				return fmt.Errorf("clientState.FinalBlock(): %w", err)
			}
			if err := utils.TestDiff(want, got); err != nil {
				return err
			}

			wantQC, err := server.data.QC(ctx, n)
			if err != nil {
				return fmt.Errorf("serverState.CommitQC(): %w", err)
			}
			gotQC, err := client.data.QC(ctx, n)
			if err != nil {
				return fmt.Errorf("clientState.CommitQC(): %w", err)
			}
			if err := utils.TestDiff(wantQC, gotQC); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
