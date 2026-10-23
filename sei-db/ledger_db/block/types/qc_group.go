package types

import (
	"fmt"
	"math"
)

// Validate checks whether the QC group fits in one stored value.
func (qc QCGroupRecord) Validate() error {
	if len(qc.Blocks) == 0 {
		return fmt.Errorf("QC qc requires blocks")
	}
	size := uint64(4) + uint64(len(qc.QC.Data))
	for _, block := range qc.Blocks {
		if block.Number == math.MaxUint64 {
			return fmt.Errorf("block number has no exclusive upper bound")
		}
		size += 12 + uint64(len(block.Value))
	}
	if size >= math.MaxUint32 {
		return fmt.Errorf("QC qc exceeds value size limit")
	}
	return nil
}
