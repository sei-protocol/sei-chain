package p2p

import (
	"errors"
	"slices"
	"maps"
	"cmp"
	"iter"
	"crypto/sha256"
	"crypto/rand"
	"encoding/binary"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type peerConnInfo struct {
	ID       types.NodeID
	Channels ChannelIDSet
	DialAddr utils.Option[NodeAddress]
	SelfAddr utils.Option[NodeAddress]
}

func (i peerConnInfo) connID() connID {
	return connID{NodeID:i.ID,outbound:i.DialAddr.IsPresent()}
}

type peerConn interface {
	comparable
	Info() peerConnInfo
	Close()
}

type poolConfig struct {
	SelfID types.NodeID
	MaxIn  int // Maximal number of inbound connections.
	MaxOut int // Maximal number of outbound connections. 
	MaxDials int // Maximal number of concurrent dials.
	FixedAddrs []NodeAddress // Addresses which are always available for dialing.
	InPool func(types.NodeID) bool // InPool(id) <=> id belongs to this pool. 
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

// Pops a dialing candidate from the queue.
// May rebuild queue if runs out of candidates.
// Complexity: O(max num of connections) + amortized O(1) 
// TODO(gprusak): amortization is rather complex currently:
// * skipping over in/out/dialing entries is covered by O(max num of connections)
// * rebuilding queue is covered by the pop() calls + PexPush calls, since the last rebuild
// This amortization is rather coarse, but it makes address selection as reliable as going
// through all the addresses in the pex table, while reasonably limiting the search complexity.
func (p *poolManager) pop() (dialQueueEntry,bool) {
	canRebuild := true
	for {
		for len(p.dialQueue)>0 {
			// Pop dial candidate from the queue.
			e := p.dialQueue[0]
			p.dialQueue = p.dialQueue[1:]
			// Skip candidates which are not eligible for dialing.
			if _,ok := p.dialing[e.NodeID]; ok { continue }
			if _,ok := p.in[e.NodeID]; ok { continue }
			if _,ok := p.out[e.NodeID]; ok { continue }
			// Return the candidate.
			return e,true
		}
		if !canRebuild {
			return dialQueueEntry{},false
		}
		// Reset recent, rebuild queue and try again.
		canRebuild = false
		p.dialRecent = map[types.NodeID]struct{}{}
		p.RebuildQueue()
	}
}

func (p *poolManager) setDialing(id types.NodeID) {
	const maxRecent = 1000
	if len(p.dialRecent)>=maxRecent {
		p.dialRecent = map[types.NodeID]struct{}{}
	}
	p.dialing[id] = struct{}{}
	p.dialRecent[id] = struct{}{}
}

type poolManager struct {
	cfg *poolConfig
	withPriority func(types.NodeID) pNodeID
	
	in map[types.NodeID]struct{}
	out map[types.NodeID]uint64
	dialing map[types.NodeID]struct{}
	dialRecent map[types.NodeID]struct{}
	dialQueue []dialQueueEntry
	upgradePermit bool
	pex pexTable
}

func newPoolManager(cfg *poolConfig) *poolManager {
	h := sha256.New()
	var seed [32]byte
	utils.OrPanic1(rand.Read(seed[:]))
	utils.OrPanic1(h.Write(seed[:]))
	return &poolManager {
		cfg: cfg,
		// PRF defining peer priority.
		// It makes the global topology converge to an uniformly random graph
		// of a bounded degree.
		withPriority: func(id types.NodeID) pNodeID {
			return pNodeID{
				priority: binary.LittleEndian.Uint64(h.Sum([]byte(id)[:8])),
				NodeID: id,
			}
		},
		in: map[types.NodeID]struct{}{},
		out: map[types.NodeID]uint64{},
		dialing: map[types.NodeID]struct{}{},
		dialRecent: map[types.NodeID]struct{}{},
		upgradePermit: false,
		pex: pexTable{
			fixed: cfg.FixedAddrs,
			bySender: map[types.NodeID][]NodeAddress{},
			extra: make([][]NodeAddress,10),
			extraNext: 0,
			pushes: 0,
		},
	}
}

type pexTable struct {
	fixed []NodeAddress
	bySender map[types.NodeID][]NodeAddress
	extra [][]NodeAddress
	extraNext int
	pushes int
}

func (t *pexTable) All() iter.Seq[NodeAddress] {
	return func(yield func(NodeAddress) bool) {
		for _,addr := range t.fixed {
			if !yield(addr) { return }
		}
		for _,addrs := range t.bySender {
			for _,addr := range addrs {
				if !yield(addr) { return }
			}
		}
		for _,addrs := range t.extra {
			for _,addr := range addrs {
				if !yield(addr) { return }
			}
		}
	}
}

// RebuildQueue rebuild the dial queue.
// It is expected to be executed periodically to amortize the amount of work it is doing.
// RebuildQueue is usualy amortized over the amount of data pushed to pex since the last RebuildQueue().
// RebuildQueue is expected to be executed frequently enough to prune stale addresses from the queue:
// in particular we need to avoid a situation in which we have enough addresses to dial for ~10h, while we
// receive fresh addresses every ~10s.
// However to not block dialing on receiving fresh data, pop() amortizes additionally over number of dials.
// NOTE: fairness of dialing is achieved by maintaining a list of recently dialled peers (dialRecent),
// which should be enough if the global number of peers is bounded. However it does protect us from
// peers spamming with fake addresses (which may delay dialing correct addresses indefinitely).
// I.e. this is best-effort fairness, reliable connectivity is provided by persistent peers.
func (p *poolManager) RebuildQueue() {
	p.pex.pushes = 0
	// Regroup addresses by ID.
	byID := map[types.NodeID]map[NodeAddress]struct{}{}
	for addr := range p.pex.All() {
		if _,ok := p.dialRecent[addr.NodeID]; ok { continue }
		if _,ok := byID[addr.NodeID]; !ok {
			byID[addr.NodeID] = map[NodeAddress]struct{}{}
		}
		byID[addr.NodeID][addr] = struct{}{}
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

func (p *poolManager) PushPex(sender utils.Option[types.NodeID], addrs []NodeAddress) {
	// Accept at most 1 address per NodeID from each pex sender.
	dedup := map[types.NodeID]NodeAddress{}
	for _,addr := range addrs {
		if p.cfg.InPool(addr.NodeID) {
			dedup[addr.NodeID] = addr
		}
	}
	addrs = slices.Collect(maps.Values(dedup))
	if sender,ok := sender.Get(); ok {
		p.pex.bySender[sender] = addrs 
	} else {
		i := p.pex.extraNext%len(p.pex.extra)
		p.pex.extraNext = (p.pex.extraNext+1)%len(p.pex.extra)
		p.pex.extra[i] = addrs
	}
	// Amortized queue rebuild. 
	p.pex.pushes += 1 
	if p.pex.pushes==len(p.pex.bySender) {
		p.RebuildQueue()
	}
}

func (p *poolManager) ClearPex(sender types.NodeID) {
	delete(p.pex.bySender,sender)
	// Amortized queue rebuild. 
	if p.pex.pushes==len(p.pex.bySender) {
		p.RebuildQueue()
	}
}

func (p *poolManager) upgradeableTo(id pNodeID) pNodeID {
	for old,priority := range p.out {
		if priority < id.priority {
			id = pNodeID{priority,old}
		}
	}
	return id
}

// Tries to find a node for dialing.
// Marks the peer as "dialing" on success. 
func (p *poolManager) StartDial() ([]NodeAddress,bool) {
	switch {
	// Dialing is not allowed if outbound connections are disabled or dialing capacity is full.
	case p.cfg.MaxOut==0 || len(p.dialing) >= p.cfg.MaxDials: return nil,false
	// Fast dialing is allowed iff the current outbound connections (including  ongoing dials)
	// do not saturate outbound capacity.
	case len(p.out)+len(p.dialing)<p.cfg.MaxOut: // Try to find address to dial.
	// Upgrades are allowed iff:
	// * we have upgrade permit
	// * outbound connections capacity is full
	// * there are no ongoing dials
	case p.upgradePermit && len(p.dialing)==0 && len(p.out)==p.cfg.MaxOut:
	// Otherwise dialing is not feasible atm.
	default: return nil,false
	}
	for {
		// Fetch highest priority peer from the queue.
		e,ok := p.pop()
		if !ok { return nil,false }
		if len(p.out)==p.cfg.MaxOut {
			// Check if it has high enough priority to upgrade some other connection.
			// If it doesn't, then we need to wait for the queue rebuild.
			if p.upgradeableTo(e.pNodeID)==e.pNodeID {
				return nil,false
			}
		}
		// Set as dialing.
		p.setDialing(e.NodeID)
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
var errNotInPool = errors.New("peer does not belong to the pool")

// Connect registers a new connection.
// Returns an error if the connection was rejected.
// May disconnect another peer (returned in result) a fit the new one.
// In particular result may be equal to id, in which case the old connection
// under the same id needs to be disconnected.
func (p *poolManager) Connect(id connID) (utils.Option[connID],error) {
	none := utils.None[connID]()	
	if id.outbound {
		// Make sure that the peer was expected. 
		if _,ok := p.dialing[id.NodeID]; !ok { return none,errUnexpectedPeer }
		delete(p.dialing,id.NodeID)
		// Insert the peer.
		pID := p.withPriority(id.NodeID)
		if len(p.out)==p.cfg.MaxOut { 
			toDisconnect := p.upgradeableTo(pID)
			// This should never happen, because of how the algorithm works:
			// we only dial peers that we know that will be accepted.
			if toDisconnect==pID {
				panic("BUG: dialed a peer with too low priority")
			}
			// Consume the upgrade permit.
			p.upgradePermit = false
			delete(p.out,toDisconnect.NodeID)
			p.out[pID.NodeID] = pID.priority
			return utils.Some(connID{NodeID:toDisconnect.NodeID,outbound:true}),nil
		}
		p.out[pID.NodeID] = pID.priority
		return none,nil
	} else {
		// It is fine if new inbound connection overrides the old one.
		if _,ok := p.in[id.NodeID]; ok { return utils.Some(id),nil }
		// Check the inbound limit.
		if len(p.in)>=p.cfg.MaxIn { return none,errTooManyPeers }
		// Check if this is peer from our pool.
		if !p.cfg.InPool(id.NodeID) { return none,errNotInPool }
		p.in[id.NodeID] = struct{}{} 
		return none,nil
	}
}

type connID struct {
	types.NodeID
	outbound bool
}

func (p *poolManager) Disconnect(id connID) error {
	if id.outbound {
		if _,ok := p.out[id.NodeID]; !ok { return errUnexpectedPeer }
		delete(p.out,id.NodeID)
	} else {
		if _,ok := p.in[id.NodeID]; !ok { return errUnexpectedPeer }
		delete(p.in,id.NodeID)
	}
	return nil
}
