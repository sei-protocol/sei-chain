// TODO: Block file persistence is a temporary solution that will be replaced by
// a WAL (Write-Ahead Log) library before launch. CommitQC file persistence
// (commitqcs.go) shares the same migration plan. With a WAL, atomic appends
// eliminate several complexities in both files:
//   - Corrupt file handling (WAL handles its own integrity).
//   - Per-file naming, parsing, and directory scanning.
//   - Orphaned file cleanup (WAL truncation replaces DeleteBefore).
//   - Gap handling in newInner (WAL replay is always contiguous).
//
// What survives: the BlockPersister abstraction (PersistBlock/DeleteBefore).

package persist

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"log/slog"

	"google.golang.org/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "internal", "autobahn", "consensus", "persist")

// LoadedBlock is a block loaded from disk during state restoration.
type LoadedBlock struct {
	Number   types.BlockNumber
	Proposal *types.Signed[*types.LaneProposal]
}

// BlockPersister manages individual block files in a blocks/ subdirectory.
// Each block is stored as <lane_hex>_<blocknum>.pb.
// The caller is responsible for driving persistence (typically a goroutine that
// watches in-memory block state and calls PersistBlock / DeleteBefore).
// When noop is true, all disk I/O is skipped.
type BlockPersister struct {
	dir  string // full path to the blocks/ subdirectory; empty when noop
	noop bool
}

// newNoOpBlockPersister returns a BlockPersister that skips all disk I/O.
// Used when persistence is disabled.
func newNoOpBlockPersister() *BlockPersister {
	return &BlockPersister{noop: true}
}

// NewBlockPersister creates the blocks/ subdirectory if it doesn't exist and
// returns a block persister. Loads all persisted blocks from disk as sorted
// slices per lane. Corrupt files are skipped; the caller (newInner) returns
// an error if the resulting slices are non-contiguous.
// When stateDir is None, returns a no-op persister that skips all disk I/O.
func NewBlockPersister(stateDir utils.Option[string]) (*BlockPersister, map[types.LaneID][]LoadedBlock, error) {
	sd, ok := stateDir.Get()
	if !ok {
		return newNoOpBlockPersister(), nil, nil
	}
	dir := filepath.Join(sd, "blocks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, fmt.Errorf("create blocks dir %s: %w", dir, err)
	}

	bp := &BlockPersister{
		dir: dir,
	}
	blocks, err := bp.loadAll()
	if err != nil {
		return nil, nil, err
	}
	return bp, blocks, nil
}

func blockFilename(lane types.LaneID, n types.BlockNumber) string {
	return hex.EncodeToString(lane.Bytes()) + "_" + strconv.FormatUint(uint64(n), 10) + ".pb"
}

func parseBlockFilename(name string) (types.LaneID, types.BlockNumber, error) {
	name = strings.TrimSuffix(name, ".pb")
	parts := strings.SplitN(name, "_", 2)
	if len(parts) != 2 {
		return types.PublicKey{}, 0, fmt.Errorf("bad block filename %q", name)
	}
	keyBytes, err := hex.DecodeString(parts[0])
	if err != nil {
		return types.PublicKey{}, 0, fmt.Errorf("bad lane hex in %q: %w", name, err)
	}
	lane, err := types.PublicKeyFromBytes(keyBytes)
	if err != nil {
		return types.PublicKey{}, 0, fmt.Errorf("bad lane key in %q: %w", name, err)
	}
	n, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return types.PublicKey{}, 0, fmt.Errorf("bad block number in %q: %w", name, err)
	}
	return lane, types.BlockNumber(n), nil
}

// PersistBlock writes a signed lane proposal to its own file.
func (bp *BlockPersister) PersistBlock(proposal *types.Signed[*types.LaneProposal]) error {
	if bp.noop {
		return nil
	}
	h := proposal.Msg().Block().Header()
	pb := types.SignedMsgConv[*types.LaneProposal]().Encode(proposal)
	data, err := proto.Marshal(pb)
	if err != nil {
		return fmt.Errorf("marshal block %s/%d: %w", h.Lane(), h.BlockNumber(), err)
	}
	path := filepath.Join(bp.dir, blockFilename(h.Lane(), h.BlockNumber()))
	return writeAndSync(path, data)
}

// DeleteBefore removes persisted block files that are no longer needed.
// For lanes in laneFirsts, deletes files with block number below the map value.
// For lanes NOT in laneFirsts (orphaned from a previous committee/epoch),
// deletes all files — old blocks are not reusable after a committee change.
// An empty/nil laneFirsts is a no-op (no committee info available to judge orphans).
// Returns an error if the directory cannot be read; individual file removal
// failures are logged but do not cause an error.
func (bp *BlockPersister) DeleteBefore(laneFirsts map[types.LaneID]types.BlockNumber) error {
	if bp.noop || len(laneFirsts) == 0 {
		return nil
	}
	entries, err := os.ReadDir(bp.dir)
	if err != nil {
		return fmt.Errorf("list blocks dir for cleanup: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pb") {
			continue
		}
		lane, fileN, err := parseBlockFilename(entry.Name())
		if err != nil {
			continue
		}
		first, ok := laneFirsts[lane]
		if ok && fileN >= first {
			continue
		}
		path := filepath.Join(bp.dir, entry.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			logger.Warn("failed to delete block file", "path", path, "err", err)
		}
	}
	return nil
}

// loadAll loads all persisted blocks from the blocks/ directory.
// Returns sorted slices per lane. Corrupt files are skipped; the caller
// (newInner) returns an error on gaps or parent-hash mismatches.
func (bp *BlockPersister) loadAll() (map[types.LaneID][]LoadedBlock, error) {
	entries, err := os.ReadDir(bp.dir)
	if err != nil {
		return nil, fmt.Errorf("read blocks dir %s: %w", bp.dir, err)
	}

	raw := map[types.LaneID]map[types.BlockNumber]*types.Signed[*types.LaneProposal]{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pb") {
			continue
		}
		lane, n, err := parseBlockFilename(entry.Name())
		if err != nil {
			logger.Warn("skipping unrecognized block file", "file", entry.Name(), "err", err)
			continue
		}
		proposal, err := loadBlockFile(filepath.Join(bp.dir, entry.Name()))
		if err != nil {
			logger.Warn("skipping corrupt block file", "file", entry.Name(), "err", err)
			continue
		}
		h := proposal.Msg().Block().Header()
		if h.Lane() != lane || h.BlockNumber() != n {
			logger.Warn("skipping block file with mismatched header",
				"file", entry.Name(),
				"headerLane", h.Lane(),
				slog.Uint64("headerNum", uint64(h.BlockNumber())),
				"filenameLane", lane,
				slog.Uint64("filenameNum", uint64(n)),
			)
			continue
		}
		if raw[lane] == nil {
			raw[lane] = map[types.BlockNumber]*types.Signed[*types.LaneProposal]{}
		}
		raw[lane][n] = proposal
		logger.Info("loaded persisted block", "lane", lane.String(), slog.Uint64("block", uint64(n)))
	}

	result := map[types.LaneID][]LoadedBlock{}
	for lane, bs := range raw {
		sorted := slices.Sorted(maps.Keys(bs))
		blocks := make([]LoadedBlock, 0, len(sorted))
		for _, n := range sorted {
			blocks = append(blocks, LoadedBlock{Number: n, Proposal: bs[n]})
		}
		result[lane] = blocks
	}
	return result, nil
}

func loadBlockFile(path string) (*types.Signed[*types.LaneProposal], error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is constructed from operator-configured stateDir + hardcoded filename; not user-controlled
	if err != nil {
		return nil, err
	}
	conv := types.SignedMsgConv[*types.LaneProposal]()
	return conv.Unmarshal(data)
}
