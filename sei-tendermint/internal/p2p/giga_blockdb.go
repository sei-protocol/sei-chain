package p2p

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/tmhash"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// globalBlockAdapter wraps *atypes.GlobalBlock so it satisfies block.Block
// without leaking sei-db into autobahn/types. Per-tx hashes use
// tmhash.Sum (sha256), matching CometBFT's tx-hash convention.
type globalBlockAdapter struct {
	gb *atypes.GlobalBlock
}

func (a globalBlockAdapter) Hash() []byte {
	h := a.gb.Header.Hash()
	return h.Bytes()
}

func (a globalBlockAdapter) Height() uint64 { return uint64(a.gb.GlobalNumber) }

func (a globalBlockAdapter) Time() time.Time { return a.gb.Timestamp }

func (a globalBlockAdapter) Transactions() []block.Transaction {
	txs := a.gb.Payload.Txs()
	height := uint64(a.gb.GlobalNumber)
	out := make([]block.Transaction, len(txs))
	for i, tx := range txs {
		out[i] = txAdapter{
			hash:   tmhash.Sum(tx),
			bytes:  tx,
			height: height,
			index:  uint32(i), //nolint:gosec
		}
	}
	return out
}

// txAdapter wraps a single Autobahn tx + its CometBFT-style hash + its
// position so it satisfies block.Transaction. Result() is always nil at
// WriteBlock time — execution results are attached later via
// BlockDB.SetTransactionResults, and surfaced through mem_block_db's
// composedTx wrapper on read.
type txAdapter struct {
	hash   []byte
	bytes  []byte
	height uint64
	index  uint32
}

func (t txAdapter) Hash() []byte           { return t.hash }
func (t txAdapter) Bytes() []byte          { return t.bytes }
func (t txAdapter) Result() ([]byte, bool) { return nil, false }
func (t txAdapter) Height() uint64         { return t.height }
func (t txAdapter) Index() uint32          { return t.index }

// execResultAdapter wraps *abci.ExecTxResult so it satisfies block.Result.
// Marshal happens lazily on Bytes(); the typical caller is mem_block_db's
// SetTransactionResults, which calls Bytes() exactly once and then drops
// the adapter. ExecTxResult is gogoproto-generated so it carries its own
// Marshal method and never fails on a well-formed message — we OrPanic to
// surface the impossible case loudly rather than silently dropping a result.
type execResultAdapter struct {
	r *abci.ExecTxResult
}

func (a execResultAdapter) Bytes() []byte {
	if a.r == nil {
		return nil
	}
	return utils.OrPanic1(a.r.Marshal())
}
