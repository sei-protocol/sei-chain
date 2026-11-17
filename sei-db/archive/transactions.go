package archive

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

// transactionByHashQuery returns the transactions with hash {hash} that hash_locator places in
// blocks {first} through {last}, in block order.
var transactionByHashQuery = fmt.Sprintf(`SELECT block_number, transaction_index, sender, transaction_payload
FROM %s
WHERE (block_number, transaction_index) IN (
        SELECT bitShiftRight(location, %d), toUInt32(bitAnd(location, %d))
        FROM %s
        WHERE hash_prefix = {prefix:UInt64} AND object_kind = %d)
  AND transaction_hash = unhex({hash:String})
  AND block_number BETWEEN {first:UInt64} AND {last:UInt64}
ORDER BY block_number, transaction_index`, tableTxs, txIndexBits, 1<<txIndexBits-1, tableLocator, locatorTx)

// blockHeaderQuery returns the header fields transaction responses carry for block {n}.
var blockHeaderQuery = fmt.Sprintf(
	`SELECT block_hash, block_time, base_fee_per_gas FROM %s WHERE block_number = {n:UInt64} LIMIT 1`, tableBlocks)

// BlockHeader is the part of an archived block header that transaction responses carry.
type BlockHeader struct {
	Number uint64
	Hash   common.Hash
	// Time is the block's unix time in seconds.
	Time    uint64
	BaseFee *big.Int
}

// Transaction is an archived transaction with its position and its block's header.
type Transaction struct {
	Tx     *ethtypes.Transaction
	Sender common.Address
	Index  uint32
	Block  BlockHeader
}

// TransactionByHash returns the committed transaction with hash. It returns ErrNotFound when the
// archive holds none.
func (r *Reader) TransactionByHash(ctx context.Context, hash common.Hash) (Transaction, error) {
	ranges, known := r.coverage.load()
	if !known {
		return Transaction{}, errCoverageUnknown
	}
	first, last, ok := ranges.bounds()
	if !ok {
		return Transaction{}, ErrNotFound
	}
	var (
		number  proto.ColUInt64
		index   proto.ColUInt32
		sender  = fixedString(20)
		payload proto.ColStr
		found   bool
		tx      Transaction
		raw     []byte
	)
	err := r.run(ctx, "transaction_by_hash", r.lookup, ch.Query{
		Body: transactionByHashQuery,
		Parameters: ch.Parameters(map[string]any{
			"prefix": hashPrefix(hash),
			"hash":   hex.EncodeToString(hash[:]),
			"first":  first,
			"last":   last,
		}),
		Result: proto.Results{
			{Name: "block_number", Data: &number},
			{Name: "transaction_index", Data: &index},
			{Name: "sender", Data: sender},
			{Name: "transaction_payload", Data: &payload},
		},
		OnResult: func(context.Context, proto.Block) error {
			// Rows of a block in a gap between commits belong to a batch whose commit never
			// landed, so the first row of a committed block is the answer.
			for i := 0; i < number.Rows() && !found; i++ {
				if !ranges.contains(number[i]) {
					continue
				}
				found = true
				tx.Block.Number = number[i]
				tx.Index = index[i]
				tx.Sender = common.BytesToAddress(sender.Row(i))
				// The column reuses its buffer for the next block of rows.
				raw = append([]byte(nil), payload.RowBytes(i)...)
			}
			return nil
		},
	})
	if err != nil {
		return Transaction{}, err
	}
	if !found {
		return Transaction{}, ErrNotFound
	}
	tx.Tx = new(ethtypes.Transaction)
	if err := tx.Tx.UnmarshalBinary(raw); err != nil {
		return Transaction{}, fmt.Errorf("decode archived transaction at block %d index %d: %w", tx.Block.Number, tx.Index, err)
	}
	if tx.Block, err = r.blockHeader(ctx, tx.Block.Number); err != nil {
		return Transaction{}, err
	}
	return tx, nil
}

// blockHeader returns the header of committed block n.
func (r *Reader) blockHeader(ctx context.Context, n uint64) (BlockHeader, error) {
	var (
		hash      proto.ColFixedStr32
		blockTime proto.ColDateTime
		baseFee   proto.ColUInt256
		header    BlockHeader
		found     bool
	)
	err := r.run(ctx, "block_header", r.lookup, ch.Query{
		Body:       blockHeaderQuery,
		Parameters: ch.Parameters(map[string]any{"n": n}),
		Result: proto.Results{
			{Name: "block_hash", Data: &hash},
			{Name: "block_time", Data: &blockTime},
			{Name: "base_fee_per_gas", Data: &baseFee},
		},
		OnResult: func(context.Context, proto.Block) error {
			if found || hash.Rows() == 0 {
				return nil
			}
			found = true
			header = BlockHeader{
				Number:  n,
				Hash:    hash[0],
				Time:    uint64(blockTime.Row(0).Unix()), //nolint:gosec // block times are after 1970
				BaseFee: bigFromU256(baseFee[0]),
			}
			return nil
		},
	})
	if err != nil {
		return BlockHeader{}, err
	}
	if !found {
		// The block's commit row says every table holds it.
		return BlockHeader{}, fmt.Errorf("committed archive block %d has no header", n)
	}
	return header, nil
}

// bigFromU256 returns v as a big.Int; it is the inverse of u256 for values that fit.
func bigFromU256(v proto.UInt256) *big.Int {
	return (&uint256.Int{v.Low.Low, v.Low.High, v.High.Low, v.High.High}).ToBig()
}
