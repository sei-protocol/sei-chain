package types

import (
	"fmt"
	"math"
)

// Validate checks whether the QC group fits in one stored value.
func (qc QCGroupRecord) Validate() error {
	if len(qc.Blocks) == 0 {
		return fmt.Errorf("QC group requires blocks")
	}
	size := uint64(5) + uint64(len(qc.Data))
	for _, block := range qc.Blocks {
		if block.Number == math.MaxUint64 {
			return fmt.Errorf("block number has no exclusive upper bound")
		}
		size += 16 + uint64(len(block.Hash)) + uint64(len(block.Value))
	}
	if size >= math.MaxUint32 {
		return fmt.Errorf("QC group exceeds value size limit")
	}
	return nil
}
