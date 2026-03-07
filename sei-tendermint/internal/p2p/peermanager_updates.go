package p2p

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// PeerUpdatesRecv.
// NOT THREAD-SAFE.
type peerUpdatesRecv[C peerConn] struct {
	recv utils.AtomicRecv[connSet[C]]
	last map[types.NodeID]struct{}
}

// PeerUpdate is a peer update event sent via PeerUpdates.
type PeerUpdate struct {
	NodeID   types.NodeID
	Status   PeerStatus
	Channels ChannelIDSet
}

func (s *peerUpdatesRecv[C]) Recv(ctx context.Context) (PeerUpdate, error) {
	var update PeerUpdate
	_, err := s.recv.Wait(ctx, func(conns connSet[C]) bool {
		// Check for disconnected peers.
		for id := range s.last {
			if _,ok := GetAny(conns,id); !ok {
				delete(s.last, id)
				update = PeerUpdate{
					NodeID: id,
					Status: PeerStatusDown,
				}
				return true
			}
		}
		// Check for connected peers.
		for id, conn := range conns.All() {
			if _, ok := s.last[id.NodeID]; !ok {
				s.last[id.NodeID] = struct{}{}
				update = PeerUpdate{
					NodeID:   id.NodeID,
					Status:   PeerStatusUp,
					Channels: conn.Info().Channels,
				}
				return true
			}
		}
		return false
	})
	return update, err
}

func (m *peerManager[C]) Subscribe() *peerUpdatesRecv[C] {
	return &peerUpdatesRecv[C]{
		recv: m.conns,
		last: map[types.NodeID]struct{}{},
	}
}
