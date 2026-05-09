package p2p

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/tmhash"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
)

// globalBlockAdapter wraps *atypes.GlobalBlock so it satisfies block.Block
// without leaking sei-db into autobahn/types. Per-tx hashes use
// tmhash.Sum (sha256), matching CometBFT's tx-hash convention.
//
// txs is computed eagerly in newGlobalBlockAdapter and cached for the
// lifetime of the adapter. mem_block_db calls Transactions() multiple
// times (WriteBlock, Prune); without the cache each call would
// re-allocate the slice and re-sha256 every payload tx — under the
// write lock, on the Prune path.
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
	// times per block. Each call re-runs the proto marshal + sha256 over
	// the header. Not hot today but trivial to cache when we revisit.
	h := a.gb.Header.Hash()
	return h.Bytes()
}

func (a globalBlockAdapter) Height() uint64 { return uint64(a.gb.GlobalNumber) }

func (a globalBlockAdapter) Time() time.Time { return a.gb.Timestamp }

func (a globalBlockAdapter) Transactions() []block.Transaction { return a.txs }

// txAdapter wraps a single Autobahn tx + its CometBFT-style hash so it
// satisfies block.Transaction. The interface carries only the invariant
// tx body — BlockDB doesn't index by tx hash; per-tx execution results
// belong on a future Receipt Store, not here.
type txAdapter struct {
	hash  []byte
	bytes []byte
}

func (t txAdapter) Hash() []byte  { return t.hash }
func (t txAdapter) Bytes() []byte { return t.bytes }
