package evm

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	sssnapshot "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/snapshot"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// CatchUpFrom replays EVM changesets from wal onto this store, up to and including target.
func (s *EVMStateStore) CatchUpFrom(wal statewal.StateWAL, target int64) error {
	head := s.GetLatestVersion()
	if head >= target {
		return nil
	}
	if wal == nil {
		return fmt.Errorf("nil WAL cannot replay from %d to %d", head, target)
	}

	it, err := wal.Iterator(uint64(head+1), uint64(target)) //nolint:gosec // versions are non-negative
	if err != nil {
		return fmt.Errorf("WAL iterator [%d,%d]: %w", head+1, target, err)
	}
	defer func() { _ = it.Close() }()

	for {
		hasNext, nErr := it.Next()
		if nErr != nil {
			return fmt.Errorf("WAL iterate: %w", nErr)
		}
		if !hasNext {
			break
		}
		block, changesets := it.Entry()
		if err := s.applyReplayedBlock(int64(block), changesets); err != nil { //nolint:gosec // block heights fit within int64
			return err
		}
	}
	if got := s.GetLatestVersion(); got < target {
		if err := s.SetLatestVersion(target); err != nil {
			return fmt.Errorf("advance EVM state store head from %d to %d: %w", got, target, err)
		}
	}
	return nil
}

func (s *EVMStateStore) applyReplayedBlock(block int64, changesets []*proto.NamedChangeSet) error {
	evmChangesets := filterEVMChangesets(changesets)
	if len(evmChangesets) == 0 {
		return s.SetLatestVersion(block)
	}
	return s.ApplyChangesetSync(block, evmChangesets)
}

// RollbackTo restores the newest snapshot at or below target, then replays wal onto it.
func (s *EVMStateStore) RollbackTo(target int64, wal statewal.StateWAL) error {
	if target <= 0 {
		return fmt.Errorf("invalid rollback target %d", target)
	}
	base, err := s.rollbackBaseVersion(target)
	if err != nil {
		return err
	}
	if err := s.closeDBs(); err != nil {
		return fmt.Errorf("close EVM state store before rollback: %w", err)
	}
	if err := s.restoreSnapshot(base); err != nil {
		return fmt.Errorf("restore snapshot %d: %w", base, err)
	}
	if err := s.openDBs(); err != nil {
		return fmt.Errorf("reopen EVM state store after rollback: %w", err)
	}
	return s.CatchUpFrom(wal, target)
}

func (s *EVMStateStore) rollbackBaseVersion(target int64) (int64, error) {
	if s.snapshotMgr == nil {
		return 0, fmt.Errorf("no snapshot at or below the target")
	}
	versions, err := s.snapshotMgr.Versions()
	if err != nil {
		return 0, fmt.Errorf("list snapshots: %w", err)
	}
	var base int64
	for _, version := range versions {
		if version <= target {
			base = version
		}
	}
	if base == 0 {
		return 0, fmt.Errorf("no snapshot at or below the target")
	}
	return base, nil
}

func (s *EVMStateStore) restoreSnapshot(version int64) error {
	src := filepath.Join(s.snapshotMgr.Root(), sssnapshot.SnapshotDirName(version))
	if s.separateDBs {
		for _, storeType := range AllEVMStoreTypes() {
			if err := replacePebbleDir(subDBPath(src, storeType), subDBPath(s.dir, storeType)); err != nil {
				return err
			}
		}
		return nil
	}
	return replacePebbleDir(src, s.dir)
}

func replacePebbleDir(src, dst string) error {
	tmp := dst + ".restore-tmp"
	bak := dst + ".restore-bak"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.RemoveAll(bak); err != nil {
		return err
	}
	if err := utils.ClonePebbleDir(src, tmp); err != nil {
		return err
	}
	if err := os.Rename(dst, bak); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.RemoveAll(bak)
}
