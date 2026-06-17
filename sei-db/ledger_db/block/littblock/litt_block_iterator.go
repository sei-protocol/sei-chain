package littblock

import (
	"fmt"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

var (
	_ types.BlockIterator = (*blockIterator)(nil)
	_ types.QCIterator    = (*qcIterator)(nil)
)

// blockIterator wraps a litt iterator, skipping secondary (hash-alias) keys so
// it yields one entry per block.
type blockIterator struct {
	it littdb.Iterator
}

func (b *blockIterator) Next() (bool, error) {
	for {
		ok, err := b.it.Next()
		if err != nil {
			return false, fmt.Errorf("failed to advance blocks iterator: %w", err)
		}
		if !ok {
			return false, nil
		}
		if _, isPrimary := b.it.GetKey(); isPrimary {
			return true, nil
		}
	}
}

func (b *blockIterator) Number() types.GlobalBlockNumber {
	key, _ := b.it.GetKey()
	return decodeKey(key)
}

func (b *blockIterator) Block() (*types.Block, error) {
	value, err := b.it.GetValue()
	if err != nil {
		return nil, fmt.Errorf("failed to read block value: %w", err)
	}
	blk, err := decodeBlock(value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}
	return blk, nil
}

func (b *blockIterator) Close() error {
	if err := b.it.Close(); err != nil {
		return fmt.Errorf("failed to close blocks iterator: %w", err)
	}
	return nil
}

// qcIterator wraps a litt iterator, skipping secondary (covered-number) keys so
// it yields one entry per QC.
type qcIterator struct {
	it littdb.Iterator
}

func (q *qcIterator) Next() (bool, error) {
	for {
		ok, err := q.it.Next()
		if err != nil {
			return false, fmt.Errorf("failed to advance qcs iterator: %w", err)
		}
		if !ok {
			return false, nil
		}
		if _, isPrimary := q.it.GetKey(); isPrimary {
			return true, nil
		}
	}
}

func (q *qcIterator) QC() (*types.FullCommitQC, error) {
	value, err := q.it.GetValue()
	if err != nil {
		return nil, fmt.Errorf("failed to read QC value: %w", err)
	}
	qc, err := decodeQC(value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal QC: %w", err)
	}
	return qc, nil
}

func (q *qcIterator) Close() error {
	if err := q.it.Close(); err != nil {
		return fmt.Errorf("failed to close qcs iterator: %w", err)
	}
	return nil
}
