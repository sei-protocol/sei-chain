package data

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
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
		state := utils.OrPanic1(NewState(&Config{
			Committee: committee,
		}, utils.OrPanic1(NewDataWAL(utils.None[string](), committee))))
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
			gr := qc.QC().GlobalRange(committee)
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
				Timestamp:     want.QCs[n].QC().Proposal().BlockTimestamp(committee, n).OrPanic("global block not in QC"),
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
	state := utils.OrPanic1(NewState(&Config{
		Committee: committee,
	}, utils.OrPanic1(NewDataWAL(utils.None[string](), committee))))

	// Push a valid QC to advance inner.nextQC.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	nextQC := qc1.QC().GlobalRange(committee).Next

	// Construct a malicious QC signed by non-committee keys.
	// It starts from block 0 (stale) but extends beyond nextQC.
	badKeys := make([]types.SecretKey, len(keys))
	for i := range badKeys {
		badKeys[i] = types.GenSecretKey(rng)
	}
	blocksPerLane := int(nextQC/types.GlobalBlockNumber(committee.Lanes().Len())) + 2
	laneBlocks := map[types.LaneID][]*types.Block{}
	for lane := range committee.Lanes().All() {
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
	for lane := range committee.Lanes().All() {
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
	malGR := proposal.Proposal().Msg().GlobalRange(committee)
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
	gr1 := qc1.QC().GlobalRange(committee)
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
	gr2 := qc2.QC().GlobalRange(committee)
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
	state := utils.OrPanic1(NewState(&Config{
		Committee: committee,
	}, utils.OrPanic1(NewDataWAL(utils.None[string](), committee))))

	// Push qc1 with NO blocks — only the QC is stored.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushQC(ctx, qc1, nil))
	gr := qc1.QC().GlobalRange(committee)

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
		state := utils.OrPanic1(NewState(&Config{
			Committee: committee,
		}, utils.OrPanic1(NewDataWAL(utils.None[string](), committee))))
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
			gr := qc.QC().GlobalRange(committee)
			// PushAppHash for a block beyond nextBlock should not succeed:
			// it waits for persistence which never happens for unfinalised blocks.
			shortCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
			if err := state.PushAppHash(shortCtx, gr.Next, types.GenAppHash(rng)); err == nil {
				cancel()
				return errors.New("PushAppProposal expected to fail on non-finalized blocks")
			}
			cancel()
			for n := gr.First; n < gr.Next; n += 1 {
				if err := state.PushAppHash(ctx, n, types.GenAppHash(rng)); err != nil {
					return fmt.Errorf("state.PushAppProposal(): %w", err)
				}
				if err := state.PushAppHash(ctx, n, types.GenAppHash(rng)); err == nil {
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

	state := utils.OrPanic1(NewState(&Config{
		Committee: committee,
	}, utils.OrPanic1(NewDataWAL(utils.None[string](), committee))))

	// Push QC without blocks.
	qc, blocks := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushQC(ctx, qc, nil))
	gr := qc.QC().GlobalRange(committee)

	// PushBlock for a block whose QC is already present succeeds immediately.
	require.NoError(t, state.PushBlock(ctx, gr.First, blocks[0]))
	got, err := state.TryBlock(gr.First)
	require.NoError(t, err)
	require.Equal(t, blocks[0], got)
}

func TestStateRecoveryFromWAL(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	// Build two sequential QCs with their blocks.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange(committee)
	gr2 := qc2.QC().GlobalRange(committee)

	// First run: push both QCs and persist to WALs.
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state1 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw1))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state1.PushQC(ctx, qc2, blocks2))
	require.Equal(t, gr2.Next, state1.NextBlock())
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc2))
	allBlocks := append(blocks1, blocks2...)
	for i, n := 0, gr1.First; n < gr2.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, allBlocks[i]))
		i++
	}
	require.NoError(t, dw1.Close())

	// Second run: reopen from the same directory.
	// NewState should recover blocks and QCs, and updateNextBlock
	// should advance nextBlock using preloaded QCs.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state2 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw2))

	// nextBlock should already be at the end — no PushQC needed.
	require.Equal(t, gr2.Next, state2.NextBlock())

	// All blocks should be available.
	for n := gr1.First; n < gr2.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}

	// All QCs should be available.
	for n := gr1.First; n < gr2.Next; n++ {
		got, err := state2.QC(ctx, n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	require.NoError(t, dw2.Close())

	// Third run: reopen again to verify that the second NewState did
	// not truncate the WALs. A bug where TruncateBefore(nextBlock)
	// with nextBlock == persister cursor would TruncateAll would cause
	// this third restart to find empty WALs.
	dw3 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state3 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw3))
	require.Equal(t, gr2.Next, state3.NextBlock())
	for n := gr1.First; n < gr2.Next; n++ {
		got, err := state3.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	require.NoError(t, dw3.Close())
}

// TestStateRecoveryBlocksOnly simulates a crash after blocks are written
// to the WAL but before QCs are written (or after QCs WAL is truncated but
// blocks WAL is not). On recovery, blocks are loaded but no QCs exist.
// NewState must still produce a functional state that accepts new QCs.
func TestStateRecoveryBlocksOnly(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	// First run: populate both WALs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange(committee)

	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state1 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw1))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	for i, n := 0, gr1.First; n < gr1.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, blocks1[i]))
		i++
	}
	require.NoError(t, dw1.Close())

	// Simulate crash: delete QCs WAL directory, keep blocks WAL.
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "fullcommitqcs")))

	// Second run: only blocks WAL survives.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state2 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw2))

	// Without QCs, blocks are ignored on load (no QC to validate against).
	// Re-pushing the QC with blocks makes them available again.
	require.NoError(t, state2.PushQC(ctx, qc1, blocks1))

	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}

	// State should accept the next QC normally.
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
	require.NoError(t, state2.PushQC(ctx, qc2, blocks2))
	require.Equal(t, qc2.QC().GlobalRange(committee).Next, state2.NextBlock())
	require.NoError(t, dw2.Close())
}

// TestStateRecoveryQCsOnly simulates a crash after QCs are written to the
// WAL but before blocks are written (or after blocks WAL is truncated but
// QCs WAL is not). On recovery, QCs are loaded but no blocks exist.
// The cursor sync in NewState must advance the blocks persister so that
// subsequent PersistBlock calls don't fail with "out of sequence".
func TestStateRecoveryQCsOnly(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	// First run: populate both WALs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange(committee)

	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state1 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw1))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	for i, n := 0, gr1.First; n < gr1.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, blocks1[i]))
		i++
	}
	require.NoError(t, dw1.Close())

	// Simulate crash: delete blocks WAL directory, keep QCs WAL.
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "globalblocks")))

	// Second run: only QCs WAL survives.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state2 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw2))

	// QCs loaded, blocks empty. The state needs blocks re-pushed.
	// Without the cursor sync fix, PushBlock here would fail with
	// "out of sequence" because the blocks persister cursor is 0
	// but inner.nextBlock was advanced by QC data.
	for i, n := 0, gr1.First; n < gr1.Next; n++ {
		require.NoError(t, state2.PushBlock(ctx, n, blocks1[i]))
		i++
	}

	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}

	// State should accept the next QC normally.
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
	require.NoError(t, state2.PushQC(ctx, qc2, blocks2))
	require.Equal(t, qc2.QC().GlobalRange(committee).Next, state2.NextBlock())
	require.NoError(t, dw2.Close())
}

// TestStateRecoveryAfterPruning verifies that after pruning removes entries
// from both WALs and the state is restarted, it recovers correctly with
// only the unpruned tail.
func TestStateRecoveryAfterPruning(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	// Build 3 sequential QCs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
	qc3, blocks3 := TestCommitQC(rng, committee, keys, utils.Some(qc2.QC()))
	gr2 := qc2.QC().GlobalRange(committee)
	gr3 := qc3.QC().GlobalRange(committee)

	// First run: push all 3 QCs, persist to WALs, then truncate before qc2.
	gr1 := qc1.QC().GlobalRange(committee)
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state1 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw1))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state1.PushQC(ctx, qc2, blocks2))
	require.NoError(t, state1.PushQC(ctx, qc3, blocks3))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc2))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc3))
	allBlocks := append(append(blocks1, blocks2...), blocks3...)
	for i, n := 0, gr1.First; n < gr3.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, allBlocks[i]))
		i++
	}
	require.NoError(t, dw1.TruncateBefore(gr2.First))
	require.NoError(t, dw1.Close())

	// Second run: only qc2 and qc3 data should survive.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state2 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw2))

	require.Equal(t, gr3.Next, state2.NextBlock())

	// qc1 blocks should be pruned.
	for n := qc1.QC().GlobalRange(committee).First; n < qc1.QC().GlobalRange(committee).Next; n++ {
		_, err := state2.TryBlock(n)
		require.ErrorIs(t, err, ErrPruned)
	}

	// qc2 and qc3 blocks should be available.
	for n := gr2.First; n < gr3.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	require.NoError(t, dw2.Close())
}

// TestStateRecoverySkipsStaleBlocks verifies that blocks loaded from the WAL
// that fall before the first QC range are not inserted into the state map.
// This can happen when the QCs WAL is pruned but the blocks WAL still has
// older entries (e.g., a crash between the two TruncateBefore calls).
func TestStateRecoverySkipsStaleBlocks(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	// Build 2 sequential QCs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange(committee)
	gr2 := qc2.QC().GlobalRange(committee)

	// First run: push both QCs and persist to WALs.
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state1 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw1))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state1.PushQC(ctx, qc2, blocks2))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc2))
	allBlocks := append(blocks1, blocks2...)
	for i, n := 0, gr1.First; n < gr2.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, allBlocks[i]))
		i++
	}

	// Prune only the QCs WAL past qc1, but leave blocks WAL intact.
	// This simulates a crash between the two TruncateBefore calls.
	require.NoError(t, dw1.CommitQCs.TruncateBefore(gr2.First))
	require.NoError(t, dw1.Close())

	// Second run: QCs WAL starts at qc2 but blocks WAL still has qc1's blocks.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state2 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw2))

	// Blocks before qc2's range should NOT be in the state.
	for n := gr1.First; n < gr1.Next; n++ {
		_, err := state2.TryBlock(n)
		require.ErrorIs(t, err, ErrPruned)
	}

	// Blocks in qc2's range should be available.
	for n := gr2.First; n < gr2.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	require.NoError(t, dw2.Close())
}

// TestStateRecoveryBlocksBehindQCs verifies recovery when QCs cover a wider
// range than blocks (e.g. crash during block persistence). Blocks up to
// blocksEnd are available; the rest must be re-fetched via PushBlock.
func TestStateRecoveryBlocksBehindQCs(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	// Build 2 sequential QCs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange(committee)
	gr2 := qc2.QC().GlobalRange(committee)

	// Persist both QCs but only qc1's blocks to WALs.
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc2))
	for i, n := 0, gr1.First; n < gr1.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, blocks1[i]))
		i++
	}
	require.NoError(t, dw1.Close())

	// On recovery: both QCs loaded, but only qc1's blocks.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state2 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw2))

	// qc1's blocks should be available.
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}

	// qc2's blocks are missing — not yet available.
	for n := gr2.First; n < gr2.Next; n++ {
		_, err := state2.TryBlock(n)
		require.ErrorIs(t, err, ErrNotFound)
	}

	// Re-push qc2's blocks to fill the gap.
	for i, n := 0, gr2.First; n < gr2.Next; n++ {
		require.NoError(t, state2.PushBlock(ctx, n, blocks2[i]))
		i++
	}
	require.Equal(t, gr2.Next, state2.NextBlock())
	require.NoError(t, dw2.Close())
}

// TestStateRecoveryIgnoresBlocksBeyondQC verifies that blocks loaded from the
// WAL with numbers >= nextQC are ignored. This can happen when blocks are
// persisted in parallel with QCs and we crash before QCs catch up.
func TestStateRecoveryIgnoresBlocksBeyondQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	// Build 2 sequential QCs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange(committee)
	gr2 := qc2.QC().GlobalRange(committee)

	// Persist only qc1 to QCs WAL but persist ALL blocks (qc1 + qc2) to blocks WAL.
	// This simulates blocks being persisted ahead of QCs.
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	allBlocks := append(blocks1, blocks2...)
	for i, n := 0, gr1.First; n < gr2.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, allBlocks[i]))
		i++
	}
	require.NoError(t, dw1.Close())

	// On recovery, only blocks within qc1's range should be loaded.
	// Blocks in qc2's range have no QC and should be ignored.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state2 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw2))

	// Blocks in qc1's range should be available.
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}

	// Blocks in qc2's range should NOT be in the state (no QC for them).
	for n := gr2.First; n < gr2.Next; n++ {
		_, err := state2.TryBlock(n)
		require.ErrorIs(t, err, ErrNotFound)
	}

	// State should still accept qc2 normally.
	require.NoError(t, state2.PushQC(t.Context(), qc2, blocks2))
	require.Equal(t, gr2.Next, state2.NextBlock())
	require.NoError(t, dw2.Close())
}

// TestReconcileTruncatesBlocksTail verifies that blocks persisted without
// corresponding QCs are removed during WAL reconciliation. This prevents
// stale blocks from blocking new (different) blocks at the same positions.
func TestReconcileTruncatesBlocksTail(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	// Build 2 sequential QCs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange(committee)
	gr2 := qc2.QC().GlobalRange(committee)

	// Persist qc1 to both WALs, but only blocks (not QC) for qc2.
	// This simulates a crash during parallel persistence in runPersist.
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	allBlocks := append(blocks1, blocks2...)
	for i, n := 0, gr1.First; n < gr2.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, allBlocks[i]))
		i++
	}
	require.NoError(t, dw1.Close())

	// Reopen: reconcile should truncate the blocks tail (qc2's blocks).
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))

	// Blocks persister cursor should now match QCs range.
	require.Equal(t, dw2.CommitQCs.Next(), dw2.Blocks.Next())

	state := utils.OrPanic1(NewState(&Config{Committee: committee}, dw2))

	// qc1's blocks should be available.
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}

	// Now push qc2 with its blocks — should succeed because stale blocks
	// were removed and the persister cursor was reset.
	require.NoError(t, state.PushQC(ctx, qc2, blocks2))
	require.Equal(t, gr2.Next, state.NextBlock())
	require.NoError(t, dw2.Close())
}

// TestPruningKeepsLastEntry verifies that pruning never removes the last
// block/QC, ensuring WALs are never empty and inner.first is recoverable
// on restart.
func TestPruningKeepsLastEntry(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	dw := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state := utils.OrPanic1(NewState(&Config{
		Committee:  committee,
		PruneAfter: utils.Some(time.Duration(0)), // prune immediately
	}, dw))

	// Push one QC with blocks and execute all of them.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange(committee)
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))

	// Run state (starts runPersist + runPruning).
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- state.Run(runCtx) }()

	// Execute all blocks. PushAppHash waits for persistence internally.
	for n := gr1.First; n < gr1.Next; n++ {
		require.NoError(t, state.PushAppHash(ctx, n, types.GenAppHash(rng)))
	}

	// Give pruning time to run.
	time.Sleep(50 * time.Millisecond)

	// Verify at least one entry survives.
	for inner := range state.inner.Lock() {
		require.Less(t, inner.first, inner.nextAppProposal,
			"pruning should keep at least one entry")
	}

	cancel()
	<-done

	// Restart from WALs — inner.first should be recoverable.
	require.NoError(t, dw.Close())
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), committee))
	state2 := utils.OrPanic1(NewState(&Config{Committee: committee}, dw2))

	// State should have a valid first (not reset to committee.FirstBlock()).
	require.GreaterOrEqual(t, state2.NextBlock(), gr1.First,
		"after restart, state should recover from WAL, not reset to beginning")
	require.NoError(t, dw2.Close())
}

func TestPushBlockWaitsForQC(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		committee, keys := types.GenCommittee(rng, 3)

		state := utils.OrPanic1(NewState(&Config{
			Committee: committee,
		}, utils.OrPanic1(NewDataWAL(utils.None[string](), committee))))

		// Push first QC covering [0, N).
		qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC]())
		require.NoError(t, state.PushQC(ctx, qc1, blocks1))

		// Prepare second QC covering [N, M) but don't push it yet.
		qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()))
		gr2 := qc2.QC().GlobalRange(committee)

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
