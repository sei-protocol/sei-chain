package p2p

import (
	"context"
	"errors"
	"time"
	"slices"
	"cmp"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/ordered"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"golang.org/x/time/rate"
)

type poolConfig struct {
	SelfID types.NodeID
	// Maximal number of inbound connections.
	MaxIn  int
	// Maximal number of outbound connections. 
	MaxOut int
	// Maximal number of concurrent dials.
	MaxDials int
	// Rate at which outbound connections are replaced with connections with higher priority.
	UpgradeRate rate.Limit
	// Addresses to dial once at the start.
	InitialAddrs []NodeAddress
	// Addresses to dial periodically.
	BootstrapAddrs []NodeAddress
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

func prio(types.NodeID) pNodeID { panic("unimplemented") }

type dialQueueEntry struct {
	pNodeID
	addrs []NodeAddress
}

type poolInner[C peerConn] struct {
	in map[types.NodeID]C
	out ordered.Map[pNodeID,C]
	dialing map[types.NodeID]struct{}
	dialQueue []dialQueueEntry
	upgradePermit bool
}

func (cs *poolInner[C]) Busy(id pNodeID) bool {
	if _,ok := cs.in[id.NodeID]; ok { return true }
	if _,ok := cs.out.Get(id); ok { return true }
	if _,ok := cs.dialing[id.NodeID]; ok { return true }
	return false
}

type pexTable struct {
	initial []NodeAddress
	bootstrap []NodeAddress
	bySender map[types.NodeID][]NodeAddress
	cleared [][]NodeAddress
}

func (t *pexTable) Empty() bool {
	return len(t.initial)==0 && len(t.bootstrap)==0 && len(t.bySender) == 0 && len(t.cleared) == 0
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
	if len(t.bootstrap)>0 {
		out = append(out,t.bootstrap)
	}
	for _,addrs := range t.bySender {
		out = append(out, addrs)
	}
	// Clear the table.
	*t = pexTable{}
	return out
}

type pool[C peerConn] struct {
	cfg *poolConfig
	inner utils.Watch[*poolInner[C]]
	pexAddrs utils.Watch[*pexTable]
}

func newPool[C peerConn](cfg *poolConfig) *pool[C] {
	return &pool[C]{
		cfg: cfg,
		inner: utils.NewWatch(&poolInner[C]{}), 
		pexAddrs: utils.NewWatch(&pexTable{
			initial:cfg.InitialAddrs,
			bootstrap:cfg.BootstrapAddrs,
		}),
	}
}

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

// Sets pex addresses received from id. 
func (p *pool[C]) SetPexAddrs(id types.NodeID, addrs []NodeAddress) {
	for pexAddrs,ctrl := range p.pexAddrs.Lock() {
		pexAddrs.bySender[id] = addrs
		ctrl.Updated()
	}
}

func (p *pool[C]) ClearPexAddrs(id types.NodeID) {
	const maxClearedCache = 10
	for pexAddrs := range p.pexAddrs.Lock() {
		addrs,ok := pexAddrs.bySender[id]
		if !ok { return }
		delete(pexAddrs.bySender,id)
		if len(pexAddrs.cleared) < maxClearedCache {
			pexAddrs.cleared = append(pexAddrs.cleared, addrs)
		}
	}
}

func (p *pool[C]) Close(id types.NodeID) {
	for inner := range p.inner.Lock() {
		if c, ok := inner.in[id]; ok { c.Close() }
		if c, ok := inner.out.Get(prio(id)); ok { c.Close() }
	}
}

func (p *pool[C]) setDialQueue(addrs [][]NodeAddress) {
	// Regroup addresses by ID.
	byID := map[types.NodeID]map[NodeAddress]struct{}{}
	for _,addrs := range addrs {
		for _,addr := range addrs {
			if _,ok := byID[addr.NodeID]; !ok {
				byID[addr.NodeID] = map[NodeAddress]struct{}{}
			}
			byID[addr.NodeID][addr] = struct{}{}
		}
	}
	// Sort by priority.
	q := make([]dialQueueEntry,0,len(byID))
	for id,addrSet := range byID {
		addrs := make([]NodeAddress,0,len(addrSet))
		for addr,_ := range addrSet { addrs = append(addrs, addr) }
		q = append(q,dialQueueEntry{prio(id),addrs})
	}
	slices.SortFunc(q, func(a,b dialQueueEntry) int {
		return -cmp.Compare(a.priority,b.priority)
	})
	// Set the queue.
	for inner,ctrl := range p.inner.Lock() {
		inner.dialQueue = q
		ctrl.Updated()
	}
}

func (p *pool[C]) tryStartDial(inner *poolInner[C]) ([]NodeAddress,bool) {
	switch {
	case p.cfg.MaxOut==0 || len(inner.dialing) < p.cfg.MaxDials: return nil,false
	case inner.out.Len()+len(inner.dialing)<p.cfg.MaxOut: // Try to find address to dial.
	case inner.upgradePermit && inner.out.Len()==p.cfg.MaxOut: // Try to find address to upgrade.
	default: return nil,false
	}
	// Drop already-connected peers from the queue.
	for len(inner.dialQueue)>0 && inner.Busy(inner.dialQueue[0].pNodeID) {
		inner.dialQueue = inner.dialQueue[1:]
	}
	if len(inner.dialQueue)==0 {
		return nil,false
	}
	// Check if the highest prio peer from the queue can be dialed.
	e := inner.dialQueue[0]
	inner.dialQueue = inner.dialQueue[1:]
	if id,_,ok := inner.out.GetAt(p.cfg.MaxOut-1); ok {
		if id.priority >= e.priority {
			inner.dialQueue = nil
			return nil,false
		}
		inner.upgradePermit = false
	}
	// Set as dialing.
	inner.dialing[e.NodeID] = struct{}{}
	return e.addrs,true
}

func (p *pool[C]) StartDial(ctx context.Context) ([]NodeAddress,error) {
	for {
		// Try to start dial until success, or until queue is empty.
		for inner,ctrl := range p.inner.Lock() {
			for {
				addrs,ok := p.tryStartDial(inner)
				if ok {
					ctrl.Updated()
					return addrs,nil
				}
				if len(inner.dialQueue)==0 { break }
				if err:=ctrl.Wait(ctx); err!=nil {
					return nil,err
				}
			}
		}
		// Refill the queue.
		// TODO(gprusak): wrap the whole call into an async mutex, so that there is no race condition on refilling the queue.
		var addrs [][]NodeAddress 
		for pexAddrs,ctrl := range p.pexAddrs.Lock() {
			if err:=ctrl.WaitUntil(ctx,func() bool { return !pexAddrs.Empty() }); err!=nil {
				return nil, err
			}
		  addrs = pexAddrs.Pop()	
		}
		p.setDialQueue(addrs)
	}
}

func (p *pool[C]) DialFailed(id types.NodeID) {
	for inner,ctrl := range p.inner.Lock() {
		delete(inner.dialing,id)
		ctrl.Updated()
	}
}

func (p *pool[C]) Connected(conn C) (err error) {
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	info := conn.Info()
	if info.ID == p.cfg.SelfID {
		return errors.New("connection to self")
	}
	for inner,ctrl := range p.inner.Lock() {
		if info.DialAddr.IsPresent() {
			delete(inner.dialing,info.ID)
			if old,ok := inner.out.Set(prio(info.ID),conn); ok { old.Close() }
			for inner.out.Len() > p.cfg.MaxOut {
				_,lowPrioConn,_ := inner.out.PopMin()
				lowPrioConn.Close()
			}
		} else {
			if old,ok := inner.in[info.ID]; ok {
				old.Close()
			} else if len(inner.in)>=p.cfg.MaxIn {
				return errors.New("too many connections")
			}
			inner.in[info.ID] = conn 
		}
		ctrl.Updated()
	}
	return nil
}

func (p *pool[C]) Disconnected(conn C) {
	info := conn.Info()
	for inner,ctrl := range p.inner.Lock() {
		pID := prio(info.ID)
		if old, ok := inner.in[info.ID]; ok && old == conn {
			old.Close()
			delete(inner.in,info.ID)
			ctrl.Updated()
		}
		if old, ok := inner.out.Get(pID); ok && old == conn {
			old.Close()
			inner.out.Delete(pID)
			ctrl.Updated()
		}
	}
}

func (p *pool[C]) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Run upgrade limiter.
		s.Spawn(func() error {
			limiter := rate.NewLimiter(p.cfg.UpgradeRate,1)
			for {
				if err := limiter.Wait(ctx); err!=nil { return err }
				for inner,ctrl := range p.inner.Lock() {
					if err:=ctrl.WaitUntil(ctx,func() bool { return !inner.upgradePermit }); err!=nil { return err }
					inner.upgradePermit = true
					ctrl.Updated()
				}
			}
		})

		if len(p.cfg.BootstrapAddrs)>0 {
			// Task feeding bootstrap addresses periodically.
			s.Spawn(func() error {
				const bootstrapInterval = 10 * time.Second
				for {
					for pexAddrs,ctrl := range p.pexAddrs.Lock() {
						pexAddrs.bootstrap = p.cfg.BootstrapAddrs
						ctrl.Updated()
					}
					if err:=utils.Sleep(ctx,bootstrapInterval); err!=nil {
						return err
					}
				}
			})
		}
		return nil
	})
}


