package littblock

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// qcReader is the slice of littdb.Table that readQCCovering needs, so tests can
// supply QCs without building a table.
type qcReader interface {
	Get(key []byte) ([]byte, bool, error)
}

// readQCCovering point-reads and decodes the QC covering n. Every covered
// number carries a QC alias key holding the full QC value, so any number inside
// a retained range resolves.
func readQCCovering(table qcReader, n types.GlobalBlockNumber) (*types.FullCommitQC, error) {
	value, exists, err := table.Get(qcKey(n))
	if err != nil {
		return nil, fmt.Errorf("failed to read covering QC for %d: %w", n, err)
	}
	if !exists {
		return nil, fmt.Errorf("corrupt store: no QC record at %d despite coverage past it", n)
	}
	qc, err := decodeQC(value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode covering QC for %d: %w", n, err)
	}
	return qc, nil
}
