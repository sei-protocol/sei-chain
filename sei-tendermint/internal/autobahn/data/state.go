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

const blocksCacheSize = 4000

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

// qcEntry is a FullCommitQC stored in qcs, with the epoch used to verify it.
// The epoch is resolved once at insertQC (or load); later PushBlock / AppQC
// paths use this pointer instead of querying the registry again.
type qcEntry struct {
	qc    *types.FullCommitQC
	epoch *types.Epoch
}

type inner struct {
	qcs              map[types.RoadIndex]qcEntry            // [first, nextQC)
	blocks           map[types.LaneBlockPos]*types.Block    // [first, nextBlock) + gap-fills in [nextBlock, nextQC)
	appProposals     map[types.RoadIndex]*types.AppProposal // [first, nextAppProposal)
	appQCs           map[types.RoadIndex]*types.AppQC       // [first, nextAppQC)
	superBlockHashes map[common.Hash]types.RoadIndex        // superBlockHashes mirrors blocks (insertBlock / setPersisted)

	// first is the inclusive low end of retained in-memory state: maps keep [first, next*).
	// Advanced by runPersist(). Durable copies below first live in BlockStore.
	// Heights below first have a persisted AppQC that matched local execution.
	// first <= nextAppQC <= nextAppProposal <= nextBlock <= nextQC
	first            types.RoadIndex
	nextAppQC        types.RoadIndex
	nextAppProposal  types.RoadIndex
	nextBlock        types.RoadIndex
	nextQC           types.RoadIndex
	persisted        types.SuffixRange
	cachedLaneBlocks uint64

	// Anchor represents the highest fully processed row:
	// CommitQC, Blocks, AppProposal, AppQC present and persisted.
	anchor utils.AtomicSend[utils.Option[Anchor]]
	// nextCommitEpoch covers nextQC once that epoch is registered; otherwise
	// the previous publish stands (genesis epoch 0 before any CommitQC).
	nextCommitEpoch utils.AtomicSend[*types.Epoch]
}

func (i *inner) publishNextCommitEpoch(registry *epoch.Registry) {
	ep, err := registry.EpochAt(i.nextQC)
	if err != nil {
		return
	}
	if ep.EpochIndex() == i.nextCommitEpoch.Load().EpochIndex() {
		return
	}
	i.nextCommitEpoch.Store(ep)
}

// insertQC verifies and inserts a FullCommitQC into the inner state.
// Already retained QCs are skipped; future QCs must have index nextQC.
func (i *inner) insertQC(registry *epoch.Registry, qc *types.FullCommitQC) error {
	index, next := qc.QC().Index(), qc.QC().Next()
	if next <= i.nextQC {
		return nil // fully behind, skip
	}
	if index > i.nextQC {
		return fmt.Errorf("QC gap: expected first<=%d, got %d", i.nextQC, index)
	}
	e, err := registry.EpochByIndex(qc.QC().Proposal().EpochIndex())
	if err != nil {
		return err
	}
	if err := qc.Verify(e); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	i.qcs[index] = qcEntry{qc: qc, epoch: e}
	i.cachedLaneBlocks += uint64(len(qc.Headers()))
	i.nextQC = next
	i.publishNextCommitEpoch(registry)
	return nil
}

func (i *inner) insertAppQC(appQC *types.AppQC) error {
	index, next := appQC.Proposal().Index(), appQC.Proposal().Next()
	if next <= i.nextAppQC {
		return nil
	}
	if next > i.nextAppProposal {
		return fmt.Errorf("missing AppProposal for this AppQC")
	}
	if index > i.nextAppQC {
		return fmt.Errorf("AppQC gap: expected first<=%d, got %d", i.nextAppQC, index)
	}
	ep := i.qcs[i.nextAppQC].epoch
	if err := appQC.Verify(ep.Committee()); err != nil {
		return fmt.Errorf("appQC.Verify(): %w", err)
	}
	i.appQCs[index] = appQC
	i.nextAppQC = next
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
	index, next := appProposal.Index(), appProposal.Next()
	if next <= i.nextAppProposal {
		return nil
	}
	if next > i.nextQC {
		return fmt.Errorf("missing CommitQC for this AppProposal")
	}
	if index > i.nextAppProposal {
		return fmt.Errorf("AppProposal gap: expected first<=%d, got %d", i.nextAppProposal, index)
	}
	if err := appProposal.Verify(i.qcs[i.nextAppProposal].qc.QC()); err != nil {
		return fmt.Errorf("appProposal.Verify(): %w", err)
	}
	i.appProposals[index] = appProposal
	i.nextAppProposal = next
	return nil
}

// insertBlock inserts a pre-verified block into the inner state.
// Requires a QC to already be present for block n. Callers must verify
// payload integrity before calling; insertBlock matches the header against
// the stored FullCommitQC (unlike insertQC, which verifies the QC itself).
//
// insertBlock does NOT advance nextBlock — callers should call
// updateNextBlock after inserting one or more blocks. This separation
// allows batch insertion (e.g. PushQC inserts multiple blocks, then
// advances nextBlock once).
func (i *inner) insertBlock(n types.LaneBlockPos, block *types.Block) error {
	if n.Index < i.nextBlock || n.Index >= i.nextQC {
		return nil
	}
	if _, ok := i.blocks[n]; ok {
		return nil
	}
	qc := i.qcs[n.Index].qc
	if n.Offset >= uint64(len(qc.Headers())) {
		return fmt.Errorf("invalid lane block position %v", n)
	}
	if want, got := qc.Headers()[n.Offset].Hash(), block.Header().Hash(); want != got {
		return fmt.Errorf("block %v header hash mismatch: want %v, got %v", n, want, got)
	}
	i.blocks[n] = block
	if blocks, ok := i.completeBlocks(n.Index); ok {
		b := types.NewSuperBlock(qc, blocks)
		i.superBlockHashes[b.CalculateBlockHash()] = n.Index
	}
	return nil
}

// completeBlocks returns all lane blocks at n in header order when every block is present.
func (i *inner) completeBlocks(n types.RoadIndex) ([]*types.Block, bool) {
	qc := i.qcs[n].qc
	blocks := make([]*types.Block, len(qc.Headers()))
	for offset := range blocks {
		block, ok := i.blocks[types.LaneBlockPos{Index: n, Offset: uint64(offset)}]
		if !ok {
			return nil, false
		}
		blocks[offset] = block
	}
	return blocks, true
}

func (i *inner) updateNextBlock(m *metrics.Metrics) {
	t := time.Now()
	for i.nextBlock < i.nextQC {
		blocks, ok := i.completeBlocks(i.nextBlock)
		if !ok {
			break
		}
		for _, b := range blocks {
			latency := t.Sub(b.Payload().CreatedAt()).Seconds()
			m.BlockLatency.Receive.Observe(latency)
			m.TxLatency.Receive.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
		}
		i.nextBlock++
	}
	m.NextBlock.Receive.Set(utils.Clamp[int64](i.nextBlock))
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
	m.NextBlock.QC.Set(utils.Clamp[int64](inner.nextQC))
	m.NextBlock.Receive.Set(utils.Clamp[int64](inner.nextBlock))
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
		NextBlock:       firstBlock,
	})
	// Empty-chain default; loaded QCs publish the epoch of their successor.
	genesis, err := cfg.Registry.EpochByIndex(0)
	if err != nil {
		return nil, fmt.Errorf("missing genesis epoch: %w", err)
	}
	inner := &inner{
		qcs:              map[types.RoadIndex]qcEntry{},
		blocks:           map[types.LaneBlockPos]*types.Block{},
		appQCs:           map[types.RoadIndex]*types.AppQC{},
		appProposals:     map[types.RoadIndex]*types.AppProposal{},
		superBlockHashes: map[common.Hash]types.RoadIndex{},
		first:            status.First,
		nextAppProposal:  status.First,
		nextAppQC:        status.First,
		nextBlock:        status.First,
		nextQC:           status.First,
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
			prop := types.NewLaneProposal(block)
			if err := prop.Verify(); err != nil {
				return nil, fmt.Errorf("verify block %v from BlockStore: %w", n, err)
			}
			if err := inner.insertBlock(n, block); err != nil {
				return nil, fmt.Errorf("insert block %v from BlockStore: %w", n, err)
			}
		}
	}
	// Advance nextBlock through contiguous loaded blocks. Don't use
	// updateNextBlock: stale timestamps would skew metrics.
	inner.nextBlock = max(inner.first, status.NextBlock)
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

// insertBlocksByHash matches byHash against stored (already verified) QC
// headers over gr ∩ [nextBlock, nextQC) and inserts hits. Advances nextBlock
// when the contiguous prefix grows. Caller must hold inner's lock.
func (s *State) insertBlocksByHash(inner *inner, index types.RoadIndex, byHash map[types.BlockHeaderHash]*types.Block) error {
	if index < inner.nextBlock || index >= inner.nextQC {
		return nil
	}
	for offset, header := range inner.qcs[index].qc.Headers() {
		if b, ok := byHash[header.Hash()]; ok {
			if err := inner.insertBlock(types.LaneBlockPos{Index: index, Offset: uint64(offset)}, b); err != nil {
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
	index, next := qc.QC().Index(), qc.QC().Next()
	needQC, err := func() (bool, error) {
		for inner, ctrl := range s.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				return index <= inner.nextQC && (index < inner.nextQC || inner.cachedLaneBlocks < blocksCacheSize)
			}); err != nil {
				return false, err
			}
			return inner.nextQC == index, nil
		}
		panic("unreachable")
	}()
	if err != nil {
		return err
	}
	var ep *types.Epoch
	if needQC {
		ep, err = s.cfg.Registry.EpochByIndex(qc.QC().Proposal().EpochIndex())
		if err != nil {
			return err
		}
		if err := qc.Verify(ep); err != nil {
			return fmt.Errorf("qc.Verify(): %w", err)
		}
	}
	byHash := map[types.BlockHeaderHash]*types.Block{}
	for _, b := range blocks {
		byHash[b.Header().Hash()] = b
		if err := types.NewLaneProposal(b).Verify(); err != nil {
			return fmt.Errorf("block.Verify(): %w", err)
		}
	}
	// Atomically insert QC and blocks.
	for inner, ctrl := range s.inner.Lock() {
		if needQC {
			if inner.nextQC == index {
				inner.qcs[index] = qcEntry{qc: qc, epoch: ep}
				inner.cachedLaneBlocks += uint64(len(qc.Headers()))
				inner.nextQC = next
			}
			s.metrics.NextBlock.QC.Set(utils.Clamp[int64](inner.nextQC))
			inner.publishNextCommitEpoch(s.cfg.Registry)
			ctrl.Updated()
		}
		if len(byHash) > 0 {
			if err := s.insertBlocksByHash(inner, qc.Index(), byHash); err != nil {
				return err
			}
			ctrl.Updated()
		}
	}
	return nil
}

// QC returns the FullCommitQC proving finality of the block n.
func (s *State) QC(ctx context.Context, n types.RoadIndex) (*types.FullCommitQC, error) {
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
		return inner.qcs[n].qc, nil
	}
	return s.qcFromDB(n)
}

// PushBlock pushes block to the state.
// The QC for n must already be present (guaranteed by PushQC ordering), unless
// the height is already in the contiguous block prefix (n < nextBlock) — in
// that case the block is dropped silently (already stored or executed/evicted).
func (s *State) PushBlock(ctx context.Context, n types.LaneBlockPos, block *types.Block) error {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n.Index < inner.nextQC }); err != nil {
			return err
		}
		if n.Index < inner.nextBlock {
			return nil
		}
	}
	if err := types.NewLaneProposal(block).Verify(); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if err := inner.insertBlock(n, block); err != nil {
			return err
		}
		inner.updateNextBlock(s.metrics)
		ctrl.Updated()
	}
	return nil
}

// NextBlock returns the index of the next block to be pushed.
func (s *State) NextBlock() types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.nextBlock
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
		if n, ok := inner.superBlockHashes[hash]; ok {
			blocks, complete := inner.completeBlocks(n)
			if !complete {
				panic("hash indexed for incomplete SuperBlock")
			}
			return utils.Some(types.NewSuperBlock(inner.qcs[n].qc, blocks)), nil
		}
	}
	return s.blockStore.ReadSuperBlockByHash(hash)
}

// Block returns the lane block at position n.
// Waits until the contiguous prefix reaches n (n < nextBlock), then returns
// it from memory or BlockStore. Does not expose gaps ahead of nextBlock.
// This function is used to sync individual lane blocks.
func (s *State) Block(ctx context.Context, n types.LaneBlockPos) (*types.Block, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n.Index < inner.nextBlock }); err != nil {
			return nil, err
		}
		if n.Index < inner.first {
			break
		}
		b, ok := inner.blocks[n]
		if !ok {
			return nil, types.ErrNotFound
		}
		return b, nil
	}
	return s.blockFromDB(n)
}

// TryBlock returns the lane block at position n.
// Returns ErrNotFound if n is not yet in the contiguous prefix (n >= nextBlock),
// including gap-fills stored above nextBlock — same no-gap contract as Block.
// Returns ErrPruned if BlockStore no longer has an evicted height.
// Evicted-but-still-durable heights (n < nextBlock) load from BlockStore.
func (s *State) TryBlock(n types.LaneBlockPos) (*types.Block, error) {
	for inner := range s.inner.Lock() {
		if n.Index >= inner.nextBlock {
			return nil, types.ErrNotFound
		}
		if n.Index < inner.first {
			break
		}
		b, ok := inner.blocks[n]
		if !ok {
			return nil, types.ErrNotFound
		}
		return b, nil
	}
	return s.blockFromDB(n)
}

// TryQC returns the FullCommitQC covering global height n without waiting.
// Returns ErrNotFound if n is not yet covered (n >= nextQC).
// Returns ErrPruned if BlockStore no longer has an evicted height.
func (s *State) TryQC(n types.RoadIndex) (*types.FullCommitQC, error) {
	for inner := range s.inner.Lock() {
		if n >= inner.nextQC {
			return nil, types.ErrNotFound
		}
		if n < inner.first {
			break
		}
		return inner.qcs[n].qc, nil
	}
	return s.qcFromDB(n)
}

// NeedBlock reports whether catch-up still needs to fetch height n.
// False when n is already past nextBlock (including heights pruned or
// evicted from RAM) or an in-memory gap-fill is present. Unlike TryBlock,
// gap-fills count as satisfied so the fetcher does not keep re-requesting
// them while a lower contiguous hole is open. Does not consult BlockStore.
func (s *State) NeedBlock(n types.LaneBlockPos) bool {
	for inner := range s.inner.Lock() {
		if n.Index < inner.nextBlock {
			return false
		}
		_, ok := inner.blocks[n]
		return !ok
	}
	panic("unreachable")
}

// SuperBlock waits for all lane blocks through road n and returns their merged transactions.
// Evicted roads are read from BlockStore; pruned roads return ErrPruned.
func (s *State) SuperBlock(ctx context.Context, n types.RoadIndex) (*types.SuperBlock, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextBlock }); err != nil {
			return nil, err
		}
		if n < inner.first {
			break
		}
		blocks, ok := inner.completeBlocks(n)
		if !ok {
			panic("incomplete SuperBlock below nextBlock")
		}
		return types.NewSuperBlock(inner.qcs[n].qc, blocks), nil
	}
	opt, err := s.blockStore.ReadSuperBlockByNumber(n)
	if err != nil {
		return nil, err
	}
	b, ok := opt.Get()
	if !ok {
		return nil, types.ErrPruned
	}
	return b, nil
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

func (s *State) appQCFromDB(n types.RoadIndex) (*types.AppQC, error) {
	opt, err := s.blockStore.ReadAppQCByBlockNumber(n)
	if err != nil {
		return nil, fmt.Errorf("blockStore.ReadAppQCByBlockNumber(%d): %w", n, err)
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
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextBlock }); err != nil {
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
		proposal := types.NewAppProposal(inner.qcs[n].qc.QC().Proposal(), hash)
		blocks, ok := inner.completeBlocks(n)
		if !ok {
			panic("incomplete executed SuperBlock")
		}
		t := time.Now()
		for _, b := range blocks {
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
// This prevents any possible equivocation and ensures that local node has the executed blocks persisted (since nextAppProposa <= nextBlock).
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
	index, next := appQC.Proposal().Index(), appQC.Proposal().Next()
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return next <= inner.nextAppProposal
		}); err != nil {
			return err
		}
		if index < inner.nextAppQC {
			return nil
		}
		if err := inner.insertAppQC(appQC); err != nil {
			return err
		}
		t := time.Now()
		for n := index; n < next; n++ {
			blocks, _ := inner.completeBlocks(n)
			for _, b := range blocks {
				latency := t.Sub(b.Payload().CreatedAt()).Seconds()
				s.metrics.BlockLatency.Certify.Observe(latency)
				s.metrics.TxLatency.Certify.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
			}
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
// in qcs, or genesis epoch 0 when qcs is empty. If that epoch is not registered
// yet, the previous value stands. Used by giga EVM tx sharding.
func (s *State) NextCommitEpoch() utils.AtomicRecv[*types.Epoch] {
	for inner := range s.inner.Lock() {
		return inner.nextCommitEpoch.Subscribe()
	}
	panic("unreachable")
}

func (i *inner) nextToExecute(lane types.LaneID) types.BlockNumber {
	if i.nextAppProposal < i.nextQC {
		return i.qcs[i.nextAppProposal].qc.QC().LaneRange(lane).First()
	}
	if i.first < i.nextAppProposal {
		return i.qcs[i.nextAppProposal-1].qc.QC().LaneRange(lane).Next()
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
func (s *State) PruneBefore(retainFrom types.RoadIndex) error {
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
				if status.NextQC < inner.nextQC && inner.qcs[status.NextQC].qc.QC().Next() <= inner.nextBlock {
					return true
				}
				return status.NextAppProposal < inner.nextAppProposal || status.NextAppQC < inner.nextAppQC
			}); err != nil {
				return err
			}
			// Collect data to persist.
			for status.NextQC < inner.nextQC {
				qc := inner.qcs[status.NextQC].qc
				next := qc.QC().Next()
				if next > inner.nextBlock {
					break
				}
				covered, complete := inner.completeBlocks(qc.Index())
				if !complete {
					panic("incomplete persisted SuperBlock")
				}
				blocks = append(blocks, covered)
				qcs = append(qcs, qc)
				status.NextQC = next
				status.NextBlock = next
			}
			for status.NextAppProposal < inner.nextAppProposal {
				appProposal := inner.appProposals[status.NextAppProposal]
				appProposals = append(appProposals, appProposal)
				status.NextAppProposal = appProposal.Next()
			}
			for status.NextAppQC < inner.nextAppQC {
				n := status.NextAppQC
				appQC := inner.appQCs[n]
				if got, want := inner.appProposals[n].AppHash(), appQC.Proposal().AppHash(); !bytes.Equal(got, want) {
					return fmt.Errorf("%w at block %v: local AppHash = %v, quorum AppHash = %v", ErrAppHashDivergence, n, got, want)
				}
				appQCs = append(appQCs, appQC)
				status.NextAppQC = appQC.Proposal().Next()
				status.First = status.NextAppQC - 1
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
				n := inner.first
				blocks, complete := inner.completeBlocks(n)
				if !complete {
					panic("incomplete evicted SuperBlock")
				}
				block := types.NewSuperBlock(inner.qcs[n].qc, blocks)
				delete(inner.superBlockHashes, block.CalculateBlockHash())
				for offset, b := range blocks {
					latency := t.Sub(b.Payload().CreatedAt()).Seconds()
					s.metrics.BlockLatency.Evict.Observe(latency)
					s.metrics.TxLatency.Evict.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
					delete(inner.blocks, types.LaneBlockPos{Index: n, Offset: uint64(offset)})
				}
				inner.cachedLaneBlocks -= uint64(len(blocks))
				delete(inner.qcs, n)
				delete(inner.appQCs, n)
				delete(inner.appProposals, n)
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
		entry := i.qcs[i.first]
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
