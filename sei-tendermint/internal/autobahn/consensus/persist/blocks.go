// TODO: Block file persistence is a temporary solution that will be replaced by
// a WAL (Write-Ahead Log) library before launch. With a WAL, atomic appends
// eliminate several complexities in this file:
//   - Gap detection / contiguous prefix truncation in loadAll (WAL replay is
//     always contiguous).
//   - Corrupt file handling (WAL handles its own integrity).
//   - Per-block file naming, parsing, and directory scanning.
//   - Orphaned file cleanup (WAL truncation replaces DeleteBefore).
//   - The blocking Queue (holes become impossible with sequential atomic
//     appends, so dropping on overflow is safe).
//
// What survives: the async channel, the AtomicSend tips, and the
// BlockPersister abstraction (Queue/Run/Tips contract).

package persist

import (
	"context"
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
type BlockPersister struct {
	dir string // full path to the blocks/ subdirectory
	ch  chan persistJob
	// tips publishes the highest persisted block number + 1 (exclusive upper
	// bound) per lane as an immutable map snapshot. Updated after each
	// successful persist. Subscribers can watch for changes without callbacks.
	tips utils.AtomicSend[map[types.LaneID]types.BlockNumber]
}

type persistJob struct {
	proposal *types.Signed[*types.LaneProposal]
}

// persistQueueSize is the buffer for async block persistence. With 40 validators,
// a 1/3 Byzantine burst produces up to ~13 lanes × 30 blocks = 390 blocks at once.
// 512 covers that with margin.
const persistQueueSize = 512

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
		ch:  make(chan persistJob, persistQueueSize),
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
// Scans the directory once. Best-effort: logs warnings on individual failures.
func (bp *BlockPersister) DeleteBefore(laneFirsts map[types.LaneID]types.BlockNumber) {
	if len(laneFirsts) == 0 {
		return
	}
	entries, err := os.ReadDir(bp.dir)
	if err != nil {
		log.Warn().Err(err).Str("dir", bp.dir).Msg("failed to list blocks dir for cleanup")
		return
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
}

// Queue enqueues a block for async persistence. Blocks if the queue is full
// until space is available or ctx is cancelled. We must not drop blocks because
// the nextBlockToPersist cursor advances sequentially — a hole would stall voting
// on the affected lane until restart (which reconstructs the cursor from disk).
// TODO: add retry on persistence failure to avoid restart-only recovery.
func (bp *BlockPersister) Queue(ctx context.Context, proposal *types.Signed[*types.LaneProposal]) error {
	select {
	case bp.ch <- persistJob{proposal: proposal}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Tips returns a subscription to the persisted lane tips. Each value is an
// immutable map snapshot of lane -> highest persisted block number + 1
// (exclusive upper bound). Updated after each successful persist.
func (bp *BlockPersister) Tips() utils.AtomicRecv[map[types.LaneID]types.BlockNumber] {
	return bp.tips.Subscribe()
}

// Run drains the internal queue and fsyncs each block to disk.
// After each successful write, it publishes an updated tips snapshot via Tips().
// Blocks until ctx is cancelled.
func (bp *BlockPersister) Run(ctx context.Context) error {
	cur := bp.tips.Load()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case job := <-bp.ch:
			h := job.proposal.Msg().Block().Header()
			if err := bp.PersistBlock(job.proposal); err != nil {
				return fmt.Errorf("persist block %s/%d: %w", h.Lane(), h.BlockNumber(), err)
			}
			// Publish a new immutable snapshot with the updated tip.
			next := make(map[types.LaneID]types.BlockNumber, len(cur))
			for k, v := range cur {
				next[k] = v
			}
			next[h.Lane()] = h.BlockNumber() + 1
			cur = next
			bp.tips.Store(cur)
		}
	}
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
