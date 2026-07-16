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

// blockIterator wraps a litt iterator over the shared ledger table, yielding one
// entry per block: it skips QC keys and the secondary (hash-alias) keys, keeping
// only primary block-number keys. It also skips blocks strictly below watermark,
// which may be stranded from their covering QC (see blockDB.watermark); the
// watermark is captured when the iterator is created.
type blockIterator struct {
	it        littdb.Iterator
	watermark uint64
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
		key, isPrimary := b.it.GetKey()
		if !isPrimary || keyKind(key) != kindBlock {
			continue
		}
		if uint64(decodeNumberKey(key)) < b.watermark {
			continue
		}
		return true, nil
	}
}

func (b *blockIterator) Number() types.GlobalBlockNumber {
	key, _ := b.it.GetKey()
	return decodeNumberKey(key)
}

func (b *blockIterator) Block() (*types.Block, error) {
	value, err := b.it.GetValue()
	if err != nil {
		return nil, fmt.Errorf("failed to read block value: %w", err)
	}
	_, blk, err := decodeBlock(value)
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

// qcIterator wraps a litt iterator over the shared ledger table, yielding one
// entry per QC: it skips block keys and the secondary (covered-number) keys,
// keeping only primary QC keys. It also skips any QC whose entire covered range
// is strictly below watermark (Next <= watermark), since none of its blocks are
// served; a QC straddling the watermark still covers served blocks and is kept.
// The watermark is captured when the iterator is created.
type qcIterator struct {
	it        littdb.Iterator
	watermark uint64
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
		key, isPrimary := q.it.GetKey()
		if !isPrimary || keyKind(key) != kindQC {
			continue
		}
		// The First of this QC is its primary-key number. If First >= watermark
		// the whole range is served; otherwise decode the value to learn Next and
		// keep it only if it straddles the watermark (covers a served block).
		if uint64(decodeNumberKey(key)) >= q.watermark {
			return true, nil
		}
		qc, err := q.QC()
		if err != nil {
			return false, err
		}
		next := uint64(decodeNumberKey(key)) + uint64(len(qc.Headers()))
		if next > q.watermark {
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
