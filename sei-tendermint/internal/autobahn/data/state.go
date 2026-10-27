package data

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const qcsCacheSize = 100

// ErrOutOfOrder is returned when PushAppHash receives an app hash past the
// next CommitQC range waiting for execution.
var ErrOutOfOrder = errors.New("out of order")

// ErrAppHashDivergence is returned when a quorum-certified AppHash differs from
// the one local execution produced.
var ErrAppHashDivergence = errors.New("AppHash divergence")

// Config is the config for the data State.
type Config struct {
	// Registry is the authoritative source of committee and stake information.
	Registry *epoch.Registry
}

// blockEntry contains a FullCommitQC, its lane blocks, and its verifying epoch.
type blockEntry struct {
	qc         *types.FullCommitQC
	epoch      *types.Epoch
	laneBlocks map[uint64]*types.Block
}

func (e *blockEntry) isFull() bool {
	return len(e.laneBlocks) == len(e.qc.Headers())
}

func (e *blockEntry) superBlock() utils.Option[*types.SuperBlock] {
	if !e.isFull() {
		return utils.None[*types.SuperBlock]()
	}
	blocks := make([]*types.Block, len(e.laneBlocks))
	for i := range blocks {
		blocks[i] = e.laneBlocks[uint64(i)]
	}
	return utils.Some(types.NewSuperBlock(e.qc, blocks))
}

type inner struct {
	blocks       map[types.RoadIndex]*blockEntry        // [first, nextPartialBlock)
	appProposals map[types.RoadIndex]*types.AppProposal // [first, nextAppProposal)
	appQCs       map[types.RoadIndex]*types.AppQC       // [first, nextAppQC)
	byHash       map[common.Hash]types.RoadIndex        // complete SuperBlocks retained in blocks

	// first is the inclusive low end of retained in-memory state: maps keep [first, next*).
	// Advanced by runPersist(). Durable copies below first live in BlockStore.
	// Heights below first have a persisted AppQC that matched local execution.
	// first <= nextAppQC <= nextAppProposal <= nextFullBlock <= nextPartialBlock
	first            types.RoadIndex
	nextAppQC        types.RoadIndex
	nextAppProposal  types.RoadIndex
	nextFullBlock    types.RoadIndex
	nextPartialBlock types.RoadIndex
	persisted        types.SuffixRange

	// Anchor represents the highest fully processed row:
	// CommitQC, Blocks, AppProposal, AppQC present and persisted.
	anchor utils.AtomicSend[utils.Option[Anchor]]
	// nextCommitEpoch covers nextPartialBlock once that epoch is registered; otherwise
	// the previous publish stands (genesis epoch 0 before any CommitQC).
	nextCommitEpoch utils.AtomicSend[*types.Epoch]
}

func (i *inner) publishNextCommitEpoch(registry *epoch.Registry) {
	ep, err := registry.EpochAt(i.nextPartialBlock)
	if err != nil {
		return
	}
	if ep.EpochIndex() == i.nextCommitEpoch.Load().EpochIndex() {
		return
	}
	i.nextCommitEpoch.Store(ep)
}

// insertQC verifies and inserts a FullCommitQC into the inner state.
// Already retained QCs are skipped; future QCs must have index nextPartialBlock.
func (i *inner) insertQC(registry *epoch.Registry, qc *types.FullCommitQC) error {
	n := qc.QC().Index()
	if n < i.nextPartialBlock {
		return nil // fully behind, skip
	}
	if n != i.nextPartialBlock {
		return fmt.Errorf("QC gap: got %d, want %d", n, i.nextPartialBlock)
	}
	e, err := registry.EpochByIndex(qc.QC().Proposal().EpochIndex())
	if err != nil {
		return err
	}
	if err := qc.Verify(e); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	i.insertVerifiedQC(registry, qc, e)
	return nil
}

// insertVerifiedQC records a verified QC and its empty SuperBlock, if any.
func (i *inner) insertVerifiedQC(registry *epoch.Registry, qc *types.FullCommitQC, ep *types.Epoch) {
	n := qc.Index()
	i.blocks[n] = &blockEntry{qc: qc, epoch: ep, laneBlocks: make(map[uint64]*types.Block, len(qc.Headers()))}
	i.nextPartialBlock += 1
	i.publishNextCommitEpoch(registry)
}

func (i *inner) insertAppQC(appQC *types.AppQC) error {
	n := appQC.Proposal().Index()
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

// appQCCertifiesRoad reports whether road has an AppQC matching local execution.
// Roads below first are already certified by a persisted matching AppQC.
func (i *inner) appQCCertifiesRoad(road types.RoadIndex) bool {
	if road < i.first {
		return true
	}
	if road >= i.nextAppQC {
		return false
	}
	return bytes.Equal(i.appProposals[road].AppHash(), i.appQCs[road].Proposal().AppHash())
}

func (i *inner) insertAppProposal(appProposal *types.AppProposal) error {
	n := appProposal.Index()
	if n < i.nextAppProposal {
		return nil
	}
	if n > i.nextPartialBlock {
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

// insertBlock inserts a pre-verified block into the inner state.
// Requires a QC to already be present for block n. Callers must verify
// payload integrity before calling; insertBlock matches the header against
// the stored FullCommitQC (unlike insertQC, which verifies the QC itself).
//
// insertBlock does NOT advance nextFullBlock — callers should call
// updateNextBlock after inserting one or more blocks. This separation
// allows batch insertion (e.g. PushQC inserts multiple blocks, then
// advances nextFullBlock once).
func (i *inner) insertBlock(n types.LaneBlockPos, block *types.Block) error {
	if n.Index < i.nextFullBlock || n.Index >= i.nextPartialBlock {
		return nil
	}
	e := i.blocks[n.Index]
	if _, ok := e.laneBlocks[n.Offset]; ok {
		return nil
	}
	if n.Offset >= uint64(len(e.qc.Headers())) {
		return fmt.Errorf("invalid lane block position %v", n)
	}
	if want, got := e.qc.Headers()[n.Offset].Hash(), block.Header().Hash(); want != got {
		return fmt.Errorf("block %v header hash mismatch: want %v, got %v", n, want, got)
	}
	e.laneBlocks[n.Offset] = block
	return nil
}

func (i *inner) updateNextFullBlock(m utils.Option[*metrics.Metrics]) {
	t := time.Now()
	for i.nextFullBlock < i.nextPartialBlock {
		e := i.blocks[i.nextFullBlock]
		b, ok := e.superBlock().Get()
		if !ok {
			break
		}
		i.byHash[b.CalculateBlockHash()] = e.qc.Index()
		if m, ok := m.Get(); ok {
			for _, b := range e.laneBlocks {
				latency := t.Sub(b.Payload().CreatedAt()).Seconds()
				m.BlockLatency.Receive.Observe(latency)
				m.TxLatency.Receive.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
			}
		}
		i.nextFullBlock += 1
	}
	if m, ok := m.Get(); ok {
		m.NextBlock.Receive.Set(utils.Clamp[int64](i.nextFullBlock))
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
	m.NextBlock.QC.Set(utils.Clamp[int64](inner.nextPartialBlock))
	m.NextBlock.Receive.Set(utils.Clamp[int64](inner.nextFullBlock))
	m.NextBlock.Execute.Set(utils.Clamp[int64](inner.nextAppProposal))
	m.NextBlock.Certify.Set(utils.Clamp[int64](inner.nextAppQC))
	m.NextBlock.Evict.Set(utils.Clamp[int64](inner.first))
	s := &State{
		cfg:        cfg,
		metrics:    m,
		inner:      utils.NewWatch(inner),
		blockStore: blockStore,
	}
	if err := s.activateStagedEpoch(); err != nil {
		return nil, fmt.Errorf("activateStagedEpoch: %w", err)
	}
	return s, nil
}

// loadFromBlockStore replays the persisted suffix from blockStore into s.inner.
// Called from NewState before any goroutines are spawned.
func loadFromBlockStore(cfg *Config, blockStore types.BlockStore) (*inner, error) {
	suffix, err := blockStore.ReadSuffix()
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadSuffix(): %w", err)
	}
	firstBlock := cfg.Registry.FirstRoad()
	status := suffix.Status.Or(types.SuffixRange{
		First:           firstBlock,
		NextQC:          firstBlock,
		NextAppProposal: firstBlock,
		NextAppQC:       firstBlock,
	})
	// Empty-chain default; loaded QCs publish the epoch of their successor.
	genesis, err := cfg.Registry.EpochByIndex(0)
	if err != nil {
		return nil, fmt.Errorf("missing genesis epoch: %w", err)
	}
	inner := &inner{
		blocks:           map[types.RoadIndex]*blockEntry{},
		appQCs:           map[types.RoadIndex]*types.AppQC{},
		appProposals:     map[types.RoadIndex]*types.AppProposal{},
		byHash:           map[common.Hash]types.RoadIndex{},
		first:            status.First,
		nextAppProposal:  status.First,
		nextAppQC:        status.First,
		nextFullBlock:    status.First,
		nextPartialBlock: status.First,
		persisted:        status,
		anchor:           utils.NewAtomicSend(utils.None[Anchor]()),
		nextCommitEpoch:  utils.NewAtomicSend(genesis),
	}
	for _, group := range suffix.QCGroups {
		qc := group.QC
		if err := inner.insertQC(cfg.Registry, qc); err != nil {
			return nil, fmt.Errorf("load QC from BlockStore: %w", err)
		}
	}
	for _, group := range suffix.QCGroups {
		for i, block := range group.Blocks {
			n := types.LaneBlockPos{Index: group.QC.Index(), Offset: uint64(i)} //nolint:gosec // non-negative slice index
			if n.Index < inner.first {
				continue
			}
			if err := block.Verify(); err != nil {
				return nil, fmt.Errorf("verify block %v from BlockStore: %w", n, err)
			}
			if err := inner.insertBlock(n, block); err != nil {
				return nil, fmt.Errorf("insert block %v from BlockStore: %w", n, err)
			}
		}
	}
	// Advance nextFullBlock through contiguous loaded blocks.
	inner.updateNextFullBlock(utils.None[*metrics.Metrics]())
	for _, appProposal := range suffix.AppProposals {
		if err := inner.insertAppProposal(appProposal); err != nil {
			return nil, fmt.Errorf("load AppProposal from BlockStore: %w", err)
		}
		inner.publishNextCommitEpoch(cfg.Registry)
	}
	for _, appQC := range suffix.AppQCs {
		if err := inner.insertAppQC(appQC); err != nil {
			return nil, fmt.Errorf("load AppQC from BlockStore: %w", err)
		}
	}
	inner.setAnchor()
	return inner, nil
}

func (s *State) First() types.RoadIndex {
	return max(s.blockStore.First(), s.cfg.Registry.FirstRoad())
}

// Registry returns the epoch registry.
func (s *State) Registry() *epoch.Registry { return s.cfg.Registry }

// activateStagedEpoch publishes the staged committee when LastRoad of the
// deriving epoch has a matching AppQC, or when that road is already below first.
func (s *State) activateStagedEpoch() error {
	idx, ok := s.cfg.Registry.Pending().Get()
	if !ok {
		return nil
	}
	for inner := range s.inner.Lock() {
		endEpoch := idx - 2
		if !inner.appQCCertifiesRoad(s.cfg.Registry.LastRoad(endEpoch)) {
			return nil
		}
		break
	}
	if err := s.cfg.Registry.ActivateEpoch(idx); err != nil {
		return fmt.Errorf("activate epoch %d: %w", idx, err)
	}
	for inner := range s.inner.Lock() {
		inner.publishNextCommitEpoch(s.cfg.Registry)
		return nil
	}
	panic("unreachable")
}

// PushQC pushes FullCommitQC and a subset of blocks that were finalized by it.
// Pushing the qc and blocks is atomic, so that no unnecessary GetBlock RPCs are issued.
// Even if the qc was already pushed earlier, the blocks are pushed anyway.
func (s *State) PushQC(ctx context.Context, qc *types.FullCommitQC, blocks map[uint64]*types.Block) error {
	n := qc.Index()
	nextFullBlock, nextPartialBlock, err := func() (types.RoadIndex, types.RoadIndex, error) {
		for inner, ctrl := range s.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				return n <= min(inner.nextPartialBlock, inner.first+qcsCacheSize-1)
			}); err != nil {
				return 0, 0, err
			}
			return inner.nextFullBlock, inner.nextPartialBlock, nil
		}
		panic("unreachable")
	}()
	if err != nil {
		return err
	}
	if n < nextFullBlock || len(blocks) == 0 && n < nextPartialBlock {
		return nil
	}
	var ep *types.Epoch
	if n == nextPartialBlock {
		ep, err = s.cfg.Registry.EpochByIndex(qc.QC().Proposal().EpochIndex())
		if err != nil {
			return err
		}
		if err := qc.Verify(ep); err != nil {
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
		if inner.nextPartialBlock == n {
			updated = true
			inner.insertVerifiedQC(s.cfg.Registry, qc, ep)
			s.metrics.NextBlock.QC.Set(utils.Clamp[int64](inner.nextPartialBlock))
		}
		e := inner.blocks[n]
		hs := e.qc.Headers()
		for i, b := range blocks {
			if _, ok := e.laneBlocks[i]; !ok && i < uint64(len(hs)) && hs[i].Hash() == b.Header().Hash() {
				updated = true
				e.laneBlocks[i] = b
			}
		}
		if updated {
			inner.updateNextFullBlock(utils.Some(s.metrics))
			ctrl.Updated()
		}
	}
	return nil
}

// QC returns the FullCommitQC proving finality of the block n.
func (s *State) QC(ctx context.Context, n types.RoadIndex) (*types.FullCommitQC, error) {
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
// The QC for n must already be present (guaranteed by PushQC ordering), unless
// the height is already in the contiguous block prefix (n < nextFullBlock) — in
// that case the block is dropped silently (already stored or executed/evicted).
func (s *State) PushBlock(ctx context.Context, n types.LaneBlockPos, block *types.Block) error {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n.Index < inner.nextPartialBlock }); err != nil {
			return err
		}
		if n.Index < inner.nextFullBlock {
			return nil
		}
	}
	if err := block.Verify(); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if err := inner.insertBlock(n, block); err != nil {
			return err
		}
		inner.updateNextFullBlock(utils.Some(s.metrics))
		ctrl.Updated()
	}
	return nil
}

// NextBlock returns the index of the next block to be pushed.
func (s *State) NextBlock() types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.nextFullBlock
	}
	panic("unreachable")
}

// NextAppQC returns the index of the next AppQC to be pushed.
func (s *State) NextAppQC() types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.nextAppQC
	}
	panic("unreachable")
}

// SuperBlockByHash returns a retained complete SuperBlock, or None for an unknown hash.
// Complete SuperBlocks ahead of an incomplete road are served from memory.
func (s *State) SuperBlockByHash(hash common.Hash) (utils.Option[*types.SuperBlock], error) {
	for inner := range s.inner.Lock() {
		if n, ok := inner.byHash[hash]; ok {
			return utils.Some(inner.blocks[n].superBlock().OrPanic("hash without superblock")), nil
		}
	}
	return s.blockStore.ReadSuperBlockByHash(hash)
}

// Block returns the lane block at position n.
// Waits until the contiguous prefix reaches n (n < nextFullBlock), then returns
// it from memory or BlockStore. Does not expose gaps ahead of nextFullBlock.
// This function is used to sync individual lane blocks.
func (s *State) Block(ctx context.Context, n types.LaneBlockPos) (*types.Block, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n.Index < inner.nextFullBlock }); err != nil {
			return nil, err
		}
		if n.Index < inner.first {
			break
		}
		b, ok := inner.blocks[n.Index].laneBlocks[n.Offset]
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
func (s *State) TryBlock(n types.LaneBlockPos) (*types.Block, error) {
	for inner := range s.inner.Lock() {
		if n.Index >= inner.nextPartialBlock {
			return nil, types.ErrNotFound
		}
		if n.Index < inner.first {
			break
		}
		b, ok := inner.blocks[n.Index].laneBlocks[n.Offset]
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
func (s *State) TryQC(n types.RoadIndex) (*types.FullCommitQC, error) {
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
		if n.Index < inner.nextFullBlock {
			return false
		}
		if inner.nextPartialBlock <= n.Index {
			return true
		}
		e := inner.blocks[n.Index]
		_, ok := e.laneBlocks[n.Offset]
		return n.Offset < uint64(len(e.qc.Headers())) && !ok
	}
	panic("unreachable")
}

// SuperBlock waits for all lane blocks through road n and returns their merged transactions.
// Evicted roads are read from BlockStore; pruned roads return ErrPruned.
func (s *State) SuperBlock(ctx context.Context, n types.RoadIndex) (*types.SuperBlock, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextFullBlock }); err != nil {
			return nil, err
		}
		if inner.first <= n {
			return inner.blocks[n].superBlock().OrPanic("bad full block"), nil
		}
	}
	opt, err := s.blockStore.ReadSuperBlockByNumber(n)
	if err != nil {
		return nil, err
	}
	if b, ok := opt.Get(); ok {
		return b, nil
	}
	return nil, types.ErrPruned
}

func (s *State) blockFromDB(n types.LaneBlockPos) (*types.Block, error) {
	opt, err := s.blockStore.ReadLaneBlock(n)
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadLaneBlock(%v): %w", n, err)
	}
	b, ok := opt.Get()
	if !ok {
		if n.Index < s.blockStore.First() {
			return nil, types.ErrPruned
		}
		return nil, types.ErrNotFound
	}
	return b, nil
}

func (s *State) qcFromDB(n types.RoadIndex) (*types.FullCommitQC, error) {
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

func (s *State) appQCFromDB(n types.RoadIndex) (*types.AppQC, error) {
	opt, err := s.blockStore.ReadAppQC(n)
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadAppQC(%d): %w", n, err)
	}
	if appQC, ok := opt.Get(); ok {
		return appQC, nil
	}
	return nil, types.ErrPruned
}

// PushAppHash records the AppHash for executed blocks through n and stages
// C_{E+2} when n closes LastRoad(E).
func (s *State) PushAppHash(ctx context.Context, n types.RoadIndex, hash types.AppHash, weights map[types.PublicKey]uint64) error {
	var endEpoch utils.Option[types.EpochIndex]
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextFullBlock }); err != nil {
			return err
		}
		if n < inner.nextAppProposal {
			if n >= inner.first {
				endEpoch = s.cfg.Registry.ClosingEpoch(n)
			}
			break
		}
		if n != inner.nextAppProposal {
			return fmt.Errorf("received appHash for %v: %w", n, ErrOutOfOrder)
		}
		e := inner.blocks[n]
		proposal := types.NewAppProposal(e.qc.QC().Proposal(), hash)
		t := time.Now()
		for _, b := range e.laneBlocks {
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
		endEpoch = s.cfg.Registry.ClosingEpoch(n)
		ctrl.Updated()
		// CRITICAL: We need to persist AppHash before we return and start executing the next block,
		// otherwise we lose the apphash on restart.
		// TODO(gprusak): this is a temporary measure, until AppHashes are persisted outside of BlockStore.
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.persisted.NextAppProposal }); err != nil {
			return err
		}
	}
	if e, ok := endEpoch.Get(); ok {
		if err := s.cfg.Registry.StageEpoch(e, weights); err != nil {
			return fmt.Errorf("StageEpoch(%d): %w", e, err)
		}
	}
	return s.activateStagedEpoch()
}

func (s *State) PushGasUsed(gasUsed int64) {
	s.metrics.GasUsed.Add(gasUsed)
}

// AppVote returns an appVote for a block >= n.
// Vote is available ONLY once AppHash has been pushed AND persisted.
// This prevents any possible equivocation and ensures that local node has the executed blocks persisted (since nextAppProposa <= nextFullBlock).
func (s *State) AppVote(ctx context.Context, n types.RoadIndex) (*types.AppVote, error) {
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
	n := appQC.Proposal().Index()
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
		for _, b := range inner.blocks[n].laneBlocks {
			latency := t.Sub(b.Payload().CreatedAt()).Seconds()
			s.metrics.BlockLatency.Certify.Observe(latency)
			s.metrics.TxLatency.Certify.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
		}
		s.metrics.NextBlock.Certify.Set(utils.Clamp[int64](inner.nextAppQC))
		ctrl.Updated()
		break
	}
	return s.activateStagedEpoch()
}

func (s *State) AppQC(ctx context.Context, n types.RoadIndex) (*types.AppQC, error) {
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
	// Epoch of CommitQC.
	Epoch *types.Epoch
}

// Anchor is the AppQC/CommitQC covering inner.first.
//
// TODO: replace with AvailSpec = Option[Anchor] + nextEpoch (None + first
// epoch initially) so avail does not consult the registry.
func (s *State) Anchor() utils.AtomicRecv[utils.Option[Anchor]] {
	for inner := range s.inner.Lock() {
		return inner.anchor.Subscribe()
	}
	panic("unreachable")
}

// NextCommitEpoch returns the epoch covering the road after the latest CommitQC
// in blocks, or genesis epoch 0 when blocks is empty. If that epoch is not registered
// yet, the previous value stands. Used by giga EVM tx sharding.
func (s *State) NextCommitEpoch() utils.AtomicRecv[*types.Epoch] {
	for inner := range s.inner.Lock() {
		return inner.nextCommitEpoch.Subscribe()
	}
	panic("unreachable")
}

func (i *inner) nextToExecute(lane types.LaneID) types.BlockNumber {
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
func (s *State) PruneBefore(retainFrom types.RoadIndex) error {
	return s.blockStore.PruneBefore(retainFrom)
}

// runPersist is a background goroutine that persists QCs, blocks,
// AppProposals, and AppQCs to BlockStore. It waits for in-memory data to advance
// past the SuffixRange persistence cursor, then writes each stream in cursor
// order and flushes once per batch. persisted.NextQC advances with the
// block tip to unblock PushAppHash only when data is durable.
// Errors propagate vertically (kill the component).
// AppQCs are persisted only when their AppHash matches local execution.
//
// Cursors seed from BlockStore.Status() so data staged before Run is persisted.
// In-memory block/QC/AppProposal/AppQC eviction is driven by persisted SuffixRange
// changes. A QC is persisted only once all its blocks are available.
func (s *State) runPersist(ctx context.Context) error {
	for {
		var qcs []*types.FullCommitQC
		var blocks [][]*types.Block
		var appProposals []*types.AppProposal
		var appQCs []*types.AppQC
		var status types.SuffixRange
		for inner, ctrl := range s.inner.Lock() {
			status = inner.persisted
			// Wait until there is anything to persist.
			if err := ctrl.WaitUntil(ctx, func() bool {
				return status.NextQC < inner.nextFullBlock ||
					status.NextAppProposal < inner.nextAppProposal ||
					status.NextAppQC < inner.nextAppQC
			}); err != nil {
				return err
			}
			// Collect data to persist.
			for status.NextQC < inner.nextFullBlock {
				e := inner.blocks[status.NextQC]
				qcs = append(qcs, e.qc)
				bs := make([]*types.Block, len(e.laneBlocks))
				for i := range bs {
					bs[i] = e.laneBlocks[uint64(i)]
				}
				blocks = append(blocks, bs)
				status.NextQC += 1
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
		for i, qc := range qcs {
			if err := s.blockStore.WriteQC(qc, blocks[i]); err != nil {
				return fmt.Errorf("write QC %d: %w", qc.QC().Index(), err)
			}
		}
		for _, appProposal := range appProposals {
			if err := s.blockStore.WriteAppProposal(appProposal); err != nil {
				return fmt.Errorf("write AppProposal %d: %w", appProposal.Index(), err)
			}
		}
		for _, appQC := range appQCs {
			if err := s.blockStore.WriteAppQC(appQC); err != nil {
				return fmt.Errorf("write AppQC %d: %w", appQC.Proposal().Index(), err)
			}
		}
		// Flush the new data.
		if err := s.blockStore.Flush(); err != nil {
			return fmt.Errorf("flush BlockStore: %w", err)
		}
		// Prune the inner state.
		var pruneEpoch utils.Option[types.EpochIndex]
		for inner, ctrl := range s.inner.Lock() {
			inner.persisted = status
			t := time.Now()
			from := inner.first
			for inner.first < inner.persisted.First {
				e := inner.blocks[inner.first]
				for _, b := range e.laneBlocks {
					latency := t.Sub(b.Payload().CreatedAt()).Seconds()
					s.metrics.BlockLatency.Evict.Observe(latency)
					s.metrics.TxLatency.Evict.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
				}
				delete(inner.byHash, e.superBlock().OrPanic("bad full block").CalculateBlockHash())
				delete(inner.blocks, inner.first)
				delete(inner.appQCs, inner.first)
				delete(inner.appProposals, inner.first)
				inner.first += 1
			}
			s.metrics.NextBlock.Evict.Set(utils.Clamp[int64](inner.first))
			inner.setAnchor()
			if from < inner.first {
				a, ok := inner.anchor.Load().Get()
				if !ok {
					return fmt.Errorf("evict advanced first but Anchor is None")
				}
				pruneEpoch = utils.Some(a.Epoch.EpochIndex())
			}
			ctrl.Updated()
		}
		if idx, ok := pruneEpoch.Get(); ok {
			if err := s.cfg.Registry.PruneBefore(idx); err != nil {
				return fmt.Errorf("prune epoch registry before %d: %w", idx, err)
			}
		}
	}
}

func (i *inner) setAnchor() {
	if i.first < i.persisted.NextAppQC {
		entry := i.blocks[i.first]
		i.anchor.Store(utils.Some(Anchor{
			CommitQC: entry.qc.QC(),
			AppQC:    i.appQCs[i.first],
			Epoch:    entry.epoch,
		}))
	}
}

// Run starts the background persistence loop.
func (s *State) Run(ctx context.Context) error {
	return s.runPersist(ctx)
}
