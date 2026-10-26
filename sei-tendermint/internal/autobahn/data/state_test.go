package data

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ethereum/go-ethereum/common"

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
		qcs := make(map[types.RoadIndex]*types.FullCommitQC, len(inner.qcs))
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
		utils.OrPanic(store.WriteQC(qc, blockss[i]))
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
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(proposal)))
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
	ep1 := registry.MustEpoch(1)
	qcMid, blocksMid := commitQCAtRoad(ep1, keys, registry.LastRoad(1)-1)
	store := newMemoryBlockStore(t)
	writeToBlockStore(t, store, []*types.FullCommitQC{qcMid}, [][]*types.Block{blocksMid})
	state := newTestState(t, &Config{Registry: registry}, store)
	require.Equal(t, ep1, state.NextCommitEpoch().Load())
	require.NoError(t, pushAppHashesRunning(ctx, state, rng, qcMid.Index(), qcMid.QC().Next()))
	qcLast, blocksLast := commitQCAtRoad(ep1, keys, registry.LastRoad(1))
	require.NoError(t, state.PushQC(ctx, qcLast, blocksLast))
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
			if err := state.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("state.PushQC(): %w", err)
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
			wantG := types.NewSuperBlock(qc, blocks)
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

	// Push a valid QC to advance inner.nextQC.
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}

	// Construct a malicious QC signed by non-committee keys.
	// It starts from block 0 (stale) but extends beyond nextQC.
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
	malGR := types.RoadRange{First: proposal.Proposal().Msg().Index(), Next: proposal.Proposal().Msg().Next()}
	require.Less(t, malGR.First, gr1.Next, "test setup: malicious gr.First must be < nextQC")
	require.Equal(t, malGR.Next, gr1.Next)

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
		got, err := state.TryBlock(types.LaneBlockPos{Index: n})
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
	gr2 := types.RoadRange{First: qc2.QC().Index(), Next: qc2.QC().Next()}
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
	gr := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}

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
		_, err := state.TryBlock(types.LaneBlockPos{Index: n})
		require.ErrorIs(t, err, types.ErrNotFound)
	}

	// Push the real blocks (matching qc1's headers) and verify they work.
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	for i, n := 0, gr.First; n < gr.Next; n++ {
		got, err := state.TryBlock(types.LaneBlockPos{Index: n})
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
			gr := types.RoadRange{First: qc.QC().Index(), Next: qc.QC().Next()}
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
		if err := state.PushAppHash(ctx, qcs[0].Next()-1, types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc1): %w", err)
		}

		if err := state.PushAppHash(ctx, qcs[2].Next()-1, types.GenAppHash(rng), nil); !errors.Is(err, ErrOutOfOrder) {
			return fmt.Errorf("PushAppHash(qc3 before qc2) error = %w, want %w", err, ErrOutOfOrder)
		}

		if err := state.PushAppHash(ctx, qcs[1].Next()-1, types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc2): %w", err)
		}
		if err := state.PushAppHash(ctx, qcs[2].Next()-1, types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc3): %w", err)
		}
		// Inserting old stuff should be a noop.
		if err := state.PushAppHash(ctx, qcs[1].Next()-1, types.GenAppHash(rng), nil); err != nil {
			return fmt.Errorf("PushAppHash(qc2): %w", err)
		}
		if err := state.PushAppHash(ctx, qcs[2].Next()-2, types.GenAppHash(rng), nil); err != nil {
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
		if err := state.PushAppHash(ctx, qc.QC().Next()-1, types.GenAppHash(rng), nil); err != nil {
			return err
		}
		if _, err := registry.EpochAt(registry.EpochFirstRoad(2)); err == nil {
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
	ep := registry.MustEpoch(0)
	qc, blocks := commitQCAtRoad(ep, keys, registry.LastRoad(0))
	store := newMemoryBlockStore(t)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	state := newTestState(t, &Config{Registry: registry}, store)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		if err := state.PushQC(ctx, qc, blocks); err != nil {
			return err
		}
		n := qc.QC().Next() - 1
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
	n := qc.QC().Next() - 1

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
	gr0 := types.RoadRange{First: qc0.QC().Index(), Next: qc0.QC().Next()}
	qc1, blocks1 := commitQCAtRoad(ep1, keys, registry.EpochFirstRoad(1))

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
		return state.PushAppHash(ctx, qc1.QC().Next()-1, types.GenAppHash(rng), nil)
	}))

	divergent := types.NewAppProposal(qc0.QC().Proposal(), types.GenAppHash(rng))
	require.NoError(t, state.PushAppQC(ctx, TestAppQC(keys, divergent)))
	vote1, err := state.AppVote(ctx, qc1.QC().Index())
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
	n := qc.QC().Next() - 1

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
	gr := types.RoadRange{First: qc.QC().Index(), Next: qc.QC().Next()}

	// PushBlock for a block whose QC is already present succeeds immediately.
	require.NoError(t, state.PushBlock(ctx, types.LaneBlockPos{Index: gr.First}, blocks[0]))
	require.False(t, state.NeedBlock(types.LaneBlockPos{Index: gr.First}))
	for offset, block := range blocks[1:] {
		require.NoError(t, state.PushBlock(ctx, types.LaneBlockPos{Index: gr.First, Offset: uint64(offset + 1)}, block))
	}
	got, err := state.TryBlock(types.LaneBlockPos{Index: gr.First})
	require.NoError(t, err)
	require.Equal(t, blocks[0], got)
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
	for i, key := range keys {
		votes[i] = types.Sign(key, types.NewCommitVote(proposal.Proposal().Msg()))
	}
	qc := types.NewFullCommitQC(types.NewCommitQC(votes), nil)
	state := newTestState(t, &Config{Registry: registry}, newMemoryBlockStore(t))
	require.NoError(t, state.PushQC(t.Context(), previous, blocks))
	require.NoError(t, state.PushQC(t.Context(), qc, nil))
	want := types.NewSuperBlock(qc, nil)
	byHash, err := state.SuperBlockByHash(want.CalculateBlockHash())
	require.NoError(t, err)
	require.True(t, byHash.IsPresent())
	require.Equal(t, qc.QC().Next(), state.NextBlock())
	got, err := state.SuperBlock(t.Context(), qc.Index())
	require.NoError(t, err)
	require.Equal(t, want.Height, got.Height)
	require.Empty(t, got.Txs())
}

func TestSuperBlockByHash(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))

	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	require.NoError(t, state.PushQC(ctx, qc, blocks))
	gr := types.RoadRange{First: qc.QC().Index(), Next: qc.QC().Next()}
	n := gr.First
	wantBlock := types.NewSuperBlock(qc, blocks)
	wantHash := wantBlock.CalculateBlockHash()
	gotOpt, err := state.SuperBlockByHash(wantHash)
	require.NoError(t, err)
	gotGB, ok := gotOpt.Get()
	require.True(t, ok)
	require.Equal(t, n, gotGB.Height)
	require.Equal(t, wantBlock.Txs(), gotGB.Txs())
	require.Equal(t, wantBlock.GasWanted, gotGB.GasWanted)

	// Zero hash → None.
	zeroOpt, err := state.SuperBlockByHash(common.Hash{})
	require.NoError(t, err)
	_, ok = zeroOpt.Get()
	require.False(t, ok, "SuperBlockByHash(zero) returned Some")

	// Random unknown hash → None.
	var randHash common.Hash
	rng.Read(randHash[:])
	randOpt, err := state.SuperBlockByHash(randHash)
	require.NoError(t, err)
	_, ok = randOpt.Get()
	require.False(t, ok, "SuperBlockByHash(random) returned Some")
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
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}

	db := newTestBlockStore(t, dir)
	state := newTestState(t, &Config{Registry: registry}, db)

	// Transport-race window: PushQC before data.Run / runPersist starts.
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	require.False(t, db.Status().IsPresent(), "PushQC must not write BlockDB before Run")

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		// PushAppHash waits on persisted.NextQC, so success implies Flush.
		for n := gr1.First; n < gr1.Next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng), nil); err != nil {
				return fmt.Errorf("PushAppHash(%d): %w", n, err)
			}
		}
		return nil
	}))

	tips := db.Status().OrPanic("non-empty BlockDB status")
	require.Equal(t, gr1.Next, tips.NextQC)
	require.Equal(t, gr1.Next, tips.NextQC)

	require.NoError(t, db.Close())
	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	require.Equal(t, gr1.Next, state2.NextBlock())
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state2.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err, "block %d", n)
		require.NotNil(t, got)
	}
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
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state.PushQC(ctx, qc2, blocks2[:len(blocks2)-1]))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		for inner, ctrl := range state.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool { return inner.persisted.NextQC == qc1.QC().Next() }); err != nil {
				return err
			}
		}
		if got := store.Status().OrPanic("persisted QC"); got.NextQC != qc1.QC().Next() {
			return fmt.Errorf("incomplete QC was persisted: %v", got)
		}
		for range blocks2 {
			got, err := store.ReadSuperBlockByHash(types.NewSuperBlock(qc2, blocks2).CalculateBlockHash())
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
			return ctrl.WaitUntil(ctx, func() bool { return inner.persisted.NextQC == qc2.QC().Next() })
		}
		panic("unreachable")
	}))
	require.Equal(t, qc2.QC().Next(), store.Status().OrPanic("complete QCs").NextQC)
}

// TestEvictionWaitsForAppQC checks that setPersisted does not drop
// AppProposals until AppQC is persisted, and that once it is, heights below
// persisted.First are evicted.
func TestEvictionWaitsForAppQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	gr2 := types.RoadRange{First: qc2.QC().Index(), Next: qc2.QC().Next()}

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
				return anchor.AppQC.Proposal().Index() >= qc1.Index()
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
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}

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
			gr := types.RoadRange{First: qc.QC().Index(), Next: qc.QC().Next()}
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
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}
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
				return anchor.AppQC.Proposal().Index() >= qc1.Index()
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
			_, ok := inner.blocks[types.LaneBlockPos{Index: evictionBound - 1}]
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
			gr := types.RoadRange{First: fqc.QC().Index(), Next: fqc.QC().Next()}
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
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}

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
				return anchor.AppQC.Proposal().Index() >= qc1.Index()
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
	require.Equal(t, gr1, types.RoadRange{First: appQC.Proposal().Index(), Next: appQC.Proposal().Next()})
	require.Equal(t, gr1, types.RoadRange{First: fQC.QC().Index(), Next: fQC.QC().Next()})
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
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}

	state1 := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, pushAppHashesRunning(ctx, state1, rng, gr1.First, gr1.Next))

	// Prune past every block; BlockDB clamps to retain the newest cohort.
	require.NoError(t, state1.PruneBefore(gr1.Next))
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state1.TryBlock(types.LaneBlockPos{Index: n})
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
		got, err := state2.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
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
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}
	gr2 := types.RoadRange{First: qc2.QC().Index(), Next: qc2.QC().Next()}

	state1 := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state1.PushQC(ctx, qc2, blocks2))

	require.NoError(t, pushAppHashesRunning(ctx, state1, rng, gr1.First, gr2.Next))
	var exclusiveFloor types.RoadIndex
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run", func() error { return utils.IgnoreCancel(state1.Run(ctx)) })
		if err := pushAppQCForBlock(ctx, state1, keys, gr1.First); err != nil {
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
	require.NoError(t, state1.PruneBefore(gr2.Next))
	// Evicted heights (< exclusive App floor) fall through to BlockDB, but the
	// persisted AppQC cohort is retained by the prune cap.
	for n := gr1.First; n < exclusiveFloor; n++ {
		got, err := state1.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	// Exclusive floor and above stay cached for AppVotes despite BlockDB prune.
	// ByHash must match TryBlock here — not fall through to a pruned BlockDB.
	for n := exclusiveFloor; n < gr2.Next; n++ {
		got, err := state1.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err, "height %d must remain readable from RAM (>= exclusive floor)", n)
		require.NotNil(t, got)
		byHash, err := state1.SuperBlockByHash(utils.OrPanic1(state1.SuperBlock(ctx, n)).CalculateBlockHash())
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
	require.Equal(t, gr2.Next, state2.NextBlock())
	for n := gr2.First; n < gr2.Next; n++ {
		got, err := state2.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
}

func TestPushBlockWaitsForQC(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 3)
		state := newTestState(t, &Config{Registry: registry}, newMemoryBlockStore(t))
		qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
		require.NoError(t, state.PushQC(ctx, qc1, blocks1))
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
			if err := state.PushQC(ctx, qc2, blocks2[1:]); err != nil {
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

// TestTryBlockHidesGapFills verifies the no-gap contract: a block stored above
// nextBlock (gap-fill) is not visible via TryBlock until the contiguous prefix
// catches up. SuperBlockByHash still serves it from RAM (hash index) even
// though it is not yet durable in BlockDB.
func TestTryBlockHidesGapFills(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	state := newTestState(t, &Config{Registry: registry}, newMemoryBlockStore(t))
	require.NoError(t, state.PushQC(ctx, qc, nil))
	last := types.LaneBlockPos{Index: qc.Index(), Offset: uint64(len(blocks) - 1)}
	require.NoError(t, state.PushBlock(ctx, last, blocks[len(blocks)-1]))
	_, err := state.TryBlock(last)
	require.ErrorIs(t, err, types.ErrNotFound)
	require.False(t, state.NeedBlock(last))
	require.True(t, state.NeedBlock(types.LaneBlockPos{Index: qc.Index()}))
	want := types.NewSuperBlock(qc, blocks)
	opt, err := state.SuperBlockByHash(want.CalculateBlockHash())
	require.NoError(t, err)
	require.False(t, opt.IsPresent(), "incomplete SuperBlock must not be indexed")
	for offset, block := range blocks {
		require.NoError(t, state.PushBlock(ctx, types.LaneBlockPos{Index: qc.Index(), Offset: uint64(offset)}, block))
	}
	require.Equal(t, qc.QC().Next(), state.NextBlock())
	got, err := state.SuperBlock(ctx, qc.Index())
	require.NoError(t, err)
	require.Equal(t, want.Txs(), got.Txs())
	opt, err = state.SuperBlockByHash(want.CalculateBlockHash())
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

func TestPushQC_RecordsNextBlockQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	store := newTestBlockStore(t, t.TempDir())
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	writeToBlockStore(t, store, []*types.FullCommitQC{qc1}, [][]*types.Block{blocks1})

	state := newTestState(t, &Config{Registry: registry}, store)
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}
	require.Equal(t, int64(gr1.Next), gathered(t, "tendermint_internal_autobahn_data_next_block", map[string]string{"stage": "qc"}))

	qc2, _ := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	require.NoError(t, state.PushQC(ctx, qc2, nil))
	gr2 := types.RoadRange{First: qc2.QC().Index(), Next: qc2.QC().Next()}
	require.Equal(t, int64(gr2.Next), gathered(t, "tendermint_internal_autobahn_data_next_block", map[string]string{"stage": "qc"}))

	require.NoError(t, state.PushQC(ctx, qc2, nil))
	require.Equal(t, int64(gr2.Next), gathered(t, "tendermint_internal_autobahn_data_next_block", map[string]string{"stage": "qc"}))
}

func TestLaneBlockInvalidOffset(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	store := newMemoryBlockStore(t)
	state := newTestState(t, &Config{Registry: registry}, store)
	require.NoError(t, state.PushQC(t.Context(), qc, blocks))
	pos := types.LaneBlockPos{Index: qc.Index(), Offset: uint64(len(blocks))}
	_, err := state.TryBlock(pos)
	require.ErrorIs(t, err, types.ErrNotFound)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	recovered := newTestState(t, &Config{Registry: registry}, store)
	_, err = recovered.Block(t.Context(), pos)
	require.ErrorIs(t, err, types.ErrNotFound)
}
