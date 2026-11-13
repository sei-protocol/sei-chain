// Package archive writes the EVM-only chain's history to ClickHouse: blocks, transactions with
// their receipts, logs, and every account and storage change.
package archive

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// Archive table names.
const (
	tableCommits        = "archive_commits"
	tableBlocks         = "blocks"
	tableTxs            = "transactions_receipts"
	tableLocator        = "hash_locator"
	tableLogs           = "logs"
	tableAccountChanges = "account_changes"
	tableAccountHistory = "account_history"
	tableStorageChanges = "storage_changes"
	tableStorageHistory = "storage_history"
	tableCode           = "contract_code"
)

// hash_locator object kinds.
const (
	locatorBlock uint8 = 1
	locatorTx    uint8 = 2
)

// txIndexBits is the width of the transaction index inside a packed hash_locator location.
const txIndexBits = 24

// batch holds the rows of consecutive blocks for every archive table, column by column, in the
// form the ClickHouse native protocol sends them.
type batch struct {
	// first and last are the batch's block range, valid once blocks > 0.
	first, last uint64
	blocks, txs int
	// bytes is the variable-length data the batch holds: transactions, log data and bytecode.
	bytes int

	blockRows   blockColumns
	txRows      txColumns
	locatorRows locatorColumns
	logRows     logColumns
	accountRows accountColumns
	storageRows storageColumns
	codeRows    codeColumns

	// encoded, payloads, topics and hasher are scratch space for add: encoded holds the block's
	// encoded transactions, and payloads each one's bounds in it.
	encoded  bytes.Buffer
	payloads [][2]int
	topics   [][32]byte
	hasher   crypto.KeccakState
}

func newBatch() *batch {
	return &batch{
		txRows:      txColumns{sender: fixedString(20), contract: fixedString(20)},
		logRows:     logColumns{address: fixedString(20), topics: proto.NewArray[[32]byte](new(proto.ColFixedStr32))},
		accountRows: accountColumns{address: fixedString(20)},
		storageRows: storageColumns{address: fixedString(20)},
		hasher:      crypto.NewKeccakState(),
	}
}

// add appends the rows of block and its result to the batch. On error the batch is unchanged.
func (b *batch) add(block *evmonly.PreparedBlock, result *evmonly.BlockResult) error {
	number := block.Context.Number
	n := len(block.Txs)
	if number >= 1<<(64-txIndexBits) || n >= 1<<txIndexBits {
		return fmt.Errorf("block %d with %d transactions exceeds the hash_locator encoding", number, n)
	}
	if len(result.Txs) != n || len(result.Receipts) != n {
		return fmt.Errorf("block %d has %d transactions but %d results and %d receipts",
			number, n, len(result.Txs), len(result.Receipts))
	}
	if err := b.encodeTxs(block, result.Receipts); err != nil {
		return err
	}
	b.addHeader(&block.Context, result.GasUsed, n)
	for i := range n {
		b.addTx(number, block.Context.BlockHash, i, &result.Txs[i], result.Receipts[i])
	}
	b.addStateChanges(number, &result.ChangeSet)
	if b.blocks == 0 {
		b.first = number
	}
	b.last = number
	b.blocks++
	b.txs += n
	return nil
}

// encodeTxs writes the canonical encoding of block's transactions to encoded, refusing a
// transaction without its receipt.
func (b *batch) encodeTxs(block *evmonly.PreparedBlock, receipts ethtypes.Receipts) error {
	number := block.Context.Number
	b.encoded.Reset()
	b.payloads = b.payloads[:0]
	for i := range block.Txs {
		tx := block.Txs[i].Tx
		if tx == nil || receipts[i] == nil {
			return fmt.Errorf("block %d: transaction %d has no transaction or no receipt", number, i)
		}
		start := b.encoded.Len()
		// Unlike MarshalBinary, EncodeRLP allocates no buffer per transaction.
		err := tx.EncodeRLP(&b.encoded)
		if err == nil && tx.Type() != ethtypes.LegacyTxType {
			// EncodeRLP wraps a typed transaction's encoding in an RLP string.
			var content []byte
			_, content, _, err = rlp.Split(b.encoded.Bytes()[start:])
			start = b.encoded.Len() - len(content)
		}
		if err != nil {
			return fmt.Errorf("block %d: encode transaction %d: %w", number, i, err)
		}
		b.payloads = append(b.payloads, [2]int{start, b.encoded.Len()})
	}
	return nil
}

// addHeader appends the header of a block with txs transactions that used gasUsed.
func (b *batch) addHeader(h *evmonly.BlockContext, gasUsed uint64, txs int) {
	r := &b.blockRows
	r.number.Append(h.Number)
	r.hash.Append(h.BlockHash)
	r.parent.Append(h.ParentHash)
	r.time.Append(time.Unix(int64(h.Time), 0)) //nolint:gosec // block times are far below 2^63
	r.txCount.Append(uint32(txs))              //nolint:gosec // checked below 2^24
	r.gasUsed.Append(gasUsed)
	r.gasLimit.Append(h.GasLimit)
	r.baseFee.Append(u256(h.BaseFee))
	b.locatorRows.append(h.BlockHash, locatorBlock, h.Number)
}

// addTx appends transaction i of block number, its receipt and its logs.
func (b *batch) addTx(number uint64, blockHash common.Hash, i int, res *evmonly.TxResult, rcpt *ethtypes.Receipt) {
	index := uint32(i) //nolint:gosec // checked below 2^24
	payload := b.encoded.Bytes()[b.payloads[i][0]:b.payloads[i][1]]
	r := &b.txRows
	r.number.Append(number)
	r.index.Append(index)
	r.hash.Append(res.Hash)
	r.sender.Append(res.Sender[:])
	r.txType.Append(rcpt.Type)
	r.payload.AppendBytes(payload)
	r.status.Append(uint8(rcpt.Status)) //nolint:gosec // 0 or 1
	r.cumGas.Append(rcpt.CumulativeGasUsed)
	r.gasUsed.Append(rcpt.GasUsed)
	r.price.Append(u256(rcpt.EffectiveGasPrice))
	r.hasContract.Append(boolByte(res.ContractAddress != common.Address{}))
	r.contract.Append(res.ContractAddress[:])
	r.executed.Append(boolByte(!res.Rejected))
	b.bytes += len(payload)
	if reachableByHash(res) {
		b.locatorRows.append(res.Hash, locatorTx, number<<txIndexBits|uint64(index))
	}
	for _, lg := range rcpt.Logs {
		l := &b.logRows
		l.number.Append(number)
		l.txIndex.Append(index)
		l.logIndex.Append(uint32(lg.Index)) //nolint:gosec // bounded by the block
		l.blockHash.Append(blockHash)
		l.txHash.Append(res.Hash)
		l.address.Append(lg.Address[:])
		b.topics = b.topics[:0]
		for _, t := range lg.Topics {
			b.topics = append(b.topics, t)
		}
		l.topics.Append(b.topics)
		l.data.AppendBytes(lg.Data)
		b.bytes += len(lg.Data)
	}
}

// reachableByHash reports whether a transaction is found by its hash, as in the receipt store:
// every executed transaction, and a stale-nonce rejection. A hash with several locations
// resolves to the earliest.
func reachableByHash(res *evmonly.TxResult) bool {
	return !res.Rejected || errors.Is(res.Err, core.ErrNonceTooLow)
}

// addStateChanges appends the post-block state of every account and slot the block changed, and
// the bytecode it deployed.
func (b *batch) addStateChanges(number uint64, cs *gigatypes.StateChangeSet) {
	for i := range cs.Accounts {
		c := &cs.Accounts[i]
		codeHash := ethtypes.EmptyCodeHash
		if len(c.Account.Code) > 0 {
			codeHash = b.keccak(c.Account.Code)
			if c.CodeChanged() {
				b.codeRows.hash.Append(codeHash)
				b.codeRows.code.AppendBytes(c.Account.Code)
				b.bytes += len(c.Account.Code)
			}
		}
		r := &b.accountRows
		r.address.Append(c.Address[:])
		r.number.Append(number)
		r.nonce.Append(c.Account.Nonce)
		r.balance.Append(u256(c.Account.Balance))
		r.codeHash.Append(codeHash)
	}
	for i := range cs.Storage {
		s := &cs.Storage[i]
		value := s.Value
		if s.Delete {
			value = common.Hash{}
		}
		r := &b.storageRows
		r.address.Append(s.Address[:])
		r.slot.Append(s.Key)
		r.number.Append(number)
		r.value.Append(value)
	}
}

func (b *batch) keccak(data []byte) common.Hash {
	var h common.Hash
	b.hasher.Reset()
	_, _ = b.hasher.Write(data)
	_, _ = b.hasher.Read(h[:])
	return h
}

// maxRows is the row count of the batch's largest table.
func (b *batch) maxRows() int {
	return max(b.txRows.number.Rows(), b.locatorRows.prefix.Rows(), b.logRows.number.Rows(),
		b.accountRows.number.Rows(), b.storageRows.number.Rows())
}

// tableRows is one table's rows in a batch.
type tableRows struct {
	name  string
	input proto.Input
}

// tables returns the batch's rows per data table.
func (b *batch) tables() []tableRows {
	return []tableRows{
		{tableBlocks, b.blockRows.input()},
		{tableTxs, b.txRows.input()},
		{tableLocator, b.locatorRows.input()},
		{tableLogs, b.logRows.input()},
		{tableAccountChanges, b.accountRows.input()},
		{tableAccountHistory, b.accountRows.historyInput()},
		{tableStorageChanges, b.storageRows.input()},
		{tableStorageHistory, b.storageRows.historyInput()},
		{tableCode, b.codeRows.input()},
	}
}

// reset empties the batch, keeping its buffers.
func (b *batch) reset() {
	for _, t := range b.tables() {
		for _, col := range t.input {
			col.Data.(proto.Resettable).Reset()
		}
	}
	b.first, b.last, b.blocks, b.txs, b.bytes = 0, 0, 0, 0, 0
}

type blockColumns struct {
	number, gasUsed, gasLimit proto.ColUInt64
	hash, parent              proto.ColFixedStr32
	time                      proto.ColDateTime
	txCount                   proto.ColUInt32
	baseFee                   proto.ColUInt256
}

func (c *blockColumns) input() proto.Input {
	return proto.Input{
		{Name: "block_number", Data: &c.number},
		{Name: "block_hash", Data: &c.hash},
		{Name: "parent_hash", Data: &c.parent},
		{Name: "block_time", Data: &c.time},
		{Name: "transaction_count", Data: &c.txCount},
		{Name: "gas_used", Data: &c.gasUsed},
		{Name: "gas_limit", Data: &c.gasLimit},
		{Name: "base_fee_per_gas", Data: &c.baseFee},
	}
}

type txColumns struct {
	number, cumGas, gasUsed               proto.ColUInt64
	index                                 proto.ColUInt32
	hash                                  proto.ColFixedStr32
	sender, contract                      *proto.ColFixedStr
	txType, status, hasContract, executed proto.ColUInt8
	payload                               proto.ColStr
	price                                 proto.ColUInt256
}

func (c *txColumns) input() proto.Input {
	return proto.Input{
		{Name: "block_number", Data: &c.number},
		{Name: "transaction_index", Data: &c.index},
		{Name: "transaction_hash", Data: &c.hash},
		{Name: "sender", Data: c.sender},
		{Name: "transaction_type", Data: &c.txType},
		{Name: "transaction_payload", Data: &c.payload},
		{Name: "receipt_status", Data: &c.status},
		{Name: "cumulative_gas_used", Data: &c.cumGas},
		{Name: "gas_used", Data: &c.gasUsed},
		{Name: "effective_gas_price", Data: &c.price},
		{Name: "has_contract_address", Data: &c.hasContract},
		{Name: "contract_address", Data: c.contract},
		{Name: "executed", Data: &c.executed},
	}
}

type locatorColumns struct {
	prefix, location proto.ColUInt64
	kind             proto.ColUInt8
}

// append adds the location of the object with hash h; the key is the hash's big-endian first
// eight bytes.
func (c *locatorColumns) append(h common.Hash, kind uint8, location uint64) {
	c.prefix.Append(binary.BigEndian.Uint64(h[:8]))
	c.kind.Append(kind)
	c.location.Append(location)
}

func (c *locatorColumns) input() proto.Input {
	return proto.Input{
		{Name: "hash_prefix", Data: &c.prefix},
		{Name: "object_kind", Data: &c.kind},
		{Name: "location", Data: &c.location},
	}
}

type logColumns struct {
	number            proto.ColUInt64
	txIndex, logIndex proto.ColUInt32
	blockHash, txHash proto.ColFixedStr32
	address           *proto.ColFixedStr
	topics            *proto.ColArr[[32]byte]
	data              proto.ColStr
}

func (c *logColumns) input() proto.Input {
	return proto.Input{
		{Name: "block_number", Data: &c.number},
		{Name: "transaction_index", Data: &c.txIndex},
		{Name: "log_index", Data: &c.logIndex},
		{Name: "block_hash", Data: &c.blockHash},
		{Name: "transaction_hash", Data: &c.txHash},
		{Name: "address", Data: c.address},
		{Name: "topics", Data: c.topics},
		{Name: "data", Data: &c.data},
	}
}

type accountColumns struct {
	address       *proto.ColFixedStr
	number, nonce proto.ColUInt64
	balance       proto.ColUInt256
	codeHash      proto.ColFixedStr32
}

func (c *accountColumns) input() proto.Input {
	return proto.Input{
		{Name: "block_number", Data: &c.number},
		{Name: "address", Data: c.address},
		{Name: "nonce", Data: &c.nonce},
		{Name: "balance", Data: &c.balance},
		{Name: "code_hash", Data: &c.codeHash},
	}
}

// historyInput returns the account_history rows. They share their columns with input's, which
// is safe because an insert only reads its columns.
func (c *accountColumns) historyInput() proto.Input {
	return proto.Input{
		{Name: "address", Data: c.address},
		{Name: "block_number", Data: &c.number},
	}
}

type storageColumns struct {
	address     *proto.ColFixedStr
	slot, value proto.ColFixedStr32
	number      proto.ColUInt64
}

func (c *storageColumns) input() proto.Input {
	return proto.Input{
		{Name: "block_number", Data: &c.number},
		{Name: "address", Data: c.address},
		{Name: "slot", Data: &c.slot},
		{Name: "value", Data: &c.value},
	}
}

// historyInput returns the storage_history rows. They share their columns with input's, which
// is safe because an insert only reads its columns.
func (c *storageColumns) historyInput() proto.Input {
	return proto.Input{
		{Name: "address", Data: c.address},
		{Name: "slot", Data: &c.slot},
		{Name: "block_number", Data: &c.number},
	}
}

type codeColumns struct {
	hash proto.ColFixedStr32
	code proto.ColStr
}

func (c *codeColumns) input() proto.Input {
	return proto.Input{
		{Name: "code_hash", Data: &c.hash},
		{Name: "code", Data: &c.code},
	}
}

func fixedString(size int) *proto.ColFixedStr {
	c := new(proto.ColFixedStr)
	c.SetSize(size)
	return c
}

// u256 converts a non-negative integer to a ClickHouse UInt256, saturating values that do not
// fit. A nil or negative integer is zero.
func u256(v *big.Int) proto.UInt256 {
	var u uint256.Int
	if v != nil && v.Sign() > 0 && u.SetFromBig(v) {
		u.SetAllOne()
	}
	return proto.UInt256{
		Low:  proto.UInt128{Low: u[0], High: u[1]},
		High: proto.UInt128{Low: u[2], High: u[3]},
	}
}

func boolByte(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}
