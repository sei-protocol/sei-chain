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
		out[i] = txAdapter{hash: tmhash.Sum(tx), bytes: tx}
	}
	return out
}

// txAdapter wraps a single Autobahn tx + its CometBFT-style hash so it
// satisfies block.Transaction.
type txAdapter struct {
	hash  []byte
	bytes []byte
}

func (t txAdapter) Hash() []byte  { return t.hash }
func (t txAdapter) Bytes() []byte { return t.bytes }
