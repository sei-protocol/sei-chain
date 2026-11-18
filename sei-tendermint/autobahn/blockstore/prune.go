package blockstore

import (
	"fmt"

	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Pruning retains the latest complete application row as a recovery anchor.

var _ controllertypes.PrunableStore = (*Store)(nil)

// reclaimable reports whether a key is eligible for reclamation below watermark.
// Hash aliases are always eligible; their primary record and numbered aliases govern retention.
func (k ledgerKey) reclaimable(watermark types.RoadIndex) (bool, error) {
	var index types.RoadIndex
	switch k.kind {
	case kindBlock:
		key, err := k.decodeBlock()
		if err != nil {
			return false, err
		}
		index = key.Index
	case kindQC:
		key, err := k.decodeQC()
		if err != nil {
			return false, err
		}
		index = key.Index
	case kindAppQC:
		key, err := k.decodeAppQC()
		if err != nil {
			return false, err
		}
		index = key.Index
	case kindAppProp:
		key, err := k.decodeAppProposal()
		if err != nil {
			return false, err
		}
		index = key.Index
	case kindLaneBlock:
		key, err := k.decodeLaneBlock()
		if err != nil {
			return false, err
		}
		index = key.Index
	case kindBlockHash:
		_, err := k.decodeBlockHash()
		return err == nil, err
	default:
		return false, fmt.Errorf("unknown ledger key kind %d", k.kind)
	}
	return index < watermark, nil
}

// gcFilter reports whether a ledger key is below the reclamation floor.
func (s *Store) gcFilter(key []byte, _ bool) (bool, error) {
	parsed, err := decodeLedgerKey(key)
	if err != nil {
		return false, fmt.Errorf("decodeLedgerKey(): %w", err)
	}
	return parsed.reclaimable(s.First())
}

func (s *Store) PruneBefore(blockHeight types.RoadIndex) error {
	for status := range s.status.Lock() {

		current, ok := status.suffix.Get()
		if !ok {
			// Ignore prune requests if we've not got any data yet. Simplifies several edge cases
			// and is technically a legal implementation of the contract in the godocs.
			return nil
		}

		blockHeight = min(blockHeight, current.First)

		s.setPruneWatermark(blockHeight)
		return nil
	}
	panic("unreachable")
}

// setPruneWatermark raises the floor below which reads return ErrPruned to n, and
// leaves it unchanged when it already sits at or above n.
func (s *Store) setPruneWatermark(n types.RoadIndex) {
	for {
		current := s.first.Load()
		if uint64(n) <= current {
			return
		}
		if s.first.CompareAndSwap(current, uint64(n)) {
			Global.pruneWatermarkAt().Set(utils.Clamp[int64](n))
			return
		}
	}
}

func (s *Store) First() types.RoadIndex {
	return types.RoadIndex(s.first.Load())
}

// Name identifies the store in the garbage collector's logs and errors.
func (s *Store) Name() string { return "BlockDB" }

// ExternalPruning is unconditionally true: this store has no pruner of its own.
func (s *Store) ExternalPruning() bool { return true }

// PruneHistory advances the retention watermark to blockNumber, capping it at the newest retained
// (block, QC) pair. Reclaiming the data happens later, on the block database's own schedule.
func (s *Store) PruneHistory(blockNumber uint64) error {
	return s.PruneBefore(types.RoadIndex(blockNumber))
}

// PruneSnapshots does nothing: the block store keeps no snapshots.
func (s *Store) PruneSnapshots(uint64) error { return nil }

// GetRollbackFloor returns head - rollbackWindow, measured against this store's own head. It returns
// 0 — nothing here is eligible for pruning — when the window is deeper than the whole history, which
// includes the empty store, or when the head cannot be read.
func (s *Store) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, err := s.GetLatestBlock()
	if err != nil || head <= rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

// GetLatestBlock returns the newest block number written, or 0 when none has been. It reports the
// written status rather than the flushed one, so a block a crash would lose still counts as ingested.
//
// Road indices can start at 0, so a store holding only that block is
// indistinguishable from an empty one.
func (s *Store) GetLatestBlock() (uint64, error) {
	for status := range s.status.Lock() {
		if current, ok := status.suffix.Get(); ok && s.First() < current.NextBlock {
			return uint64(current.NextBlock - 1), nil
		}
	}
	return 0, nil
}
