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

// newTestBlockDB opens (or creates) a LittDB-backed BlockDB at dir.
// Retention is set to 1ns so ForceGC reclaims pruned data immediately in tests.
// Errors panic so the helper is safe to call from non-main test goroutines.
func newTestBlockDB(t *testing.T, dir string) types.BlockDB {
	t.Helper()
	cfg, err := littblock.DefaultConfig(dir)
	if err != nil {
		panic(fmt.Sprintf("littblock.DefaultConfig: %v", err))
	}
	cfg.Retention = time.Nanosecond
	db, err := littblock.NewBlockDB(cfg)
	if err != nil {
		panic(fmt.Sprintf("littblock.NewBlockDB: %v", err))
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newTestState constructs a State, replays db, and returns it ready to Run.
// Errors panic so the helper is safe to call from non-main test goroutines.
func newTestState(t testing.TB, cfg *Config, db types.BlockDB) *State {
	t.Helper()
	s, err := NewState(cfg, db)
	if err != nil {
		panic(fmt.Sprintf("NewState: %v", err))
	}
	return s
}

// writeToBlockDB writes QC+block pairs sequentially to db and flushes once.
// qcs[i] and blockss[i] must correspond; QCs must be in ascending order.
// Errors panic so the helper is safe to call from non-main test goroutines.
func writeToBlockDB(t *testing.T, db types.BlockDB, qcs []*types.FullCommitQC, blockss [][]*types.Block) {
	t.Helper()
	for i, qc := range qcs {
		gr := qc.QC().GlobalRange()
		if err := db.WriteQC(gr.First, gr.Next, qc); err != nil {
			panic(fmt.Sprintf("WriteQC: %v", err))
		}
		for j, n := 0, gr.First; n < gr.Next; n++ {
			if err := db.WriteBlock(n, blockss[i][j]); err != nil {
				panic(fmt.Sprintf("WriteBlock: %v", err))
			}
			j++
		}
	}
	if err := db.Flush(); err != nil {
		panic(fmt.Sprintf("Flush: %v", err))
	}
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
	registry, keys := epoch.GenRegistry(rng, 3)
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
	registry, keys := epoch.GenRegistry(rng, 3)
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
	registry, keys := epoch.GenRegistry(rng, 3)
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
	registry, keys := epoch.GenRegistry(rng, 3)

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
	registry, keys := epoch.GenRegistry(rng, 3)

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

// TestEvictedReadsFallBackToBlockDB verifies that after runPersist evicts
// executed QCs/blocks from memory, GlobalBlock/Block/QC/TryBlock/ByHash still
// succeed by reading from BlockDB.
func TestEvictedReadsFallBackToBlockDB(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange()
	require.GreaterOrEqual(t, gr1.Len(), 2, "need ≥2 blocks so at least one can be fully evicted")

	evicted := gr1.First // will be deleted from memory; gr1.Next-1 is kept as sentinel
	wantBlock := blocks1[0]
	wantHash := wantBlock.Header().Hash()
	wantQC := qc1

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))
		s.SpawnBgNamed("state.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		if err := state.PushQC(ctx, qc1, blocks1); err != nil {
			return err
		}
		// Execute qc1 so those heights become eligible for eviction.
		for n := gr1.First; n < gr1.Next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng)); err != nil {
				return fmt.Errorf("PushAppHash(%d): %w", n, err)
			}
		}
		// Pushing qc2 wakes runPersist, which flushes and evicts heights
		// below nextAppProposal-1 (i.e. all of qc1 except the last).
		if err := state.PushQC(ctx, qc2, blocks2); err != nil {
			return err
		}

		// Wait until the target height is gone from the in-memory maps.
		deadline := time.Now().Add(5 * time.Second)
		for {
			gone := false
			for inner := range state.inner.Lock() {
				_, hasQC := inner.qcs[evicted]
				_, hasBlk := inner.blocks[evicted]
				gone = !hasQC && !hasBlk
			}
			if gone {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timed out waiting for height %d to be evicted from memory", evicted)
			}
			time.Sleep(10 * time.Millisecond)
		}

		gotBlk, err := state.Block(ctx, evicted)
		if err != nil {
			return fmt.Errorf("Block(%d) after eviction: %w", evicted, err)
		}
		if err := utils.TestDiff(wantBlock, gotBlk); err != nil {
			return fmt.Errorf("Block(%d) after eviction: %w", evicted, err)
		}

		gotTry, err := state.TryBlock(evicted)
		if err != nil {
			return fmt.Errorf("TryBlock(%d) after eviction: %w", evicted, err)
		}
		if err := utils.TestDiff(wantBlock, gotTry); err != nil {
			return fmt.Errorf("TryBlock(%d) after eviction: %w", evicted, err)
		}

		gotQC, err := state.QC(ctx, evicted)
		if err != nil {
			return fmt.Errorf("QC(%d) after eviction: %w", evicted, err)
		}
		if err := utils.TestDiff(wantQC, gotQC); err != nil {
			return fmt.Errorf("QC(%d) after eviction: %w", evicted, err)
		}

		wantG := &types.GlobalBlock{
			GlobalNumber:  evicted,
			Timestamp:     wantQC.QC().Proposal().BlockTimestamp(evicted).OrPanic("global block not in QC"),
			Header:        wantBlock.Header(),
			Payload:       wantBlock.Payload(),
			FinalAppState: wantQC.QC().Proposal().App(),
		}
		gotG, err := state.GlobalBlock(ctx, evicted)
		if err != nil {
			return fmt.Errorf("GlobalBlock(%d) after eviction: %w", evicted, err)
		}
		if err := utils.TestDiff(wantG, gotG); err != nil {
			return fmt.Errorf("GlobalBlock(%d) after eviction: %w", evicted, err)
		}

		gotByHash, err := state.GlobalBlockByHash(wantHash)
		if err != nil {
			return fmt.Errorf("GlobalBlockByHash after eviction: %w", err)
		}
		gb, ok := gotByHash.Get()
		if !ok {
			return fmt.Errorf("GlobalBlockByHash after eviction returned None")
		}
		if err := utils.TestDiff(wantG, gb); err != nil {
			return fmt.Errorf("GlobalBlockByHash after eviction: %w", err)
		}

		// Eviction leaves first behind; PruneBefore must not panic when the
		// in-memory block at first is already gone (metrics are best-effort).
		if err := state.PruneBefore(gr1.Next); err != nil {
			return fmt.Errorf("PruneBefore after eviction: %w", err)
		}
		for inner := range state.inner.Lock() {
			if inner.first != gr1.Next-1 {
				return fmt.Errorf("after PruneBefore, first=%d want %d", inner.first, gr1.Next-1)
			}
		}
		if _, err := state.TryBlock(evicted); !errors.Is(err, ErrPruned) {
			return fmt.Errorf("TryBlock(%d) after PruneBefore: got %v, want ErrPruned", evicted, err)
		}
		return nil
	}))
}

// TestPushQCBeforeRunPersistsToBlockDB seeds in-memory QCs/blocks before Run
// (mirroring inbound PushQC after transport start) and asserts runPersist still
// writes them — WriteHighWaterMarks seeding, not inner.nextQC/nextBlock.
func TestPushQCBeforeRunPersistsToBlockDB(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()

	db := newTestBlockDB(t, dir)
	state := newTestState(t, &Config{Registry: registry}, db)

	// Transport-race window: PushQC before data.Run / runPersist starts.
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	tips := db.WriteHighWaterMarks()
	require.False(t, tips.LastBlockNumber.IsPresent(), "PushQC must not write BlockDB before Run")
	require.False(t, tips.LastQCNext.IsPresent())

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

	tips = db.WriteHighWaterMarks()
	gotBlock, ok := tips.LastBlockNumber.Get()
	require.True(t, ok)
	require.Equal(t, gr1.Next-1, gotBlock)
	gotQC, ok := tips.LastQCNext.Get()
	require.True(t, ok)
	require.Equal(t, gr1.Next, gotQC)

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

// TestPruningKeepsLastQCRange verifies that pruning never removes the last
// retained block, and that a restart from a BlockDB with only that block
// recovers correctly.
func TestPruningKeepsLastQCRange(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()

	// In-memory: push QC, execute all blocks, then prune everything.
	state1 := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))

	require.NoError(t, pushAppHashesRunning(ctx, state1, rng, gr1.First, gr1.Next))

	require.NoError(t, state1.PruneBefore(gr1.Next))
	var survivingBlock types.GlobalBlockNumber
	for inner := range state1.inner.Lock() {
		survivingBlock = inner.first
		require.Less(t, survivingBlock, gr1.Next, "pruning should keep at least one block")
	}
	for n := gr1.First; n < survivingBlock; n++ {
		_, err := state1.TryBlock(n)
		require.ErrorIs(t, err, ErrPruned)
	}

	// Restart: build a fresh BlockDB containing only the surviving block (as GC
	// would leave it) and verify NewState correctly sets first = survivingBlock.
	dir := t.TempDir()
	db := newTestBlockDB(t, dir)
	require.NoError(t, db.WriteQC(gr1.First, gr1.Next, qc1))
	require.NoError(t, db.WriteBlock(survivingBlock, blocks1[survivingBlock-gr1.First]))
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	db2 := newTestBlockDB(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	for inner := range state2.inner.Lock() {
		require.Equal(t, survivingBlock, inner.first,
			"after restart, first should match the surviving block")
	}
}

// TestPruningWithPartialQCRange verifies per-block pruning across QC ranges:
// pruning advances first in-memory, and a restart from a post-GC BlockDB
// (with only the last surviving block) recovers correctly.
func TestPruningWithPartialQCRange(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()

	// In-memory: push 2 QCs, execute, then prune.
	state1 := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))
	require.NoError(t, state1.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state1.PushQC(ctx, qc2, blocks2))

	require.NoError(t, pushAppHashesRunning(ctx, state1, rng, gr1.First, gr2.Next))

	// Per-block prune into middle of qc1's range.
	midQC1 := gr1.First + (gr1.Next-gr1.First)/2
	if midQC1 > gr1.First+1 {
		require.NoError(t, state1.PruneBefore(midQC1))
		for inner := range state1.inner.Lock() {
			require.Greater(t, inner.first, gr1.First,
				"per-block pruning should advance past gr1.First")
		}
	}

	// Prune past qc1 entirely.
	require.NoError(t, state1.PruneBefore(gr2.Next))
	var survivingBlock types.GlobalBlockNumber
	for inner := range state1.inner.Lock() {
		survivingBlock = inner.first
		require.GreaterOrEqual(t, survivingBlock, gr2.First)
		require.Less(t, survivingBlock, gr2.Next, "at least one block should survive")
	}
	for n := gr1.First; n < survivingBlock; n++ {
		_, err := state1.TryBlock(n)
		require.ErrorIs(t, err, ErrPruned)
	}

	// Restart: build a fresh BlockDB containing only the surviving block from qc2
	// (qc1 is pruned entirely) and verify NewState recovers correctly.
	dir := t.TempDir()
	db := newTestBlockDB(t, dir)
	require.NoError(t, db.WriteQC(gr2.First, gr2.Next, qc2))
	require.NoError(t, db.WriteBlock(survivingBlock, blocks2[survivingBlock-gr2.First]))
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	db2 := newTestBlockDB(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)
	for inner := range state2.inner.Lock() {
		require.Equal(t, survivingBlock, inner.first,
			"after restart, first should match the surviving block")
	}
}

// TestRunPruningEmptyState verifies that runPruning does not panic when
// the state has no QCs (e.g. on first startup before any data arrives).
func TestRunPruningEmptyState(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 3)

	state := newTestState(t, &Config{
		Registry:   registry,
		PruneAfter: utils.Some(time.Duration(0)),
	}, newTestBlockDB(t, t.TempDir()))

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

		state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))

		// Push first QC covering [0, N).
		qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
		require.NoError(t, state.PushQC(ctx, qc1, blocks1))

		// Prepare second QC covering [N, M) but don't push it yet.
		qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
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
