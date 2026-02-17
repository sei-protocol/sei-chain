package data

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type Snapshot struct {
	Blocks       map[types.GlobalBlockNumber]*types.Block
	QCs          map[types.GlobalBlockNumber]*types.FullCommitQC
	AppProposals map[types.GlobalBlockNumber]*types.AppProposal
}

func newSnapshot() Snapshot {
	return Snapshot{
		Blocks:       map[types.GlobalBlockNumber]*types.Block{},
		QCs:          map[types.GlobalBlockNumber]*types.FullCommitQC{},
		AppProposals: map[types.GlobalBlockNumber]*types.AppProposal{},
	}
}

func snapshot(s *State) Snapshot {
	for inner := range s.inner.Lock() {
		aps := map[types.GlobalBlockNumber]*types.AppProposal{}
		for n, apt := range inner.appProposals {
			aps[n] = apt.proposal
		}
		return Snapshot{
			QCs:          maps.Clone(inner.qcs),
			Blocks:       maps.Clone(inner.blocks),
			AppProposals: aps,
		}
	}
	panic("unreachable")
}

func TestState(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := NewState(&Config{
			Committee: committee,
		}, utils.None[BlockStore]())
		s.SpawnBgNamed("state.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		want := newSnapshot()
		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := TestCommitQC(rng, committee, keys, prev)
			prev = utils.Some(qc.QC())
			if err := state.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("state.PushQC(): %w", err)
			}
			gr := qc.QC().GlobalRange()
			for n := gr.First; n < gr.Next; n += 1 {
				want.QCs[n] = qc
				want.Blocks[n] = blocks[n-gr.First]
			}
			if err := utils.TestDiff(want, snapshot(state)); err != nil {
				return fmt.Errorf("snapshot: %w", err)
			}
		}
		for n, wantB := range want.Blocks {
			gotB, err := state.Block(ctx, n)
			if err != nil {
				return fmt.Errorf("state.Block(%v): %w", n, err)
			}
			if err := utils.TestDiff(wantB, gotB); err != nil {
				return fmt.Errorf("state.Block(%v): %w", n, err)
			}

			gotB, err = state.TryBlock(n)
			if err != nil {
				return fmt.Errorf("state.TryBlock(%v): %w", n, err)
			}
			if err := utils.TestDiff(wantB, gotB); err != nil {
				return fmt.Errorf("state.TryBlock(%v): %w", n, err)
			}

			wantG := &types.GlobalBlock{
				GlobalNumber:  n,
				Header:        wantB.Header(),
				Payload:       wantB.Payload(),
				FinalAppState: want.QCs[n].QC().Proposal().App(),
			}
			gotG, err := state.GlobalBlock(ctx, n)
			if err != nil {
				return fmt.Errorf("state.GlobalBlock(%v): %w", n, err)
			}
			if err := utils.TestDiff(wantG, gotG); err != nil {
				return fmt.Errorf("state.GlobalBlock(%v): %w", n, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPushQCStaleQCDoesNotCorruptState(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	state := NewState(&Config{
		Committee: committee,
	}, utils.None[BlockStore]())

	// Push a valid QC to advance inner.nextQC.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	nextQC := qc1.QC().GlobalRange().Next

	// Construct a malicious QC signed by non-committee keys.
	// It starts from block 0 (stale) but extends beyond nextQC.
	badKeys := make([]types.SecretKey, len(keys))
	for i := range badKeys {
		badKeys[i] = types.GenSecretKey(rng)
	}
	blocksPerLane := int(nextQC/types.GlobalBlockNumber(committee.Lanes().Len())) + 2
	laneBlocks := map[types.LaneID][]*types.Block{}
	for _, lane := range committee.Lanes().All() {
		for range blocksPerLane {
			var b *types.Block
			if bs := laneBlocks[lane]; len(bs) > 0 {
				parent := bs[len(bs)-1]
				b = types.NewBlock(lane, parent.Header().Next(), parent.Header().Hash(), types.GenPayload(rng))
			} else {
				b = types.NewBlock(lane, 0, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
			}
			laneBlocks[lane] = append(laneBlocks[lane], b)
		}
	}
	laneQCs := map[types.LaneID]*types.LaneQC{}
	var headers []*types.BlockHeader
	for _, lane := range committee.Lanes().All() {
		bs := laneBlocks[lane]
		laneQCs[lane] = TestLaneQC(badKeys, bs[len(bs)-1].Header())
		for _, b := range bs {
			headers = append(headers, b.Header())
		}
	}
	viewSpec := types.ViewSpec{CommitQC: utils.None[*types.CommitQC]()}
	proposal, _ := types.NewProposal(
		types.GenSecretKey(rng),
		committee,
		viewSpec,
		time.Now(),
		laneQCs,
		utils.None[*types.AppQC](),
	)
	malGR := proposal.Proposal().Msg().GlobalRange()
	require.Less(t, malGR.First, nextQC, "test setup: malicious gr.First must be < nextQC")
	require.Greater(t, malGR.Next, nextQC, "test setup: malicious gr.Next must be > nextQC")

	votes := make([]*types.Signed[*types.CommitVote], 0, len(badKeys))
	for _, k := range badKeys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(proposal.Proposal().Msg())))
	}
	maliciousQC := types.NewFullCommitQC(types.NewCommitQC(votes), headers)

	// Stale QC is silently ignored (verification is skipped, insert guard prevents
	// advancing nextQC). No error is returned.
	require.NoError(t, state.PushQC(ctx, maliciousQC, nil))

	// Verify state is not corrupted: the next valid QC should still be accepted.
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
	require.NoError(t, state.PushQC(ctx, qc2, blocks2))
}

func TestExecution(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := NewState(&Config{
			Committee: committee,
		}, utils.None[BlockStore]())
		s.SpawnBgNamed("state.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := TestCommitQC(rng, committee, keys, prev)
			if err := state.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("state.PushQC(): %w", err)
			}
			prev = utils.Some(qc.QC())
			gr := qc.QC().GlobalRange()
			if err := state.PushAppHash(gr.Next, types.GenAppHash(rng)); err == nil {
				return errors.New("PushAppProposal expected to fail on non-finalized blocks")
			}
			for n := gr.First; n < gr.Next; n += 1 {
				if err := state.PushAppHash(n, types.GenAppHash(rng)); err != nil {
					return fmt.Errorf("state.PushAppProposal(): %w", err)
				}
				if err := state.PushAppHash(n, types.GenAppHash(rng)); err == nil {
					return errors.New("PushAppProposal expected to fail on duplicate proposal")
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
