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
	out := make([]block.Transaction, len(txs))
	for i, tx := range txs {
		out[i] = txAdapter{
			hash:  tmhash.Sum(tx),
			bytes: tx,
		}
	}
	return out
}

// txAdapter wraps a single Autobahn tx + its CometBFT-style hash so it
// satisfies block.Transaction. The interface only carries the invariant
// tx body — per-block-instance data (height, index, result) lives on
// block.Result, attached separately via SetTransactionResults.
type txAdapter struct {
	hash  []byte
	bytes []byte
}

func (t txAdapter) Hash() []byte  { return t.hash }
func (t txAdapter) Bytes() []byte { return t.bytes }

// execResultAdapter wraps *abci.ExecTxResult plus its block height +
// position so it satisfies block.Result. Marshal happens lazily on
// Bytes(); the typical caller is mem_block_db's SetTransactionResults,
// which calls Bytes() exactly once and then drops the adapter.
// ExecTxResult is gogoproto-generated so it carries its own Marshal
// method and never fails on a well-formed message — we OrPanic to surface
// the impossible case loudly rather than silently dropping a result.
type execResultAdapter struct {
	r      *abci.ExecTxResult
	height uint64
	index  uint32
}

func (a execResultAdapter) Bytes() []byte {
	if a.r == nil {
		return nil
	}
	return utils.OrPanic1(a.r.Marshal())
}

func (a execResultAdapter) Height() uint64 { return a.height }
func (a execResultAdapter) Index() uint32  { return a.index }
