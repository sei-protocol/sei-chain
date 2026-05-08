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
//
// txs is computed eagerly in newGlobalBlockAdapter and cached for the
// lifetime of the adapter. mem_block_db calls Transactions() multiple
// times (WriteBlock, SetTransactionResults validation, Prune); without
// the cache each call would re-allocate the slice and re-sha256 every
// payload tx — under the write lock, on the Prune path.
type globalBlockAdapter struct {
	gb  *atypes.GlobalBlock
	txs []block.Transaction
}

func newGlobalBlockAdapter(gb *atypes.GlobalBlock) globalBlockAdapter {
	src := gb.Payload.Txs()
	txs := make([]block.Transaction, len(src))
	for i, tx := range src {
		txs[i] = txAdapter{
			hash:  tmhash.Sum(tx),
			bytes: tx,
		}
	}
	return globalBlockAdapter{gb: gb, txs: txs}
}

func (a globalBlockAdapter) Hash() []byte {
	// TODO(autobahn): memoize parallel to txs — Hash() is called multiple
	// times per block (mem_block_db's WriteBlock, runExecute's
	// SetTransactionResults call site, BlockByHash translation). Each call
	// re-runs the proto marshal + sha256 over the header. Not hot today
	// but trivial to cache when we revisit.
	h := a.gb.Header.Hash()
	return h.Bytes()
}

func (a globalBlockAdapter) Height() uint64 { return uint64(a.gb.GlobalNumber) }

func (a globalBlockAdapter) Time() time.Time { return a.gb.Timestamp }

func (a globalBlockAdapter) Transactions() []block.Transaction { return a.txs }

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
