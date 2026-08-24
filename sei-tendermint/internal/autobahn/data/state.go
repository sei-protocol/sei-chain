package data

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const blocksCacheSize = 4000

// ErrOutOfOrder is returned when PushAppHash receives an app hash past the
// next CommitQC range waiting for execution.
var ErrOutOfOrder = errors.New("out of order")

// Config is the config for the data State.
type Config struct {
	// Registry is the authoritative source of committee and stake information.
	Registry *epoch.Registry
}

// blockEntry is a (number, block) pair collected in runPersist batches.
type blockEntry struct {
	n     types.GlobalBlockNumber
	block *types.Block
}

type inner struct {
	qcs          map[types.GlobalBlockNumber]*types.FullCommitQC   // [first, nextQC)
	blocks       map[types.GlobalBlockNumber]*types.Block          // [first, nextBlock) + gap-fills in [nextBlock, nextQC)
	appProposals map[types.GlobalBlockNumber]*types.AppProposal    // [first, nextAppProposal)
	appQCs       map[types.GlobalBlockNumber]*types.AppQC          // [first, nextAppQC)
	blockHashes  map[types.BlockHeaderHash]types.GlobalBlockNumber // blockHashes mirrors blocks (insertBlock / setPersisted)

	// first is the exclusive low end of retained in-memory state: maps keep [first, next*).
	// Advanced by runPersist(). Durable copies below first live in BlockStore.
	//
	// first <= nextAppQC <= nextAppProposal <= nextBlock <= nextQC
	first           types.GlobalBlockNumber
	nextAppQC       types.GlobalBlockNumber
	nextAppProposal types.GlobalBlockNumber
	nextBlock       types.GlobalBlockNumber
	nextQC          types.GlobalBlockNumber
	persisted       types.SuffixRange

	// Anchor represents the highest fully processed row:
	// CommitQC, Blocks, AppProposal, AppQC present and persisted.
	anchor utils.AtomicSend[utils.Option[Anchor]]
}

// insertQC verifies and inserts a FullCommitQC into the inner state.
// Accepts QCs whose range starts at or before nextQC (partially pruned
// prefix is silently skipped). Rejects gaps where gr.First > nextQC.
func (i *inner) insertQC(registry *epoch.Registry, qc *types.FullCommitQC) error {
	gr := qc.QC().GlobalRange()
	if gr.Next <= i.nextQC {
		return nil // fully behind, skip
	}
	if gr.First > i.nextQC {
		return fmt.Errorf("QC gap: expected first<=%d, got %d", i.nextQC, gr.First)
	}
	e, ok := registry.EpochByIndex(qc.QC().Proposal().EpochIndex())
	if !ok {
		return fmt.Errorf("unknown epoch_index %d", qc.QC().Proposal().EpochIndex())
	}
	if err := qc.Verify(e); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	for i.nextQC < gr.Next {
		i.qcs[i.nextQC] = qc
		i.nextQC++
	}
	return nil
}

func (i *inner) insertAppQC(registry *epoch.Registry, appQC *types.AppQC) error {
	gr := appQC.Proposal().GlobalRange()
	if gr.Next <= i.nextAppQC {
		return nil
	}
	if gr.Next > i.nextAppProposal {
		return fmt.Errorf("missing AppProposal for this AppQC")
	}
	if gr.First > i.nextAppQC {
		return fmt.Errorf("AppQC gap: expected first<=%d, got %d", i.nextAppQC, gr.First)
	}
	ei := appQC.Proposal().EpochIndex()
	epoch, ok := registry.EpochByIndex(ei)
	if !ok {
		return fmt.Errorf("unknown epoch_index %d", ei)
	}
	if err := appQC.Verify(epoch.Committee()); err != nil {
		return fmt.Errorf("appQC.Verify(): %w", err)
	}
	for i.nextAppQC < gr.Next {
		i.appQCs[i.nextAppQC] = appQC
		i.nextAppQC++
	}
	return nil
}

func (i *inner) insertAppProposal(appProposal *types.AppProposal) error {
	gr := appProposal.GlobalRange()
	if gr.Next <= i.nextAppProposal {
		return nil
	}
	if gr.Next > i.nextQC {
		return fmt.Errorf("missing CommitQC for this AppProposal")
	}
	if gr.First > i.nextAppProposal {
		return fmt.Errorf("AppProposal gap: expected first<=%d, got %d", i.nextAppProposal, gr.First)
	}
	if err := appProposal.Verify(i.qcs[i.nextAppProposal].QC()); err != nil {
		return fmt.Errorf("appProposal.Verify(): %w", err)
	}
	for i.nextAppProposal < gr.Next {
		i.appProposals[i.nextAppProposal] = appProposal
		i.nextAppProposal++
	}
	return nil
}

// insertBlock inserts a pre-verified block into the inner state.
// Requires a QC to already be present for block n. Callers must verify
// the block signature before calling (unlike insertQC, which verifies).
//
// insertBlock does NOT advance nextBlock — callers should call
// updateNextBlock after inserting one or more blocks. This separation
// allows batch insertion (e.g. PushQC inserts multiple blocks, then
// advances nextBlock once).
func (i *inner) insertBlock(n types.GlobalBlockNumber, block *types.Block) error {
	// Contiguous prefix is done or evicted; only [nextBlock, nextQC) inserts.
	// After success, blocks/blockHashes gain n; qcs[n] is already set.
	if n < i.nextBlock || n >= i.nextQC {
		return nil
	}
	if _, ok := i.blocks[n]; ok {
		return nil // already have it (gap fill)
	}
	// n is in [nextBlock, nextQC); QCs are contiguous and first <=
	// nextAppProposal <= nextBlock, so qcs[n] is always present.
	qc := i.qcs[n]
	storedGR := qc.QC().GlobalRange()
	want := qc.Headers()[n-storedGR.First].Hash()
	got := block.Header().Hash()
	if want != got {
		return fmt.Errorf("block %d header hash mismatch: want %v, got %v", n, want, got)
	}
	i.blocks[n] = block
	i.blockHashes[got] = n
	return nil
}

func (i *inner) updateNextBlock(m *metrics.Metrics) {
	t := time.Now()
	oldNextBlock := i.nextBlock
	for {
		b, ok := i.blocks[i.nextBlock]
		if !ok {
			break
		}
		i.nextBlock += 1
		latency := t.Sub(b.Payload().CreatedAt()).Seconds()
		m.BlockLatency.Receive.Observe(latency)
		m.TxLatency.Receive.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
	}
	if oldNextBlock < i.nextBlock {
		m.NextBlock.Receive.Set(utils.Clamp[int64](i.nextBlock))
	}
}

// State of the chain.
// Contains blocks in global order and proofs of sequencing: (CommitQC) and execution result (AppQC).
type State struct {
	cfg        *Config
	metrics    *metrics.Metrics
	inner      utils.Watch[*inner]
	blockStore types.BlockStore
}

// NewState constructs a data State, replaying persisted state from blockStore.
// The caller owns blockStore and must close it after State.Run returns (nodeImpl
// owns this in production); State never closes it.
func NewState(cfg *Config, blockStore types.BlockStore) (*State, error) {
	inner, err := loadFromBlockStore(cfg, blockStore)
	if err != nil {
		return nil, fmt.Errorf("loadFromBlockStore: %w", err)
	}
	m := metrics.Get()
	m.NextBlock.Receive.Set(utils.Clamp[int64](inner.nextBlock))
	m.NextBlock.Execute.Set(utils.Clamp[int64](inner.nextAppProposal))
	m.NextBlock.Certify.Set(utils.Clamp[int64](inner.nextAppQC))
	m.NextBlock.Evict.Set(utils.Clamp[int64](inner.first))
	return &State{
		cfg:        cfg,
		metrics:    m,
		inner:      utils.NewWatch(inner),
		blockStore: blockStore,
	}, nil
}

// loadFromBlockStore replays the persisted suffix from blockStore into s.inner.
// Called from NewState before any goroutines are spawned.
func loadFromBlockStore(cfg *Config, blockStore types.BlockStore) (*inner, error) {
	suffix, err := blockStore.ReadSuffix()
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadSuffix(): %w", err)
	}
	firstBlock := cfg.Registry.FirstBlock()
	status := suffix.Status.Or(types.SuffixRange{
		First:           firstBlock,
		NextQC:          firstBlock,
		NextAppProposal: firstBlock,
		NextAppQC:       firstBlock,
		NextBlock:       firstBlock,
	})
	inner := &inner{
		qcs:             map[types.GlobalBlockNumber]*types.FullCommitQC{},
		blocks:          map[types.GlobalBlockNumber]*types.Block{},
		appQCs:          map[types.GlobalBlockNumber]*types.AppQC{},
		appProposals:    map[types.GlobalBlockNumber]*types.AppProposal{},
		blockHashes:     map[types.BlockHeaderHash]types.GlobalBlockNumber{},
		first:           status.First,
		nextAppProposal: status.First,
		nextAppQC:       status.First,
		nextBlock:       status.First,
		nextQC:          status.First,
		persisted:       status,
		anchor:          utils.NewAtomicSend(utils.None[Anchor]()),
	}
	for _, qc := range suffix.CommitQCs {
		if err := inner.insertQC(cfg.Registry, qc); err != nil {
			return nil, fmt.Errorf("load QC from BlockStore: %w", err)
		}
	}
	for _, b := range suffix.Blocks {
		qc := inner.qcs[b.Number]
		ei := qc.QC().Proposal().EpochIndex()
		e, ok := cfg.Registry.EpochByIndex(ei)
		if !ok {
			return nil, fmt.Errorf("unknown epoch_index %d", ei)
		}
		if err := b.Block.Verify(e.Committee()); err != nil {
			return nil, fmt.Errorf("verify block %d from BlockStore: %w", b.Number, err)
		}
		if err := inner.insertBlock(b.Number, b.Block); err != nil {
			return nil, fmt.Errorf("insert block %d from BlockStore: %w", b.Number, err)
		}
	}
	// Advance nextBlock through contiguous loaded blocks. Don't use
	// updateNextBlock: stale timestamps would skew metrics.
	inner.nextBlock = max(inner.first, status.NextBlock)
	for _, appProposal := range suffix.AppProposals {
		if err := inner.insertAppProposal(appProposal); err != nil {
			return nil, fmt.Errorf("load AppProposal from BlockStore: %w", err)
		}
	}
	for _, appQC := range suffix.AppQCs {
		if err := inner.insertAppQC(cfg.Registry, appQC); err != nil {
			return nil, fmt.Errorf("load AppQC from BlockStore: %w", err)
		}
	}
	inner.setAnchor()
	return inner, nil
}

func (s *State) First() types.GlobalBlockNumber {
	return max(s.blockStore.First(), s.cfg.Registry.FirstBlock())
}

// Registry returns the epoch registry.
func (s *State) Registry() *epoch.Registry { return s.cfg.Registry }

// insertBlocksByHash matches byHash against stored (already verified) QC
// headers over gr ∩ [nextBlock, nextQC) and inserts hits. Advances nextBlock
// when the contiguous prefix grows. Caller must hold inner's lock.
func (s *State) insertBlocksByHash(inner *inner, gr types.GlobalRange, byHash map[types.BlockHeaderHash]*types.Block) error {
	for n := max(inner.nextBlock, gr.First); n < min(gr.Next, inner.nextQC); n++ {
		storedQC := inner.qcs[n]
		storedGR := storedQC.QC().GlobalRange()
		if b, ok := byHash[storedQC.Headers()[n-storedGR.First].Hash()]; ok {
			if err := inner.insertBlock(n, b); err != nil {
				return err
			}
		}
	}
	inner.updateNextBlock(s.metrics)
	return nil
}

// PushQC pushes FullCommitQC and a subset of blocks that were finalized by it.
// Pushing the qc and blocks is atomic, so that no unnecessary GetBlock RPCs are issued.
// Even if the qc was already pushed earlier, the blocks are pushed anyway.
func (s *State) PushQC(ctx context.Context, qc *types.FullCommitQC, blocks []*types.Block) error {
	// Wait until QC is needed.
	ep, ok := s.cfg.Registry.EpochByIndex(qc.QC().Proposal().EpochIndex())
	if !ok {
		return fmt.Errorf("unknown epoch_index %d", qc.QC().Proposal().EpochIndex())
	}
	gr := qc.QC().GlobalRange()
	needQC, err := func() (bool, error) {
		for inner, ctrl := range s.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				return gr.First <= inner.nextQC && gr.First < inner.first+blocksCacheSize
			}); err != nil {
				return false, err
			}
			return inner.nextQC == gr.First, nil
		}
		panic("unreachable")
	}()
	if err != nil {
		return err
	}
	// Verify data.
	if needQC {
		if err := qc.Verify(ep); err != nil {
			return fmt.Errorf("qc.Verify(): %w", err)
		}
	}
	byHash := map[types.BlockHeaderHash]*types.Block{}
	committee := ep.Committee()
	for _, b := range blocks {
		byHash[b.Header().Hash()] = b
		if err := b.Verify(committee); err != nil {
			return fmt.Errorf("b.Verify(): %w", err)
		}
	}
	// Atomically insert QC and blocks.
	for inner, ctrl := range s.inner.Lock() {
		if needQC {
			for inner.nextQC < gr.Next {
				inner.qcs[inner.nextQC] = qc
				inner.nextQC += 1
			}
			ctrl.Updated()
		}
		if len(byHash) > 0 {
			if err := s.insertBlocksByHash(inner, gr, byHash); err != nil {
				return err
			}
			ctrl.Updated()
		}
	}
	return nil
}

// QC returns the FullCommitQC proving finality of the block n.
func (s *State) QC(ctx context.Context, n types.GlobalBlockNumber) (*types.FullCommitQC, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return n < inner.nextQC
		}); err != nil {
			return nil, err
		}
		// [first, nextQC) is retained in RAM; below that use BlockStore.
		if n < inner.first {
			break
		}
		return inner.qcs[n], nil
	}
	return s.qcFromDB(n)
}

// PushBlock pushes block to the state.
// The QC for n must already be present (guaranteed by PushQC ordering), unless
// the height is already in the contiguous block prefix (n < nextBlock) — in
// that case the block is dropped silently (already stored or executed/evicted).
func (s *State) PushBlock(ctx context.Context, n types.GlobalBlockNumber, block *types.Block) error {
	var epochIdx types.EpochIndex
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextQC }); err != nil {
			return err
		}
		// Already in/below the contiguous prefix: insertBlock would no-op
		// (stored, or evicted below first with nextBlock advanced past n).
		if n < inner.nextBlock {
			return nil
		}
		// n in [nextBlock, nextQC): QC is contiguous in that range.
		epochIdx = inner.qcs[n].QC().Proposal().EpochIndex()
	}
	ep, ok := s.cfg.Registry.EpochByIndex(epochIdx)
	if !ok {
		return fmt.Errorf("unknown epoch_index %d", epochIdx)
	}
	// Verify outside the lock against the known epoch.
	if err := block.Verify(ep.Committee()); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		// insertBlock may no-op if n fell into the contiguous prefix (or was
		// gap-filled) while we verified outside the lock; Updated is still
		// signaled so waiters re-check.
		if err := inner.insertBlock(n, block); err != nil {
			return err
		}
		inner.updateNextBlock(s.metrics)
		ctrl.Updated()
	}
	return nil
}

// NextBlock returns the index of the next block to be pushed.
func (s *State) NextBlock() types.GlobalBlockNumber {
	for inner := range s.inner.Lock() {
		return inner.nextBlock
	}
	panic("unreachable")
}

// NextAppQC returns the index of the next AppQC to be pushed.
func (s *State) NextAppQC() types.GlobalBlockNumber {
	for inner := range s.inner.Lock() {
		return inner.nextAppQC
	}
	panic("unreachable")
}

// GlobalBlockByHash returns the finalized GlobalBlock whose stored header
// hashes to the given value, or None if no such block is currently retained.
// Non-blocking. Serves from RAM whenever the hash is still indexed (contiguous
// prefix, gap-fills, and executed heights not yet dropped by setPersisted).
// Falls back to BlockStore only after eviction removes the hash — matching
// Block/TryBlock/QC, which also prefer maps before the store. Gap-fills are
// not written to BlockStore until nextBlock catches up, so they must be served
// from RAM here; Block/TryBlock continue to hide gaps by number.
func (s *State) GlobalBlockByHash(hash types.BlockHeaderHash) (utils.Option[*types.GlobalBlock], error) {
	for inner := range s.inner.Lock() {
		n, ok := inner.blockHashes[hash]
		if !ok {
			break
		}
		// blockHashes stays in lockstep with blocks; a hit means both block and
		// covering QC are still cached (including n < nextAppProposal when
		// AppQC eviction has not advanced first past n yet).
		return utils.Some(assembleGlobalBlock(n, inner.blocks[n], inner.qcs[n])), nil
	}
	return s.globalBlockByHashFromDB(hash)
}

// Block returns the block with the given global number.
// Waits until the contiguous prefix reaches n (n < nextBlock), then returns
// it from memory or BlockStore. Does not expose gaps ahead of nextBlock.
// This function is used for syncing - GlobalBlock can be derived from Block and FullCommitQC,
// which have to be fetched upfront anyway.
func (s *State) Block(ctx context.Context, n types.GlobalBlockNumber) (*types.Block, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return n < inner.nextBlock
		}); err != nil {
			return nil, err
		}
		// [first, nextBlock) is retained in RAM; below that use BlockStore.
		if n < inner.first {
			break
		}
		return inner.blocks[n], nil
	}
	return s.blockFromDB(n)
}

// TryBlock returns the block with the given global number.
// Returns ErrNotFound if n is not yet in the contiguous prefix (n >= nextBlock),
// including gap-fills stored above nextBlock — same no-gap contract as Block.
// Returns ErrPruned if BlockStore no longer has an evicted height.
// Evicted-but-still-durable heights (n < nextBlock) load from BlockStore.
func (s *State) TryBlock(n types.GlobalBlockNumber) (*types.Block, error) {
	for inner := range s.inner.Lock() {
		if n >= inner.nextBlock {
			return nil, types.ErrNotFound
		}
		if n < inner.first {
			break
		}
		return inner.blocks[n], nil
	}
	return s.blockFromDB(n)
}

// NeedBlock reports whether catch-up still needs to fetch height n.
// False when n is already past nextBlock (including heights pruned or
// evicted from RAM) or an in-memory gap-fill is present. Unlike TryBlock,
// gap-fills count as satisfied so the fetcher does not keep re-requesting
// them while a lower contiguous hole is open. Does not consult BlockStore.
func (s *State) NeedBlock(n types.GlobalBlockNumber) bool {
	for inner := range s.inner.Lock() {
		if n < inner.nextBlock {
			return false
		}
		_, ok := inner.blocks[n]
		return !ok
	}
	panic("unreachable")
}

// assembleGlobalBlock builds a GlobalBlock from a block and its covering QC.
// Callers must supply non-nil b and fqc for height n. In-memory paths look up
// maps only for heights still indexed there (including gap-fills); BlockStore
// fallbacks pass values already loaded from the store.
func assembleGlobalBlock(n types.GlobalBlockNumber, b *types.Block, fqc *types.FullCommitQC) *types.GlobalBlock {
	qc := fqc.QC()
	return &types.GlobalBlock{
		GlobalNumber: n,
		Timestamp:    qc.Proposal().BlockTimestamp(n).OrPanic("global block not in QC"),
		Header:       b.Header(),
		Payload:      b.Payload(),
	}
}

// GlobalBlock returns the block with the given global number.
// Waits until the contiguous prefix reaches n (same no-gap contract as Block).
// Returns ErrPruned if the block has already been pruned from BlockStore.
// Falls back to BlockStore when the entry was evicted from memory after persist.
func (s *State) GlobalBlock(ctx context.Context, n types.GlobalBlockNumber) (*types.GlobalBlock, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return n < inner.nextBlock
		}); err != nil {
			return nil, err
		}
		if n < inner.first {
			break
		}
		return assembleGlobalBlock(n, inner.blocks[n], inner.qcs[n]), nil
	}
	return s.globalBlockFromDB(n)
}

func (s *State) blockFromDB(n types.GlobalBlockNumber) (*types.Block, error) {
	opt, err := s.blockStore.ReadBlockByNumber(n)
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadBlockByNumber(%d): %w", n, err)
	}
	b, ok := opt.Get()
	if !ok {
		// Caller only falls through for heights below nextBlock (already seen).
		// None here means the store no longer has them (pruned/reclaimed).
		return nil, types.ErrPruned
	}
	return b, nil
}

func (s *State) qcFromDB(n types.GlobalBlockNumber) (*types.FullCommitQC, error) {
	opt, err := s.blockStore.ReadQCByBlockNumber(n)
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadQCByBlockNumber(%d): %w", n, err)
	}
	qc, ok := opt.Get()
	if !ok {
		return nil, types.ErrPruned
	}
	return qc, nil
}

func (s *State) appQCFromDB(n types.GlobalBlockNumber) (*types.AppQC, error) {
	opt, err := s.blockStore.ReadAppQCByBlockNumber(n)
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadAppQCByBlockNumber(%d): %w", n, err)
	}
	if appQC, ok := opt.Get(); ok {
		return appQC, nil
	}
	return nil, types.ErrPruned
}

func (s *State) globalBlockFromDB(n types.GlobalBlockNumber) (*types.GlobalBlock, error) {
	b, err := s.blockFromDB(n)
	if err != nil {
		return nil, err
	}
	qc, err := s.qcFromDB(n)
	if err != nil {
		return nil, err
	}
	return assembleGlobalBlock(n, b, qc), nil
}

func (s *State) globalBlockByHashFromDB(hash types.BlockHeaderHash) (utils.Option[*types.GlobalBlock], error) {
	opt, err := s.blockStore.ReadBlockByHash(hash)
	if err != nil {
		return utils.None[*types.GlobalBlock](), fmt.Errorf("blockStore.ReadBlockByHash: %w", err)
	}
	bn, ok := opt.Get()
	if !ok {
		return utils.None[*types.GlobalBlock](), nil
	}
	qc, err := s.qcFromDB(bn.Number)
	if err != nil {
		if errors.Is(err, types.ErrPruned) || errors.Is(err, types.ErrNotFound) {
			return utils.None[*types.GlobalBlock](), nil
		}
		return utils.None[*types.GlobalBlock](), err
	}
	return utils.Some(assembleGlobalBlock(bn.Number, bn.Block, qc)), nil
}

// PushAppHash marks blocks up to n as executed.
func (s *State) PushAppHash(ctx context.Context, n types.GlobalBlockNumber, hash types.AppHash) error {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextBlock }); err != nil {
			return err
		}
		if n < inner.nextAppProposal {
			return nil
		}
		p := inner.qcs[n].QC().Proposal()
		if next, first := inner.nextAppProposal, p.GlobalRange().First; next < first {
			// We expect the AppHashes to be pushed in order.
			return fmt.Errorf("received appHash for %v: %w", n, ErrOutOfOrder)
		} else if n != p.GlobalRange().Next-1 {
			// We only care about the AppHash of the last block of the range.
			return nil
		}
		proposal := types.NewAppProposal(p, hash)
		t := time.Now()
		for inner.nextAppProposal <= n {
			b := inner.blocks[inner.nextAppProposal]
			latency := t.Sub(b.Payload().CreatedAt()).Seconds()
			s.metrics.BlockLatency.Execute.Observe(latency)
			s.metrics.TxLatency.Execute.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
			if txCount := len(b.Payload().Txs()); txCount > 0 {
				var totalTxSize int
				for _, tx := range b.Payload().Txs() {
					totalTxSize += len(tx)
				}
				s.metrics.TxSize.ObserveWithWeight(float64(totalTxSize)/float64(txCount), uint64(txCount))
			}
			inner.appProposals[inner.nextAppProposal] = proposal
			inner.nextAppProposal += 1
		}
		s.metrics.NextBlock.Execute.Set(utils.Clamp[int64](inner.nextAppProposal))
		ctrl.Updated()
		// CRITICAL: We need to persist AppHash before we return and start executing the next block,
		// otherwise we lose the apphash on restart.
		// TODO(gprusak): this is a temporary measure, until AppHashes are persisted outside of BlockStore.
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.persisted.NextAppProposal }); err != nil {
			return err
		}
	}
	return nil
}

func (s *State) PushGasUsed(gasUsed int64) {
	s.metrics.GasUsed.Add(gasUsed)
}

// AppVote returns an appVote for a block >= n.
// Vote is available ONLY once AppHash has been pushed AND persisted.
// This prevents any possible equivocation and ensures that local node has the executed blocks persisted (since nextAppProposa <= nextBlock).
func (s *State) AppVote(ctx context.Context, n types.GlobalBlockNumber) (*types.AppVote, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.persisted.NextAppProposal }); err != nil {
			return nil, err
		}
		if n < inner.first {
			return nil, types.ErrPruned
		}
		return types.NewAppVote(inner.appProposals[n]), nil
	}
	panic("unreachable")
}

// PushAppQC pushes an AppQC to the state and advances the AppQC cursor.
func (s *State) PushAppQC(ctx context.Context, appQC *types.AppQC) error {
	gr := appQC.Proposal().GlobalRange()
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return gr.Next <= inner.nextAppProposal
		}); err != nil {
			return err
		}
		if gr.First < inner.nextAppQC {
			return nil
		}
		if err := inner.insertAppQC(s.cfg.Registry, appQC); err != nil {
			return err
		}
		t := time.Now()
		for n := gr.First; n < gr.Next; n++ {
			b := inner.blocks[n]
			latency := t.Sub(b.Payload().CreatedAt()).Seconds()
			s.metrics.BlockLatency.Certify.Observe(latency)
			s.metrics.TxLatency.Certify.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
		}
		s.metrics.NextBlock.Certify.Set(utils.Clamp[int64](inner.nextAppQC))
		ctrl.Updated()
		return nil
	}
	panic("unreachable")
}

func (s *State) AppQC(ctx context.Context, n types.GlobalBlockNumber) (*types.AppQC, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextAppQC }); err != nil {
			return nil, err
		}
		if inner.first <= n {
			return inner.appQCs[n], nil
		}
	}
	return s.appQCFromDB(n)
}

type Anchor struct {
	CommitQC *types.CommitQC
	AppQC    *types.AppQC
}

// Anchor represents the AppQC/CommitQC covering inner.first.
// It is used by avail.State.
func (s *State) Anchor() utils.AtomicRecv[utils.Option[Anchor]] {
	for inner := range s.inner.Lock() {
		return inner.anchor.Subscribe()
	}
	panic("unreachable")
}

func (i *inner) nextToExecute(lane types.LaneID) types.BlockNumber {
	if i.nextAppProposal < i.nextQC {
		return i.qcs[i.nextAppProposal].QC().LaneRange(lane).First()
	}
	if i.first < i.nextAppProposal {
		return i.qcs[i.nextAppProposal-1].QC().LaneRange(lane).Next()
	}
	// Genesis state: i.first == i.nextQC
	return 0
}

// Waits until lane block n is executed, returns the next block of this lane to be executed (>n)
func (s *State) WaitUntilExecuted(ctx context.Context, lane types.LaneID, n types.BlockNumber) (types.BlockNumber, error) {
	for inner, ctrl := range s.inner.Lock() {
		for {
			if next := inner.nextToExecute(lane); n < next {
				return next, nil
			}
			if err := ctrl.Wait(ctx); err != nil {
				return 0, err
			}
		}
	}
	panic("unreachable")
}

// PruneBefore asks BlockStore to drop data before retainFrom. This is independent
// of in-memory retention: RAM is cleared only by setPersisted.
// BlockStore enforces its own never-empty retention and refuses reads below its
// watermark.
func (s *State) PruneBefore(retainFrom types.GlobalBlockNumber) error {
	return s.blockStore.PruneBefore(retainFrom)
}

// runPersist is a background goroutine that persists QCs, blocks,
// AppProposals, and AppQCs to BlockStore. It waits for in-memory data to advance
// past the SuffixRange persistence cursor, then writes each stream in cursor
// order and flushes once per batch. persisted.NextBlock advances with the
// block tip to unblock PushAppHash only when data is durable.
// Errors propagate vertically (kill the component).
//
// Cursors seed from BlockStore.Status() when non-zero so PushQC-before-Run heights
// are not skipped. When a tip is zero, seed from the recovery floor, never bare
// registry.FirstBlock() — a QC-only store can skipTo past genesis while
// NextBlock is still zero.
//
// Under the BlockStore write contract (QC before covered blocks), NextQC is never
// behind NextBlock after a successful write. Persistence is driven by the block
// cursor; a QC is emitted only when n equals GlobalRange.First and that First
// is still at or past NextQC (enough coverage for each new block, not every
// in-memory QC, and no rewrite of QCs already on disk).
//
// In-memory block/QC/AppProposal/AppQC eviction is driven by persisted SuffixRange
// changes.
func (s *State) runPersist(ctx context.Context) error {
	for {
		var qcs []*types.FullCommitQC
		var blocks []blockEntry
		var appProposals []*types.AppProposal
		var appQCs []*types.AppQC
		var status types.SuffixRange
		for inner, ctrl := range s.inner.Lock() {
			status = inner.persisted
			// Wait until there is anything to persist.
			if err := ctrl.WaitUntil(ctx, func() bool {
				return status.NextQC < inner.nextQC || status.NextBlock < inner.nextBlock ||
					status.NextAppProposal < inner.nextAppProposal || status.NextAppQC < inner.nextAppQC
			}); err != nil {
				return err
			}
			// Collect data to persist.
			for status.NextQC < inner.nextQC {
				qc := inner.qcs[status.NextQC]
				qcs = append(qcs, qc)
				status.NextQC = qc.QC().GlobalRange().Next
			}
			for status.NextBlock < inner.nextBlock {
				blocks = append(blocks, blockEntry{n: status.NextBlock, block: inner.blocks[status.NextBlock]})
				status.NextBlock += 1
			}
			for status.NextAppProposal < inner.nextAppProposal {
				appProposal := inner.appProposals[status.NextAppProposal]
				appProposals = append(appProposals, appProposal)
				status.NextAppProposal = appProposal.GlobalRange().Next
			}
			for status.NextAppQC < inner.nextAppQC {
				appQC := inner.appQCs[status.NextAppQC]
				appQCs = append(appQCs, appQC)
				status.NextAppQC = appQC.Proposal().GlobalRange().Next
				status.First = status.NextAppQC - 1
			}
		}
		// Write data in order: QCs,blocks,appProposals,appQCs
		// to maintain the invariants.
		for _, qc := range qcs {
			if err := s.blockStore.WriteQC(qc); err != nil {
				return fmt.Errorf("write QC %d: %w", qc.QC().Index(), err)
			}
		}
		for _, lb := range blocks {
			if err := s.blockStore.WriteBlock(lb.n, lb.block); err != nil {
				return fmt.Errorf("write block %d: %w", lb.n, err)
			}
		}
		for _, appProposal := range appProposals {
			if err := s.blockStore.WriteAppProposal(appProposal); err != nil {
				return fmt.Errorf("write AppProposal %d: %w", appProposal.RoadIndex(), err)
			}
		}
		for _, appQC := range appQCs {
			if err := s.blockStore.WriteAppQC(appQC); err != nil {
				return fmt.Errorf("write AppQC %d: %w", appQC.Proposal().RoadIndex(), err)
			}
		}
		// Flush the new data.
		if err := s.blockStore.Flush(); err != nil {
			return fmt.Errorf("flush BlockStore: %w", err)
		}
		// Prune the inner state.
		for inner, ctrl := range s.inner.Lock() {
			inner.persisted = status
			t := time.Now()
			for inner.first < inner.persisted.First {
				// Divergence detection
				n := inner.first
				if got, want := inner.appProposals[n].AppHash(), inner.appQCs[n].Proposal().AppHash(); !bytes.Equal(got, want) {
					return fmt.Errorf("AppHash divergence detected at block %v: local AppHash = %v, quorum Apphash = %v", n, got, want)
				}
				b := inner.blocks[n]
				latency := t.Sub(b.Payload().CreatedAt()).Seconds()
				s.metrics.BlockLatency.Evict.Observe(latency)
				s.metrics.TxLatency.Evict.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
				delete(inner.blockHashes, b.Header().Hash())
				delete(inner.blocks, n)
				delete(inner.qcs, n)
				delete(inner.appQCs, n)
				delete(inner.appProposals, n)
				inner.first += 1
			}
			s.metrics.NextBlock.Evict.Set(utils.Clamp[int64](inner.first))
			inner.setAnchor()
			ctrl.Updated()
		}
	}
}

func (i *inner) setAnchor() {
	if i.first < i.persisted.NextAppQC {
		i.anchor.Store(utils.Some(Anchor{
			CommitQC: i.qcs[i.first].QC(),
			AppQC:    i.appQCs[i.first],
		}))
	}
}

// Run starts the background persistence loop.
func (s *State) Run(ctx context.Context) error {
	return s.runPersist(ctx)
}
