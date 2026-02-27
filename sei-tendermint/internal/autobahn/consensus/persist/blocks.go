// TODO: Block file persistence is a temporary solution that will be replaced by
// a WAL (Write-Ahead Log) library before launch. CommitQC file persistence
// (commitqcs.go) shares the same migration plan. With a WAL, atomic appends
// eliminate several complexities in both files:
//   - Gap detection / contiguous prefix truncation in loadAll (WAL replay is
//     always contiguous).
//   - Corrupt file handling (WAL handles its own integrity).
//   - Per-file naming, parsing, and directory scanning.
//   - Orphaned file cleanup (WAL truncation replaces DeleteBefore).
//
// What survives: the AtomicSend tips and the BlockPersister abstraction
// (PersistBlock/DeleteBefore/Tips contract).

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

	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// LoadedBlock is a block loaded from disk during state restoration.
type LoadedBlock struct {
	Number   types.BlockNumber
	Proposal *types.Signed[*types.LaneProposal]
}

// BlockPersister manages individual block files in a blocks/ subdirectory.
// Each block is stored as <lane_hex>_<blocknum>.pb.
// The caller is responsible for driving persistence (typically a goroutine that
// watches in-memory block state and calls PersistBlock / DeleteBefore / StoreTips).
// When noop is true, all disk I/O is skipped but cursor/tips tracking still works.
type BlockPersister struct {
	dir  string // full path to the blocks/ subdirectory; empty when noop
	noop bool
	// tips publishes the highest persisted block number + 1 (exclusive upper
	// bound) per lane as an immutable map snapshot. Updated via StoreTips
	// after each successful persist.
	tips utils.AtomicSend[map[types.LaneID]types.BlockNumber]
}

// NewNoOpBlockPersister returns a BlockPersister that skips all disk I/O
// but still tracks tips. Used when persistence is disabled.
func NewNoOpBlockPersister() *BlockPersister {
	return &BlockPersister{
		noop: true,
		tips: utils.NewAtomicSend(map[types.LaneID]types.BlockNumber{}),
	}
}

// NewBlockPersister creates the blocks/ subdirectory if it doesn't exist and
// returns a block persister. Loads all persisted blocks from disk as sorted,
// contiguous slices per lane. Gaps from corrupt or missing files are resolved
// by truncating at the first gap; blocks after the gap will be re-fetched.
func NewBlockPersister(stateDir string) (*BlockPersister, map[types.LaneID][]LoadedBlock, error) {
	dir := filepath.Join(stateDir, "blocks")
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
	initial := make(map[types.LaneID]types.BlockNumber, len(blocks))
	for lane, bs := range blocks {
		if len(bs) > 0 {
			initial[lane] = bs[len(bs)-1].Number + 1
		}
	}
	bp.tips = utils.NewAtomicSend(initial)
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
			log.Warn().Err(err).Str("path", path).Msg("failed to delete block file")
		}
	}
	return nil
}

// PersistBatch persists a batch of blocks to disk, updates tips after each
// successful write, and cleans up old files below laneFirsts.
// Returns the updated tips snapshot.
func (bp *BlockPersister) PersistBatch(
	cur map[types.LaneID]types.BlockNumber,
	batch []*types.Signed[*types.LaneProposal],
	laneFirsts map[types.LaneID]types.BlockNumber,
) (map[types.LaneID]types.BlockNumber, error) {
	for _, proposal := range batch {
		h := proposal.Msg().Block().Header()
		if err := bp.PersistBlock(proposal); err != nil {
			return cur, fmt.Errorf("persist block %s/%d: %w", h.Lane(), h.BlockNumber(), err)
		}
		next := maps.Clone(cur)
		next[h.Lane()] = h.BlockNumber() + 1
		cur = next
		bp.tips.Store(cur)
	}
	if err := bp.DeleteBefore(laneFirsts); err != nil {
		return cur, fmt.Errorf("deleteBefore: %w", err)
	}
	return cur, nil
}

// LoadTips returns the current tips snapshot.
func (bp *BlockPersister) LoadTips() map[types.LaneID]types.BlockNumber {
	return bp.tips.Load()
}

// loadAll loads all persisted blocks from the blocks/ directory.
// Returns sorted, contiguous slices per lane (truncated at the first gap).
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
			log.Warn().Err(err).Str("file", entry.Name()).Msg("skipping unrecognized block file")
			continue
		}
		proposal, err := loadBlockFile(filepath.Join(bp.dir, entry.Name()))
		if err != nil {
			// Corrupt or partially-written file (e.g. crash mid-write).
			// Skip it — the block will be re-received from peers or re-produced.
			log.Warn().Err(err).Str("file", entry.Name()).Msg("skipping corrupt block file")
			continue
		}
		// Verify the block's header matches the filename.
		h := proposal.Msg().Block().Header()
		if h.Lane() != lane || h.BlockNumber() != n {
			log.Warn().
				Str("file", entry.Name()).
				Stringer("headerLane", h.Lane()).
				Uint64("headerNum", uint64(h.BlockNumber())).
				Stringer("filenameLane", lane).
				Uint64("filenameNum", uint64(n)).
				Msg("skipping block file with mismatched header")
			continue
		}
		if raw[lane] == nil {
			raw[lane] = map[types.BlockNumber]*types.Signed[*types.LaneProposal]{}
		}
		raw[lane][n] = proposal
		log.Info().Str("lane", lane.String()).Uint64("block", uint64(n)).Msg("loaded persisted block")
	}

	result := map[types.LaneID][]LoadedBlock{}
	for lane, bs := range raw {
		sorted := slices.Sorted(maps.Keys(bs))
		var contiguous []LoadedBlock
		for i, n := range sorted {
			if i > 0 && n != sorted[i-1]+1 {
				log.Warn().
					Str("lane", lane.String()).
					Uint64("gapAt", uint64(sorted[i-1]+1)).
					Int("skipped", len(sorted)-i).
					Msg("truncating loaded blocks at gap; remaining will be re-fetched")
				break
			}
			contiguous = append(contiguous, LoadedBlock{Number: n, Proposal: bs[n]})
		}
		result[lane] = contiguous
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
