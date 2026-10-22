package littblock

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// PutQC stores a QC and its blocks in one atomic LittDB value.
func (s *blockDB) PutQC(first uint64, qc []byte, blocks []blocktypes.BlockRecord) error {
	if len(blocks) == 0 || uint64(len(blocks)) > math.MaxUint64-first {
		return fmt.Errorf("invalid QC block range starting at %d with %d blocks", first, len(blocks))
	}
	size := uint64(8) + uint64(len(qc)) + 4*uint64(len(blocks))
	for _, block := range blocks {
		size += uint64(len(block.Value))
	}
	if size >= math.MaxUint32 {
		return fmt.Errorf("QC group exceeds LittDB value size limit: %d bytes", size)
	}

	// The value contains a block count followed by length-prefixed QC and block
	// bytes. Lookup keys address only the corresponding bytes, without framing.
	value := make([]byte, 0, int(size))                               //nolint:gosec // size is bounded by LittDB's uint32 value limit
	value = binary.BigEndian.AppendUint32(value, uint32(len(blocks))) //nolint:gosec // count fits within size
	value = binary.BigEndian.AppendUint32(value, uint32(len(qc)))     //nolint:gosec // length fits within size
	qcOffset := uint32(len(value))                                    //nolint:gosec // offset fits within size
	value = append(value, qc...)
	aliases := make([]*litttypes.SecondaryKey, 0, 3*len(blocks))
	for i := range blocks {
		aliases = append(aliases, &litttypes.SecondaryKey{
			Key: numberKey(kindQC, first+uint64(i)), Offset: qcOffset,
			Length: uint32(len(qc)), //nolint:gosec // length fits within size
		})
	}
	for i, block := range blocks {
		length := uint32(len(block.Value)) //nolint:gosec // length fits within size
		value = binary.BigEndian.AppendUint32(value, length)
		offset := uint32(len(value)) //nolint:gosec // offset fits within size
		value = append(value, block.Value...)
		aliases = append(aliases,
			&litttypes.SecondaryKey{Key: numberKey(kindBlock, first+uint64(i)), Offset: offset, Length: length},
			&litttypes.SecondaryKey{Key: blockHashKey(block.Hash), Offset: offset, Length: length})
	}
	if err := s.table.Put(numberKey(kindQCGroup, first), value, aliases...); err != nil {
		return fmt.Errorf("failed to put QC group at %d: %w", first, err)
	}
	return nil
}

// groupRecord is a logical record within a packed QC value.
type groupRecord struct {
	kind   blocktypes.RecordKind
	number uint64
	value  []byte
}

// decodeQCGroup returns a packed value's records in scan order.
func decodeQCGroup(first uint64, value []byte, newestFirst bool) ([]groupRecord, error) {
	if len(value) < 8 {
		return nil, fmt.Errorf("truncated QC group at %d", first)
	}
	count := uint64(binary.BigEndian.Uint32(value))
	if count == 0 || count > math.MaxUint64-first || count > uint64((len(value)-8)/4) {
		return nil, fmt.Errorf("invalid QC group block count %d at %d", count, first)
	}
	value = value[4:]
	records := make([]groupRecord, 0, count+1)
	for i := uint64(0); i <= count; i++ {
		if len(value) < 4 {
			return nil, fmt.Errorf("truncated QC group record %d at %d", i, first)
		}
		length := uint64(binary.BigEndian.Uint32(value))
		value = value[4:]
		if length > uint64(len(value)) {
			return nil, fmt.Errorf("truncated QC group value %d at %d", i, first)
		}
		kind, number := blocktypes.KindQC, first
		if i > 0 {
			kind, number = blocktypes.KindBlock, first+i-1
		}
		records = append(records, groupRecord{kind: kind, number: number, value: value[:length]})
		value = value[length:]
	}
	if len(value) != 0 {
		return nil, fmt.Errorf("trailing bytes in QC group at %d", first)
	}
	if newestFirst {
		slices.Reverse(records)
	}
	return records, nil
}
