package consensus

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestReconstructRoadBlocksAcrossEpochs(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()

	// epoch length = 3, 4 validators: 3 honest, 1 byzantine
	keys := utils.GenSliceN(rng, 4, types.GenSecretKey)
	weights := map[types.PublicKey]uint64{}
	for _, key := range keys {
		weights[key.Public()] = 1
	}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	registry := utils.OrPanic1(epoch.NewRegistry(committee, 0, time.Now(), 3, utils.None[[]byte]()))
	shared, isolated := newTestDataState(t, registry), newTestDataState(t, registry)

	// Only byzantine validator can be the leader.
	viewTimeout := func(view types.View) time.Duration {
		if committee.Leader(view) == keys[3].Public() {
			return time.Hour
		}
		return 0
	}
	var states []*State
	for i, key := range keys {
		ds := shared
		if i == 3 {
			ds = isolated
		}
		state := utils.OrPanic1(NewState(&Config{
			Key: key, ViewTimeout: viewTimeout, ProposalTimeout: time.Hour, MaxRate: rate.Inf,
			PersistentStateDir: utils.None[string](),
		}, ds))
		t.Cleanup(func() { require.NoError(t, state.Close()) })
		states = append(states, state)
	}
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		anchor := shared.Anchor().Load()
		// Run states.
		for _, ds := range []*data.State{shared, isolated} {
			s.SpawnBg(func() error { return utils.IgnoreCancel(ds.Run(ctx)) })
		}
		for _, state := range states {
			s.SpawnBg(func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		}
		byzantine := states[3]
		if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			// Run all consensus connections.
			for _, from := range states {
				for _, to := range states {
					s.SpawnBg(func() error { return utils.IgnoreCancel(RunTestConn(ctx, from, to)) })
				}
			}
			// Run avail connections from A,B,C,D to D.
			for _, from := range states {
				s.SpawnBg(func() error { return utils.IgnoreCancel(avail.RunTestConn(ctx, from.Avail(), byzantine.Avail())) })
			}
			// D finalizes one block from A,B,C in each of two epochs.
			for _, epoch := range anchor.Epochs {
				rr := epoch.RoadRange()
				for road := rr.First; road < rr.Next; road += 1 {
					// WARNING: here we utilize the fact that nodes need a loopback avail connection to receive their own vote.
					// A test case which exercises the same scenario BUT with loopback connections would be way more involved.
					producer := states[road%3]
					av := producer.Avail()
					lane := av.LocalLane().OrPanic("producer lane")
					if _, err := av.ProduceLocalBlock(lane, av.NextBlock(lane), types.GenPayload(rng)); err != nil {
						return err
					}
					// Byzantine node is guaranteed to get CommitQC.
					qc, err := byzantine.Avail().CommitQC(ctx, road)
					if err != nil {
						return err
					}
					// Push CommitQC to other nodes - they might not receive it, because of restricted avail channels.
					for _, state := range states[:3] {
						if err := state.Avail().PushCommitQC(ctx, qc); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
		// FullCommitQCs should NOT get reconstructed.
		for _, epoch := range anchor.Epochs {
			rr := epoch.RoadRange()
			for road := rr.First; road < rr.Next; road++ {
				if _, err := shared.TryQC(road); !errors.Is(err, types.ErrNotFound) {
					return fmt.Errorf("shared data reconstructed road %d before final availability exchange: %v", road, err)
				}
			}
		}

		// Run full network of honest nodes.
		s.SpawnBg(func() error { return utils.IgnoreCancel(RunTestNetwork(ctx, states[:3])) })
		// FullCommitQCs should get reconstructed.
		for _, ep := range anchor.Epochs {
			for road := ep.RoadRange().First; road < ep.RoadRange().Next; road++ {
				if _, err := shared.QC(ctx, road); err != nil {
					return fmt.Errorf("reconstruction of road %d failed: %w", road, err)
				}
			}
		}
		return nil
	}))
}
