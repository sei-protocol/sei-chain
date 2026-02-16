// TODO: Block file persistence is a temporary solution. It does not handle many
// corner cases (e.g. disk full, partial directory listings, orphaned files after
// lane changes, no garbage collection on unclean shutdown). This will be replaced
// by proper storage solution before launch.

package persist

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
)

// BlockPersister manages individual block files in a blocks/ subdirectory.
// Each block is stored as <lane_hex>_<blocknum>.pb.
type BlockPersister struct {
	dir string // full path to the blocks/ subdirectory
}

// NewBlockPersister creates the blocks/ subdirectory if it doesn't exist and
// returns a block persister. Loads all persisted blocks from disk.
func NewBlockPersister(stateDir string) (*BlockPersister, map[types.LaneID]map[types.BlockNumber]*types.Signed[*types.LaneProposal], error) {
	dir := filepath.Join(stateDir, "blocks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, fmt.Errorf("create blocks dir %s: %w", dir, err)
	}

	bp := &BlockPersister{dir: dir}
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
func (bp *BlockPersister) PersistBlock(lane types.LaneID, n types.BlockNumber, proposal *types.Signed[*types.LaneProposal]) error {
	pb := types.SignedMsgConv[*types.LaneProposal]().Encode(proposal)
	data, err := proto.Marshal(pb)
	if err != nil {
		return fmt.Errorf("marshal block %s/%d: %w", lane, n, err)
	}
	path := filepath.Join(bp.dir, blockFilename(lane, n))
	return WriteAndSync(path, data)
}

// DeleteBefore removes all persisted block files for lanes in the given map
// where the block number is below the map value. Scans the directory once.
// Best-effort: logs warnings on individual failures.
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
		if !ok || fileN >= first {
			continue
		}
		path := filepath.Join(bp.dir, entry.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", path).Msg("failed to delete block file")
		}
	}
}

// loadAll loads all persisted blocks from the blocks/ directory.
func (bp *BlockPersister) loadAll() (map[types.LaneID]map[types.BlockNumber]*types.Signed[*types.LaneProposal], error) {
	entries, err := os.ReadDir(bp.dir)
	if err != nil {
		return nil, fmt.Errorf("read blocks dir %s: %w", bp.dir, err)
	}

	result := map[types.LaneID]map[types.BlockNumber]*types.Signed[*types.LaneProposal]{}
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
		if result[lane] == nil {
			result[lane] = map[types.BlockNumber]*types.Signed[*types.LaneProposal]{}
		}
		result[lane][n] = proposal
		log.Info().Str("lane", lane.String()).Uint64("block", uint64(n)).Msg("loaded persisted block")
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
