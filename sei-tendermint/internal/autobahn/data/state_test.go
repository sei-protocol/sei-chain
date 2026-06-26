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

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
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
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := utils.OrPanic1(NewState(&Config{
			Registry: registry,
		}, utils.OrPanic1(NewDataWAL(utils.None[string](), registry.FirstBlock()))))
		s.SpawnBgNamed("state.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		want := newSnapshot()
		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := TestCommitQC(rng, committee, keys, prev, registry.FirstBlock(), time.Time{})
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
				Timestamp:     want.QCs[n].QC().Proposal().BlockTimestamp(n).OrPanic("global block not in QC"),
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

// Scenario:
// * a valid CommitQC is pushed.
// * an invalid CommitQC with the same road index, but more blocks is pushed.
// * data State should verify and reject the CommitQC, in particular:
//   - NOT replace the previous CommitQC
//   - NOT append the extra blocks for this road index.
func TestPushConflictingBadCommitQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	state := utils.OrPanic1(NewState(&Config{
		Registry: registry,
	}, utils.OrPanic1(NewDataWAL(utils.None[string](), registry.FirstBlock()))))

	// Push a valid QC to advance inner.nextQC.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	nextQC := qc1.QC().GlobalRange().Next

	// Construct a malicious QC signed by non-committee keys.
	// It starts from block 0 (stale) but extends beyond nextQC.
	// Keep each lane range within the protocol max; we only need the
	// total finalized span to exceed the previously accepted QC by 1.
	badKeys := make([]types.SecretKey, len(keys))
	for i := range badKeys {
		badKeys[i] = types.GenSecretKey(rng)
	}
	laneBlocks := map[types.LaneID][]*types.Block{}
	maliciousBlocksTotal := int(nextQC-registry.FirstBlock()) + 1
	require.LessOrEqual(t, maliciousBlocksTotal, committee.Lanes().Len()*types.MaxLaneRangeInProposal)
	for i := range maliciousBlocksTotal {
		lane := committee.Lanes().At(i % committee.Lanes().Len())
		var b *types.Block
		if bs := laneBlocks[lane]; len(bs) > 0 {
			parent := bs[len(bs)-1]
			b = types.NewBlock(lane, parent.Header().Next(), parent.Header().Hash(), types.GenPayload(rng))
		} else {
			b = types.NewBlock(lane, 0, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
		}
		laneBlocks[lane] = append(laneBlocks[lane], b)
	}
	laneQCs := map[types.LaneID]*types.LaneQC{}
	var headers []*types.BlockHeader
	var malBlocks []*types.Block
	for lane := range committee.Lanes().All() {
		bs := laneBlocks[lane]
		if len(bs) == 0 {
			continue
		}
		laneQCs[lane] = TestLaneQC(badKeys, bs[len(bs)-1].Header(), 0)
		for _, b := range bs {
			headers = append(headers, b.Header())
			malBlocks = append(malBlocks, b)
		}
	}
	viewSpec := types.ViewSpec{CommitQC: utils.None[*types.CommitQC](), Epoch: registry.LatestEpoch()}
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
		require.Equal(t, gr1.Next, inner.nextQC)
	}

	// Verify state is still functional: the next valid QC is accepted and visible.
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()), registry.FirstBlock(), time.Time{})
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
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	state := utils.OrPanic1(NewState(&Config{
		Registry: registry,
	}, utils.OrPanic1(NewDataWAL(utils.None[string](), registry.FirstBlock()))))

	// Push qc1 with NO blocks — only the QC is stored.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
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
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := utils.OrPanic1(NewState(&Config{
			Registry: registry,
		}, utils.OrPanic1(NewDataWAL(utils.None[string](), registry.FirstBlock()))))
		s.SpawnBgNamed("state.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := TestCommitQC(rng, committee, keys, prev, registry.FirstBlock(), time.Time{})
			if err := state.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("state.PushQC(): %w", err)
			}
			prev = utils.Some(qc.QC())
			gr := qc.QC().GlobalRange()
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
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()

	state := utils.OrPanic1(NewState(&Config{
		Registry: registry,
	}, utils.OrPanic1(NewDataWAL(utils.None[string](), registry.FirstBlock()))))

	// Push QC without blocks.
	qc, blocks := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	require.NoError(t, state.PushQC(ctx, qc, nil))
	gr := qc.QC().GlobalRange()

	// PushBlock for a block whose QC is already present succeeds immediately.
	require.NoError(t, state.PushBlock(ctx, gr.First, blocks[0]))
	got, err := state.TryBlock(gr.First)
	require.NoError(t, err)
	require.Equal(t, blocks[0], got)
}

// TestGlobalBlockByHash isolates the hash-keyed lookup from the
// consensus-driven harness. We push a single QC + block via the same code
// path the network would (insertBlock writes to inner.blockHashes), then:
//
//   - the block's own header hash resolves to Some(*GlobalBlock) with the
//     expected height/header/payload — the index points at the right
//     block, atomically with the block construction
//   - a zero hash and a random hash both resolve to None — distinct
//     unknown-hash inputs all read as "not found", no panics
//   - err is nil throughout — today's in-memory implementation has no
//     failure mode; the error return on GlobalBlockByHash is reserved
//     for the future BlockDB-backed path
func TestGlobalBlockByHash(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()

	state := utils.OrPanic1(NewState(&Config{
		Registry: registry,
	}, utils.OrPanic1(NewDataWAL(utils.None[string](), registry.FirstBlock()))))

	qc, blocks := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	require.NoError(t, state.PushQC(ctx, qc, blocks))
	gr := qc.QC().GlobalRange()
	n := gr.First
	wantBlock := blocks[0]
	wantHash := wantBlock.Header().Hash()

	// Known hash → Some with correct fields.
	gotOpt, err := state.GlobalBlockByHash(wantHash)
	require.NoError(t, err)
	gotGB, ok := gotOpt.Get()
	require.True(t, ok, "GlobalBlockByHash(known) returned None")
	require.Equal(t, n, gotGB.GlobalNumber)
	require.Equal(t, wantBlock.Header(), gotGB.Header)
	require.Equal(t, wantBlock.Payload(), gotGB.Payload)

	// Zero hash → None.
	zeroOpt, err := state.GlobalBlockByHash(types.BlockHeaderHash{})
	require.NoError(t, err)
	_, ok = zeroOpt.Get()
	require.False(t, ok, "GlobalBlockByHash(zero) returned Some")

	// Random unknown hash → None.
	var randHash types.BlockHeaderHash
	rng.Read(randHash[:])
	randOpt, err := state.GlobalBlockByHash(randHash)
	require.NoError(t, err)
	_, ok = randOpt.Get()
	require.False(t, ok, "GlobalBlockByHash(random) returned Some")
}

// ── Reconcile tests (grouped by case number) ──────────────────────────

// TestStateRecoveryBlocksOnly simulates a crash after blocks are written
// TestReconcileCase1Empty verifies that reconcile is a no-op on a fresh
// WAL directory with no data.
func TestReconcileCase1Empty(t *testing.T) {
	t.Log("Reconcile case 1: Fresh start (empty/empty)")
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()
	fb := registry.FirstBlock()

	dw := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state := utils.OrPanic1(NewState(&Config{Registry: registry}, dw))

	for inner := range state.inner.Lock() {
		require.Equal(t, fb, inner.first)
		require.Equal(t, fb, inner.nextQC)
		require.Equal(t, fb, inner.nextBlock)
	}
	require.NoError(t, dw.Close())
}

// TestReconcileCase2Corrupted verifies that when blocks are persisted but
// QCs WAL is deleted (corruption), NewDataWAL returns a corruption error.
func TestReconcileCase2Corrupted(t *testing.T) {
	t.Log("Reconcile case 2: QCs lost (corruption), returns error")
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	// Persist blocks and QCs normally.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()

	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	for i, n := 0, gr1.First; n < gr1.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, blocks1[i]))
		i++
	}
	require.NoError(t, dw1.Close())

	// Simulate corruption: delete QCs WAL.
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "fullcommitqcs")))

	// Reopen should fail — blocks exist but QCs are gone.
	_, err := NewDataWAL(utils.Some(dir), registry.FirstBlock())
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupted")
}

// TestStateRecoveryQCsOnly simulates a crash after QCs are written to the
// WAL but before blocks are written (or after blocks WAL is truncated but
// QCs WAL is not). On recovery, QCs are loaded but no blocks exist.
// The cursor sync in NewState must advance the blocks persister so that
// subsequent PersistBlock calls don't fail with "out of sequence".
func TestReconcileCase3BlocksLost(t *testing.T) {
	t.Log("Reconcile case 3: Blocks lost (crash), QCs survive")
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	// First run: populate both WALs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()

	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state1 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw1))
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
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state2 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw2))

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
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()), registry.FirstBlock(), time.Time{})
	require.NoError(t, state2.PushQC(ctx, qc2, blocks2))
	require.Equal(t, qc2.QC().GlobalRange().Next, state2.NextBlock())
	require.NoError(t, dw2.Close())
}

func TestReconcileCase4Normal(t *testing.T) {
	t.Log("Reconcile case 4: Normal (a=X, b<Y)")
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	// Build two sequential QCs with their blocks.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()

	// First run: push both QCs and persist to WALs.
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state1 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw1))
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
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state2 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw2))

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
	dw3 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state3 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw3))
	require.Equal(t, gr2.Next, state3.NextBlock())
	for n := gr1.First; n < gr2.Next; n++ {
		got, err := state3.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	require.NoError(t, dw3.Close())
}

// TestStateRecoveryAfterPruning verifies that after pruning removes entries
// from both WALs and the state is restarted, it recovers correctly with
// only the unpruned tail.
func TestReconcileCase4AfterPruning(t *testing.T) {
	t.Log("Reconcile case 4 variant: Normal after pruning, both WALs truncated")
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	// Build 3 sequential QCs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()), registry.FirstBlock(), time.Time{})
	qc3, blocks3 := TestCommitQC(rng, committee, keys, utils.Some(qc2.QC()), registry.FirstBlock(), time.Time{})
	gr2 := qc2.QC().GlobalRange()
	gr3 := qc3.QC().GlobalRange()

	// First run: push all 3 QCs, persist to WALs, then truncate before qc2.
	gr1 := qc1.QC().GlobalRange()
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state1 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw1))
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
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state2 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw2))

	require.Equal(t, gr3.Next, state2.NextBlock())

	// qc1 blocks should be pruned.
	for n := qc1.QC().GlobalRange().First; n < qc1.QC().GlobalRange().Next; n++ {
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

// Reconcile case 5: Prune crash, blocks ahead (a>X).
// Blocks WAL was truncated further than QCs during a crash between
// the two parallel TruncateBefore calls.
func TestReconcileCase5BlocksAhead(t *testing.T) {
	t.Log("Reconcile case 5: Prune crash, blocks ahead (a>X)")
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()

	// Persist both QCs and all blocks.
	dw := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	require.NoError(t, dw.CommitQCs.PersistQC(qc1))
	require.NoError(t, dw.CommitQCs.PersistQC(qc2))
	allBlocks := append(blocks1, blocks2...)
	for i, n := 0, gr1.First; n < gr2.Next; n++ {
		require.NoError(t, dw.Blocks.PersistBlock(n, allBlocks[i]))
		i++
	}
	// Simulate crash: blocks truncated to gr2.First but QCs not.
	require.NoError(t, dw.Blocks.TruncateBefore(gr2.First))
	require.NoError(t, dw.Close())

	// Reopen: blocks start at gr2.First, QCs start at gr1.First.
	// Reconcile should truncate QCs to match blocks.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state := utils.OrPanic1(NewState(&Config{Registry: registry}, dw2))

	for inner := range state.inner.Lock() {
		require.Equal(t, gr2.First, inner.first)
	}
	// Blocks in qc2's range should be available.
	for n := gr2.First; n < gr2.Next; n++ {
		got, err := state.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	require.NoError(t, dw2.Close())
}

// TestStateRecoverySkipsStaleBlocks verifies that blocks loaded from the WAL
// that fall before the first QC range are not inserted into the state map.
// This can happen when the QCs WAL is pruned but the blocks WAL still has
// older entries (e.g., a crash between the two TruncateBefore calls).
func TestReconcileCase6QCsAhead(t *testing.T) {
	t.Log("Reconcile case 6: Prune crash, QCs ahead (a<X)")
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	// Build 2 sequential QCs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()

	// First run: push both QCs and persist to WALs.
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state1 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw1))
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
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state2 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw2))

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

// TestStateRecoveryIgnoresBlocksBeyondQC verifies that blocks loaded from the
// WAL with numbers >= nextQC are ignored. This can happen when blocks are
// persisted in parallel with QCs and we crash before QCs catch up.
func TestReconcileCase7BlocksPastQCs(t *testing.T) {
	t.Log("Reconcile case 7: Persist crash, blocks past QCs (b>=Y)")
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	// Build 2 sequential QCs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()

	// Persist only qc1 to QCs WAL but persist ALL blocks (qc1 + qc2) to blocks WAL.
	// This simulates blocks being persisted ahead of QCs.
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	allBlocks := append(blocks1, blocks2...)
	for i, n := 0, gr1.First; n < gr2.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, allBlocks[i]))
		i++
	}
	require.NoError(t, dw1.Close())

	// On recovery, only blocks within qc1's range should be loaded.
	// Blocks in qc2's range have no QC and should be ignored.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state2 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw2))

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
func TestReconcileCase7BlocksTail(t *testing.T) {
	t.Log("Reconcile case 7: Persist crash, tail truncation with re-push")
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	// Build 2 sequential QCs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()

	// Persist qc1 to both WALs, but only blocks (not QC) for qc2.
	// This simulates a crash during parallel persistence in runPersist.
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	allBlocks := append(blocks1, blocks2...)
	for i, n := 0, gr1.First; n < gr2.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, allBlocks[i]))
		i++
	}
	require.NoError(t, dw1.Close())

	// Reopen: reconcile should truncate the blocks tail (qc2's blocks).
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))

	// Blocks persister cursor should now match QCs range.
	require.Equal(t, dw2.CommitQCs.Next(), dw2.Blocks.Next())

	state := utils.OrPanic1(NewState(&Config{Registry: registry}, dw2))

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

// TestStateRecoveryBlocksBehindQCs verifies recovery when QCs cover a wider
// range than blocks (e.g. crash during block persistence). Blocks up to
// blocksEnd are available; the rest must be re-fetched via PushBlock.
func TestReconcileCase8BlocksBehind(t *testing.T) {
	t.Log("Reconcile case 8: QCs ahead normal (b<Y)")
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	// Build 2 sequential QCs.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()

	// Persist both QCs but only qc1's blocks to WALs.
	dw1 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc1))
	require.NoError(t, dw1.CommitQCs.PersistQC(qc2))
	for i, n := 0, gr1.First; n < gr1.Next; n++ {
		require.NoError(t, dw1.Blocks.PersistBlock(n, blocks1[i]))
		i++
	}
	require.NoError(t, dw1.Close())

	// On recovery: both QCs loaded, but only qc1's blocks.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state2 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw2))

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

// TestRecoveryWithPartialQCPrefix verifies that after per-block pruning
// splits a QC range, recovery sets first from blocks (not QCs), and the
// node can serve surviving blocks without re-fetching pruned ones.
func TestReconcilePartialQCPrefix(t *testing.T) {
	// Reconcile: partial QC prefix from per-block pruning
	t.Log("Reconcile: partial QC prefix from per-block pruning")
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	// Build one QC with enough blocks to split.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()
	if gr1.Next-gr1.First < 3 {
		t.Skip("need at least 3 blocks in QC range to test split")
	}

	// Persist QC and all blocks.
	dw := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	require.NoError(t, dw.CommitQCs.PersistQC(qc1))
	for i, n := 0, gr1.First; n < gr1.Next; n++ {
		require.NoError(t, dw.Blocks.PersistBlock(n, blocks1[i]))
		i++
	}
	// Simulate per-block pruning: truncate blocks prefix, keep QC intact.
	// This creates the split: QC covers [gr1.First, gr1.Next) but blocks
	// start at mid.
	mid := gr1.First + (gr1.Next-gr1.First)/2
	require.NoError(t, dw.Blocks.TruncateBefore(mid))
	require.NoError(t, dw.Close())

	// Recovery should use blocks as golden, not try to re-fetch pruned prefix.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state2 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw2))

	// first should be at mid (where blocks start), not gr1.First (where QC starts).
	for inner := range state2.inner.Lock() {
		require.Equal(t, mid, inner.first,
			"first should be where blocks start, not where QC starts")
		require.Equal(t, gr1.Next, inner.nextQC,
			"QC should still cover the full range")
	}

	// Blocks before mid should be pruned, not ErrNotFound.
	for n := gr1.First; n < mid; n++ {
		_, err := state2.TryBlock(n)
		require.ErrorIs(t, err, ErrPruned)
	}

	// Blocks at mid and above should be available.
	for n := mid; n < gr1.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	require.NoError(t, dw2.Close())
}

// TestStateRejectsBlockGapInWAL verifies that NewState returns an error
// if loaded blocks have a gap (defense in depth for future storage backends).
func TestReconcileBlockGap(t *testing.T) {
	// Reconcile: block gap in WAL detected (defense in depth)
	t.Log("Reconcile: block gap in WAL detected (defense in depth)")
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()
	if gr1.Next-gr1.First < 3 {
		t.Skip("need at least 3 blocks in QC range to test gap")
	}

	// Persist QC to real WAL so it loads on restart.
	dw := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	require.NoError(t, dw.CommitQCs.PersistQC(qc1))
	require.NoError(t, dw.Close())

	// Reopen and inject blocks with a gap via test helper.
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	dw2.Blocks.SetLoadedForTest([]persist.LoadedGlobalBlock{
		{Number: gr1.First, Block: blocks1[0]},
		// skip gr1.First+1
		{Number: gr1.First + 2, Block: blocks1[2]},
	})

	_, err := NewState(&Config{Registry: registry}, dw2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "block gap")
	require.NoError(t, dw2.Close())
}

// ── Non-reconcile tests ───────────────────────────────────────────────

// TestPruningKeepsLastQCRange verifies that pruning never removes the last
// QC range, ensuring WALs are never empty and inner.first is recoverable
// on restart.
func TestPruningKeepsLastQCRange(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	dw := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state := utils.OrPanic1(NewState(&Config{Registry: registry}, dw))

	// Push one QC with blocks and execute all of them.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))

	// Persist and execute.
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- state.Run(runCtx) }()
	for n := gr1.First; n < gr1.Next; n++ {
		require.NoError(t, state.PushAppHash(ctx, n, types.GenAppHash(rng)))
	}
	cancel()
	<-done

	// Try to prune everything — at least one block should survive.
	state.PruneBefore(gr1.Next)
	for inner := range state.inner.Lock() {
		require.Less(t, inner.first, gr1.Next,
			"pruning should keep at least one block")
	}

	// Restart from WALs — inner.first should be recoverable.
	require.NoError(t, dw.Close())
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state2 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw2))
	for inner := range state2.inner.Lock() {
		require.GreaterOrEqual(t, inner.first, gr1.First,
			"after restart, first should be recoverable from WAL")
	}
	require.NoError(t, dw2.Close())
}

// TestPruningWithPartialQCRange verifies that per-block pruning can split
// a QC range, and recovery handles it correctly by using blocks as the
// golden source for inner.first.
func TestPruningWithPartialQCRange(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	dw := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state := utils.OrPanic1(NewState(&Config{Registry: registry}, dw))

	// Push two QCs so we have two distinct QC ranges.
	qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
	qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()), registry.FirstBlock(), time.Time{})
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state.PushQC(ctx, qc2, blocks2))

	// Run to persist, then execute all blocks.
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- state.Run(runCtx) }()
	for n := gr1.First; n < gr2.Next; n++ {
		require.NoError(t, state.PushAppHash(ctx, n, types.GenAppHash(rng)))
	}
	cancel()
	<-done

	// Prune into the middle of qc1's range. Per-block pruning allows this.
	midQC1 := gr1.First + (gr1.Next-gr1.First)/2
	if midQC1 > gr1.First+1 {
		state.PruneBefore(midQC1)
		for inner := range state.inner.Lock() {
			require.Greater(t, inner.first, gr1.First,
				"per-block pruning should advance past gr1.First")
		}
	}

	// Prune past qc1 entirely.
	state.PruneBefore(gr2.Next)

	// Restart — recovery uses blocks as golden, skips partial QC prefix.
	require.NoError(t, dw.Close())
	dw2 := utils.OrPanic1(NewDataWAL(utils.Some(dir), registry.FirstBlock()))
	state2 := utils.OrPanic1(NewState(&Config{Registry: registry}, dw2))

	// first should be recoverable and blocks should be available.
	for inner := range state2.inner.Lock() {
		require.GreaterOrEqual(t, inner.first, gr2.First,
			"after restart, first should be at or past gr2.First")
		require.Less(t, inner.first, gr2.Next,
			"at least one block should survive")
	}
	require.NoError(t, dw2.Close())
}

// TestRunPruningEmptyState verifies that runPruning does not panic when
// the state has no QCs (e.g. on first startup before any data arrives).
func TestRunPruningEmptyState(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 3)

	state := utils.OrPanic1(NewState(&Config{
		Registry:   registry,
		PruneAfter: utils.Some(time.Duration(0)),
	}, utils.OrPanic1(NewDataWAL(utils.None[string](), registry.FirstBlock()))))

	// Run briefly — runPruning should not panic on empty state.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_ = state.Run(ctx) // returns context.DeadlineExceeded, that's fine
}

func TestPushBlockWaitsForQC(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 3)
		committee := registry.LatestEpoch().Committee()

		state := utils.OrPanic1(NewState(&Config{
			Registry: registry,
		}, utils.OrPanic1(NewDataWAL(utils.None[string](), registry.FirstBlock()))))

		// Push first QC covering [0, N).
		qc1, blocks1 := TestCommitQC(rng, committee, keys, utils.None[*types.CommitQC](), registry.FirstBlock(), time.Time{})
		require.NoError(t, state.PushQC(ctx, qc1, blocks1))

		// Prepare second QC covering [N, M) but don't push it yet.
		qc2, blocks2 := TestCommitQC(rng, committee, keys, utils.Some(qc1.QC()), registry.FirstBlock(), time.Time{})
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
