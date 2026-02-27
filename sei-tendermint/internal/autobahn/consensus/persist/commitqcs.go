// TODO: CommitQC file persistence is a temporary solution that will be replaced
// by the same WAL (Write-Ahead Log) library as block persistence (see blocks.go).
// With a WAL, atomic appends eliminate gap detection, corrupt file handling,
// per-file naming/parsing, directory scanning, and DeleteBefore cleanup.

package persist

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
)

// LoadedCommitQC is a CommitQC loaded from disk during state restoration.
type LoadedCommitQC struct {
	Index types.RoadIndex
	QC    *types.CommitQC
}

// CommitQCPersister manages individual CommitQC files in a commitqcs/ subdirectory.
// Each CommitQC is stored as <roadindex>.pb.
// The caller is responsible for driving persistence (typically a goroutine that
// watches in-memory state and calls PersistCommitQC / DeleteBefore).
// When noop is true, all disk I/O is skipped but cursor tracking still works.
type CommitQCPersister struct {
	dir  string // full path to the commitqcs/ subdirectory; empty when noop
	noop bool
	next types.RoadIndex
}

// NewNoOpCommitQCPersister returns a CommitQCPersister that skips all disk I/O
// but still tracks the next index. Used when persistence is disabled.
func NewNoOpCommitQCPersister() *CommitQCPersister {
	return &CommitQCPersister{noop: true}
}

// NewCommitQCPersister creates the commitqcs/ subdirectory if it doesn't exist
// and returns a persister. Loads all persisted CommitQCs from disk as a sorted,
// contiguous slice. Gaps from corrupt or missing files are resolved by truncating
// at the first gap; CommitQCs after the gap will be re-received from peers.
func NewCommitQCPersister(stateDir string) (*CommitQCPersister, []LoadedCommitQC, error) {
	dir := filepath.Join(stateDir, "commitqcs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, fmt.Errorf("create commitqcs dir %s: %w", dir, err)
	}

	cp := &CommitQCPersister{dir: dir}
	loaded, err := cp.loadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(loaded) > 0 {
		cp.next = loaded[len(loaded)-1].Index + 1
	}
	return cp, loaded, nil
}

// LoadNext returns the road index of the first CommitQC that has not been
// persisted (exclusive upper bound of what's on disk).
func (cp *CommitQCPersister) LoadNext() types.RoadIndex {
	return cp.next
}

func commitQCFilename(idx types.RoadIndex) string {
	return strconv.FormatUint(uint64(idx), 10) + ".pb"
}

func parseCommitQCFilename(name string) (types.RoadIndex, error) {
	name = strings.TrimSuffix(name, ".pb")
	n, err := strconv.ParseUint(name, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad commitqc filename %q: %w", name, err)
	}
	return types.RoadIndex(n), nil
}

// PersistCommitQC writes a CommitQC to its own file.
func (cp *CommitQCPersister) PersistCommitQC(qc *types.CommitQC) error {
	idx := qc.Index()
	if !cp.noop {
		data := types.CommitQCConv.Marshal(qc)
		path := filepath.Join(cp.dir, commitQCFilename(idx))
		if err := writeAndSync(path, data); err != nil {
			return fmt.Errorf("persist commitqc %d: %w", idx, err)
		}
	}
	if idx >= cp.next {
		cp.next = idx + 1
	}
	return nil
}

// DeleteBefore removes persisted CommitQC files with road index below idx.
// Returns an error if the directory cannot be read; individual file removal
// failures are logged but do not cause an error.
func (cp *CommitQCPersister) DeleteBefore(idx types.RoadIndex) error {
	if cp.noop || idx == 0 {
		return nil
	}
	entries, err := os.ReadDir(cp.dir)
	if err != nil {
		return fmt.Errorf("list commitqcs dir for cleanup: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pb") {
			continue
		}
		fileIdx, err := parseCommitQCFilename(entry.Name())
		if err != nil {
			continue
		}
		if fileIdx >= idx {
			continue
		}
		path := filepath.Join(cp.dir, entry.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", path).Msg("failed to delete commitqc file")
		}
	}
	return nil
}

// loadAll loads all persisted CommitQCs from the commitqcs/ directory.
// Returns a sorted, contiguous slice (truncated at the first gap).
func (cp *CommitQCPersister) loadAll() ([]LoadedCommitQC, error) {
	entries, err := os.ReadDir(cp.dir)
	if err != nil {
		return nil, fmt.Errorf("read commitqcs dir %s: %w", cp.dir, err)
	}

	raw := map[types.RoadIndex]*types.CommitQC{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pb") {
			continue
		}
		idx, err := parseCommitQCFilename(entry.Name())
		if err != nil {
			log.Warn().Err(err).Str("file", entry.Name()).Msg("skipping unrecognized commitqc file")
			continue
		}
		qc, err := loadCommitQCFile(filepath.Join(cp.dir, entry.Name()))
		if err != nil {
			log.Warn().Err(err).Str("file", entry.Name()).Msg("skipping corrupt commitqc file")
			continue
		}
		if qc.Index() != idx {
			log.Warn().
				Str("file", entry.Name()).
				Uint64("headerIdx", uint64(qc.Index())).
				Uint64("filenameIdx", uint64(idx)).
				Msg("skipping commitqc file with mismatched index")
			continue
		}
		raw[idx] = qc
		log.Info().Uint64("roadIndex", uint64(idx)).Msg("loaded persisted commitqc")
	}

	if len(raw) == 0 {
		return nil, nil
	}

	sorted := slices.Sorted(maps.Keys(raw))

	var contiguous []LoadedCommitQC
	for i, idx := range sorted {
		if i > 0 && idx != sorted[i-1]+1 {
			log.Warn().
				Uint64("gapAt", uint64(sorted[i-1]+1)).
				Int("skipped", len(sorted)-i).
				Msg("truncating loaded commitqcs at gap; remaining will be re-fetched")
			break
		}
		contiguous = append(contiguous, LoadedCommitQC{Index: idx, QC: raw[idx]})
	}
	return contiguous, nil
}

func loadCommitQCFile(path string) (*types.CommitQC, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is constructed from operator-configured stateDir + hardcoded filename; not user-controlled
	if err != nil {
		return nil, err
	}
	return types.CommitQCConv.Unmarshal(data)
}
