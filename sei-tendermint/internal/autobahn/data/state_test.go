package data

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type Snapshot struct {
	Blocks       map[types.LaneBlockPos]*types.Block
	QCs          map[types.RoadIndex]*types.FullCommitQC
	AppProposals map[types.RoadIndex]*types.AppProposal
}

func newSnapshot() Snapshot {
	return Snapshot{
		Blocks:       map[types.LaneBlockPos]*types.Block{},
		QCs:          map[types.RoadIndex]*types.FullCommitQC{},
		AppProposals: map[types.RoadIndex]*types.AppProposal{},
	}
}

func snapshot(s *State) Snapshot {
	for inner := range s.inner.Lock() {
		qcs := make(map[types.RoadIndex]*types.FullCommitQC, len(inner.blocks))
		blocks := make(map[types.LaneBlockPos]*types.Block)
		for n, e := range inner.blocks {
			qcs[n] = e.qc
			for offset, block := range e.laneBlocks {
				blocks[types.LaneBlockPos{Index: n, Offset: offset}] = block
			}
		}
		return Snapshot{
			QCs:          qcs,
			Blocks:       blocks,
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
		utils.OrPanic(store.WriteBlock(types.NewRoadBlock(qc, blockss[i])))
	}
	utils.OrPanic(store.Flush())
}

func writeAppDataToBlockStore(t testing.TB, rng utils.Rng, store types.BlockStore, keys []types.SecretKey, qcs ...*types.FullCommitQC) {
	t.Helper()
	for _, qc := range qcs {
		appProposal := types.NewAppProposal(qc.QC().View(), types.GenAppHash(rng))
		utils.OrPanic(store.WriteAppProposal(appProposal))
		utils.OrPanic(store.WriteAppQC(TestAppQC(keys, appProposal)))
	}
	utils.OrPanic(store.Flush())
}

// pushAppHashesRunning runs state.Run under scope.Run long enough to accept
// PushAppHash for [first, next), then cancels Run. Prefers scope.Run over a
// raw goroutine so cleanup is structured.
func pushAppHashesRunning(ctx context.Context, state *State, rng utils.Rng, first, next types.RoadIndex) error {
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

func pushAppQCForBlock(ctx context.Context, state *State, keys []types.SecretKey, n types.RoadIndex) error {
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
) (*types.FullCommitQC, []*types.Block) {
	proposal := types.ProposalAt(ep, types.View{Index: road, Number: 0})
	block := types.NewBlock(ep.Committee().Lanes().At(0), 0, types.BlockHeaderHash{}, &types.Payload{})
	votes := make([]*types.Signed[*types.CommitVote], 0, len(keys))
	prepareQC := types.BuildPrepareQC(keys, proposal)
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(prepareQC)))
	}
	return types.NewFullCommitQC(types.NewCommitQC(votes), []*types.BlockHeader{block.Header()}), []*types.Block{block}
}

func commitQCAtRoadBlocks(
	ep *types.Epoch,
	keys []types.SecretKey,
	road types.RoadIndex,
	n int,
) (*types.FullCommitQC, []*types.Block) {
	proposal, blocks := types.ProposalAtBlocks(ep, types.View{Index: road, Number: 0}, n)
	votes := make([]*types.Signed[*types.CommitVote], 0, len(keys))
	prepareQC := types.BuildPrepareQC(keys, proposal)
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(prepareQC)))
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
	ep1 := registry.MustEpoch(1)
	qcMid, blocksMid := commitQCAtRoad(ep1, keys, registry.LastRoad(1)-1)
	store := newMemoryBlockStore(t)
	writeToBlockStore(t, store, []*types.FullCommitQC{qcMid}, [][]*types.Block{blocksMid})
	state := newTestState(t, &Config{Registry: registry}, store)
	require.Equal(t, ep1, state.NextCommitEpoch().Load())
	require.NoError(t, pushAppHashesRunning(ctx, state, rng, qcMid.Index(), qcMid.Index()+1))
	qcLast, blocksLast := commitQCAtRoad(ep1, keys, registry.LastRoad(1))
	require.NoError(t, state.PushPartialBlock(ctx, qcLast, TestBlockMap(blocksLast)))
	require.Equal(t, registry.MustEpoch(2), state.NextCommitEpoch().Load())
}

func TestNextCommitEpoch_AfterAppQCActivation(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, blocks := commitQCAtRoad(registry.MustEpoch(0), keys, registry.LastRoad(0))
	store := newMemoryBlockStore(t)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	state := newTestState(t, &Config{Registry: registry}, store)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushAppHash(ctx, qc.Index(), types.GenAppHash(rng), map[types.PublicKey]uint64{keys[0].Public(): 9}); err != nil {
			return err
		}
		return pushAppQCForBlock(ctx, state, keys, qc.Index())
	}))
	require.Equal(t, registry.MustEpoch(1), state.NextCommitEpoch().Load())
	require.Equal(t, types.EpochIndex(2), registry.MustEpoch(2).EpochIndex())
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
			if err := state.PushPartialBlock(ctx, qc, TestBlockMap(blocks)); err != nil {
				return fmt.Errorf("state.PushPartialBlock(): %w", err)
			}
			want.QCs[qc.Index()] = qc
			for offset, block := range blocks {
				want.Blocks[types.LaneBlockPos{Index: qc.Index(), Offset: uint64(offset)}] = block
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
				return fmt.Errorf("state.TryBlock(types.LaneBlockPos{Index: %v}): %w", n, err)
			}
			if err := utils.TestDiff(wantB, gotB); err != nil {
				return fmt.Errorf("state.TryBlock(types.LaneBlockPos{Index: %v}): %w", n, err)
			}

			qc := want.QCs[n.Index]
			blocks := make([]*types.Block, len(qc.Headers()))
			for offset := range blocks {
				blocks[offset] = want.Blocks[types.LaneBlockPos{Index: n.Index, Offset: uint64(offset)}]
			}
			wantG := types.NewRoadBlock(qc, blocks).SuperBlock()
			gotG, err := state.SuperBlock(ctx, n.Index)
			if err != nil {
				return fmt.Errorf("state.SuperBlock(%v): %w", n, err)
			}
			if err := utils.TestDiff(wantG, gotG); err != nil {
				return fmt.Errorf("state.SuperBlock(%v): %w", n, err)
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

	// Push a valid QC to advance inner.nextPartialBlock.
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)))

	// Construct a malicious QC signed by non-committee keys.
	// It starts from block 0 (stale) but extends beyond nextPartialBlock.
	// Keep each lane range within the protocol max; we only need the
	// total finalized span to exceed the previously accepted QC by 1.
	badKeys := make([]types.SecretKey, len(keys))
	for i := range badKeys {
		badKeys[i] = types.GenSecretKey(rng)
	}
	laneBlocks := map[types.LaneID][]*types.Block{}
	maliciousBlocksTotal := len(blocks1) + 1
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
	require.Less(t, proposal.Proposal().Msg().Index(), qc1.Index()+1, "test setup: malicious index must be < nextPartialBlock")
	require.Equal(t, proposal.Proposal().Msg().Index(), qc1.Index())

	votes := make([]*types.Signed[*types.CommitVote], 0, len(badKeys))
	prepareQC := types.BuildPrepareQC(keys, proposal.Proposal().Msg())
	for _, k := range badKeys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(prepareQC)))
	}
	maliciousQC := types.NewFullCommitQC(types.NewCommitQC(votes), headers)

	// Push the malicious QC with its blocks. Whether it returns an error is an
	// implementation detail — what matters is that the state is unchanged afterward.
	// Passing blocks also checks them against the retained QC headers.
	_ = state.PushPartialBlock(ctx, maliciousQC, TestBlockMap(malBlocks))

	// Verify state was not corrupted: all previously pushed QCs and blocks are intact.
	gotQC, err := state.QC(ctx, qc1.Index())
	require.NoError(t, err)
	require.Equal(t, qc1, gotQC)
	got, err := state.TryBlock(types.LaneBlockPos{Index: qc1.Index()})
	require.NoError(t, err)
	require.Equal(t, blocks1[0], got)

	// Verify nextPartialBlock did not advance beyond the valid range.
	for inner := range state.inner.Lock() {
		require.Equal(t, qc1.Index()+1, inner.nextPartialBlock)
	}

	// Verify state is still functional: the next valid QC is accepted and visible.
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	require.NoError(t, state.PushPartialBlock(ctx, qc2, TestBlockMap(blocks2)))
	gotQC, err = state.QC(ctx, qc2.Index())
	require.NoError(t, err)
	require.Equal(t, qc2, gotQC)
}

func TestPushPartialBlockIgnoresBlocksMatchingUnverifiedHeaders(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))

	// Push qc1 with NO blocks — only the QC is stored.
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushPartialBlock(ctx, qc1, nil))

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
	_ = state.PushPartialBlock(ctx, tamperedQC, TestBlockMap(fakeBlocks))

	// Verify no fake blocks were inserted.
	_, err := state.TryBlock(types.LaneBlockPos{Index: qc1.Index()})
	require.ErrorIs(t, err, types.ErrNotFound)

	// Push the real blocks (matching qc1's headers) and verify they work.
	require.NoError(t, state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)))
	got, err := state.TryBlock(types.LaneBlockPos{Index: qc1.Index()})
	require.NoError(t, err)
	require.Equal(t, blocks1[0], got)
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
			if err := state.PushPartialBlock(ctx, qc, TestBlockMap(blocks)); err != nil {
				return fmt.Errorf("state.PushPartialBlock(): %w", err)
			}
			prev = utils.Some(qc.QC())
			// PushAppHash for a block beyond nextFullBlock should not succeed:
			// it waits for persistence which never happens for unfinalised blocks.
			shortCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
			if err := state.PushAppHash(shortCtx, qc.Index()+1, types.GenAppHash(rng), nil); err == nil {
				cancel()
				return errors.New("PushAppHash expected to fail on non-finalized blocks")
			}
			cancel()
			if err := state.PushAppHash(ctx, qc.Index(), types.GenAppHash(rng), nil); err != nil {
				return fmt.Errorf("state.PushAppHash(): %w", err)
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
			if err := state.PushPartialBlock(ctx, qc, TestBlockMap(blocks)); err != nil {
				return fmt.Errorf("PushPartialBlock(): %w", err)
			}
			qcs = append(qcs, qc.QC())
		}
		if err := state.PushAppHash(ctx, qcs[0].Index(), types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc1): %w", err)
		}

		if err := state.PushAppHash(ctx, qcs[2].Index(), types.GenAppHash(rng), nil); !errors.Is(err, ErrOutOfOrder) {
			return fmt.Errorf("PushAppHash(qc3 before qc2) error = %w, want %w", err, ErrOutOfOrder)
		}

		if err := state.PushAppHash(ctx, qcs[1].Index(), types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc2): %w", err)
		}
		if err := state.PushAppHash(ctx, qcs[2].Index(), types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc3): %w", err)
		}
		// Inserting old stuff should be a noop.
		if err := state.PushAppHash(ctx, qcs[1].Index(), types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc2): %w", err)
		}
		if err := state.PushAppHash(ctx, qcs[2].Index()-1, types.GenAppHash(rng), nil); err != nil {
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
		if err := state.PushPartialBlock(ctx, qc, TestBlockMap(blocks)); err != nil {
			return err
		}
		if err := state.PushAppHash(ctx, qc.Index(), types.GenAppHash(rng), nil); err != nil {
			return err
		}
		if _, err := registry.EpochAt(registry.EpochFirstRoad(2)); err == nil {
			return fmt.Errorf("epoch 2 must stay absent for road %d", qc.QC().Index())
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
	ep := registry.MustEpoch(0)
	qc, blocks := commitQCAtRoad(ep, keys, registry.LastRoad(0))
	store := newMemoryBlockStore(t)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	state := newTestState(t, &Config{Registry: registry}, store)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		if err := state.PushPartialBlock(ctx, qc, TestBlockMap(blocks)); err != nil {
			return err
		}
		n := qc.Index()
		if err := state.PushAppHash(ctx, n, types.GenAppHash(rng), map[types.PublicKey]uint64{keeper: 9}); err != nil {
			return err
		}
		if _, err := registry.EpochAt(registry.EpochFirstRoad(2)); err == nil {
			return errors.New("epoch 2 must stay staged until an AppQC finalizes end(0)")
		}
		if want := utils.Some(types.EpochIndex(2)); registry.Pending() != want {
			return fmt.Errorf("Pending() = %v, want %v", registry.Pending(), want)
		}

		if err := pushAppQCForBlock(ctx, state, keys, n); err != nil {
			return err
		}
		filled, err := registry.EpochAt(registry.EpochFirstRoad(2))
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
	ep := registry.MustEpoch(0)
	qc, blocks := commitQCAtRoad(ep, keys, registry.LastRoad(0))
	store := newMemoryBlockStore(t)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	state := newTestState(t, &Config{Registry: registry}, store)
	n := qc.Index()

	// Stop Run before the divergent AppQC: persisting one kills the node.
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushPartialBlock(ctx, qc, TestBlockMap(blocks)); err != nil {
			return err
		}
		return state.PushAppHash(ctx, n, types.GenAppHash(rng), map[types.PublicKey]uint64{keeper: 9})
	}))

	divergent := types.NewAppProposal(qc.QC().View(), types.GenAppHash(rng))
	require.NoError(t, state.PushAppQC(ctx, TestAppQC(keys, divergent)))
	_, err := registry.EpochAt(registry.EpochFirstRoad(2))
	require.Error(t, err, "epoch 2 must stay staged after divergence")
	require.Equal(t, utils.Some(types.EpochIndex(2)), registry.Pending())
}

func TestCommitteeFill_LaterMatchingAppQCDoesNotSkipDivergentLastRoad(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	keeper := keys[0].Public()
	ep0 := registry.MustEpoch(0)
	ep1 := registry.MustEpoch(1)
	qc0, blocks0 := commitQCAtRoad(ep0, keys, registry.LastRoad(0))
	store := newMemoryBlockStore(t)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc0}, [][]*types.Block{blocks0})
	state := newTestState(t, &Config{Registry: registry}, store)
	qc1, blocks1 := commitQCAtRoad(ep1, keys, registry.EpochFirstRoad(1))

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushPartialBlock(ctx, qc0, TestBlockMap(blocks0)); err != nil {
			return err
		}
		if err := state.PushAppHash(ctx, qc0.Index(), types.GenAppHash(rng), map[types.PublicKey]uint64{keeper: 9}); err != nil {
			return err
		}
		if err := state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)); err != nil {
			return err
		}
		return state.PushAppHash(ctx, qc1.Index(), types.GenAppHash(rng), nil)
	}))

	divergent := types.NewAppProposal(qc0.QC().View(), types.GenAppHash(rng))
	require.NoError(t, state.PushAppQC(ctx, TestAppQC(keys, divergent)))
	vote1, err := state.AppVote(ctx, qc1.Index())
	require.NoError(t, err)
	require.NoError(t, state.PushAppQC(ctx, TestAppQC(keys, vote1.Proposal())))

	_, err = registry.EpochAt(registry.EpochFirstRoad(2))
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
	qc, blocks := commitQCAtRoad(ep, keys, ep.RoadRange().First)
	n := qc.Index()

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushPartialBlock(ctx, qc, TestBlockMap(blocks)); err != nil {
			return err
		}
		return state.PushAppHash(ctx, n, types.GenAppHash(rng), nil)
	}))
	before := store.Status().OrPanic("status after AppHash")

	divergent := types.NewAppProposal(qc.QC().View(), types.GenAppHash(rng))
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
	require.NoError(t, state.PushPartialBlock(ctx, qc, nil))

	// PushBlock for a block whose QC is already present succeeds immediately.
	require.NoError(t, state.PushBlock(ctx, types.LaneBlockPos{Index: qc.Index()}, blocks[0]))
	require.False(t, state.NeedBlock(types.LaneBlockPos{Index: qc.Index()}))
	for offset, block := range blocks[1:] {
		require.NoError(t, state.PushBlock(ctx, types.LaneBlockPos{Index: qc.Index(), Offset: uint64(offset + 1)}, block))
	}
	got, err := state.TryBlock(types.LaneBlockPos{Index: qc.Index()})
	require.NoError(t, err)
	require.Equal(t, blocks[0], got)
}

func TestInsertAppProposalRequiresCommitQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	state := newTestState(t, &Config{Registry: registry}, newMemoryBlockStore(t))
	previous := utils.None[*types.CommitQC]()

	for range 2 {
		qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, previous)
		proposal := types.NewAppProposal(qc.QC().View(), types.GenAppHash(rng))
		for inner := range state.inner.Lock() {
			require.Equal(t, proposal.Index(), inner.nextPartialBlock)
			require.Equal(t, proposal.Index(), inner.nextAppProposal)
			require.Error(t, inner.insertAppProposal(proposal))
			require.Equal(t, proposal.Index(), inner.nextAppProposal)
			_, inserted := inner.appProposals[proposal.Index()]
			require.False(t, inserted)
		}

		require.NoError(t, state.PushPartialBlock(t.Context(), qc, TestBlockMap(blocks)))
		for inner := range state.inner.Lock() {
			require.NoError(t, inner.insertAppProposal(proposal))
			require.Equal(t, proposal.Index()+1, inner.nextAppProposal)
		}
		previous = utils.Some(qc.QC())
	}
}

func TestPushEmptyQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep := registry.MustEpoch(0)
	previous, blocks := TestCommitQC(rng, ep, keys, utils.None[*types.CommitQC]())
	vs := types.ViewSpec{ConsensusSpec: types.ConsensusSpec{CommitQC: utils.Some(previous.QC()), Epoch: ep}}
	leader := keys[0]
	for _, key := range keys {
		if key.Public() == ep.Committee().Leader(vs.View()) {
			leader = key
			break
		}
	}
	proposal, err := types.NewProposal(leader, vs, vs.NextTimestamp(), nil)
	require.NoError(t, err)
	votes := make([]*types.Signed[*types.CommitVote], len(keys))
	prepareQC := types.BuildPrepareQC(keys, proposal.Proposal().Msg())
	for i, key := range keys {
		votes[i] = types.Sign(key, types.NewCommitVote(prepareQC))
	}
	qc := types.NewFullCommitQC(types.NewCommitQC(votes), nil)
	state := newTestState(t, &Config{Registry: registry}, newMemoryBlockStore(t))
	require.NoError(t, state.PushPartialBlock(t.Context(), previous, TestBlockMap(blocks)))
	require.NoError(t, state.PushPartialBlock(t.Context(), qc, nil))
	want := types.NewRoadBlock(qc, nil).SuperBlock()
	byHash, err := state.SuperBlockByHash(want.Hash)
	require.NoError(t, err)
	require.True(t, byHash.IsPresent())
	require.Equal(t, qc.Index()+1, state.NextQC())
	got, err := state.SuperBlock(t.Context(), qc.Index())
	require.NoError(t, err)
	require.Equal(t, want.Height, got.Height)
	require.Empty(t, got.Txs)
}

func TestSuperBlockByHashDoesNotServeGaps(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	state := newTestState(t, &Config{Registry: registry}, newMemoryBlockStore(t))
	ep := registry.MustEpoch(0)
	qc1, blocks1 := TestCommitQC(rng, ep, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, ep, keys, utils.Some(qc1.QC()))
	require.NoError(t, state.PushPartialBlock(ctx, qc1, nil))
	require.NoError(t, state.PushPartialBlock(ctx, qc2, TestBlockMap(blocks2)))

	for _, qc := range []*types.FullCommitQC{qc1, qc2} {
		hash := qc.Header().Hash()
		for inner := range state.inner.Lock() {
			n, indexed := inner.byHash[hash]
			require.True(t, indexed, "CommitQC must be indexed before the contiguous prefix advances")
			require.Equal(t, qc.Index(), n)
		}
		got, err := state.SuperBlockByHash(hash)
		require.NoError(t, err)
		require.False(t, got.IsPresent(), "hash lookups must not serve incomplete blocks or skip gaps")
	}

	require.NoError(t, state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)))
	for _, qc := range []*types.FullCommitQC{qc1, qc2} {
		got, err := state.SuperBlockByHash(qc.Header().Hash())
		require.NoError(t, err)
		block, ok := got.Get()
		require.True(t, ok)
		require.Equal(t, qc.Index(), block.Height)
		require.Equal(t, qc.Header().Hash(), block.Hash)
	}
}

func TestSuperBlockByHash(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))

	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushPartialBlock(ctx, qc, TestBlockMap(blocks)))
	n := qc.Index()
	wantBlock := types.NewRoadBlock(qc, blocks).SuperBlock()
	wantHash := wantBlock.Hash
	gotOpt, err := state.SuperBlockByHash(wantHash)
	require.NoError(t, err)
	gotGB, ok := gotOpt.Get()
	require.True(t, ok)
	require.Equal(t, n, gotGB.Height)
	require.Equal(t, wantBlock.Txs, gotGB.Txs)
	require.Equal(t, wantBlock.GasWanted, gotGB.GasWanted)

	// Zero hash → None.
	zeroOpt, err := state.SuperBlockByHash(types.RoadBlockHeaderHash{})
	require.NoError(t, err)
	_, ok = zeroOpt.Get()
	require.False(t, ok, "SuperBlockByHash(zero) returned Some")

	// Random unknown hash → None.
	var randHash types.RoadBlockHeaderHash
	rng.Read(randHash[:])
	randOpt, err := state.SuperBlockByHash(randHash)
	require.NoError(t, err)
	_, ok = randOpt.Get()
	require.False(t, ok, "SuperBlockByHash(random) returned Some")
}

// TestPushPartialBlockBeforeRunPersistsToBlockStore seeds in-memory QCs/blocks before Run
// (mirroring inbound PushPartialBlock after transport start) and asserts runPersist still
// writes them — Status seeding, not inner.nextPartialBlock/nextFullBlock.
func TestPushPartialBlockBeforeRunPersistsToBlockStore(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())

	db := newTestBlockStore(t, dir)
	state := newTestState(t, &Config{Registry: registry}, db)

	// Transport-race window: PushPartialBlock before data.Run / runPersist starts.
	require.NoError(t, state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)))
	require.False(t, db.Status().IsPresent(), "PushPartialBlock must not write BlockDB before Run")

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		// PushAppHash waits on persisted.NextBlock, so success implies Flush.
		if err := state.PushAppHash(ctx, qc1.Index(), types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(%d): %w", qc1.Index(), err)
		}
		return nil
	}))

	tips := db.Status().OrPanic("non-empty BlockDB status")
	require.Equal(t, qc1.Index()+1, tips.NextBlock)

	require.NoError(t, db.Close())
	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	require.Equal(t, qc1.Index()+1, state2.NextQC())
	got, err := state2.TryBlock(types.LaneBlockPos{Index: qc1.Index()})
	require.NoError(t, err, "block %d", qc1.Index())
	require.NotNil(t, got)
}

// TestRunPersistWaitsForCompleteQC verifies that a pending QC's partial blocks
// stay out of storage until its final block arrives.
func TestRunPersistWaitsForCompleteQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	store := newTestBlockStore(t, t.TempDir())
	state := newTestState(t, &Config{Registry: registry}, store)
	require.NoError(t, state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)))
	require.NoError(t, state.PushPartialBlock(ctx, qc2, TestBlockMap(blocks2[:len(blocks2)-1])))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		for inner, ctrl := range state.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool { return inner.persisted.NextBlock == qc1.Index()+1 }); err != nil {
				return err
			}
		}
		if got := store.Status().OrPanic("persisted QC"); got.NextBlock != qc1.Index()+1 {
			return fmt.Errorf("incomplete QC was persisted: %v", got)
		}
		for range blocks2 {
			got, err := store.ReadBlockByHash(qc2.Header().Hash())
			if err != nil {
				return err
			}
			if got.IsPresent() {
				return fmt.Errorf("partial QC block was persisted")
			}
		}
		if err := state.PushBlock(ctx, types.LaneBlockPos{Index: qc2.Index(), Offset: uint64(len(blocks2) - 1)}, blocks2[len(blocks2)-1]); err != nil {
			return err
		}
		for inner, ctrl := range state.inner.Lock() {
			return ctrl.WaitUntil(ctx, func() bool { return inner.persisted.NextBlock == qc2.Index()+1 })
		}
		panic("unreachable")
	}))
	require.Equal(t, qc2.Index()+1, store.Status().OrPanic("complete QCs").NextBlock)
}

// TestEvictionWaitsForAppQC checks that setPersisted does not drop
// AppProposals until AppQC is persisted, and that once it is, heights below
// persisted.First are evicted.
func TestEvictionWaitsForAppQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		if err := state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)); err != nil {
			return fmt.Errorf("PushPartialBlock(qc1): %w", err)
		}
		if err := state.PushAppHash(ctx, qc1.Index(), types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(%d): %w", qc1.Index(), err)
		}

		// No AppQC yet -> eviction must not strip AppProposals; first stays put.
		for inner := range state.inner.Lock() {
			if inner.first != qc1.Index() {
				return fmt.Errorf("no certified App: first = %d, want %d", inner.first, qc1.Index())
			}
			_, ok := inner.appProposals[qc1.Index()]
			if !ok {
				return fmt.Errorf("AppProposal %d missing before AppQC", qc1.Index())
			}
		}

		if err := pushAppQCForBlock(ctx, state, keys, qc1.Index()); err != nil {
			return fmt.Errorf("pushAppQCForBlock(%d): %w", qc1.Index(), err)
		}
		if _, err := state.Anchor().Wait(ctx, func(anchor utils.Option[Anchor]) bool {
			if anchor, ok := anchor.Get(); ok {
				return anchor.AppQC.Proposal().Index() >= qc1.Index()
			}
			return false
		}); err != nil {
			return fmt.Errorf("state.Anchor.Wait(): %w", err)
		}

		if err := state.PushPartialBlock(ctx, qc2, TestBlockMap(blocks2)); err != nil {
			return fmt.Errorf("PushPartialBlock(qc2): %w", err)
		}
		if err := state.PushAppHash(ctx, qc2.Index(), types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(%d): %w", qc2.Index(), err)
		}

		for inner := range state.inner.Lock() {
			evictionBound := inner.persisted.First
			if inner.first != evictionBound {
				return fmt.Errorf("after catching up, first = %d, want eviction bound %d", inner.first, evictionBound)
			}
			if anchor, ok := inner.anchor.Load().Get(); !ok || anchor.AppQC != inner.appQCs[inner.first] || anchor.CommitQC != inner.blocks[inner.first].qc.QC() {
				return fmt.Errorf("anchor must cover inner.first %d", inner.first)
			}
			for n := qc1.Index(); n < inner.first; n++ {
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
			// Tip QC (nextPartialBlock-1) stays; nextToExecute uses maps at/above first.
			if inner.nextPartialBlock-1 < inner.first {
				return fmt.Errorf("tip QC height %d below first %d", inner.nextPartialBlock-1, inner.first)
			}
			_, ok := inner.blocks[inner.nextPartialBlock-1]
			if !ok {
				return fmt.Errorf("tip QC %d missing from maps", inner.nextPartialBlock-1)
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

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)))
	require.NoError(t, pushAppHashesRunning(ctx, state, rng, qc1.Index(), qc1.Index()+1))
	require.NoError(t, pushAppQCForBlock(ctx, state, keys, qc1.Index()))

	for inner := range state.inner.Lock() {
		require.Equal(t, qc1.Index()+1, inner.nextAppQC)
		require.Equal(t, qc1.Index(), inner.persisted.NextAppQC)
		require.Equal(t, qc1.Index(), inner.first, "accepted but unpersisted AppQC must not advance eviction")
		_, ok := inner.appProposals[qc1.Index()]
		require.True(t, ok, "AppProposal %d must survive until AppQC is persisted", qc1.Index())
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
			if err := state.PushPartialBlock(ctx, qc, TestBlockMap(blocks)); err != nil {
				return fmt.Errorf("PushPartialBlock: %w", err)
			}
			if err := state.PushAppHash(ctx, qc.Index(), types.GenAppHash(rng), nil); err != nil {
				return fmt.Errorf("PushAppHash(tip): %w", err)
			}
			if err := pushAppQCForBlock(ctx, state, keys, qc.Index()); err != nil {
				return fmt.Errorf("pushAppQCForBlock(%d): %w", qc.Index(), err)
			}
		}
		// Wait for anchor to progress past first block.
		if _, err := state.Anchor().Wait(ctx, func(anchor utils.Option[Anchor]) bool {
			if anchor, ok := anchor.Get(); ok {
				return registry.FirstRoad() < anchor.AppQC.Proposal().Index()
			}
			return false
		}); err != nil {
			return fmt.Errorf("state.Anchor.Wait(): %w", err)
		}
		// Pushing apphash for height below the anchor should NOT expolode.
		if err := state.PushAppHash(ctx, registry.FirstRoad(), types.GenAppHash(rng), nil); err != nil {
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
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		if err := state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)); err != nil {
			return fmt.Errorf("PushPartialBlock(qc1): %w", err)
		}
		if err := state.PushAppHash(ctx, qc1.Index(), types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(%d): %w", qc1.Index(), err)
		}
		// Sticky case: persisted.NextAppQC == nextAppProposal. first advances to
		// NAP-1; NAP-2 is gone; nextToExecute reads qc[NAP-1] until the next QC executes.
		if err := pushAppQCForBlock(ctx, state, keys, qc1.Index()); err != nil {
			return fmt.Errorf("pushAppQCForBlock(%d): %w", qc1.Index(), err)
		}
		if _, err := state.Anchor().Wait(ctx, func(anchor utils.Option[Anchor]) bool {
			if anchor, ok := anchor.Get(); ok {
				return anchor.AppQC.Proposal().Index() >= qc1.Index()
			}
			return false
		}); err != nil {
			return fmt.Errorf("state.Anchor.Wait(): %w", err)
		}
		if err := state.PushPartialBlock(ctx, qc2, TestBlockMap(blocks2)); err != nil {
			return fmt.Errorf("PushPartialBlock(qc2): %w", err)
		}

		var tipLane types.LaneID
		var tipBlockNum types.BlockNumber
		for inner := range state.inner.Lock() {
			if inner.nextAppProposal != qc1.Index()+1 {
				return fmt.Errorf("nextAppProposal = %d, want %d", inner.nextAppProposal, qc1.Index()+1)
			}
			evictionBound := inner.persisted.First
			if inner.first != evictionBound {
				return fmt.Errorf("first = %d, want eviction bound %d", inner.first, evictionBound)
			}
			_, ok := inner.blocks[evictionBound-1]
			if ok {
				return fmt.Errorf("block %d present, want evicted", inner.nextAppProposal-1)
			}
			if inner.nextAppProposal >= inner.nextPartialBlock {
				return fmt.Errorf("nextAppProposal = %d, want < nextPartialBlock %d", inner.nextAppProposal, inner.nextPartialBlock)
			}
			fqc := inner.blocks[inner.nextAppProposal].qc
			if fqc == nil {
				return fmt.Errorf("QC %d missing", inner.nextAppProposal)
			}
			h := fqc.Headers()[0]
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

	db1 := newTestBlockStore(t, dir)
	state1 := newTestState(t, &Config{Registry: registry}, db1)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state1.Run(ctx)) })

		if err := state1.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)); err != nil {
			return fmt.Errorf("PushPartialBlock(qc1): %w", err)
		}
		if err := state1.PushAppHash(ctx, qc1.Index(), types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(%d): %w", qc1.Index(), err)
		}
		if err := pushAppQCForBlock(ctx, state1, keys, qc1.Index()); err != nil {
			return fmt.Errorf("pushAppQCForBlock(%d): %w", qc1.Index(), err)
		}
		if _, err := state1.Anchor().Wait(ctx, func(anchor utils.Option[Anchor]) bool {
			if anchor, ok := anchor.Get(); ok {
				return anchor.AppQC.Proposal().Index() >= qc1.Index()
			}
			return false
		}); err != nil {
			return fmt.Errorf("state.Anchor.Wait(): %w", err)
		}
		return nil
	}))

	storedProposal, err := db1.ReadAppProposal(qc1.Index())
	require.NoError(t, err)
	require.True(t, storedProposal.IsPresent(), "PushAppHash must persist the AppProposal")
	stored, err := db1.ReadAppQC(qc1.Index())
	require.NoError(t, err)
	require.True(t, stored.IsPresent(), "PushAppQC must persist the AppQC")
	require.NoError(t, db1.Close())

	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	for inner := range state2.inner.Lock() {
		require.Equal(t, qc1.Index()+1, inner.nextAppProposal)
		require.Equal(t, qc1.Index()+1, inner.nextAppQC)
	}
	appQC, err := state2.AppQC(ctx, qc1.Index())
	require.NoError(t, err)
	fQC, err := state2.QC(ctx, qc1.Index())
	require.NoError(t, err)
	require.Equal(t, qc1.Index(), appQC.Proposal().Index())
	require.Equal(t, qc1.Index(), fQC.Index())
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

	state1 := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state1.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)))
	require.NoError(t, pushAppHashesRunning(ctx, state1, rng, qc1.Index(), qc1.Index()+1))

	// Prune past every block; BlockDB clamps to retain the newest cohort.
	require.NoError(t, state1.PruneBefore(qc1.Index()+1))
	got, err := state1.TryBlock(types.LaneBlockPos{Index: qc1.Index()})
	require.NoError(t, err, "never-empty prune should keep cohort block %d", qc1.Index())
	require.NotNil(t, got)

	// Consistent post-GC shape: full QC range of blocks. Restart recovers at QC start.
	dir := t.TempDir()
	db := newTestBlockStore(t, dir)
	writeToBlockStore(t, db, []*types.FullCommitQC{qc1}, [][]*types.Block{blocks1})
	require.NoError(t, db.Close())

	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	require.Equal(t, qc1.Index()+1, state2.NextQC())
	got, err = state2.TryBlock(types.LaneBlockPos{Index: qc1.Index()})
	require.NoError(t, err)
	require.NotNil(t, got)
}

// TestPruningPreservesRAMAndRecoveryAnchor verifies BlockDB watermark pruning across QC
// ranges, and that a restart from a consistent BlockDB recovers from the
// retained QC start. BlockDB clamps prune requests to QC First (cohort-atomic
// readability), so a mid-range prune does not refuse heights inside that QC.
//
// PruneBefore is BlockDB-only: heights still retained in RAM for AppVotes
// (at/above persisted.First) remain
// readable via TryBlock even after the store watermark advances past them.
func TestPruningPreservesRAMAndRecoveryAnchor(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))

	state1 := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state1.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)))
	require.NoError(t, state1.PushPartialBlock(ctx, qc2, TestBlockMap(blocks2)))

	require.NoError(t, pushAppHashesRunning(ctx, state1, rng, qc1.Index(), qc2.Index()+1))
	var exclusiveFloor types.RoadIndex
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state1.Run(ctx)) })
		if err := pushAppQCForBlock(ctx, state1, keys, qc1.Index()); err != nil {
			return err
		}
		if _, err := state1.Anchor().Wait(ctx, func(anchor utils.Option[Anchor]) bool {
			if anchor, ok := anchor.Get(); ok {
				return anchor.AppQC.Proposal().Index() >= qc1.Index()
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

	// Prune past qc1 entirely. Because qc1 now has a persisted AppQC, BlockDB's
	// never-empty rule keeps that newest AppQC+CommitQC+Block cohort readable.
	require.NoError(t, state1.PruneBefore(qc2.Index()+1))
	// Evicted heights (< exclusive App floor) fall through to BlockDB, but the
	// persisted AppQC cohort is retained by the prune cap.
	for n := qc1.Index(); n < exclusiveFloor; n++ {
		got, err := state1.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	// Exclusive floor and above stay cached for AppVotes despite BlockDB prune.
	// ByHash must match TryBlock here — not fall through to a pruned BlockDB.
	for n := exclusiveFloor; n < qc2.Index()+1; n++ {
		got, err := state1.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err, "height %d must remain readable from RAM (>= exclusive floor)", n)
		require.NotNil(t, got)
		byHash, err := state1.SuperBlockByHash(utils.OrPanic1(state1.SuperBlock(ctx, n)).Hash)
		require.NoError(t, err)
		gb, ok := byHash.Get()
		require.True(t, ok, "SuperBlockByHash must serve RAM-cached height %d after BlockDB prune", n)
		require.Equal(t, n, gb.Height)
	}

	// Consistent retained range: full qc2.
	dir := t.TempDir()
	db := newTestBlockStore(t, dir)
	writeToBlockStore(t, db, []*types.FullCommitQC{qc2}, [][]*types.Block{blocks2})
	require.NoError(t, db.Close())

	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	require.Equal(t, qc2.Index()+1, state2.NextQC())
	got, err := state2.TryBlock(types.LaneBlockPos{Index: qc2.Index()})
	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestPushBlockWaitsForQC(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 3)
		state := newTestState(t, &Config{Registry: registry}, newMemoryBlockStore(t))
		qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
		require.NoError(t, state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)))
		qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
		pos := types.LaneBlockPos{Index: qc2.Index()}
		done := make(chan struct{})
		require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			s.Spawn(func() error {
				if err := state.PushBlock(ctx, pos, blocks2[0]); err != nil {
					return err
				}
				close(done)
				return nil
			})
			synctest.Wait()
			if _, err := state.TryBlock(pos); !errors.Is(err, types.ErrNotFound) {
				return fmt.Errorf("block available before QC: %w", err)
			}
			remainingBlocks := TestBlockMap(blocks2)
			delete(remainingBlocks, 0)
			if err := state.PushPartialBlock(ctx, qc2, remainingBlocks); err != nil {
				return err
			}
			if _, _, err := utils.RecvOrClosed(ctx, done); err != nil {
				return err
			}
			got, err := state.TryBlock(pos)
			if err != nil {
				return err
			}
			return utils.TestDiff(blocks2[0], got)
		}))
	})
}

// TestTryBlockServesPartialBlocks verifies that received lane blocks are readable
// before their SuperBlock is complete.
func TestTryBlockServesPartialBlocks(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	state := newTestState(t, &Config{Registry: registry}, newMemoryBlockStore(t))
	require.NoError(t, state.PushPartialBlock(ctx, qc, nil))
	require.Equal(t, qc.Index()+1, state.NextQC())
	last := types.LaneBlockPos{Index: qc.Index(), Offset: uint64(len(blocks) - 1)}
	_, err := state.TryBlock(last)
	require.ErrorIs(t, err, types.ErrNotFound)
	_, err = state.TryBlock(types.LaneBlockPos{Index: qc.Index() + 1})
	require.ErrorIs(t, err, types.ErrNotFound)
	require.NoError(t, state.PushBlock(ctx, last, blocks[len(blocks)-1]))
	gotLane, err := state.TryBlock(last)
	require.NoError(t, err)
	require.Equal(t, blocks[len(blocks)-1], gotLane)
	_, err = state.TryBlock(types.LaneBlockPos{Index: qc.Index()})
	require.ErrorIs(t, err, types.ErrNotFound)
	require.Equal(t, qc.Index(), state.NextFullBlock())
	require.False(t, state.NeedBlock(last))
	require.True(t, state.NeedBlock(types.LaneBlockPos{Index: qc.Index()}))
	want := types.NewRoadBlock(qc, blocks).SuperBlock()
	opt, err := state.SuperBlockByHash(want.Hash)
	require.NoError(t, err)
	require.False(t, opt.IsPresent(), "incomplete SuperBlock must not be served")
	for offset, block := range blocks {
		require.NoError(t, state.PushBlock(ctx, types.LaneBlockPos{Index: qc.Index(), Offset: uint64(offset)}, block))
	}
	require.Equal(t, qc.Index()+1, state.NextFullBlock())
	got, err := state.SuperBlock(ctx, qc.Index())
	require.NoError(t, err)
	require.Equal(t, want.Txs, got.Txs)
	opt, err = state.SuperBlockByHash(want.Hash)
	require.NoError(t, err)
	require.True(t, opt.IsPresent())
}

// gathered reads one series from the default registry. A missing label set is 0.
func gathered(t *testing.T, name string, labels map[string]string) int64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			got := map[string]string{}
			for _, lp := range m.GetLabel() {
				got[lp.GetName()] = lp.GetValue()
			}
			if len(got) != len(labels) {
				continue
			}
			match := true
			for k, v := range labels {
				if got[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			if c := m.GetCounter(); c != nil {
				return int64(c.GetValue())
			}
			return int64(m.GetGauge().GetValue())
		}
		return 0
	}
	return 0
}

func TestPushPartialBlock_RecordsNextBlockQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	store := newTestBlockStore(t, t.TempDir())
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	writeToBlockStore(t, store, []*types.FullCommitQC{qc1}, [][]*types.Block{blocks1})

	state := newTestState(t, &Config{Registry: registry}, store)
	require.Equal(t, int64(qc1.Index()+1), gathered(t, "tendermint_internal_autobahn_data_next_block", map[string]string{"stage": "qc"}))

	qc2, _ := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	require.NoError(t, state.PushPartialBlock(ctx, qc2, nil))
	require.Equal(t, int64(qc2.Index()+1), gathered(t, "tendermint_internal_autobahn_data_next_block", map[string]string{"stage": "qc"}))

	require.NoError(t, state.PushPartialBlock(ctx, qc2, nil))
	require.Equal(t, int64(qc2.Index()+1), gathered(t, "tendermint_internal_autobahn_data_next_block", map[string]string{"stage": "qc"}))
}

func TestLaneBlockInvalidOffset(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	store := newMemoryBlockStore(t)
	state := newTestState(t, &Config{Registry: registry}, store)
	require.NoError(t, state.PushPartialBlock(t.Context(), qc, TestBlockMap(blocks)))
	pos := types.LaneBlockPos{Index: qc.Index(), Offset: uint64(len(blocks))}
	_, err := state.TryBlock(pos)
	require.ErrorIs(t, err, types.ErrNotFound)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	recovered := newTestState(t, &Config{Registry: registry}, store)
	_, err = recovered.Block(t.Context(), pos)
	require.ErrorIs(t, err, types.ErrNotFound)
}

func TestQCCacheBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 1)
		ep := registry.MustEpoch(0)
		state := newTestState(t, &Config{Registry: registry}, newMemoryBlockStore(t))
		firstQC, blocks := TestCommitQC(rng, ep, keys, utils.None[*types.CommitQC]())
		require.NoError(t, state.PushPartialBlock(t.Context(), firstQC, nil))
		previous := firstQC.QC()
		var nextPartialBlock *types.FullCommitQC
		for i := range qcsCacheSize {
			vs := types.ViewSpec{ConsensusSpec: types.ConsensusSpec{CommitQC: utils.Some(previous), Epoch: ep}}
			proposal, err := types.NewProposal(keys[0], vs, vs.NextTimestamp(), nil)
			require.NoError(t, err)
			qc := types.NewCommitQC([]*types.Signed[*types.CommitVote]{types.Sign(keys[0], types.NewCommitVote(types.BuildPrepareQC(keys, proposal.Proposal().Msg())))})
			nextPartialBlock = types.NewFullCommitQC(qc, nil)
			if i < qcsCacheSize-1 {
				require.NoError(t, state.PushPartialBlock(t.Context(), nextPartialBlock, nil))
			}
			previous = qc
		}
		hashes := []types.AppHash{types.GenAppHash(rng), types.GenAppHash(rng)}
		done := utils.NewAtomicSend(false)
		require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
			s.Spawn(func() error {
				if err := state.PushPartialBlock(ctx, nextPartialBlock, nil); err != nil {
					return err
				}
				done.Store(true)
				return nil
			})
			synctest.Wait()
			if done.Load() {
				return fmt.Errorf("new QC admitted while the cache is full")
			}
			// Already admitted QCs must still accept their missing blocks at capacity.
			if err := state.PushPartialBlock(ctx, firstQC, TestBlockMap(blocks)); err != nil {
				return err
			}
			s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
			// The second persisted AppQC evicts the first road and frees one entry.
			for i, hash := range hashes {
				road := firstQC.Index() + types.RoadIndex(i)
				if err := state.PushAppHash(ctx, road, hash, nil); err != nil {
					return err
				}
				if err := pushAppQCForBlock(ctx, state, keys, road); err != nil {
					return err
				}
			}
			_, err := done.Subscribe().Wait(ctx, func(v bool) bool { return v })
			return err
		}))
		require.Len(t, snapshot(state).QCs, qcsCacheSize)
	})
}
