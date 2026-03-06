package p2p

import (
	"errors"
	"slices"
	"cmp"
	"crypto/sha256"
	"crypto/rand"
	"encoding/binary"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/ordered"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type peerConnInfo struct {
	ID       types.NodeID
	Channels ChannelIDSet
	DialAddr utils.Option[NodeAddress]
	SelfAddr utils.Option[NodeAddress]
}

type peerConn interface {
	comparable
	Info() peerConnInfo
	Close()
}

type poolConfig struct {
	SelfID types.NodeID
	// Maximal number of inbound connections.
	MaxIn  int
	// Maximal number of outbound connections. 
	MaxOut int
	// Maximal number of concurrent dials.
	MaxDials int
	// InPool(id) <=> id belongs to this pool. 
	InPool func(types.NodeID) bool
}

type pNodeID struct {
	priority uint64
	types.NodeID
}

func (a pNodeID) Less(b pNodeID) bool {
	if a.priority!=b.priority {
		return a.priority<b.priority
	}
	return a.NodeID < b.NodeID
}

type dialQueueEntry struct {
	pNodeID
	addrs []NodeAddress
}

type poolManager struct {
	cfg *poolConfig
	withPriority func(types.NodeID) pNodeID
	
	in map[types.NodeID]struct{}
	out ordered.Map[pNodeID,struct{}]
	dialing map[types.NodeID]struct{}
	
	upgradePermit bool
	dialQueue []dialQueueEntry
	pex pexTable
}

func newPoolManager(cfg *poolConfig) *poolManager {
	h := sha256.New()
	var seed [32]byte
	utils.OrPanic1(rand.Read(seed[:]))
	utils.OrPanic1(h.Write(seed[:]))
	return &poolManager {
		cfg: cfg,
		withPriority: func(id types.NodeID) pNodeID {
			return pNodeID{
				priority: binary.LittleEndian.Uint64(h.Sum([]byte(id)[:8])),
				NodeID: id,
			}
		},
	}
}

type pexTable struct {
	initial []NodeAddress
	bySender map[types.NodeID][]NodeAddress
	cleared [][]NodeAddress
}

func (t *pexTable) Empty() bool {
	return len(t.initial)==0 && len(t.bySender) == 0 && len(t.cleared) == 0
}

func (t *pexTable) Pop() [][]NodeAddress {
	// If there are initial addresses, then return just them so that they are prioritized.
	if len(t.initial)>0 {
		out := utils.Slice(t.initial)
		t.initial = nil
		return out
	}
	// Otherwise just aggregate everything
	out := t.cleared
	for _,addrs := range t.bySender {
		out = append(out, addrs)
	}
	// Clear the table.
	*t = pexTable{}
	return out
}

func (p *poolManager) PushPex(sender types.NodeID, addrs []NodeAddress) {
	p.pex.bySender[sender] = append([]NodeAddress(nil),addrs...)
}

func (p *poolManager) ClearPex(sender types.NodeID) {
	const maxClearedCache = 10
	addrs,ok := p.pex.bySender[sender]
	if !ok { return }
	delete(p.pex.bySender, sender)
	if len(addrs)>0 && len(p.pex.cleared) < maxClearedCache {
		p.pex.cleared = append(p.pex.cleared, addrs)
	}
}

// Pops the highest priority peer from the dialing dialQueue.
// Queue is refilled from the pex data whenever it is empty.
// Pex data is single-use: if dialing an address fails,
// we one of our peers to send us this address again to retry.
// This gives us the following properties:
// - dialQueue construction time is amortized by pex data processing (no fancy data structures needed)
// - dialing fairness - data received from every peer is eventually processed
//   (except for the issue described in TryStartDial)
// - bounded pex cache size (max connections * max addrs per connection)
// - stale addresses are pruned very fast: either after the first dial,
//   or as soon as the given peer sends us fresh pex data
func (p *poolManager) pop() (dialQueueEntry,bool) {
	for {
		if len(p.dialQueue)>0 {
			e := p.dialQueue[0]
			p.dialQueue = p.dialQueue[1:]
			return e,true
		}
		if p.pex.Empty() {
			return dialQueueEntry{},false
		}
		// Regroup addresses by ID.
		byID := map[types.NodeID]map[NodeAddress]struct{}{}
		for _,addrs := range p.pex.Pop() {
			done := map[types.NodeID]bool{}
			for _,addr := range addrs {
				// Accept at most 1 address per NodeID from each pex sender.
				if done[addr.NodeID] || !p.cfg.InPool(addr.NodeID) {
					continue
				}
				done[addr.NodeID] = true 
				if _,ok := byID[addr.NodeID]; !ok {
					byID[addr.NodeID] = map[NodeAddress]struct{}{}
				}
				byID[addr.NodeID][addr] = struct{}{}
			}
		}
		// Sort by priority.
		p.dialQueue = make([]dialQueueEntry,0,len(byID))
		for id,addrSet := range byID {
			addrs := make([]NodeAddress,0,len(addrSet))
			for addr,_ := range addrSet { addrs = append(addrs, addr) }
			p.dialQueue = append(p.dialQueue,dialQueueEntry{p.withPriority(id),addrs})
		}
		slices.SortFunc(p.dialQueue, func(a,b dialQueueEntry) int {
			return -cmp.Compare(a.priority,b.priority)
		})
	}
}

// NOTE: the fact that we discard addresses if the peer is already dialing,
// induces a small fairness issue:
// * pex data is of single-time use - we try to dial, then we discard.
//   We retry connecting iff we receive the given address via pex again
//   (except for bootstrap and persistent peer addresses)
// * let say we have 2 peers: A and B, which both are connected to peer C.
// * let say that A is connected to C on a private endpoint of C (private ip/dns),
//   while B is connected to a public IP of C.
// * consider the following scenario:
//   1. A sends pex
//   2. dialQueue refill
//   3. start dial private IP of C
//   4. B sends pex
//   5. dialQueue refill
//   6. discard public IP of C (because dialing C is ongoing)
//   7. dialing C fails
//   8. back to 1.
// * In this case our node will never try to dial public IP of C.
//   We ignore this small issue because:
//   * this is a corner case which is hard to coordinate for the attacker
//   * missing connection to any specific node on the anonymous network is not a problem
//   * eclipsing our node is way easier
//   * strong connectivity guarantees are provided by persistent peers, not peer discovery via pex
func (p *poolManager) canDial(id pNodeID) bool {
	if _,ok := p.dialing[id.NodeID]; ok { return false }
	if _,ok := p.in[id.NodeID]; ok { return false }
	if _,ok := p.out.Get(id); ok { return false }
	return true
}

// Tries to find a node for dialing.
// Marks the peer as "dialing" on success. 
func (p *poolManager) StartDial() ([]NodeAddress,bool) {
	switch {
	// Dialing is not allowed if outbound connections are disabled or dialing capacity is full.
	case p.cfg.MaxOut==0 || len(p.dialing) >= p.cfg.MaxDials: return nil,false
	// Fast dialing is allowed iff the current outbound connections (including  ongoing dials)
	// do not saturate outbound capacity.
	case p.out.Len()+len(p.dialing)<p.cfg.MaxOut: // Try to find address to dial.
	// Upgrades are allowed iff:
	// * we have upgrade permit
	// * outbound connections capacity is full
	// * there are no ongoing dials
	case p.upgradePermit && len(p.dialing)==0 && p.out.Len()==p.cfg.MaxOut:
	// Otherwise dialing is not feasible atm.
	default: return nil,false
	}
	for {
		// Fetch highest priority peer from the dialing dialQueue.
		e,ok := p.pop()
		if !ok { return nil,false }
		// Make sure that the peer is eligible for dialing.
		if !p.canDial(e.pNodeID) { continue }
		// Check if it has high enough priority.
		if id,_,ok := p.out.GetAt(p.cfg.MaxOut-1); ok {
			if id.priority >= e.priority {
				// Clear the dial queue: all remaining peers have lower priority.
				p.dialQueue = nil
				return nil,false
			}
			p.upgradePermit = false
		}
		// Set as dialing.
		p.dialing[e.NodeID] = struct{}{}
		return e.addrs,true
	}
}

func (p *poolManager) DialFailed(id types.NodeID) error {
	if _,ok := p.dialing[id]; !ok { return errUnexpectedPeer }
	delete(p.dialing,id)
	return nil
}

var errUnexpectedPeer = errors.New("unexpected peer")
var errTooManyPeers = errors.New("too many peers")

func (p *poolManager) Connect(id connID) error {
	if id.outbound {
		pID := p.withPriority(id.NodeID)
		if _,ok := p.out.Get(pID); ok { return nil } 
		if _,ok := p.dialing[id.NodeID]; !ok { return errUnexpectedPeer }
		delete(p.dialing,id.NodeID)
		if _,ok := p.out.Set(pID,struct{}{}); ok { return nil }
	} else {
		if _,ok := p.in[id.NodeID]; ok { return nil }
		if len(p.in)>=p.cfg.MaxIn { return errTooManyPeers }
		p.in[id.NodeID] = struct{}{} 
	}
	return nil
}

type connID struct {
	types.NodeID
	outbound bool
}

func (p *poolManager) Disconnect(id connID) error {
	if id.outbound {
		if _,ok := p.out.Delete(p.withPriority(id.NodeID)); !ok { return errUnexpectedPeer }
	} else {
		if _,ok := p.in[id.NodeID]; !ok { return errUnexpectedPeer }
		delete(p.in,id.NodeID)
	}
	return nil
}

func (p *poolManager) TryDisconnect() (connID,bool) {
	if p.out.Len() <= p.cfg.MaxOut {
		return connID{},false
	}
	id,_,ok := p.out.PopMin()
	if !ok { panic("PopMin() failed on nonempty map") }
	return connID{NodeID: id.NodeID, outbound: true},true
}
