package littblock

import (
	"fmt"
	"math"

	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// PutQC stores a QC and its blocks in one atomic LittDB value.
func (s *blockDB) PutQC(first uint64, qc []byte, blocks []blocktypes.BlockRecord) error {
	if len(blocks) == 0 || uint64(len(blocks)) > math.MaxUint64-first {
		return fmt.Errorf("invalid QC block range starting at %d with %d blocks", first, len(blocks))
	}
	value, err := blocktypes.EncodeQCGroup(qc, blocks)
	if err != nil {
		return err
	}
	const qcOffset = 4
	aliases := make([]*litttypes.SecondaryKey, 0, 3*len(blocks))
	for i := range blocks {
		aliases = append(aliases, &litttypes.SecondaryKey{
			Key: numberKey(kindQC, first+uint64(i)), Offset: qcOffset,
			Length: uint32(len(qc)), //nolint:gosec // length fits within size
		})
	}
	offset := uint32(qcOffset + len(qc)) //nolint:gosec // offset fits within encoded value
	for i, block := range blocks {
		length := uint32(len(block.Value)) //nolint:gosec // length fits within size
		offset += 4
		aliases = append(aliases,
			&litttypes.SecondaryKey{Key: numberKey(kindBlock, first+uint64(i)), Offset: offset, Length: length},
			&litttypes.SecondaryKey{Key: blockHashKey(block.Hash), Offset: offset, Length: length})
		offset += length
	}
	if err := s.table.Put(numberKey(kindQCGroup, first), value, aliases...); err != nil {
		return fmt.Errorf("failed to put QC group at %d: %w", first, err)
	}
	return nil
}
