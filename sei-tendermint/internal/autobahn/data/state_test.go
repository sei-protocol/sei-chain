package data

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"
	"testing/synctest"
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
	var malBlocks []*types.Block
	for _, lane := range committee.Lanes().All() {
		bs := laneBlocks[lane]
		laneQCs[lane] = TestLaneQC(badKeys, bs[len(bs)-1].Header())
		for _, b := range bs {
			headers = append(headers, b.Header())
			malBlocks = append(malBlocks, b)
		}
	}
	viewSpec := types.ViewSpec{CommitQC: utils.None[*types.CommitQC]()}
	leader := committee.Leader(viewSpec.View())
	var leaderKey types.SecretKey
	for _, k := range keys {
		if k.Public() == leader {
			leaderKey = k
			break
		}
	}
	proposal := utils.OrPanic1(types.NewProposal(
		leaderKey,
		committee,
		viewSpec,
		time.Now(),
		laneQCs,
		utils.None[*types.AppQC](),
	))
	malGR := proposal.Proposal().Msg().GlobalRange()
	require.Less(t, malGR.First, nextQC, "test setup: malicious gr.First must be < nextQC")
	require.Greater(t, malGR.Next, nextQC, "test setup: malicious gr.Next must be > nextQC")

	votes := make([]*types.Signed[*types.CommitVote], 0, len(badKeys))
	for _, k := range badKeys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(proposal.Proposal().Msg())))
	}
	maliciousQC := types.NewFullCommitQC(types.NewCommitQC(votes), headers)

	// Push the malicious QC with its blocks. Whether it returns an error is an
	// implementation detail — what matters is that the state is unchanged afterward.
	// Passing blocks (not nil) exercises the min(gr.Next, inner.nextQC) cap that
	// prevents out-of-bounds access when the malicious range extends beyond stored QCs.
	_ = state.PushQC(ctx, maliciousQC, malBlocks)

	// Verify state was not corrupted: all previously pushed QCs and blocks are intact.
	gr1 := qc1.QC().GlobalRange()
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state.QC(ctx, n)
		require.NoError(t, err)
		require.Equal(t, qc1, got)
	}
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state.TryBlock(n)
		require.NoError(t, err)
		require.Equal(t, blocks1[n-gr1.First], got)
	}

	// Verify nextQC did not advance beyond the valid range.
	for inner := range state.inner.Lock() {
		require.Equal(t, nextQC, inner.nextQC)
	}

	// Verify state is still functional: the next valid QC is accepted and visible.
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
	require.NoError(t, state.PushQC(ctx, qc2, blocks2))
	gr2 := qc2.QC().GlobalRange()
	for n := gr2.First; n < gr2.Next; n++ {
		got, err := state.QC(ctx, n)
		require.NoError(t, err)
		require.Equal(t, qc2, got)
	}
}

func TestPushQCIgnoresBlocksMatchingUnverifiedHeaders(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	state := NewState(&Config{
		Committee: committee,
	}, utils.None[BlockStore]())

	// Push qc1 with NO blocks — only the QC is stored.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushQC(ctx, qc1, nil))
	gr := qc1.QC().GlobalRange()

	// Build a tampered FullCommitQC: same CommitQC (same range) but with
	// different block headers (different payloads → different hashes).
	var fakeHeaders []*types.BlockHeader
	var fakeBlocks []*types.Block
	for _, orig := range qc1.Headers() {
		fb := types.NewBlock(orig.Lane(), orig.BlockNumber(), orig.ParentHash(), types.GenPayload(rng))
		fakeHeaders = append(fakeHeaders, fb.Header())
		fakeBlocks = append(fakeBlocks, fb)
	}
	tamperedQC := types.NewFullCommitQC(qc1.QC(), fakeHeaders)

	// Push the tampered QC with blocks that match the tampered headers.
	// needQC is false (range already covered), so the tampered QC is not
	// verified. Blocks must be matched against the stored QC's headers.
	_ = state.PushQC(ctx, tamperedQC, fakeBlocks)

	// Verify no fake blocks were inserted.
	for n := gr.First; n < gr.Next; n++ {
		_, err := state.TryBlock(n)
		require.ErrorIs(t, err, ErrNotFound)
	}

	// Push the real blocks (matching qc1's headers) and verify they work.
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	for i, n := 0, gr.First; n < gr.Next; n++ {
		got, err := state.TryBlock(n)
		require.NoError(t, err)
		require.Equal(t, blocks1[i], got)
		i++
	}
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

func TestPushBlockAcceptsBlockWithQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)

	state := NewState(&Config{
		Committee: committee,
	}, utils.None[BlockStore]())

	// Push QC without blocks.
	qc, blocks := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushQC(ctx, qc, nil))
	gr := qc.QC().GlobalRange()

	// PushBlock for a block whose QC is already present succeeds immediately.
	require.NoError(t, state.PushBlock(ctx, gr.First, blocks[0]))
	got, err := state.TryBlock(gr.First)
	require.NoError(t, err)
	require.Equal(t, blocks[0], got)
}

func TestPushBlockWaitsForQC(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		committee, keys := types.GenCommittee(rng, 3)

		state := NewState(&Config{
			Committee: committee,
		}, utils.None[BlockStore]())

		// Push first QC covering [0, N).
		qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
		require.NoError(t, state.PushQC(ctx, qc1, blocks1))

		// Prepare second QC covering [N, M) but don't push it yet.
		qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
		gr2 := qc2.QC().GlobalRange()

		// Block gr2.First should not be in state yet.
		_, err := state.TryBlock(gr2.First)
		require.ErrorIs(t, err, ErrNotFound)

		// PushBlock for a block in qc2's range. With the off-by-one bug
		// (n <= inner.nextQC), this would immediately dereference a nil QC
		// pointer and panic. With the fix, it waits for the QC.
		var pushErr error
		go func() {
			pushErr = state.PushBlock(ctx, gr2.First, blocks2[0])
		}()

		// Wait for PushBlock to become durably blocked on the QC channel.
		synctest.Wait()

		// Block should still not be in state (PushBlock is blocked).
		_, err = state.TryBlock(gr2.First)
		require.ErrorIs(t, err, ErrNotFound)

		// Push qc2 to unblock PushBlock.
		require.NoError(t, state.PushQC(ctx, qc2, nil))
		synctest.Wait()
		require.NoError(t, pushErr)

		// Block gr2.First should now be in state.
		got, err := state.TryBlock(gr2.First)
		require.NoError(t, err)
		require.Equal(t, blocks2[0], got)
	})
}
