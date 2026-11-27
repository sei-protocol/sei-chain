package data

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const maxLaneBlock = 1000
const maxRoadBlocks = 100

// ErrOutOfOrder is returned when PushAppHash receives an app hash past the
// next CommitQC range waiting for execution.
var ErrOutOfOrder = errors.New("out of order")

// ErrAppHashDivergence is returned when a quorum-certified AppHash differs from
// the one local execution produced.
var ErrAppHashDivergence = errors.New("AppHash divergence")

// Config is the config for the data State.
type Config struct {
	// App supplies the retained epoch window at startup and epoch transitions.
	App *abevm.Proxy
	// GenesisTimestamp is the timestamp of the first proposal.
	GenesisTimestamp time.Time
}

// blockEntry contains a FullCommitQC, its lane blocks, and its verifying epoch.
type blockEntry struct {
	qc     *types.FullCommitQC
	epoch  *types.Epoch
	blocks map[uint64]*types.LaneBlock
}

func (e *blockEntry) IsFull() bool {
	return len(e.blocks) == len(e.qc.Headers())
}

// FullBlock returns a complete road block, or None while lane blocks are missing.
func (e *blockEntry) FullBlock() utils.Option[*types.RoadBlock] {
	if !e.IsFull() {
		return utils.None[*types.RoadBlock]()
	}
	blocks := make([]*types.LaneBlock, len(e.blocks))
	for i := range blocks {
		blocks[i] = e.blocks[uint64(i)]
	}
	return utils.Some(types.NewRoadBlock(e.qc, blocks))
}

type inner struct {
	blocks         map[types.RoadHeight]*blockEntry               // [first, nextPartialBlock)
	appProposals   map[types.RoadHeight]*types.AppProposal        // [first, nextAppProposal)
	appQCs         map[types.RoadHeight]*types.AppQC              // [first, nextAppQC)
	byHash         map[types.RoadBlockHeaderHash]types.RoadHeight // all blocks (including partial ones) by RoadBlockHeaderHash.
	laneBlockCount uint64                                         // Total lane blocks (present and missing) of all partial and full blocks.

	// first is the inclusive low end of retained in-memory state: maps keep [first, next*).
	// Advanced by runPersist(). Durable copies below first live in BlockStore.
	// Heights below first have a persisted AppQC that matched local execution.
	// first <= nextAppQC <= nextAppProposal <= nextFullBlock <= nextPartialBlock
	first            types.RoadHeight
	nextAppQC        types.RoadHeight
	nextAppProposal  types.RoadHeight
	nextFullBlock    types.RoadHeight
	nextPartialBlock types.RoadHeight
	persisted        blockstore.SuffixRange

	// Anchor represents the highest fully processed row:
	// CommitQC, Blocks, AppProposal, AppQC present and persisted.
	anchor utils.AtomicSend[Anchor]
}

// lastCommitQC returns the CommitQC of the road block at height i.nextPartialBlock - 1, or None
// when i holds no road block below i.nextPartialBlock.
func (i *inner) lastCommitQC() utils.Option[*types.CommitQC] {
	if i.nextPartialBlock > i.first {
		return utils.Some(i.blocks[i.nextPartialBlock-1].qc.QC())
	}
	return utils.None[*types.CommitQC]()
}

func (i *inner) insertAppQC(appQC *types.AppQC) error {
	n := appQC.Proposal().Height()
	if n < i.nextAppQC {
		return nil
	}
	if n >= i.nextAppProposal {
		return fmt.Errorf("missing AppProposal for this AppQC")
	}
	if n != i.nextAppQC {
		return fmt.Errorf("AppQC gap: got %d, want %d", n, i.nextAppQC)
	}
	ep := i.blocks[n].epoch
	if err := appQC.Verify(ep.Committee()); err != nil {
		return fmt.Errorf("appQC.Verify(): %w", err)
	}
	i.appQCs[n] = appQC
	i.nextAppQC += 1
	return nil
}

func (i *inner) insertAppProposal(appProposal *types.AppProposal) error {
	n := appProposal.Height()
	if n < i.nextAppProposal {
		return nil
	}
	if n >= i.nextPartialBlock {
		return fmt.Errorf("missing CommitQC for this AppProposal")
	}
	if n != i.nextAppProposal {
		return fmt.Errorf("AppProposal gap: got %d, want %d", n, i.nextAppProposal)
	}
	if err := appProposal.Verify(i.blocks[i.nextAppProposal].qc.QC()); err != nil {
		return fmt.Errorf("appProposal.Verify(): %w", err)
	}
	i.appProposals[n] = appProposal
	i.nextAppProposal += 1
	return nil
}

// insertLaneBlock inserts a pre-verified block into the inner state.
// Requires a QC to already be present for block n. Callers must verify
// payload integrity before calling; insertLaneBlock matches the header against
// the stored FullCommitQC (unlike insertQC, which verifies the QC itself).
//
// insertLaneBlock does NOT advance nextFullBlock — callers should call
// updateNextBlock after inserting one or more blocks. This separation
// allows batch insertion (e.g. PushPartialBlock inserts multiple blocks, then
// advances nextFullBlock once).
func (i *inner) insertLaneBlock(n types.LaneBlockPos, block *types.LaneBlock) error {
	if n.Height < i.nextFullBlock || n.Height >= i.nextPartialBlock {
		return nil
	}
	e := i.blocks[n.Height]
	if _, ok := e.blocks[n.Offset]; ok {
		return nil
	}
	if n.Offset >= uint64(len(e.qc.Headers())) {
		return fmt.Errorf("invalid lane block position %v", n)
	}
	if want, got := e.qc.Headers()[n.Offset].Hash(), block.Header().Hash(); want != got {
		return fmt.Errorf("block %v header hash mismatch: want %v, got %v", n, want, got)
	}
	e.blocks[n.Offset] = block
	return nil
}

func (i *inner) updateNextFullBlock(m *metrics.Metrics) {
	t := time.Now()
	for i.nextFullBlock < i.nextPartialBlock {
		e := i.blocks[i.nextFullBlock]
		if !e.IsFull() {
			break
		}
		for _, b := range e.blocks {
			latency := t.Sub(b.Payload().CreatedAt()).Seconds()
			m.BlockLatency.Receive.Observe(latency)
			m.TxLatency.Receive.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
		}
		i.nextFullBlock += 1
	}
	m.NextBlock.Receive.Set(utils.Clamp[int64](i.nextFullBlock))
}

// State of the chain.
// Contains blocks in global order and proofs of sequencing: (CommitQC) and execution result (AppQC).
type State struct {
	cfg        *Config
	metrics    *metrics.Metrics
	inner      utils.Watch[*inner]
	blockStore *blockstore.Store
	anchor     utils.AtomicRecv[Anchor]
}

// NewState constructs a data State, replaying persisted state from blockStore.
// The caller owns blockStore and must close it after State.Run returns (nodeImpl
// owns this in production); State never closes it.
func NewState(cfg *Config, blockStore *blockstore.Store) (*State, error) {
	inner, err := loadFromBlockStore(cfg, blockStore)
	if err != nil {
		return nil, fmt.Errorf("loadFromBlockStore: %w", err)
	}
	m := metrics.Get()
	m.NextBlock.QC.Set(utils.Clamp[int64](inner.nextPartialBlock))
	m.NextBlock.Receive.Set(utils.Clamp[int64](inner.nextFullBlock))
	m.NextBlock.Execute.Set(utils.Clamp[int64](inner.nextAppProposal))
	m.NextBlock.Certify.Set(utils.Clamp[int64](inner.nextAppQC))
	m.NextBlock.Evict.Set(utils.Clamp[int64](inner.first))
	return &State{
		cfg:        cfg,
		metrics:    m,
		inner:      utils.NewWatch(inner),
		blockStore: blockStore,
		anchor:     inner.anchor.Subscribe(),
	}, nil
}

// loadFromBlockStore replays the persisted suffix from blockStore into s.inner.
// Called from NewState before any goroutines are spawned.
func loadFromBlockStore(cfg *Config, blockStore *blockstore.Store) (*inner, error) {
	suffix, err := blockStore.ReadSuffix()
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadSuffix(): %w", err)
	}
	status, ok := suffix.Status.Get()
	// TODO: storage reconciliation must bring the registry row to the block store's suffix before
	// loading. Until then, a suffix the row does not cover fails here.
	registry, err := cfg.App.EpochRegistry()
	if err != nil {
		return nil, fmt.Errorf("fetch epoch registry: %w", err)
	}
	if !ok {
		genesis, err := registry.Epoch(0)
		if err != nil {
			return nil, fmt.Errorf("genesis epoch 0: %w", err)
		}
		first := genesis.RoadRange().First
		status = blockstore.SuffixRange{
			First:           first,
			NextBlock:       first,
			NextAppProposal: first,
			NextAppQC:       first,
		}
	}
	inner := &inner{
		blocks:           map[types.RoadHeight]*blockEntry{},
		appQCs:           map[types.RoadHeight]*types.AppQC{},
		appProposals:     map[types.RoadHeight]*types.AppProposal{},
		byHash:           map[types.RoadBlockHeaderHash]types.RoadHeight{},
		first:            status.First,
		nextAppProposal:  status.First,
		nextAppQC:        status.First,
		nextFullBlock:    status.First,
		nextPartialBlock: status.First,
		persisted:        status,
		anchor:           utils.NewAtomicSend(Anchor{}),
	}
	for _, block := range suffix.Blocks {
		epoch, err := registry.Epoch(block.QC().QC().View().EpochIndex)
		if err != nil {
			return nil, fmt.Errorf("load block from BlockStore: %w", err)
		}
		n := block.QC().Height()
		if n != inner.nextPartialBlock {
			return nil, fmt.Errorf("QC gap: got %d, want %d", n, inner.nextPartialBlock)
		}
		// The parent of the first stored road block may be pruned, so that road block is checked
		// against its parent only at the first road height of the chain, where it has none.
		if n > inner.first || (epoch.EpochIndex() == 0 && n == epoch.RoadRange().First) {
			err = block.Verify(epoch, inner.lastCommitQC())
		} else {
			err = block.VerifyWithoutParent(epoch)
		}
		if err != nil {
			return nil, fmt.Errorf("load block %d from BlockStore: %w", n, err)
		}
		blocks := make(map[uint64]*types.LaneBlock, len(block.LaneBlocks()))
		for offset, laneBlock := range block.LaneBlocks() {
			blocks[uint64(offset)] = laneBlock //nolint:gosec // non-negative slice index
		}
		inner.byHash[block.Header().Hash()] = n
		inner.blocks[n] = &blockEntry{qc: block.QC(), epoch: epoch, blocks: blocks}
		inner.laneBlockCount += uint64(len(block.QC().Headers()))
		inner.nextPartialBlock += 1
		inner.nextFullBlock += 1
	}
	for _, appProposal := range suffix.AppProposals {
		if err := inner.insertAppProposal(appProposal); err != nil {
			return nil, fmt.Errorf("load AppProposal from BlockStore: %w", err)
		}
	}
	for _, appQC := range suffix.AppQCs {
		if err := inner.insertAppQC(appQC); err != nil {
			return nil, fmt.Errorf("load AppQC from BlockStore: %w", err)
		}
	}
	epochs, ok := registry.ForAnchor(status.First)
	if !ok {
		return nil, fmt.Errorf("epoch registry not ready for road %d: %w", status.First, types.ErrNotFound)
	}
	inner.setAnchor(epochs)
	return inner, nil
}

func (s *State) First() types.RoadHeight {
	if !s.blockStore.Status().IsPresent() {
		for inner := range s.inner.Lock() {
			return inner.first
		}
		panic("unreachable")
	}
	return s.blockStore.First()
}

// GenesisTimestamp returns the timestamp of the first proposal.
func (s *State) GenesisTimestamp() time.Time { return s.cfg.GenesisTimestamp }

// PushPartialBlock pushes FullCommitQC and a subset of blocks that were finalized by it.
// Pushing the qc and blocks is atomic, so that no unnecessary GetBlock RPCs are issued.
// Even if the qc was already pushed earlier, the blocks are pushed anyway.
// New QCs wait for lane-block and roadblock capacity, while always allowing two retained roadblocks.
func (s *State) PushPartialBlock(ctx context.Context, qc *types.FullCommitQC, blocks map[uint64]*types.LaneBlock) error {
	n := qc.Height()
	anchor, err := s.anchor.Wait(ctx, func(a Anchor) bool { return n < a.Epochs[1].RoadRange().Next })
	if err != nil {
		return err
	}
	epoch, ok := anchor.EpochAt(n).Get()
	if !ok {
		return nil
	}
	var parent utils.Option[*types.CommitQC]
	nextFullBlock, nextPartialBlock, err := func() (types.RoadHeight, types.RoadHeight, error) {
		for inner, ctrl := range s.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				// Wait until either we already have the road block, or until this is the next road block AND we have capacity.
				// We have capacity iff road/lane block limits allow another road block OR we have <2 road blocks.
				// Count road blocks separately to account for empty road blocks.
				// For liveness we need to be able to keep in data state at least 2 road blocks,
				// because we always keep 1 executed block.
				hasCapacity := len(inner.blocks) < 2 || (len(inner.blocks) < maxRoadBlocks && inner.laneBlockCount <= maxLaneBlock)
				return n < inner.nextPartialBlock || (n == inner.nextPartialBlock && hasCapacity)
			}); err != nil {
				return 0, 0, err
			}
			// When the qc is the next road, its parent is fixed until the next road advances.
			parent = inner.lastCommitQC()
			return inner.nextFullBlock, inner.nextPartialBlock, nil
		}
		panic("unreachable")
	}()
	if err != nil {
		return err
	}
	if n < nextFullBlock || (len(blocks) == 0 && n < nextPartialBlock) {
		return nil
	}
	if n == nextPartialBlock {
		if err := qc.Verify(epoch, parent); err != nil {
			return fmt.Errorf("qc.Verify(): %w", err)
		}
	}
	for _, b := range blocks {
		if err := b.Verify(); err != nil {
			return fmt.Errorf("block.Verify(): %w", err)
		}
	}
	// Atomically insert QC and blocks.
	for inner, ctrl := range s.inner.Lock() {
		if n < inner.nextFullBlock {
			return nil
		}
		updated := false
		// qc.Verify above already checked qc against its parent, read under the first lock.
		// The insert below runs only if nextPartialBlock is still n, so that parent has not changed.
		if inner.nextPartialBlock == n {
			updated = true
			inner.blocks[n] = &blockEntry{qc: qc, epoch: epoch, blocks: make(map[uint64]*types.LaneBlock, len(qc.Headers()))}
			inner.laneBlockCount += uint64(len(qc.Headers()))
			inner.byHash[qc.Header().Hash()] = n
			inner.nextPartialBlock += 1
			s.metrics.NextBlock.QC.Set(utils.Clamp[int64](inner.nextPartialBlock))
		}
		e := inner.blocks[n]
		hs := e.qc.Headers()
		for i, b := range blocks {
			if _, ok := e.blocks[i]; !ok && i < uint64(len(hs)) && hs[i].Hash() == b.Header().Hash() {
				updated = true
				e.blocks[i] = b
			}
		}
		if updated {
			inner.updateNextFullBlock(s.metrics)
			ctrl.Updated()
		}
	}
	return nil
}

// QC returns the FullCommitQC proving finality of the block n.
func (s *State) QC(ctx context.Context, n types.RoadHeight) (*types.FullCommitQC, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return n < inner.nextPartialBlock
		}); err != nil {
			return nil, err
		}
		// [first, nextPartialBlock) is retained in RAM; below that use BlockStore.
		if inner.first <= n {
			return inner.blocks[n].qc, nil
		}
	}
	return s.qcFromDB(n)
}

// PushBlock pushes block to the state.
// The QC for n must already be present (guaranteed by PushPartialBlock ordering), unless
// the height is already in the contiguous block prefix (n < nextFullBlock) — in
// that case the block is dropped silently (already stored or executed/evicted).
func (s *State) PushBlock(ctx context.Context, n types.LaneBlockPos, block *types.LaneBlock) error {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n.Height < inner.nextPartialBlock }); err != nil {
			return err
		}
		if n.Height < inner.nextFullBlock {
			return nil
		}
	}
	if err := block.Verify(); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if err := inner.insertLaneBlock(n, block); err != nil {
			return err
		}
		inner.updateNextFullBlock(s.metrics)
		ctrl.Updated()
	}
	return nil
}

// NextQC returns the next road height awaiting a CommitQC.
func (s *State) NextQC() types.RoadHeight {
	for inner := range s.inner.Lock() {
		return inner.nextPartialBlock
	}
	panic("unreachable")
}

// NextFullBlock returns the first road height without a complete set of lane blocks.
func (s *State) NextFullBlock() types.RoadHeight {
	for inner := range s.inner.Lock() {
		return inner.nextFullBlock
	}
	panic("unreachable")
}

// NextAppProposal returns the lowest block whose AppHash has not been pushed.
func (s *State) NextAppProposal() types.RoadHeight {
	for inner := range s.inner.Lock() {
		return inner.nextAppProposal
	}
	panic("unreachable")
}

// NextAppQC returns the index of the next AppQC to be pushed.
func (s *State) NextAppQC() types.RoadHeight {
	for inner := range s.inner.Lock() {
		return inner.nextAppQC
	}
	panic("unreachable")
}

// SuperBlockByHash returns a retained SuperBlock by its road block hash.
// Unknown, pruned, or incomplete blocks and roads beyond the contiguous complete prefix return None.
func (s *State) SuperBlockByHash(hash types.RoadBlockHeaderHash) (utils.Option[*types.SuperBlock], error) {
	for inner := range s.inner.Lock() {
		if n, ok := inner.byHash[hash]; ok {
			if n >= inner.nextFullBlock {
				return utils.None[*types.SuperBlock](), nil
			}
			return utils.Some(inner.blocks[n].FullBlock().OrPanic("hash without full block").SuperBlock()), nil
		}
	}
	block, err := s.blockStore.ReadBlockByHash(hash)
	if err != nil {
		return utils.None[*types.SuperBlock](), err
	}
	return block.Map(func(b *types.RoadBlock) *types.SuperBlock {
		return b.SuperBlock()
	}), nil
}

// Block returns the lane block at position n.
// Waits until the contiguous prefix reaches n (n < nextFullBlock), then returns
// it from memory or BlockStore. Does not expose gaps ahead of nextFullBlock.
// This function is used to sync individual lane blocks.
func (s *State) Block(ctx context.Context, n types.LaneBlockPos) (*types.LaneBlock, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n.Height < inner.nextFullBlock }); err != nil {
			return nil, err
		}
		if n.Height < inner.first {
			break
		}
		b, ok := inner.blocks[n.Height].blocks[n.Offset]
		if !ok {
			return nil, types.ErrNotFound
		}
		return b, nil
	}
	return s.blockFromDB(n)
}

// TryBlock returns an available lane block, including from a partial SuperBlock.
// Missing blocks return ErrNotFound. Evicted blocks are read from BlockStore
// and return ErrPruned if they are below its retention floor.
func (s *State) TryBlock(n types.LaneBlockPos) (*types.LaneBlock, error) {
	for inner := range s.inner.Lock() {
		if n.Height >= inner.nextPartialBlock {
			return nil, types.ErrNotFound
		}
		if n.Height < inner.first {
			break
		}
		b, ok := inner.blocks[n.Height].blocks[n.Offset]
		if !ok {
			return nil, types.ErrNotFound
		}
		return b, nil
	}
	return s.blockFromDB(n)
}

// TryQC returns the FullCommitQC covering global height n without waiting.
// Returns ErrNotFound if n is not yet covered (n >= nextPartialBlock).
// Returns ErrPruned if BlockStore no longer has an evicted height.
func (s *State) TryQC(n types.RoadHeight) (*types.FullCommitQC, error) {
	for inner := range s.inner.Lock() {
		if n >= inner.nextPartialBlock {
			return nil, types.ErrNotFound
		}
		if n < inner.first {
			break
		}
		return inner.blocks[n].qc, nil
	}
	return s.qcFromDB(n)
}

// NeedBlock reports whether catch-up still needs to fetch lane block n.
// Received blocks and roads below nextFullBlock do not need fetching.
// It does not consult BlockStore.
func (s *State) NeedBlock(n types.LaneBlockPos) bool {
	for inner := range s.inner.Lock() {
		if n.Height < inner.nextFullBlock {
			return false
		}
		if inner.nextPartialBlock <= n.Height {
			return true
		}
		e := inner.blocks[n.Height]
		_, ok := e.blocks[n.Offset]
		return n.Offset < uint64(len(e.qc.Headers())) && !ok
	}
	panic("unreachable")
}

// SuperBlock waits for all lane blocks through road n and returns their merged transactions.
// Evicted roads are read from BlockStore; pruned roads return ErrPruned.
func (s *State) SuperBlock(ctx context.Context, n types.RoadHeight) (*types.SuperBlock, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextFullBlock }); err != nil {
			return nil, err
		}
		if inner.first <= n {
			return inner.blocks[n].FullBlock().OrPanic("bad full block").SuperBlock(), nil
		}
	}
	opt, err := s.blockStore.ReadBlock(n)
	if err != nil {
		return nil, err
	}
	if b, ok := opt.Get(); ok {
		return b.SuperBlock(), nil
	}
	return nil, types.ErrPruned
}

func (s *State) blockFromDB(n types.LaneBlockPos) (*types.LaneBlock, error) {
	opt, err := s.blockStore.ReadLaneBlock(n)
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadLaneBlock(%v): %w", n, err)
	}
	b, ok := opt.Get()
	if !ok {
		if n.Height < s.blockStore.First() {
			return nil, types.ErrPruned
		}
		return nil, types.ErrNotFound
	}
	return b, nil
}

func (s *State) qcFromDB(n types.RoadHeight) (*types.FullCommitQC, error) {
	opt, err := s.blockStore.ReadQC(n)
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadQC(%d): %w", n, err)
	}
	qc, ok := opt.Get()
	if !ok {
		return nil, types.ErrPruned
	}
	return qc, nil
}

func (s *State) appQCFromDB(n types.RoadHeight) (*types.AppQC, error) {
	opt, err := s.blockStore.ReadAppQC(n)
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadAppQC(%d): %w", n, err)
	}
	if appQC, ok := opt.Get(); ok {
		return appQC, nil
	}
	return nil, types.ErrPruned
}

// PushAppHash records the AppHash for road n.
func (s *State) PushAppHash(ctx context.Context, n types.RoadHeight, hash types.AppHash) error {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextFullBlock }); err != nil {
			return err
		}
		if n < inner.nextAppProposal {
			return nil
		}
		if n != inner.nextAppProposal {
			return fmt.Errorf("received appHash for %v: %w", n, ErrOutOfOrder)
		}
		e := inner.blocks[n]
		proposal := types.NewAppProposal(e.qc.QC().View(), hash)
		t := time.Now()
		for _, b := range e.blocks {
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
		}
		inner.appProposals[n] = proposal
		inner.nextAppProposal++
		s.metrics.NextBlock.Execute.Set(utils.Clamp[int64](inner.nextAppProposal))
		ctrl.Updated()
		// Durable before return. AppVote waits on the same cursor.
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.persisted.NextAppProposal }); err != nil {
			return err
		}
	}
	return nil
}

// AppVote returns an appVote for a block >= n.
// Vote is available ONLY once AppHash has been pushed AND persisted.
// This prevents any possible equivocation and ensures that local node has the executed blocks persisted (since nextAppProposa <= nextFullBlock).
func (s *State) AppVote(ctx context.Context, n types.RoadHeight) (*types.AppVote, error) {
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
	n := appQC.Proposal().Height()
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n <= inner.nextAppQC && n < inner.nextAppProposal }); err != nil {
			return err
		}
		if n != inner.nextAppQC {
			return nil
		}
		if err := inner.insertAppQC(appQC); err != nil {
			return err
		}
		t := time.Now()
		for _, b := range inner.blocks[n].blocks {
			latency := t.Sub(b.Payload().CreatedAt()).Seconds()
			s.metrics.BlockLatency.Certify.Observe(latency)
			s.metrics.TxLatency.Certify.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
		}
		s.metrics.NextBlock.Certify.Set(utils.Clamp[int64](inner.nextAppQC))
		ctrl.Updated()
		break
	}
	return nil
}

func (s *State) AppQC(ctx context.Context, n types.RoadHeight) (*types.AppQC, error) {
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

// Anchor returns the retained certified road and its two epochs.
func (s *State) Anchor() utils.AtomicRecv[Anchor] { return s.anchor }

func (i *inner) nextToExecute(lane types.LaneID) types.LaneHeight {
	if i.nextAppProposal < i.nextPartialBlock {
		return i.blocks[i.nextAppProposal].qc.QC().LaneRange(lane).First()
	}
	if i.first < i.nextAppProposal {
		return i.blocks[i.nextAppProposal-1].qc.QC().LaneRange(lane).Next()
	}
	// Genesis state: i.first == i.nextPartialBlock
	return 0
}

// Waits until lane block n is executed, returns the next block of this lane to be executed (>n)
func (s *State) WaitUntilExecuted(ctx context.Context, lane types.LaneID, n types.LaneHeight) (types.LaneHeight, error) {
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
func (s *State) PruneBefore(retainFrom types.RoadHeight) error {
	return s.blockStore.PruneBefore(retainFrom)
}

// runPersist is a background goroutine that persists QCs, blocks,
// AppProposals, and AppQCs to BlockStore. It waits for in-memory data to advance
// past the SuffixRange persistence cursor, then writes each stream in cursor
// order and flushes once per batch. persisted.NextBlock advances with the
// block tip to unblock PushAppHash only when data is durable.
// Errors propagate vertically (kill the component).
// AppQCs are persisted only when their AppHash matches local execution.
//
// Cursors seed from BlockStore.Status() so data staged before Run is persisted.
// In-memory block/QC/AppProposal/AppQC eviction is driven by persisted SuffixRange
// changes. A QC is persisted only once all its blocks are available.
func (s *State) runPersist(ctx context.Context, writeAppProposal func(*types.AppProposal) error) error {
	registry, err := s.cfg.App.EpochRegistry()
	if err != nil {
		return fmt.Errorf("fetch epoch registry: %w", err)
	}
	for {
		var blocks []*types.RoadBlock
		var appProposals []*types.AppProposal
		var appQCs []*types.AppQC
		var status blockstore.SuffixRange
		for inner, ctrl := range s.inner.Lock() {
			status = inner.persisted
			// Wait until there is anything to persist.
			if err := ctrl.WaitUntil(ctx, func() bool {
				return status.NextBlock < inner.nextFullBlock ||
					status.NextAppProposal < inner.nextAppProposal ||
					status.NextAppQC < inner.nextAppQC
			}); err != nil {
				return err
			}
			// Collect data to persist.
			for status.NextBlock < inner.nextFullBlock {
				e := inner.blocks[status.NextBlock]
				blocks = append(blocks, e.FullBlock().OrPanic("bad full block"))
				status.NextBlock += 1
			}
			for status.NextAppProposal < inner.nextAppProposal {
				appProposals = append(appProposals, inner.appProposals[status.NextAppProposal])
				status.NextAppProposal += 1
			}
			for status.NextAppQC < inner.nextAppQC {
				n := status.NextAppQC
				appQC := inner.appQCs[n]
				if got, want := inner.appProposals[n].AppHash(), appQC.Proposal().AppHash(); !bytes.Equal(got, want) {
					return fmt.Errorf("%w at block %v: local AppHash = %v, quorum AppHash = %v", ErrAppHashDivergence, n, got, want)
				}
				appQCs = append(appQCs, appQC)
				status.First = status.NextAppQC
				status.NextAppQC += 1
			}
		}
		// Persist complete QC groups before their app records.
		for _, block := range blocks {
			if err := s.blockStore.WriteBlock(block); err != nil {
				return fmt.Errorf("write block %d: %w", block.QC().Height(), err)
			}
		}
		for _, appProposal := range appProposals {
			if err := writeAppProposal(appProposal); err != nil {
				return fmt.Errorf("write AppProposal %d: %w", appProposal.Height(), err)
			}
		}
		for _, appQC := range appQCs {
			if err := s.blockStore.WriteAppQC(appQC); err != nil {
				return fmt.Errorf("write AppQC %d: %w", appQC.Proposal().Height(), err)
			}
		}
		// Flush the new data.
		if err := s.blockStore.Flush(); err != nil {
			return fmt.Errorf("flush BlockStore: %w", err)
		}
		epochs, ok := registry.ForAnchor(status.First)
		if !ok {
			// A missing anchor pair requires a fresh application snapshot. Missing epochs
			// are fatal: returning from runPersist propagates the failure to the node.
			registry, err = s.cfg.App.EpochRegistry()
			if err != nil {
				return fmt.Errorf("fetch epoch registry: %w", err)
			}
			epochs, ok = registry.ForAnchor(status.First)
			if !ok {
				return fmt.Errorf("epoch registry not ready for road %d: %w", status.First, types.ErrNotFound)
			}
		}
		for inner, ctrl := range s.inner.Lock() {
			inner.persisted = status
			// The anchor moves when the persisted low end advances, or when no anchor is
			// set yet and status persists an AppQC.
			if inner.first < status.First || (!inner.anchor.Load().First.IsPresent() && status.First < status.NextAppQC) {
				inner.setAnchor(epochs)
			}
			t := time.Now()
			for inner.first < inner.persisted.First {
				e := inner.blocks[inner.first]
				for _, b := range e.blocks {
					latency := t.Sub(b.Payload().CreatedAt()).Seconds()
					s.metrics.BlockLatency.Evict.Observe(latency)
					s.metrics.TxLatency.Evict.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
				}
				delete(inner.byHash, e.qc.Header().Hash())
				inner.laneBlockCount -= uint64(len(e.qc.Headers()))
				delete(inner.blocks, inner.first)
				delete(inner.appQCs, inner.first)
				delete(inner.appProposals, inner.first)
				inner.first += 1
			}
			s.metrics.NextBlock.Evict.Set(utils.Clamp[int64](inner.first))
			ctrl.Updated()
		}
	}
}

// setAnchor sets the anchor to the persisted first road and its two epochs.
func (i *inner) setAnchor(epochs [2]*types.Epoch) {
	a := Anchor{Epochs: epochs}
	if i.persisted.First < i.persisted.NextAppQC {
		a.First = utils.Some(AnchorFirst{
			CommitQC: i.blocks[i.persisted.First].qc.QC(),
			AppQC:    i.appQCs[i.persisted.First],
		})
	}
	i.anchor.Store(a)
}

// Run starts the background persistence loop.
func (s *State) Run(ctx context.Context) error {
	return s.runPersist(ctx, s.blockStore.WriteAppProposal)
}
