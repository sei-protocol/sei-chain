package mempool

import (
	"math"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
)

const (
	MempoolChannel = p2p.ChannelID(0x30)

	// PeerCatchupSleepIntervalMS defines how much time to sleep if a peer is behind
	PeerCatchupSleepIntervalMS = 100

	// UnknownPeerID is the peer ID to use when running CheckTx when there is
	// no peer (e.g. RPC)
	UnknownPeerID uint16 = 0

	MaxActiveIDs = math.MaxUint16
)

// TxConstraints contains the precomputed consensus-derived mempool limits for
// the current state snapshot.
type TxConstraints struct {
	MaxDataBytes int64
	MaxGas       int64
}

// TxStateFetcher returns the precomputed consensus-derived mempool limits for the current
// state snapshot.
type TxStateFetcher func() (TxConstraints, error)
