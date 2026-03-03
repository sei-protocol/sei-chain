package p2p

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/ordered"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type pNodeID struct {
	priority uint64
	types.NodeID
}

type pNodeAddress struct {
	priority uint64
	NodeAddress
}

func (addr pNodeAddress) pNodeID() pNodeID {
	return pNodeID {priority: addr.priority, NodeID: addr.NodeID }
}

func (a pNodeID) Less(b pNodeID) bool {
	if a.priority!=b.priority {
		return a.priority<b.priority
	}
	return a.NodeID < b.NodeID
}

type refCount = map[NodeAddress]int

type addrPool[C comparable] struct {
	priorityFun func(types.NodeID) uint64
	byPriority ordered.Map[pNodeID,refCount]
	byCollection map[C][]pNodeAddress
}

func (p *addrPool[C]) addRef(addrs []pNodeAddress, val int) {
	for _,addr := range addrs {
		id := addr.pNodeID()
		rc,ok := p.byPriority.Get(id)
		if !ok {
			rc = refCount{}
			p.byPriority.Set(id,rc)
		}
		a := addr.NodeAddress
		if rc[a] += val; rc[a]==0 {
			if delete(rc,a); len(rc)==0 {
				p.byPriority.Delete(id)
			}
		}
	}
}

func (p *addrPool[C]) Set(c C, addrs []NodeAddress) {
	p.addRef(addrs,1)
	p.addRef(p.byCollection[c],-1)
	p.byCollection[c] = addrs
}

func (p *addrPool[C]) Delete(c C) {
	p.addRef(p.byCollection[c],-1)
	delete(p.byCollection,c)
}

func newAddrPool[C comparable](priorityFun func(types.NodeID) uint64) *addrPool[C] {
	return &addrPool[C]{
		priorityFun: priorityFun,
		byPriority: ordered.NewMap[withPriority,refCount](),
		byCollection: map[C][]NodeAddress{},
	}
}

