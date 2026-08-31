package data

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/blockstore"
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
		qcs := make(map[types.GlobalBlockNumber]*types.FullCommitQC, len(inner.qcs))
		for n, e := range inner.qcs {
			qcs[n] = e.qc
		}
		return Snapshot{
			QCs:          qcs,
			Blocks:       maps.Clone(inner.blocks),
			AppProposals: maps.Clone(inner.appProposals),
		}
	}
	panic("unreachable")
}

// newTestBlockStore opens (or creates) a LittDB-backed BlockStore at dir.
// Retention is set to 1ns so ForceGC reclaims pruned data immediately in tests.
// Errors panic so the helper is safe to call from non-main test goroutines.
func newTestBlockStore(t *testing.T, dir string) types.BlockStore {
	t.Helper()
	cfg := utils.OrPanic1(littblock.DefaultConfig(dir))
	cfg.RetentionTime = time.Nanosecond
	store := utils.OrPanic1(blockstore.New(utils.OrPanic1(littblock.NewBlockDB(cfg))))
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newMemoryBlockStore(t testing.TB) types.BlockStore {
	t.Helper()
	store := utils.OrPanic1(blockstore.New(memblock.NewBlockDB()))
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// newTestState constructs a State, replays store, and returns it ready to Run.
// Errors panic so the helper is safe to call from non-main test goroutines.
func newTestState(t testing.TB, cfg *Config, store types.BlockStore) *State {
	t.Helper()
	return utils.OrPanic1(NewState(cfg, store))
}

// writeToBlockStore writes QC+block pairs sequentially to store and flushes once.
// qcs[i] and blockss[i] must correspond; QCs must be in ascending order.
// Errors panic so the helper is safe to call from non-main test goroutines.
func writeToBlockStore(t *testing.T, store types.BlockStore, qcs []*types.FullCommitQC, blockss [][]*types.Block) {
	t.Helper()
	for i, qc := range qcs {
		gr := qc.QC().GlobalRange()
		utils.OrPanic(store.WriteQC(qc))
		for j, n := 0, gr.First; n < gr.Next; n++ {
			utils.OrPanic(store.WriteBlock(n, blockss[i][j]))
			j++
		}
	}
	utils.OrPanic(store.Flush())
}

func writeAppDataToBlockStore(t testing.TB, rng utils.Rng, store types.BlockStore, keys []types.SecretKey, qcs ...*types.FullCommitQC) {
	t.Helper()
	for _, qc := range qcs {
		appProposal := types.NewAppProposal(qc.QC().Proposal(), types.GenAppHash(rng))
		utils.OrPanic(store.WriteAppProposal(appProposal))
		utils.OrPanic(store.WriteAppQC(TestAppQC(keys, appProposal)))
	}
	utils.OrPanic(store.Flush())
}

// pushAppHashesRunning runs state.Run under scope.Run long enough to accept
// PushAppHash for [first, next), then cancels Run. Prefers scope.Run over a
// raw goroutine so cleanup is structured.
func pushAppHashesRunning(ctx context.Context, state *State, rng utils.Rng, first, next types.GlobalBlockNumber) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		for n := first; n < next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng), nil); err != nil {
				return err
			}
		}
		return nil
	})
}

func pushAppQCForBlock(ctx context.Context, state *State, keys []types.SecretKey, n types.GlobalBlockNumber) error {
	vote, err := state.AppVote(ctx, n)
	if err != nil {
		return err
	}
	return state.PushAppQC(ctx, TestAppQC(keys, vote.Proposal()))
}

func commitQCAtRoad(
	ep *types.Epoch,
	keys []types.SecretKey,
	road types.RoadIndex,
	globalFirst types.GlobalBlockNumber,
) (*types.FullCommitQC, []*types.Block) {
	proposal := types.ProposalAt(ep, types.View{Index: road, Number: 0}, globalFirst)
	block := types.NewBlock(ep.Committee().Lanes().At(0), 0, types.BlockHeaderHash{}, &types.Payload{})
	votes := make([]*types.Signed[*types.CommitVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(proposal)))
	}
	return types.NewFullCommitQC(types.NewCommitQC(votes), []*types.BlockHeader{block.Header()}), []*types.Block{block}
}

func commitQCAtRoadBlocks(
	ep *types.Epoch,
	keys []types.SecretKey,
	road types.RoadIndex,
	globalFirst types.GlobalBlockNumber,
	n int,
) (*types.FullCommitQC, []*types.Block) {
	proposal, blocks := types.ProposalAtBlocks(ep, types.View{Index: road, Number: 0}, globalFirst, n)
	votes := make([]*types.Signed[*types.CommitVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(proposal)))
	}
	headers := make([]*types.BlockHeader, len(blocks))
	for i, b := range blocks {
		headers[i] = b.Header()
	}
	return types.NewFullCommitQC(types.NewCommitQC(votes), headers), blocks
}

func TestNextCommitEpoch_AdvancesAtIdleEpochBoundary(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryThrough(rng, 3, 2)
	ep2 := registry.MustEpoch(2)
	ep1 := registry.MustEpoch(1)
	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))

	qcMid, blocksMid := commitQCAtRoad(ep1, keys, epoch.FirstRoad(1), ep1.FirstBlock())
	require.NoError(t, state.PushQC(ctx, qcMid, blocksMid))
	require.Equal(t, ep1, state.NextCommitEpoch().Load(), "mid-epoch next road is still in epoch 1")
	grMid := qcMid.QC().GlobalRange()
	require.NoError(t, pushAppHashesRunning(ctx, state, rng, grMid.First, grMid.Next))
	require.Equal(t, ep1, state.NextCommitEpoch().Load())

	qcLast, blocksLast := commitQCAtRoad(ep1, keys, epoch.LastRoad(1), grMid.Next)
	require.NoError(t, state.PushQC(ctx, qcLast, blocksLast))
	require.Equal(t, ep2, state.NextCommitEpoch().Load(), "next road is in epoch 2, already filled from end(0)")
}

func TestNextCommitEpoch_RefreshesAfterAppQCActivation(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	keeper := keys[0].Public()
	ep0 := registry.MustEpoch(0)
	ep1 := registry.MustEpoch(1)
	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		qc0, blocks0 := commitQCAtRoad(ep0, keys, epoch.LastRoad(0), ep0.FirstBlock())
		if err := state.PushQC(ctx, qc0, blocks0); err != nil {
			return err
		}
		n0 := qc0.QC().GlobalRange().Next - 1
		if err := state.PushAppHash(ctx, n0, types.GenAppHash(rng), map[types.PublicKey]uint64{keeper: 9}); err != nil {
			return err
		}
		if got := state.NextCommitEpoch().Load(); got != ep1 {
			return fmt.Errorf("after LastRoad(0) QC: NextCommitEpoch = %v, want epoch 1", got.EpochIndex())
		}

		qc1, blocks1 := commitQCAtRoad(ep1, keys, epoch.LastRoad(1), qc0.QC().GlobalRange().Next)
		if err := state.PushQC(ctx, qc1, blocks1); err != nil {
			return err
		}
		if got := state.NextCommitEpoch().Load(); got != ep1 {
			return fmt.Errorf("LastRoad(1) QC before epoch 2 is live: NextCommitEpoch = %v, want epoch 1", got.EpochIndex())
		}

		if err := pushAppQCForBlock(ctx, state, keys, n0); err != nil {
			return err
		}
		ep2, err := registry.EpochByIndex(2)
		if err != nil {
			return fmt.Errorf("epoch 2 after AppQC: %w", err)
		}
		if got := state.NextCommitEpoch().Load(); got != ep2 {
			return fmt.Errorf("after AppQC: NextCommitEpoch = %v, want epoch 2", got.EpochIndex())
		}
		return nil
	}))
}

func TestState(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
		s.SpawnBgNamed("state.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		want := newSnapshot()
		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, prev)
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
				GlobalNumber: n,
				Timestamp:    want.QCs[n].QC().Proposal().BlockTimestamp(n).OrPanic("global block not in QC"),
				Header:       wantB.Header(),
				Payload:      wantB.Payload(),
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
	committee := registry.MustEpoch(0).Committee()
	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))

	// Push a valid QC to advance inner.nextQC.
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	gr1 := qc1.QC().GlobalRange()

	// Construct a malicious QC signed by non-committee keys.
	// It starts from block 0 (stale) but extends beyond nextQC.
	// Keep each lane range within the protocol max; we only need the
	// total finalized span to exceed the previously accepted QC by 1.
	badKeys := make([]types.SecretKey, len(keys))
	for i := range badKeys {
		badKeys[i] = types.GenSecretKey(rng)
	}
	laneBlocks := map[types.LaneID][]*types.Block{}
	maliciousBlocksTotal := int(gr1.Len()) + 1
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
		laneQCs[lane] = TestLaneQC(badKeys, bs[len(bs)-1].Header())
		for _, b := range bs {
			headers = append(headers, b.Header())
			malBlocks = append(malBlocks, b)
		}
	}
	viewSpec := types.ViewSpec{ConsensusSpec: types.ConsensusSpec{CommitQC: utils.None[*types.CommitQC](), Epoch: registry.MustEpoch(0)}}
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
	))
	malGR := proposal.Proposal().Msg().GlobalRange()
	require.Less(t, malGR.First, gr1.Next, "test setup: malicious gr.First must be < nextQC")
	require.Greater(t, malGR.Next, gr1.Next, "test setup: malicious gr.Next must be > nextQC")

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
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
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
	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))

	// Push qc1 with NO blocks — only the QC is stored.
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
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
		require.ErrorIs(t, err, types.ErrNotFound)
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
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
		s.SpawnBgNamed("state.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, prev)
			if err := state.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("state.PushQC(): %w", err)
			}
			prev = utils.Some(qc.QC())
			gr := qc.QC().GlobalRange()
			// PushAppHash for a block beyond nextBlock should not succeed:
			// it waits for persistence which never happens for unfinalised blocks.
			shortCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
			if err := state.PushAppHash(shortCtx, gr.Next, types.GenAppHash(rng), nil); err == nil {
				cancel()
				return errors.New("PushAppHash expected to fail on non-finalized blocks")
			}
			cancel()
			for n := gr.First; n < gr.Next; n += 1 {
				if err := state.PushAppHash(ctx, n, types.GenAppHash(rng), nil); err != nil {
					return fmt.Errorf("state.PushAppHash(): %w", err)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPushAppHashRejectsJumpOverCommitQCRange(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		epoch := registry.MustEpoch(0)
		var qcs []*types.CommitQC
		for range 3 {
			var prev utils.Option[*types.CommitQC]
			if len(qcs) > 0 {
				prev = utils.Some(qcs[len(qcs)-1])
			}
			qc, blocks := TestCommitQC(rng, epoch, keys, prev)
			if err := state.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("PushQC(): %w", err)
			}
			qcs = append(qcs, qc.QC())
		}
		if err := state.PushAppHash(ctx, qcs[0].GlobalRange().Next-1, types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc1): %w", err)
		}
		if qcs[2].GlobalRange().Len() < 2 {
			panic("qcs[2].Len() is too small for this test")
		}
		if err := state.PushAppHash(ctx, qcs[2].GlobalRange().Next-2, types.GenAppHash(rng), nil); !errors.Is(err, ErrOutOfOrder) {
			return fmt.Errorf("PushAppHash(qc3 before qc2) error = %w, want %w", err, ErrOutOfOrder)
		}
		if err := state.PushAppHash(ctx, qcs[2].GlobalRange().Next-1, types.GenAppHash(rng), nil); !errors.Is(err, ErrOutOfOrder) {
			return fmt.Errorf("PushAppHash(qc3 before qc2) error = %w, want %w", err, ErrOutOfOrder)
		}

		if err := state.PushAppHash(ctx, qcs[1].GlobalRange().Next-1, types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc2): %w", err)
		}
		if err := state.PushAppHash(ctx, qcs[2].GlobalRange().Next-1, types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc3): %w", err)
		}
		// Inserting old stuff should be a noop.
		if err := state.PushAppHash(ctx, qcs[1].GlobalRange().Next-1, types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc2): %w", err)
		}
		if err := state.PushAppHash(ctx, qcs[2].GlobalRange().Next-2, types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc2): %w", err)
		}
		return nil
	}))
}

func TestPushAppHash_MidEpochDoesNotRegister(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
		if err := state.PushQC(ctx, qc, blocks); err != nil {
			return err
		}
		if err := state.PushAppHash(ctx, qc.QC().GlobalRange().Next-1, types.GenAppHash(rng), nil); err != nil {
			return err
		}
		if _, err := registry.EpochAt(epoch.FirstRoad(2)); err == nil {
			return fmt.Errorf("epoch 2 must stay absent for road %d", qc.QC().Proposal().Index())
		}
		if got, ok := registry.Pending().Get(); ok {
			return fmt.Errorf("Pending() = %v, want nothing staged mid-epoch", got)
		}
		return nil
	}))
}

func TestCommitteeFill_GatedOnAppQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	keeper := keys[0].Public()
	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	ep := registry.MustEpoch(0)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		qc, blocks := commitQCAtRoad(ep, keys, epoch.LastRoad(0), ep.FirstBlock())
		if err := state.PushQC(ctx, qc, blocks); err != nil {
			return err
		}
		n := qc.QC().GlobalRange().Next - 1
		if err := state.PushAppHash(ctx, n, types.GenAppHash(rng), map[types.PublicKey]uint64{keeper: 9}); err != nil {
			return err
		}
		if _, err := registry.EpochAt(epoch.FirstRoad(2)); err == nil {
			return errors.New("epoch 2 must stay staged until an AppQC finalizes end(0)")
		}
		if want := utils.Some(types.EpochIndex(2)); registry.Pending() != want {
			return fmt.Errorf("Pending() = %v, want %v", registry.Pending(), want)
		}

		if err := pushAppQCForBlock(ctx, state, keys, n); err != nil {
			return err
		}
		filled, err := registry.EpochAt(epoch.FirstRoad(2))
		if err != nil {
			return fmt.Errorf("epoch 2 after AppQC: %w", err)
		}
		if filled.EpochIndex() != 2 {
			return fmt.Errorf("EpochIndex = %d, want 2", filled.EpochIndex())
		}
		if got := filled.Committee().Weight(keeper); got != 9 {
			return fmt.Errorf("Weight = %d, want 9", got)
		}
		if filled.Committee().Lanes().Len() != 1 {
			return fmt.Errorf("lanes = %d, want 1", filled.Committee().Lanes().Len())
		}
		return nil
	}))
}

func TestCommitteeFill_AppQCDivergenceLeavesStaged(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	keeper := keys[0].Public()
	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	ep := registry.MustEpoch(0)
	qc, blocks := commitQCAtRoad(ep, keys, epoch.LastRoad(0), ep.FirstBlock())
	n := qc.QC().GlobalRange().Next - 1

	// Stop Run before the divergent AppQC: persisting one kills the node.
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushQC(ctx, qc, blocks); err != nil {
			return err
		}
		return state.PushAppHash(ctx, n, types.GenAppHash(rng), map[types.PublicKey]uint64{keeper: 9})
	}))

	divergent := types.NewAppProposal(qc.QC().Proposal(), types.GenAppHash(rng))
	require.NoError(t, state.PushAppQC(ctx, TestAppQC(keys, divergent)))
	_, err := registry.EpochAt(epoch.FirstRoad(2))
	require.Error(t, err, "epoch 2 must stay staged after divergence")
	require.Equal(t, utils.Some(types.EpochIndex(2)), registry.Pending())
}

func TestCommitteeFill_LaterMatchingAppQCDoesNotSkipDivergentLastRoad(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	keeper := keys[0].Public()
	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	ep0 := registry.MustEpoch(0)
	ep1 := registry.MustEpoch(1)
	qc0, blocks0 := commitQCAtRoad(ep0, keys, epoch.LastRoad(0), ep0.FirstBlock())
	gr0 := qc0.QC().GlobalRange()
	qc1, blocks1 := commitQCAtRoad(ep1, keys, epoch.FirstRoad(1), gr0.Next)

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushQC(ctx, qc0, blocks0); err != nil {
			return err
		}
		if err := state.PushAppHash(ctx, gr0.Next-1, types.GenAppHash(rng), map[types.PublicKey]uint64{keeper: 9}); err != nil {
			return err
		}
		if err := state.PushQC(ctx, qc1, blocks1); err != nil {
			return err
		}
		return state.PushAppHash(ctx, qc1.QC().GlobalRange().Next-1, types.GenAppHash(rng), nil)
	}))

	divergent := types.NewAppProposal(qc0.QC().Proposal(), types.GenAppHash(rng))
	require.NoError(t, state.PushAppQC(ctx, TestAppQC(keys, divergent)))
	vote1, err := state.AppVote(ctx, qc1.QC().GlobalRange().First)
	require.NoError(t, err)
	require.NoError(t, state.PushAppQC(ctx, TestAppQC(keys, vote1.Proposal())))

	_, err = registry.EpochAt(epoch.FirstRoad(2))
	require.Error(t, err, "epoch 2 must stay staged when LastRoad(0) diverged")
	require.Equal(t, utils.Some(types.EpochIndex(2)), registry.Pending())
}

func TestRunPersist_HaltsBeforePersistingDivergentAppQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	store := newTestBlockStore(t, t.TempDir())
	state := newTestState(t, &Config{Registry: registry}, store)
	ep := registry.MustEpoch(0)
	qc, blocks := commitQCAtRoad(ep, keys, epoch.FirstRoad(0), ep.FirstBlock())
	n := qc.QC().GlobalRange().Next - 1

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushQC(ctx, qc, blocks); err != nil {
			return err
		}
		return state.PushAppHash(ctx, n, types.GenAppHash(rng), nil)
	}))
	before := store.Status().OrPanic("status after AppHash")

	divergent := types.NewAppProposal(qc.QC().Proposal(), types.GenAppHash(rng))
	require.NoError(t, state.PushAppQC(ctx, TestAppQC(keys, divergent)))

	require.ErrorIs(t, state.runPersist(ctx), ErrAppHashDivergence)
	after := store.Status().OrPanic("status after divergence")
	require.Equal(t, before.NextAppQC, after.NextAppQC, "divergent AppQC must not be persisted")
	require.Equal(t, before.First, after.First, "eviction floor must not advance past it")
}

func TestPushBlockAcceptsBlockWithQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))

	// Push QC without blocks.
	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushQC(ctx, qc, nil))
	gr := qc.QC().GlobalRange()

	// PushBlock for a block whose QC is already present succeeds immediately.
	require.NoError(t, state.PushBlock(ctx, gr.First, blocks[0]))
	got, err := state.TryBlock(gr.First)
	require.NoError(t, err)
	require.Equal(t, blocks[0], got)
}

func TestGlobalBlockByHash(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))

	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
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

// TestPushQCBeforeRunPersistsToBlockStore seeds in-memory QCs/blocks before Run
// (mirroring inbound PushQC after transport start) and asserts runPersist still
// writes them — Status seeding, not inner.nextQC/nextBlock.
func TestPushQCBeforeRunPersistsToBlockStore(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()

	db := newTestBlockStore(t, dir)
	state := newTestState(t, &Config{Registry: registry}, db)

	// Transport-race window: PushQC before data.Run / runPersist starts.
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	require.False(t, db.Status().IsPresent(), "PushQC must not write BlockDB before Run")

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		// PushAppHash waits on persisted.NextBlock, so success implies Flush.
		for n := gr1.First; n < gr1.Next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng), nil); err != nil {
				return fmt.Errorf("PushAppHash(%d): %w", n, err)
			}
		}
		return nil
	}))

	tips := db.Status().OrPanic("non-empty BlockDB status")
	require.Equal(t, gr1.Next, tips.NextBlock)
	require.Equal(t, gr1.Next, tips.NextQC)

	require.NoError(t, db.Close())
	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	require.Equal(t, gr1.Next, state2.NextBlock())
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err, "block %d", n)
		require.NotNil(t, got)
	}
}

// TestEvictionWaitsForAppQC checks that setPersisted does not drop
// AppProposals until AppQC is persisted, and that once it is, heights below
// persisted.First are evicted.
func TestEvictionWaitsForAppQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	gr2 := qc2.QC().GlobalRange()

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		if err := state.PushQC(ctx, qc1, blocks1); err != nil {
			return fmt.Errorf("PushQC(qc1): %w", err)
		}
		for n := gr1.First; n < gr1.Next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng), nil); err != nil {
				return fmt.Errorf("PushAppHash(%d): %w", n, err)
			}
		}

		// No AppQC yet -> eviction must not strip AppProposals; first stays put.
		for inner := range state.inner.Lock() {
			if inner.first != gr1.First {
				return fmt.Errorf("no certified App: first = %d, want %d", inner.first, gr1.First)
			}
			for n := gr1.First; n < gr1.Next; n++ {
				_, ok := inner.appProposals[n]
				if !ok {
					return fmt.Errorf("AppProposal %d missing before AppQC", n)
				}
			}
		}

		if err := pushAppQCForBlock(ctx, state, keys, gr1.First); err != nil {
			return fmt.Errorf("pushAppQCForBlock(%d): %w", gr1.First, err)
		}
		if _, err := state.Anchor().Wait(ctx, func(anchor utils.Option[Anchor]) bool {
			if anchor, ok := anchor.Get(); ok {
				return anchor.AppQC.Proposal().RoadIndex() >= qc1.Index()
			}
			return false
		}); err != nil {
			return fmt.Errorf("state.Anchor.Wait(): %w", err)
		}

		if err := state.PushQC(ctx, qc2, blocks2); err != nil {
			return fmt.Errorf("PushQC(qc2): %w", err)
		}
		for n := gr2.First; n < gr2.Next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng), nil); err != nil {
				return fmt.Errorf("PushAppHash(%d): %w", n, err)
			}
		}

		for inner := range state.inner.Lock() {
			evictionBound := inner.persisted.First
			if inner.first != evictionBound {
				return fmt.Errorf("after catching up, first = %d, want eviction bound %d", inner.first, evictionBound)
			}
			if anchor, ok := inner.anchor.Load().Get(); !ok || anchor.AppQC != inner.appQCs[inner.first] || anchor.CommitQC != inner.qcs[inner.first].qc.QC() {
				return fmt.Errorf("anchor must cover inner.first %d", inner.first)
			}
			for n := gr1.First; n < inner.first; n++ {
				_, ok := inner.appProposals[n]
				if ok {
					return fmt.Errorf("AppProposal %d present below first %d", n, inner.first)
				}
			}
			// Heights at/above exclusive floor stay until executed further.
			for n := inner.first; n < inner.nextAppProposal; n++ {
				_, ok := inner.appProposals[n]
				if !ok {
					return fmt.Errorf("AppProposal %d missing at/above first %d", n, inner.first)
				}
			}
			// Tip QC (nextQC-1) stays; nextToExecute uses maps at/above first.
			if inner.nextQC-1 < inner.first {
				return fmt.Errorf("tip QC height %d below first %d", inner.nextQC-1, inner.first)
			}
			_, ok := inner.qcs[inner.nextQC-1]
			if !ok {
				return fmt.Errorf("tip QC %d missing from maps", inner.nextQC-1)
			}
		}
		return nil
	}))
}

func TestEvictionWaitsForPersistedAppQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	require.NoError(t, pushAppHashesRunning(ctx, state, rng, gr1.First, gr1.Next))
	require.NoError(t, pushAppQCForBlock(ctx, state, keys, gr1.First))

	for inner := range state.inner.Lock() {
		require.Equal(t, gr1.Next, inner.nextAppQC)
		require.Equal(t, gr1.First, inner.persisted.NextAppQC)
		require.Equal(t, gr1.First, inner.first, "accepted but unpersisted AppQC must not advance eviction")
		for n := gr1.First; n < gr1.Next; n++ {
			_, ok := inner.appProposals[n]
			require.True(t, ok, "AppProposal %d must survive until AppQC is persisted", n)
		}
	}
}

func TestPushAppHashBelowAnchorSucceeds(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	epoch := registry.MustEpoch(0)

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		prev := utils.None[*types.CommitQC]()
		for range 2 {
			qc, blocks := TestCommitQC(rng, epoch, keys, prev)
			prev = utils.Some(qc.QC())
			gr := qc.QC().GlobalRange()
			if err := state.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("PushQC: %w", err)
			}
			if err := state.PushAppHash(ctx, gr.Next-1, types.GenAppHash(rng), nil); err != nil {
				return fmt.Errorf("PushAppHash(tip): %w", err)
			}
			if err := pushAppQCForBlock(ctx, state, keys, gr.First); err != nil {
				return fmt.Errorf("pushAppQCForBlock(%d): %w", gr.First, err)
			}
		}
		// Wait for anchor to progress past first block.
		if _, err := state.Anchor().Wait(ctx, func(anchor utils.Option[Anchor]) bool {
			if anchor, ok := anchor.Get(); ok {
				return registry.FirstBlock() < anchor.AppQC.Proposal().GlobalRange().First
			}
			return false
		}); err != nil {
			return fmt.Errorf("state.Anchor.Wait(): %w", err)
		}
		// Pushing apphash for height below the anchor should NOT expolode.
		if err := state.PushAppHash(ctx, registry.FirstBlock(), types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash below anchor: %w", err)
		}
		return nil
	}))
}

// TestNextToExecuteAfterAppEviction checks WaitUntilExecuted / nextToExecute
// still work when persisted AppQC aggressively evicts through nextAppProposal
// (first = persisted.First).
// nextToExecute uses the retained boundary QC.
func TestNextToExecuteAfterAppEviction(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		if err := state.PushQC(ctx, qc1, blocks1); err != nil {
			return fmt.Errorf("PushQC(qc1): %w", err)
		}
		for n := gr1.First; n < gr1.Next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng), nil); err != nil {
				return fmt.Errorf("PushAppHash(%d): %w", n, err)
			}
		}
		// Sticky case: persisted.NextAppQC == nextAppProposal. first advances to
		// NAP-1; NAP-2 is gone; nextToExecute reads qc[NAP-1] until the next QC executes.
		if err := pushAppQCForBlock(ctx, state, keys, gr1.First); err != nil {
			return fmt.Errorf("pushAppQCForBlock(%d): %w", gr1.First, err)
		}
		if _, err := state.Anchor().Wait(ctx, func(anchor utils.Option[Anchor]) bool {
			if anchor, ok := anchor.Get(); ok {
				return anchor.AppQC.Proposal().RoadIndex() >= qc1.Index()
			}
			return false
		}); err != nil {
			return fmt.Errorf("state.Anchor.Wait(): %w", err)
		}
		if err := state.PushQC(ctx, qc2, blocks2); err != nil {
			return fmt.Errorf("PushQC(qc2): %w", err)
		}

		var tipLane types.LaneID
		var tipBlockNum types.BlockNumber
		for inner := range state.inner.Lock() {
			if inner.nextAppProposal != gr1.Next {
				return fmt.Errorf("nextAppProposal = %d, want %d", inner.nextAppProposal, gr1.Next)
			}
			evictionBound := inner.persisted.First
			if inner.first != evictionBound {
				return fmt.Errorf("first = %d, want eviction bound %d", inner.first, evictionBound)
			}
			_, ok := inner.blocks[evictionBound-1]
			if ok {
				return fmt.Errorf("block %d present, want evicted", inner.nextAppProposal-1)
			}
			if inner.nextAppProposal >= inner.nextQC {
				return fmt.Errorf("nextAppProposal = %d, want < nextQC %d", inner.nextAppProposal, inner.nextQC)
			}
			fqc := inner.qcs[inner.nextAppProposal].qc
			if fqc == nil {
				return fmt.Errorf("QC %d missing", inner.nextAppProposal)
			}
			gr := fqc.QC().GlobalRange()
			h := fqc.Headers()[inner.nextAppProposal-gr.First]
			tipLane = h.Lane()
			tipBlockNum = h.BlockNumber()
			if got := inner.nextToExecute(tipLane); got != tipBlockNum {
				return fmt.Errorf("nextToExecute(%d) = %d, want %d", tipLane, got, tipBlockNum)
			}
		}
		// WaitUntilExecuted(n) returns when nextToExecute > n.
		waitFrom := tipBlockNum
		if waitFrom > 0 {
			waitFrom--
		}
		next, err := state.WaitUntilExecuted(ctx, tipLane, waitFrom)
		if err != nil {
			return fmt.Errorf("WaitUntilExecuted(%d, %d): %w", tipLane, waitFrom, err)
		}
		if next != tipBlockNum {
			return fmt.Errorf("WaitUntilExecuted(%d, %d) = %d, want %d", tipLane, waitFrom, next, tipBlockNum)
		}
		return nil
	}))
}

func TestPushAppQCPersistsAndRecovers(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()

	db1 := newTestBlockStore(t, dir)
	state1 := newTestState(t, &Config{Registry: registry}, db1)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state1.Run(ctx)) })

		if err := state1.PushQC(ctx, qc1, blocks1); err != nil {
			return fmt.Errorf("PushQC(qc1): %w", err)
		}
		for n := gr1.First; n < gr1.Next; n++ {
			if err := state1.PushAppHash(ctx, n, types.GenAppHash(rng), nil); err != nil {
				return fmt.Errorf("PushAppHash(%d): %w", n, err)
			}
		}
		if err := pushAppQCForBlock(ctx, state1, keys, gr1.First); err != nil {
			return fmt.Errorf("pushAppQCForBlock(%d): %w", gr1.First, err)
		}
		if _, err := state1.Anchor().Wait(ctx, func(anchor utils.Option[Anchor]) bool {
			if anchor, ok := anchor.Get(); ok {
				return anchor.AppQC.Proposal().RoadIndex() >= qc1.Index()
			}
			return false
		}); err != nil {
			return fmt.Errorf("state.Anchor.Wait(): %w", err)
		}
		return nil
	}))

	storedProposal, err := db1.ReadAppProposalByBlockNumber(gr1.First)
	require.NoError(t, err)
	require.True(t, storedProposal.IsPresent(), "PushAppHash must persist the AppProposal")
	stored, err := db1.ReadAppQCByBlockNumber(gr1.First)
	require.NoError(t, err)
	require.True(t, stored.IsPresent(), "PushAppQC must persist the AppQC")
	require.NoError(t, db1.Close())

	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	for inner := range state2.inner.Lock() {
		require.Equal(t, gr1.Next, inner.nextAppProposal)
		require.Equal(t, gr1.Next, inner.nextAppQC)
	}
	appQC, err := state2.AppQC(ctx, gr1.First)
	require.NoError(t, err)
	fQC, err := state2.QC(ctx, gr1.First)
	require.NoError(t, err)
	require.Equal(t, gr1, appQC.Proposal().GlobalRange())
	require.Equal(t, gr1, fQC.QC().GlobalRange())
	require.NoError(t, db2.Close())
}

// TestPruningKeepsLastQCRange verifies BlockDB's never-empty prune: asking to
// prune past the tip still leaves the newest cohort readable, and a consistent
// range recovers from the QC start.
func TestPruningKeepsLastQCRange(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()

	state1 := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, pushAppHashesRunning(ctx, state1, rng, gr1.First, gr1.Next))

	// Prune past every block; BlockDB clamps to retain the newest cohort.
	require.NoError(t, state1.PruneBefore(gr1.Next))
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state1.TryBlock(n)
		require.NoError(t, err, "never-empty prune should keep cohort block %d", n)
		require.NotNil(t, got)
	}

	// Consistent post-GC shape: full QC range of blocks. Restart recovers at QC start.
	dir := t.TempDir()
	db := newTestBlockStore(t, dir)
	writeToBlockStore(t, db, []*types.FullCommitQC{qc1}, [][]*types.Block{blocks1})
	require.NoError(t, db.Close())

	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	require.Equal(t, gr1.Next, state2.NextBlock())
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
}

// TestPruningWithPartialQCRange verifies BlockDB watermark pruning across QC
// ranges, and that a restart from a consistent BlockDB recovers from the
// retained QC start. BlockDB clamps prune requests to QC First (cohort-atomic
// readability), so a mid-range prune does not refuse heights inside that QC.
//
// PruneBefore is BlockDB-only: heights still retained in RAM for AppVotes
// (at/above persisted.First) remain
// readable via TryBlock even after the store watermark advances past them.
func TestPruningWithPartialQCRange(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()

	state1 := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state1.PushQC(ctx, qc2, blocks2))

	require.NoError(t, pushAppHashesRunning(ctx, state1, rng, gr1.First, gr2.Next))
	var exclusiveFloor types.GlobalBlockNumber
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state1.Run(ctx)) })
		if err := pushAppQCForBlock(ctx, state1, keys, gr1.First); err != nil {
			return err
		}
		if _, err := state1.Anchor().Wait(ctx, func(anchor utils.Option[Anchor]) bool {
			if anchor, ok := anchor.Get(); ok {
				return anchor.AppQC.Proposal().RoadIndex() >= qc1.Index()
			}
			return false
		}); err != nil {
			return fmt.Errorf("state.Anchor.Wait(): %w", err)
		}
		return nil
	}))
	for inner := range state1.inner.Lock() {
		exclusiveFloor = inner.persisted.First
		require.Equal(t, exclusiveFloor, inner.first)
	}

	// Mid-QC prune clamps to gr1.First, so the whole qc1 cohort stays readable.
	midQC1 := gr1.First + (gr1.Next-gr1.First)/2
	if midQC1 > gr1.First {
		require.NoError(t, state1.PruneBefore(midQC1))
		for n := gr1.First; n < midQC1; n++ {
			got, err := state1.TryBlock(n)
			require.NoError(t, err, "mid-QC prune must not refuse block %d (clamped to QC First)", n)
			require.NotNil(t, got)
		}
	}

	// Prune past qc1 entirely. Because qc1 now has a persisted AppQC, BlockDB's
	// never-empty rule keeps that newest AppQC+CommitQC+Block cohort readable.
	require.NoError(t, state1.PruneBefore(gr2.Next))
	// Evicted heights (< exclusive App floor) fall through to BlockDB, but the
	// persisted AppQC cohort is retained by the prune cap.
	for n := gr1.First; n < exclusiveFloor; n++ {
		got, err := state1.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	// Exclusive floor and above stay cached for AppVotes despite BlockDB prune.
	// ByHash must match TryBlock here — not fall through to a pruned BlockDB.
	for n := exclusiveFloor; n < gr2.Next; n++ {
		got, err := state1.TryBlock(n)
		require.NoError(t, err, "height %d must remain readable from RAM (>= exclusive floor)", n)
		require.NotNil(t, got)
		byHash, err := state1.GlobalBlockByHash(got.Header().Hash())
		require.NoError(t, err)
		gb, ok := byHash.Get()
		require.True(t, ok, "GlobalBlockByHash must serve RAM-cached height %d after BlockDB prune", n)
		require.Equal(t, n, gb.GlobalNumber)
	}

	// Consistent retained range: full qc2.
	dir := t.TempDir()
	db := newTestBlockStore(t, dir)
	writeToBlockStore(t, db, []*types.FullCommitQC{qc2}, [][]*types.Block{blocks2})
	require.NoError(t, db.Close())

	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	require.Equal(t, gr2.Next, state2.NextBlock())
	for n := gr2.First; n < gr2.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
}

func TestPushBlockWaitsForQC(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 3)

		state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))

		// Push first QC covering [0, N).
		qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
		require.NoError(t, state.PushQC(ctx, qc1, blocks1))

		// Prepare second QC covering [N, M) but don't push it yet.
		qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
		gr2 := qc2.QC().GlobalRange()

		// Block gr2.First should not be in state yet.
		_, err := state.TryBlock(gr2.First)
		require.ErrorIs(t, err, types.ErrNotFound)

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
		require.ErrorIs(t, err, types.ErrNotFound)

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

// TestTryBlockHidesGapFills verifies the no-gap contract: a block stored above
// nextBlock (gap-fill) is not visible via TryBlock until the contiguous prefix
// catches up. GlobalBlockByHash still serves it from RAM (hash index) even
// though it is not yet durable in BlockDB.
func TestTryBlockHidesGapFills(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()
	require.GreaterOrEqual(t, gr2.Len(), 2)

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state.PushQC(ctx, qc2, nil))
	require.Equal(t, gr1.Next, state.NextBlock())

	// Gap-fill the last height of qc2 before earlier qc2 blocks.
	last := gr2.Next - 1
	gapBlock := blocks2[last-gr2.First]
	require.NoError(t, state.PushBlock(ctx, last, gapBlock))
	_, err := state.TryBlock(last)
	require.ErrorIs(t, err, types.ErrNotFound, "gap-fill above nextBlock must stay hidden")
	require.False(t, state.NeedBlock(last), "NeedBlock must treat gap-fills as already satisfied")
	missing := gr2.First
	require.True(t, state.NeedBlock(missing), "NeedBlock must still request holes below a gap-fill")

	// ByHash must not fall through to BlockDB (gap-fills are not persisted yet).
	gotOpt, err := state.GlobalBlockByHash(gapBlock.Header().Hash())
	require.NoError(t, err)
	gotGB, ok := gotOpt.Get()
	require.True(t, ok, "gap-fill must be served from RAM via GlobalBlockByHash")
	require.Equal(t, last, gotGB.GlobalNumber)
	require.Equal(t, gapBlock.Header(), gotGB.Header)

	// Fill contiguous prefix; last becomes visible with the rest.
	for i, n := 0, gr2.First; n < gr2.Next; n++ {
		require.NoError(t, state.PushBlock(ctx, n, blocks2[i]))
		i++
	}
	require.Equal(t, gr2.Next, state.NextBlock())
	got, err := state.TryBlock(last)
	require.NoError(t, err)
	require.Equal(t, blocks2[last-gr2.First], got)
}
