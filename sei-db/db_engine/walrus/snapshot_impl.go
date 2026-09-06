package walrus

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// The directory name prefix of a retained snapshot, followed by its block number zero padded so that
// lexicographic order matches numeric order. This mirrors the naming the state store's snapshot manager uses.
const snapshotDirPrefix = "snapshot-"

// The number of digits a retained snapshot directory's block number is padded to.
const snapshotDirDigits = 20

// The subdirectory of an instance's path that holds the hard-linked snapshots it retains.
const snapshotsDirName = "snapshots"

// The block cache a retained snapshot's database is opened with.
//
// A snapshot is read only when a walk found nothing in any pod, and a ladder may hold many at once, so a
// cache sized for a busy database would cost far more memory across the ladder than it saves on the rare
// read that reaches one.
const snapshotCacheSize = 8 * unit.MB

// The bloom filter a snapshot's tables are read with.
//
// A reader resolves a table's filter by looking the name recorded in the table up among the policies its
// own options carry, so a snapshot opened without one ignores the filters its producer wrote. The bits per
// key are not part of that match and are not required to agree with the producer: the name is
// "rocksdb.BuiltinBloomFilter" whatever the value, and a probe takes its count from the filter block.
var snapshotFilterPolicy = bloom.FilterPolicy(10)

var _ Snapshot = (*pebbleSnapshot)(nil)

// pebbleSnapshot is a retained pebble checkpoint.
//
// The database is opened on the first read rather than when the handle is created, so a ladder of snapshots
// that are never reached costs nothing but the handle.
type pebbleSnapshot struct {
	blockNumber uint64
	path        string
	size        int64

	// Guards the lazily opened database. Reads take it only long enough to publish the handle.
	lock     sync.Mutex
	database *pebble.DB
	deleted  bool
}

// BlockNumber returns the block whose state this snapshot holds.
func (s *pebbleSnapshot) BlockNumber() uint64 {
	return s.blockNumber
}

// Get returns the value key held at the end of the snapshot's block.
func (s *pebbleSnapshot) Get(key []byte) (value []byte, found bool, err error) {
	database, err := s.open()
	if err != nil {
		return nil, false, err
	}

	stored, closer, err := database.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read %s: %w", s.path, err)
	}
	value = make([]byte, len(stored))
	copy(value, stored)
	if err := closer.Close(); err != nil {
		return nil, false, fmt.Errorf("failed to release a read of %s: %w", s.path, err)
	}
	return value, true, nil
}

// Path returns the directory the snapshot lives in.
func (s *pebbleSnapshot) Path() string {
	return s.path
}

// Size returns the bytes the snapshot occupies.
func (s *pebbleSnapshot) Size() int64 {
	return s.size
}

// Delete closes the snapshot's database and removes its directory.
func (s *pebbleSnapshot) Delete() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.deleted = true
	if s.database != nil {
		if err := s.database.Close(); err != nil {
			return fmt.Errorf("failed to close snapshot %s: %w", s.path, err)
		}
		s.database = nil
	}
	if err := os.RemoveAll(s.path); err != nil {
		return fmt.Errorf("failed to delete snapshot %s: %w", s.path, err)
	}
	return nil
}

// open returns the snapshot's database, opening it read only on first use.
func (s *pebbleSnapshot) open() (*pebble.DB, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.deleted {
		return nil, fmt.Errorf("snapshot %s has been deleted", s.path)
	}
	if s.database != nil {
		return s.database, nil
	}

	cache := pebble.NewCache(snapshotCacheSize)
	defer cache.Unref()
	database, err := pebble.Open(s.path, &pebble.Options{
		Cache:    cache,
		ReadOnly: true,
		Logger:   silentPebbleLogger{},
		Levels:   []pebble.LevelOptions{{FilterPolicy: snapshotFilterPolicy}},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open snapshot %s: %w", s.path, err)
	}
	s.database = database
	return database, nil
}

var _ Snapshot = pseudoSnapshot{}

// pseudoSnapshot is the floor of an instance that has retained no real snapshot yet.
//
// It reports every key absent, which is the right answer only while pods still cover every block above block
// zero. The catalog maintains that: it refuses to delete a pod until a real snapshot has taken over as the
// floor, so a walk can never fall through a hole and terminate here.
type pseudoSnapshot struct{}

// BlockNumber returns zero, the block a fresh instance's floor sits at.
func (pseudoSnapshot) BlockNumber() uint64 {
	return 0
}

// Get reports every key absent.
func (pseudoSnapshot) Get(_ []byte) (value []byte, found bool, err error) {
	return nil, false, nil
}

// Path returns the empty string, since nothing on disk backs this snapshot.
func (pseudoSnapshot) Path() string {
	return ""
}

// Size returns zero.
func (pseudoSnapshot) Size() int64 {
	return 0
}

// Delete does nothing.
func (pseudoSnapshot) Delete() error {
	return nil
}

// snapshotDirName returns the directory name a retained snapshot for the given block is stored under.
func snapshotDirName(blockNumber uint64) string {
	return fmt.Sprintf("%s%0*d", snapshotDirPrefix, snapshotDirDigits, blockNumber)
}

// parseSnapshotDirName reads the block number out of a retained snapshot's directory name.
func parseSnapshotDirName(name string) (blockNumber uint64, ok bool) {
	if !strings.HasPrefix(name, snapshotDirPrefix) {
		return 0, false
	}
	digits := name[len(snapshotDirPrefix):]
	if len(digits) != snapshotDirDigits {
		return 0, false
	}
	var parsed uint64
	for index := 0; index < len(digits); index++ {
		if digits[index] < '0' || digits[index] > '9' {
			return 0, false
		}
		parsed = parsed*10 + uint64(digits[index]-'0')
	}
	return parsed, true
}

// retainSnapshot hard-links a checkpoint into the instance's own tree and returns a handle to it.
//
// Hard links rather than a move are what decouple the two lifecycles: the producer keeps its directory and
// may delete it as soon as this returns, because both names point at the same inodes and the data survives
// until the last one is unlinked.
func retainSnapshot(root string, blockNumber uint64, source string) (*pebbleSnapshot, error) {
	destination := filepath.Join(root, snapshotDirName(blockNumber))
	if _, err := os.Stat(destination); err == nil {
		return nil, fmt.Errorf("a snapshot for block %d is already retained", blockNumber)
	}

	staging := destination + podPartialExtension
	if err := os.RemoveAll(staging); err != nil {
		return nil, fmt.Errorf("failed to clear %s: %w", staging, err)
	}
	size, err := hardLinkTree(source, staging)
	if err != nil {
		_ = os.RemoveAll(staging)
		return nil, err
	}
	if err := os.Rename(staging, destination); err != nil {
		_ = os.RemoveAll(staging)
		return nil, fmt.Errorf("failed to publish snapshot %s: %w", destination, err)
	}

	return &pebbleSnapshot{blockNumber: blockNumber, path: destination, size: size}, nil
}

// hardLinkTree recreates source's directory structure under destination, hard-linking every file, and
// reports the bytes those files occupy.
func hardLinkTree(source string, destination string) (int64, error) {
	var size int64
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("failed to relativize %s against %s: %w", path, source, err)
		}
		target := filepath.Join(destination, relative)

		if entry.IsDir() {
			if err := os.MkdirAll(target, 0o750); err != nil {
				return fmt.Errorf("failed to create %s: %w", target, err)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", path, err)
		}
		if err := os.Link(path, target); err != nil {
			return fmt.Errorf("failed to hard-link %s to %s (they must share a filesystem): %w",
				path, target, err)
		}
		size += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return size, nil
}

// openRetainedSnapshots lists the snapshots already retained under root, ascending by block, and clears any
// staging directory an interrupted retain left behind.
func openRetainedSnapshots(root string) ([]*pebbleSnapshot, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", root, err)
	}

	snapshots := make([]*pebbleSnapshot, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, podPartialExtension) {
			if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
				return nil, fmt.Errorf("failed to clear the interrupted retain %s: %w", name, err)
			}
			continue
		}
		blockNumber, ok := parseSnapshotDirName(name)
		if !ok {
			continue
		}
		path := filepath.Join(root, name)
		size, err := treeSize(path)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, &pebbleSnapshot{blockNumber: blockNumber, path: path, size: size})
	}

	sortSnapshotsByBlock(snapshots)
	return snapshots, nil
}

// treeSize reports the bytes the files under a directory occupy.
func treeSize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", path, err)
		}
		size += info.Size()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("failed to measure %s: %w", root, err)
	}
	return size, nil
}

// sortSnapshotsByBlock orders snapshots ascending by the block they hold.
func sortSnapshotsByBlock(snapshots []*pebbleSnapshot) {
	for index := 1; index < len(snapshots); index++ {
		position := index
		for position > 0 && snapshots[position].blockNumber < snapshots[position-1].blockNumber {
			snapshots[position], snapshots[position-1] = snapshots[position-1], snapshots[position]
			position--
		}
	}
}

// silentPebbleLogger discards what a snapshot's database has to say.
//
// Opening a checkpoint read only replays its write ahead log, and pebble narrates that at info level. A
// ladder reopens many snapshots over a run, and none of it tells the operator anything about the engine
// under measurement.
type silentPebbleLogger struct{}

// Infof discards the message.
func (silentPebbleLogger) Infof(_ string, _ ...any) {}

// Fatalf discards the message. Pebble only reaches this on a corrupt database, which surfaces as an error
// from the read that provoked it.
func (silentPebbleLogger) Fatalf(_ string, _ ...any) {}
