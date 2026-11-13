package archive

import (
	"math/big"
	"testing"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// testBlock returns block number with two transactions, and its result: a contract creation
// that emits one log, and a transaction rejected for a nonce above the sender's.
func testBlock(number uint64) (evmonly.PreparedBlock, *evmonly.BlockResult) {
	sender := common.BytesToAddress([]byte{0x5e})
	contract := common.BytesToAddress([]byte{0xc0, byte(number)})
	code := []byte{0x60, 0x00, byte(number)}
	creation := ethtypes.NewTx(&ethtypes.DynamicFeeTx{Nonce: 2 * number, Gas: 100_000, GasFeeCap: big.NewInt(10), Data: code})
	rejected := ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 1_000 + number, Gas: 21_000, GasPrice: big.NewInt(10), To: &sender})
	lg := &ethtypes.Log{Address: contract, Topics: []common.Hash{{0x70}}, Data: []byte{0xd0}, Index: 0}
	block := evmonly.PreparedBlock{
		Context: evmonly.BlockContext{
			Number:     number,
			BlockHash:  common.BytesToHash([]byte{0xb0, byte(number)}),
			ParentHash: common.BytesToHash([]byte{0xb0, byte(number - 1)}),
			Time:       1_700_000_000 + number,
			GasLimit:   10_000_000,
			BaseFee:    big.NewInt(7),
		},
		Txs: []evmonly.PreparedTx{{Tx: creation, Sender: sender}, {Tx: rejected, Sender: sender}},
	}
	result := &evmonly.BlockResult{
		GasUsed: 60_000,
		Txs: []evmonly.TxResult{
			{Hash: creation.Hash(), Sender: sender, ContractAddress: contract},
			{Hash: rejected.Hash(), Sender: sender, Rejected: true, Err: core.ErrNonceTooHigh},
		},
		Receipts: ethtypes.Receipts{
			{Type: ethtypes.DynamicFeeTxType, Status: 1, GasUsed: 60_000, CumulativeGasUsed: 60_000,
				EffectiveGasPrice: big.NewInt(7), Logs: []*ethtypes.Log{lg}},
			{Type: ethtypes.LegacyTxType, CumulativeGasUsed: 60_000},
		},
		ChangeSet: gigatypes.StateChangeSet{
			Accounts: []gigatypes.AccountChange{
				{Address: sender, Account: gigatypes.AccountState{Balance: big.NewInt(900), Nonce: 2*number + 1},
					Prior: gigatypes.AccountState{Balance: big.NewInt(1000), Nonce: 2 * number}},
				{Address: contract, Account: gigatypes.AccountState{Code: code}},
			},
			Storage: []gigatypes.StorageChange{
				{Address: contract, Key: common.Hash{0x51}, Value: common.Hash{0x7a, byte(number)}},
				{Address: contract, Key: common.Hash{0x52}, Delete: true, Value: common.Hash{0xff}},
			},
		},
	}
	return block, result
}

func TestBatchAdd(t *testing.T) {
	block, result := testBlock(3)
	b := newBatch()
	require.NoError(t, b.add(&block, result))
	require.Equal(t, uint64(3), b.first)
	require.Equal(t, uint64(3), b.last)
	require.Equal(t, 1, b.blocks)
	require.Equal(t, 2, b.txs)

	blocks := &b.blockRows
	require.Equal(t, proto.ColUInt64{3}, blocks.number)
	require.Equal(t, proto.ColFixedStr32{block.Context.BlockHash}, blocks.hash)
	require.Equal(t, proto.ColFixedStr32{block.Context.ParentHash}, blocks.parent)
	require.Equal(t, int64(1_700_000_003), blocks.time.Row(0).Unix())
	require.Equal(t, proto.ColUInt32{2}, blocks.txCount)
	require.Equal(t, proto.ColUInt64{60_000}, blocks.gasUsed)
	require.Equal(t, proto.ColUInt256{u256(big.NewInt(7))}, blocks.baseFee)

	txs := &b.txRows
	require.Equal(t, 2, txs.number.Rows())
	for i, tx := range block.Txs {
		want, err := tx.Tx.MarshalBinary()
		require.NoError(t, err)
		require.Equal(t, string(want), txs.payload.Row(i), "transaction %d is stored in its canonical encoding", i)
	}
	require.Equal(t, proto.ColUInt8{ethtypes.DynamicFeeTxType, ethtypes.LegacyTxType}, txs.txType)
	require.Equal(t, proto.ColUInt8{1, 0}, txs.status)
	require.Equal(t, proto.ColUInt8{1, 0}, txs.hasContract)
	require.Equal(t, common.BytesToAddress([]byte{0xc0, 3}).Bytes(), txs.contract.Row(0))
	require.Equal(t, proto.ColUInt8{1, 0}, txs.executed)
	require.Equal(t, u256(big.NewInt(7)), txs.price[0])

	// The block and the executed transaction are located; the curable rejection is not.
	require.Equal(t, proto.ColUInt8{locatorBlock, locatorTx}, b.locatorRows.kind)
	require.Equal(t, proto.ColUInt64{3, 3 << txIndexBits}, b.locatorRows.location)

	logs := &b.logRows
	require.Equal(t, 1, logs.number.Rows())
	require.Equal(t, [][32]byte{{0x70}}, logs.topics.Row(0))
	require.Equal(t, "\xd0", logs.data.Row(0))
	require.Equal(t, proto.ColFixedStr32{result.Txs[0].Hash}, logs.txHash)

	code := []byte{0x60, 0x00, 3}
	accounts := &b.accountRows
	require.Equal(t, proto.ColUInt64{7, 0}, accounts.nonce)
	require.Equal(t, proto.ColUInt256{u256(big.NewInt(900)), {}}, accounts.balance)
	require.Equal(t, proto.ColFixedStr32{ethtypes.EmptyCodeHash, crypto.Keccak256Hash(code)}, accounts.codeHash)
	require.Equal(t, proto.ColFixedStr32{crypto.Keccak256Hash(code)}, b.codeRows.hash)
	require.Equal(t, string(code), b.codeRows.code.Row(0))

	// A deleted slot is stored as zero.
	require.Equal(t, proto.ColFixedStr32{{0x51}, {0x52}}, b.storageRows.slot)
	require.Equal(t, proto.ColFixedStr32{{0x7a, 3}, {}}, b.storageRows.value)
}

func TestBatchLocatesStaleNonceRejections(t *testing.T) {
	block, result := testBlock(3)
	result.Txs[1].Err = core.ErrNonceTooLow
	b := newBatch()
	require.NoError(t, b.add(&block, result))
	require.Equal(t, proto.ColUInt8{1, 0}, b.txRows.executed)
	require.Equal(t, proto.ColUInt64{3, 3 << txIndexBits, 3<<txIndexBits | 1}, b.locatorRows.location)
}

func TestBatchOwnsItsRows(t *testing.T) {
	block, result := testBlock(3)
	b := newBatch()
	require.NoError(t, b.add(&block, result))
	result.Receipts[0].Logs[0].Data[0] = 0xff
	result.ChangeSet.Accounts[1].Account.Code[0] = 0xff
	block.Context.BaseFee.SetInt64(99)
	require.Equal(t, "\xd0", b.logRows.data.Row(0))
	require.Equal(t, byte(0x60), b.codeRows.code.Row(0)[0])
	require.Equal(t, u256(big.NewInt(7)), b.blockRows.baseFee[0])
}

func TestBatchRejectsATxWithoutReceipt(t *testing.T) {
	for name, drop := range map[string]func(*evmonly.BlockResult){
		"nil receipt":     func(r *evmonly.BlockResult) { r.Receipts[1] = nil },
		"missing receipt": func(r *evmonly.BlockResult) { r.Receipts = r.Receipts[:1] },
		"missing result":  func(r *evmonly.BlockResult) { r.Txs = r.Txs[:1] },
	} {
		t.Run(name, func(t *testing.T) {
			block, result := testBlock(3)
			drop(result)
			b := newBatch()
			require.Error(t, b.add(&block, result))
			require.Zero(t, b.blocks)
			require.Zero(t, b.maxRows())
			require.Zero(t, b.blockRows.number.Rows())
		})
	}
}

func TestBatchReset(t *testing.T) {
	b := newBatch()
	for n := uint64(1); n <= 2; n++ {
		block, result := testBlock(n)
		require.NoError(t, b.add(&block, result))
	}
	require.Equal(t, uint64(1), b.first)
	require.Equal(t, uint64(2), b.last)
	b.reset()
	require.Zero(t, b.blocks)
	require.Zero(t, b.bytes)
	for _, table := range b.tables() {
		for _, col := range table.input {
			require.Zero(t, col.Data.Rows(), "%s.%s", table.name, col.Name)
		}
	}
}

func TestU256(t *testing.T) {
	require.Equal(t, proto.UInt256{}, u256(nil))
	require.Equal(t, proto.UInt256{}, u256(big.NewInt(-1)))
	require.Equal(t, proto.UInt256{Low: proto.UInt128{Low: 5}}, u256(big.NewInt(5)))
	tooBig := new(big.Int).Lsh(big.NewInt(1), 256)
	allOnes := proto.UInt128{Low: ^uint64(0), High: ^uint64(0)}
	require.Equal(t, proto.UInt256{Low: allOnes, High: allOnes}, u256(tooBig))
}
