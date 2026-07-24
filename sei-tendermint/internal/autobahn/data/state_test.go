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
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
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
		for n, ap := range inner.appProposals {
			aps[n] = ap
		}
		return Snapshot{
			QCs:          maps.Clone(inner.qcs),
			Blocks:       maps.Clone(inner.blocks),
			AppProposals: aps,
		}
	}
	panic("unreachable")
}

// newTestBlockDB opens (or creates) a LittDB-backed BlockDB at dir.
// Retention is set to 1ns so ForceGC reclaims pruned data immediately in tests.
// Errors panic so the helper is safe to call from non-main test goroutines.
func newTestBlockDB(t *testing.T, dir string) types.BlockDB {
	t.Helper()
	cfg := utils.OrPanic1(littblock.DefaultConfig(dir))
	cfg.Retention = time.Nanosecond
	db := utils.OrPanic1(littblock.NewBlockDB(cfg))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newTestState constructs a State, replays db, and returns it ready to Run.
// Errors panic so the helper is safe to call from non-main test goroutines.
func newTestState(t testing.TB, cfg *Config, db types.BlockDB) *State {
	t.Helper()
	return utils.OrPanic1(NewState(cfg, db))
}

// writeToBlockDB writes QC+block pairs sequentially to db and flushes once.
// qcs[i] and blockss[i] must correspond; QCs must be in ascending order.
// Errors panic so the helper is safe to call from non-main test goroutines.
func writeToBlockDB(t *testing.T, db types.BlockDB, qcs []*types.FullCommitQC, blockss [][]*types.Block) {
	t.Helper()
	for i, qc := range qcs {
		gr := qc.QC().GlobalRange()
		utils.OrPanic(db.WriteQC(gr.First, gr.Next, qc))
		for j, n := 0, gr.First; n < gr.Next; n++ {
			utils.OrPanic(db.WriteBlock(n, blockss[i][j]))
			j++
		}
	}
	utils.OrPanic(db.Flush())
}

// pushAppHashesRunning runs state.Run under scope.Run long enough to accept
// PushAppHash for [first, next), then cancels Run. Prefers scope.Run over a
// raw goroutine so cleanup is structured.
func pushAppHashesRunning(ctx context.Context, state *State, rng utils.Rng, first, next types.GlobalBlockNumber) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		runCtx, cancel := context.WithCancel(ctx)
		s.SpawnBgNamed("state.Run", func() error {
			return utils.IgnoreCancel(state.Run(runCtx))
		})
		for n := first; n < next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng)); err != nil {
				cancel()
				return err
			}
		}
		cancel()
		return nil
	})
}

func TestState(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 3)
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))
		s.SpawnBgNamed("state.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		want := newSnapshot()
		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := TestCommitQC(rng, registry.LatestEpoch(), keys, prev)
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
	registry, keys, _ := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))

	// Push a valid QC to advance inner.nextQC.
	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
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
	viewSpec := types.ViewSpec{CommitQC: utils.None[*types.CommitQC](), Epochs: types.NewEpochDuo(registry.LatestEpoch(), utils.None[*types.Epoch]())}
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
	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
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
	registry, keys, _ := epoch.GenRegistry(rng, 3)
	state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))

	// Push qc1 with NO blocks — only the QC is stored.
	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
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
	registry, keys, _ := epoch.GenRegistry(rng, 3)
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))
		s.SpawnBgNamed("state.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := TestCommitQC(rng, registry.LatestEpoch(), keys, prev)
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
	registry, keys, _ := epoch.GenRegistry(rng, 3)

	state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))

	// Push QC without blocks.
	qc, blocks := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
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
	registry, keys, _ := epoch.GenRegistry(rng, 3)

	state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))

	qc, blocks := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
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

// TestPushQCBeforeRunPersistsToBlockDB seeds in-memory QCs/blocks before Run
// (mirroring inbound PushQC after transport start) and asserts runPersist still
// writes them — Status seeding, not inner.nextQC/nextBlock.
func TestPushQCBeforeRunPersistsToBlockDB(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()

	db := newTestBlockDB(t, dir)
	state := newTestState(t, &Config{Registry: registry}, db)

	// Transport-race window: PushQC before data.Run / runPersist starts.
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	tips := db.Status()
	require.Zero(t, tips.NextBlock, "PushQC must not write BlockDB before Run")
	require.Zero(t, tips.NextQC)

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		runCtx, cancel := context.WithCancel(ctx)
		s.SpawnBgNamed("state.Run", func() error {
			return utils.IgnoreCancel(state.Run(runCtx))
		})
		// PushAppHash waits on nextBlockToPersist, so success implies Flush.
		for n := gr1.First; n < gr1.Next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng)); err != nil {
				cancel()
				return fmt.Errorf("PushAppHash(%d): %w", n, err)
			}
		}
		cancel()
		return nil
	}))

	tips = db.Status()
	require.Equal(t, gr1.Next, tips.NextBlock)
	require.Equal(t, gr1.Next, tips.NextQC)

	require.NoError(t, db.Close())
	db2 := newTestBlockDB(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	require.Equal(t, gr1.Next, state2.NextBlock())
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err, "block %d", n)
		require.NotNil(t, got)
	}
}

// TestEvictionWaitsForCommitQCApp checks that evictBelowBound does not drop
// AppProposals until a later CommitQC embeds an App (certifying AppQC), and
// that once that App exists, heights below min(NAP, App+1) are evicted.
func TestEvictionWaitsForCommitQCApp(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()
	require.False(t, qc1.QC().Proposal().App().IsPresent(), "genesis CommitQC has no App")

	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
	app, ok := qc2.QC().Proposal().App().Get()
	require.True(t, ok, "second CommitQC embeds App for qc1 tip")
	appFloor := app.GlobalNumber()
	require.Equal(t, gr1.Next-1, appFloor)
	gr2 := qc2.QC().GlobalRange()

	state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		s.SpawnBgNamed("state.Run", func() error {
			return utils.IgnoreCancel(state.Run(runCtx))
		})

		require.NoError(t, state.PushQC(ctx, qc1, blocks1))
		for n := gr1.First; n < gr1.Next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng)); err != nil {
				return err
			}
		}

		// No CommitQC.App yet → eviction must not strip AppProposals; first stays put.
		for inner := range state.inner.Lock() {
			require.Equal(t, gr1.First, inner.first, "no certified App → first unchanged")
			for n := gr1.First; n < gr1.Next; n++ {
				_, ok := inner.appProposals[n]
				require.True(t, ok, "AppProposal %d must survive without CommitQC.App", n)
			}
		}

		require.NoError(t, state.PushQC(ctx, qc2, blocks2))
		for n := gr2.First; n < gr2.Next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng)); err != nil {
				return err
			}
		}

		for inner := range state.inner.Lock() {
			require.Equal(t, appFloor+1, inner.first, "after catching up, first reaches App+1")
			for n := gr1.First; n < inner.first; n++ {
				_, ok := inner.appProposals[n]
				require.False(t, ok, "AppProposal %d should be evicted (< first)", n)
			}
			// Heights at/above exclusive floor stay until executed further.
			for n := inner.first; n < inner.nextAppProposal; n++ {
				_, ok := inner.appProposals[n]
				require.True(t, ok, "AppProposal %d must remain (>= first)", n)
			}
			// Tip QC (nextQC-1) stays; nextToExecute uses maps at/above first.
			require.GreaterOrEqual(t, inner.nextQC-1, inner.first)
			_, ok = inner.qcs[inner.nextQC-1]
			require.True(t, ok, "tip QC must stay in maps")
		}
		return nil
	}))
}

// TestNextToExecuteAfterAppEviction checks WaitUntilExecuted / nextToExecute
// still work when PushQC embeds an App that aggressively evicts through
// nextAppProposal (first = App+1 = NAP). nextToExecute uses qc[NAP], not NAP-1.
func TestNextToExecuteAfterAppEviction(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()
	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
	app, ok := qc2.QC().Proposal().App().Get()
	require.True(t, ok)
	require.Equal(t, gr1.Next-1, app.GlobalNumber())

	state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		s.SpawnBgNamed("state.Run", func() error {
			return utils.IgnoreCancel(state.Run(runCtx))
		})

		require.NoError(t, state.PushQC(ctx, qc1, blocks1))
		for n := gr1.First; n < gr1.Next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng)); err != nil {
				return err
			}
		}
		// Sticky case: App floor == nextAppProposal. first advances to NAP;
		// NAP-1 is gone; nextToExecute reads qc[NAP].
		require.NoError(t, state.PushQC(ctx, qc2, blocks2))

		var tipLane types.LaneID
		var tipBlockNum types.BlockNumber
		for inner := range state.inner.Lock() {
			require.Equal(t, gr1.Next, inner.nextAppProposal)
			require.Equal(t, app.GlobalNumber()+1, inner.first,
				"eviction advances to App+1 == NAP")
			_, ok := inner.blocks[inner.nextAppProposal-1]
			require.False(t, ok, "NAP-1 must be evicted")
			require.Less(t, inner.nextAppProposal, inner.nextQC)
			fqc := inner.qcs[inner.nextAppProposal]
			require.NotNil(t, fqc)
			gr := fqc.QC().GlobalRange()
			h := fqc.Headers()[inner.nextAppProposal-gr.First]
			tipLane = h.Lane()
			tipBlockNum = h.BlockNumber()
			require.Equal(t, tipBlockNum, inner.nextToExecute(tipLane),
				"nextToExecute should be the next block's lane number")
		}
		// WaitUntilExecuted(n) returns when nextToExecute > n.
		waitFrom := tipBlockNum
		if waitFrom > 0 {
			waitFrom--
		}
		next, err := state.WaitUntilExecuted(ctx, tipLane, waitFrom)
		require.NoError(t, err)
		require.Equal(t, tipBlockNum, next)
		return nil
	}))
}

// TestPruningKeepsLastQCRange verifies BlockDB's never-empty prune: asking to
// prune past the tip still leaves the newest cohort readable. An incomplete
// BlockDB (QC without a full block prefix) fails NewState; a consistent range
// recovers from the QC start.
func TestPruningKeepsLastQCRange(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()

	state1 := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, pushAppHashesRunning(ctx, state1, rng, gr1.First, gr1.Next))

	// Prune past every block; BlockDB clamps to retain the newest cohort.
	require.NoError(t, state1.PruneBefore(gr1.Next))
	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state1.TryBlock(n)
		require.NoError(t, err, "never-empty prune should keep cohort block %d", n)
		require.NotNil(t, got)
	}

	// Incomplete store (QC covers a range but only one block is present)
	// must fail NewState — we do not normalize partial QC prefixes.
	survivor := gr1.Next - 1
	dirBad := t.TempDir()
	dbBad := newTestBlockDB(t, dirBad)
	require.NoError(t, dbBad.WriteQC(gr1.First, gr1.Next, qc1))
	require.NoError(t, dbBad.WriteBlock(survivor, blocks1[survivor-gr1.First]))
	require.NoError(t, dbBad.Flush())
	require.NoError(t, dbBad.Close())
	_, err := NewState(&Config{Registry: registry}, newTestBlockDB(t, dirBad))
	require.Error(t, err)

	// Consistent post-GC shape: full QC range of blocks. Restart recovers at QC start.
	dir := t.TempDir()
	db := newTestBlockDB(t, dir)
	writeToBlockDB(t, db, []*types.FullCommitQC{qc1}, [][]*types.Block{blocks1})
	require.NoError(t, db.Close())

	db2 := newTestBlockDB(t, dir)
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
// (at/above CommitQC.App+1 exclusive floor) remain readable via TryBlock even
// after the store watermark advances past them.
func TestPruningWithPartialQCRange(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()
	app, ok := qc2.QC().Proposal().App().Get()
	require.True(t, ok)
	appFloor := app.GlobalNumber()
	exclusiveFloor := appFloor + 1

	state1 := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state1.PushQC(ctx, qc2, blocks2))

	require.NoError(t, pushAppHashesRunning(ctx, state1, rng, gr1.First, gr2.Next))

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

	// Prune past qc1 entirely; BlockDB never-empty keeps the newest cohort (qc2).
	require.NoError(t, state1.PruneBefore(gr2.Next))
	// Evicted heights (< exclusive App floor) fall through to BlockDB → ErrPruned.
	for n := gr1.First; n < exclusiveFloor; n++ {
		_, err := state1.TryBlock(n)
		require.ErrorIs(t, err, types.ErrPruned)
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

	// Incomplete qc2 suffix alone must error.
	survivor := gr2.Next - 1
	dirBad := t.TempDir()
	dbBad := newTestBlockDB(t, dirBad)
	require.NoError(t, dbBad.WriteQC(gr2.First, gr2.Next, qc2))
	require.NoError(t, dbBad.WriteBlock(survivor, blocks2[survivor-gr2.First]))
	require.NoError(t, dbBad.Flush())
	require.NoError(t, dbBad.Close())
	_, err := NewState(&Config{Registry: registry}, newTestBlockDB(t, dirBad))
	require.Error(t, err)

	// Consistent retained range: full qc2.
	dir := t.TempDir()
	db := newTestBlockDB(t, dir)
	writeToBlockDB(t, db, []*types.FullCommitQC{qc2}, [][]*types.Block{blocks2})
	require.NoError(t, db.Close())

	db2 := newTestBlockDB(t, dir)
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
		registry, keys, _ := epoch.GenRegistry(rng, 3)

		state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))

		// Push first QC covering [0, N).
		qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
		require.NoError(t, state.PushQC(ctx, qc1, blocks1))

		// Prepare second QC covering [N, M) but don't push it yet.
		qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
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
	registry, keys, _ := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()
	require.GreaterOrEqual(t, gr2.Len(), 2)

	state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state.PushQC(ctx, qc2, nil))
	require.Equal(t, gr1.Next, state.NextBlock())

	// Gap-fill the last height of qc2 before earlier qc2 blocks.
	last := gr2.Next - 1
	gapBlock := blocks2[last-gr2.First]
	require.NoError(t, state.PushBlock(ctx, last, gapBlock))
	_, err := state.TryBlock(last)
	require.ErrorIs(t, err, types.ErrNotFound, "gap-fill above nextBlock must stay hidden")

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
