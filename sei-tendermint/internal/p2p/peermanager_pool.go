package p2p

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"iter"
	"maps"
	"slices"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// ID of a directed connection.
type connID struct {
	types.NodeID
	outbound bool
}

// NodeID with priority.
type pNodeID struct {
	priority uint64
	types.NodeID
}

// NodeAddress with priority.
type pAddr struct {
	priority uint64
	NodeAddress
}

func (a pAddr) pNodeID() pNodeID { return pNodeID{a.priority, a.NodeID} }

type pexEntry struct {
	searched bool
	addrs    []pAddr
}

func (p *poolManager) newPexEntry(addrs iter.Seq[NodeAddress]) *pexEntry {
	e := &pexEntry{}
	for addr := range addrs {
		if p.cfg.InPool(addr.NodeID) {
			e.addrs = append(e.addrs, pAddr{priority: p.priority(addr.NodeID), NodeAddress: addr})
		}
	}
	return e
}

type pexTable struct {
	fixed    *pexEntry
	bySender map[types.NodeID]*pexEntry
	extra    utils.RingBuf[*pexEntry]
}

func (t *pexTable) ClearSearched() {
	for e := range t.All() {
		e.searched = false
	}
}

func (t *pexTable) All() iter.Seq[*pexEntry] {
	return func(yield func(*pexEntry) bool) {
		if !yield(t.fixed) {
			return
		}
		for _, e := range t.bySender {
			if !yield(e) {
				return
			}
		}
		for e := range t.extra.All() {
			if !yield(e) {
				return
			}
		}
	}
}

type poolConfig struct {
	MaxIn      int                     // Maximal number of inbound connections.
	MaxOut     int                     // Maximal number of outbound connections.
	FixedAddrs []NodeAddress           // Addresses which are always available for dialing.
	InPool     func(types.NodeID) bool // InPool(id) <=> id belongs to this pool.
}

type poolManager struct {
	cfg      *poolConfig
	priority func(types.NodeID) uint64

	in            map[types.NodeID]struct{}
	out           map[types.NodeID]uint64
	dialing       map[types.NodeID]struct{}
	dialHistory   map[types.NodeID]struct{}
	upgradePermit bool
	pex           pexTable
}

func newPoolManager(cfg *poolConfig) *poolManager {
	h := sha256.New()
	var seed [32]byte
	utils.OrPanic1(rand.Read(seed[:]))
	utils.OrPanic1(h.Write(seed[:]))
	// PRF defining peer priority.
	// It makes the global topology converge to an uniformly random graph
	// of a bounded degree.
	priority := func(id types.NodeID) uint64 {
		return binary.LittleEndian.Uint64(h.Sum([]byte(id)))
	}
	p := &poolManager{
		cfg:           cfg,
		priority:      priority,
		in:            map[types.NodeID]struct{}{},
		out:           map[types.NodeID]uint64{},
		dialing:       map[types.NodeID]struct{}{},
		dialHistory:   map[types.NodeID]struct{}{},
		upgradePermit: false,
		pex: pexTable{
			bySender: map[types.NodeID]*pexEntry{},
			extra:    utils.NewRingBuf[*pexEntry](10),
		},
	}
	p.pex.fixed = p.newPexEntry(slices.Values(cfg.FixedAddrs))
	return p
}

func (p *poolManager) setDialing(id types.NodeID) {
	// We should keep around enough history to cover the whole pex.
	// We cannot keep it unbounded to avoid OOM.
	const maxHistory = 10000
	if len(p.dialHistory) >= maxHistory {
		p.dialHistory = map[types.NodeID]struct{}{}
	}
	p.dialing[id] = struct{}{}
	p.dialHistory[id] = struct{}{}
}

func (p *poolManager) toUpgrade() utils.Option[pNodeID] {
	low := utils.None[pNodeID]()
	if len(p.out) < p.cfg.MaxOut {
		return low
	}
	for old, priority := range p.out {
		if id, ok := low.Get(); !ok || priority < id.priority {
			low = utils.Some(pNodeID{priority, old})
		}
	}
	return low
}

// TryStartDial looks for a peer available for dialing.
// Marks peer as "dialing" on success.
// Returns a nonempty list of addresses of that peer.
// The complexity of a successful TryStartDial() is O(total pex size):
// - we assume the dialing to be infrequent
// - we assume the bound on the total number of pexEntries to be ~100 and the number of addresses per entry to be ~100.
// The complexity of failed TryStartDial() calls is amortized over the number of successful dial calls (+ number of inbound connections):
// TheTryStartDial marks entries of pex as "searched" and avoids processing them again
// until any event that can make any peer eligible for dialing again (dial failure/disconnect).
func (p *poolManager) TryStartDial() ([]NodeAddress, bool) {
	switch {
	// Dialing is not allowed if outbound connections are disabled.
	case p.cfg.MaxOut == 0:
		return nil, false
	// Regular dialing is allowed iff the current outbound connections (including ongoing dials)
	// do not saturate outbound capacity.
	case len(p.out)+len(p.dialing) < p.cfg.MaxOut: // Try to find address to dial.
	// Upgrades are allowed iff:
	// * we have upgrade permit
	// * outbound connections capacity is full
	// * there are no ongoing dials
	case p.upgradePermit && len(p.dialing) == 0 && len(p.out) == p.cfg.MaxOut:
	// Otherwise dialing is not feasible atm.
	default:
		return nil, false
	}
	// In case of upgrade, we need to find a peer better than the lowest current outbound connection.
	best := p.toUpgrade()
	bestAddrs := map[NodeAddress]struct{}{}
	for {
		clearRecent := false
		// Iterate over pex entries worth searching.
		for e := range p.pex.All() {
			if e.searched {
				continue
			}
			e.searched = true
			for _, addr := range e.addrs {
				if id, ok := best.Get(); ok {
					// Collect all addresses of the best node.
					// We check if bestAddr==0, because best is initialized to toUpgrade,
					// which is not a valid candidate.
					if len(bestAddrs) != 0 && id.NodeID == addr.NodeID {
						bestAddrs[addr.NodeAddress] = struct{}{}
						continue
					}
					if addr.priority <= id.priority {
						continue
					}
				}
				// Skip candidates which are not eligible for dialing.
				if _, ok := p.in[addr.NodeID]; ok {
					continue
				}
				if _, ok := p.out[addr.NodeID]; ok {
					continue
				}
				if _, ok := p.dialing[addr.NodeID]; ok {
					continue
				}
				if _, ok := p.dialHistory[addr.NodeID]; ok {
					clearRecent = true
					continue
				}
				// We have found a new best candidate.
				best = utils.Some(addr.pNodeID())
				clear(bestAddrs)
				bestAddrs[addr.NodeAddress] = struct{}{}
			}
		}
		if len(bestAddrs) > 0 {
			addrs := slices.Collect(maps.Keys(bestAddrs))
			p.setDialing(addrs[0].NodeID)
			p.pex.ClearSearched()
			return addrs, true
		}
		// clearRecent indicates that we have finished round robin over all available peers,
		// but if we clear dialHistory we will find a dialing candidate.
		if !clearRecent {
			return nil, false
		}
		p.dialHistory = map[types.NodeID]struct{}{}
		p.pex.ClearSearched()
	}
}

func (p *poolManager) PushUpgradePermit() {
	p.upgradePermit = true
}

func (p *poolManager) PushPex(sender utils.Option[types.NodeID], addrs []NodeAddress) {
	// Accept at most 1 address per NodeID from each pex sender.
	dedup := map[types.NodeID]NodeAddress{}
	for _, addr := range addrs {
		dedup[addr.NodeID] = addr
	}
	e := p.newPexEntry(maps.Values(dedup))
	if sender, ok := sender.Get(); ok {
		p.pex.bySender[sender] = e
	} else {
		if p.pex.extra.Full() {
			p.pex.extra.PopFront()
		}
		p.pex.extra.PushBack(e)
	}
}

func (p *poolManager) ClearPex(sender types.NodeID) {
	delete(p.pex.bySender, sender)
}

func (p *poolManager) DialFailed(id types.NodeID) error {
	if _, ok := p.dialing[id]; !ok {
		return errUnexpectedPeer
	}
	delete(p.dialing, id)
	// Failed dial -> id is available for redialing.
	p.pex.ClearSearched()
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
func (p *poolManager) Connect(id connID) (utils.Option[connID], error) {
	none := utils.None[connID]()
	if id.outbound {
		// Make sure that the peer was expected.
		if _, ok := p.dialing[id.NodeID]; !ok {
			return none, errUnexpectedPeer
		}
		delete(p.dialing, id.NodeID)
		// Insert the peer.
		priority := p.priority(id.NodeID)
		if toUpgrade, ok := p.toUpgrade().Get(); ok {
			// This should never happen, because of how the algorithm works:
			// we only dial peers that we know that will be accepted.
			if id.NodeID == toUpgrade.NodeID {
				panic("BUG: dialed a peer with too low priority")
			}
			// Consume the upgrade permit.
			p.upgradePermit = false
			delete(p.out, toUpgrade.NodeID)
			p.out[id.NodeID] = priority
			return utils.Some(connID{NodeID: toUpgrade.NodeID, outbound: true}), nil
		}
		p.out[id.NodeID] = priority
		return none, nil
	} else {
		// It is fine if new inbound connection overrides the old one.
		if _, ok := p.in[id.NodeID]; ok {
			return utils.Some(id), nil
		}
		// Check the inbound limit.
		if len(p.in) >= p.cfg.MaxIn {
			return none, errTooManyPeers
		}
		// Check if this is peer from our pool.
		if !p.cfg.InPool(id.NodeID) {
			return none, errNotInPool
		}
		p.in[id.NodeID] = struct{}{}
		return none, nil
	}
}

func (p *poolManager) Disconnect(id connID) error {
	if id.outbound {
		if _, ok := p.out[id.NodeID]; !ok {
			return errUnexpectedPeer
		}
		delete(p.out, id.NodeID)
	} else {
		if _, ok := p.in[id.NodeID]; !ok {
			return errUnexpectedPeer
		}
		delete(p.in, id.NodeID)
	}
	// Peer disconnected -> available for dialing.
	p.pex.ClearSearched()
	return nil
}
