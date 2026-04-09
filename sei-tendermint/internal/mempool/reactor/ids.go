package reactor

import (
	"fmt"
	"math"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const MaxActiveIDs = math.MaxUint16

type IDs struct {
	mtx       sync.RWMutex
	peerMap   map[types.NodeID]uint16
	nextID    uint16
	activeIDs map[uint16]struct{}
}

func NewMempoolIDs() *IDs {
	return &IDs{
		peerMap:   make(map[types.NodeID]uint16),
		activeIDs: map[uint16]struct{}{mempool.UnknownPeerID: {}},
		nextID:    1,
	}
}

func (ids *IDs) ReserveForPeer(peerID types.NodeID) {
	ids.mtx.Lock()
	defer ids.mtx.Unlock()

	if _, ok := ids.peerMap[peerID]; ok {
		return
	}

	curID := ids.nextPeerID()
	ids.peerMap[peerID] = curID
	ids.activeIDs[curID] = struct{}{}
}

func (ids *IDs) Reclaim(peerID types.NodeID) {
	ids.mtx.Lock()
	defer ids.mtx.Unlock()

	removedID, ok := ids.peerMap[peerID]
	if ok {
		delete(ids.activeIDs, removedID)
		delete(ids.peerMap, peerID)
		if removedID < ids.nextID {
			ids.nextID = removedID
		}
	}
}

func (ids *IDs) GetForPeer(peerID types.NodeID) uint16 {
	ids.mtx.RLock()
	defer ids.mtx.RUnlock()

	return ids.peerMap[peerID]
}

func (ids *IDs) nextPeerID() uint16 {
	if len(ids.activeIDs) == MaxActiveIDs {
		panic(fmt.Sprintf("node has maximum %d active IDs and wanted to get one more", MaxActiveIDs))
	}

	_, idExists := ids.activeIDs[ids.nextID]
	for idExists {
		ids.nextID++
		_, idExists = ids.activeIDs[ids.nextID]
	}

	curID := ids.nextID
	ids.nextID++

	return curID
}
