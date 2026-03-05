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
	selfID types.NodeID
	// Maximal number of inbound connections.
	maxIn  int
	// Maximal number of outbound connections. 
	maxOut int
	// Maximal number of concurrent dials.
	maxDials int
	// Rate at which peers are dialed when when we have less than maxOut connections.
	dialRate rate.Limit
	// Rate at which outbound connections are replaced with connections with higher priority.
	upgradeRate rate.Limit
	// Addresses to dial once at the start.
	initialAddrs []NodeAddress
	// Addresses to dial periodically.
	bootstrapAddrs []NodeAddress
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

type poolConns[C peerConn] struct {
	in map[types.NodeID]C
	out ordered.Map[pNodeID,C]
	dialing map[types.NodeID]struct{}
	dialQueue []dialQueueEntry
	upgradePermit bool
}

func (cs *poolConns[C]) Busy(id pNodeID) bool {
	if _,ok := cs.in[id.NodeID]; ok { return true }
	if _,ok := cs.out.Get(id); ok { return true }
	if _,ok := cs.dialing[id.NodeID]; ok { return true }
	return false
}
type pexTable = map[types.NodeID][]NodeAddress

type pool[C peerConn] struct {
	poolConfig
	conns utils.Watch[*poolConns[C]]
	pexAddrs utils.Watch[*pexTable]
}

func newPool[C peerConn](cfg poolConfig) *pool[C] {
	p := &pool[C]{
		poolConfig: cfg,
		conns: utils.NewWatch(&poolConns[C]{}), 
		pexAddrs: utils.NewWatch(&pexTable{}),
	}
	p.setDialQueue(pexTable{ cfg.selfID: cfg.initialAddrs })
	return p
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

// Pushes pex addresses received from id. 
func (p *pool[C]) PushPexAddrs(id types.NodeID, addrs []NodeAddress) {
	for pexAddrs,ctrl := range p.pexAddrs.Lock() {
		(*pexAddrs)[id] = addrs
		// TODO: we only keep data for existing connections,
		// the question is how we shall prune pexAddrs for disconnected peers
		// to prevent exponential blowup of pexAddrs size.
		// - inbound conn pexAddrs should be pruned immediately - we didn't ask for it.
		// - outbound conn pexAddrs can be pruned when a new dial occurs. 
		ctrl.Updated()
	}
}


func (p *pool[C]) Evict(id types.NodeID) {
	for conns := range p.conns.Lock() {
		if c, ok := conns.in[id]; ok { c.Close() }
		if c, ok := conns.out.Get(prio(id)); ok { c.Close() }
	}
}

func (p *pool[C]) setDialQueue(addrsBySender pexTable) {
	// Regroup addresses by ID.
	addrsByID := map[types.NodeID]map[NodeAddress]struct{}{}
	for _,addrs := range addrsBySender {
		for _,addr := range addrs {
			if _,ok := addrsByID[addr.NodeID]; !ok {
				addrsByID[addr.NodeID] = map[NodeAddress]struct{}{}
			}
			addrsByID[addr.NodeID][addr] = struct{}{}
		}
	}
	// Sort by priority.
	q := make([]dialQueueEntry,0,len(addrsByID))
	for id,addrSet := range addrsByID {
		addrs := make([]NodeAddress,0,len(addrSet))
		for addr,_ := range addrSet { addrs = append(addrs, addr) }
		q = append(q,dialQueueEntry{prio(id),addrs})
	}
	slices.SortFunc(q, func(a,b dialQueueEntry) int {
		return -cmp.Compare(a.priority,b.priority)
	})
	// Set the queue.
	for conns,ctrl := range p.conns.Lock() {
		conns.dialQueue = q
		ctrl.Updated()
	}
}

func (conns *poolConns[C]) TryStartDial(p *pool[C]) ([]NodeAddress,bool) {
	switch {
	case p.maxOut==0 || len(conns.dialing) < p.maxDials: return nil,false
	case conns.out.Len()+len(conns.dialing)<p.maxOut: // Try to find address to dial.
	case conns.upgradePermit && conns.out.Len()==p.maxOut: // Try to find address to upgrade.
	default: return nil,false
	}
	// Drop already-connected peers from the queue.
	for len(conns.dialQueue)>0 && conns.Busy(conns.dialQueue[0].pNodeID) {
		conns.dialQueue = conns.dialQueue[1:]
	}
	if len(conns.dialQueue)==0 {
		return nil,false
	}
	// Check if the highest prio peer from the queue can be dialed.
	e := conns.dialQueue[0]
	conns.dialQueue = conns.dialQueue[1:]
	if id,_,ok := conns.out.GetAt(p.maxOut-1); ok {
		if id.priority >= e.priority {
			conns.dialQueue = nil
			return nil,false
		}
		conns.upgradePermit = false
	}
	// Set as dialing.
	conns.dialing[e.NodeID] = struct{}{}
	return e.addrs,true
}

func (p *pool[C]) StartDial(ctx context.Context) ([]NodeAddress,error) {
	for {
		// Try to start dial until success, or until queue is empty.
		for conns,ctrl := range p.conns.Lock() {
			for {
				addrs,ok := conns.TryStartDial(p)
				if ok {
					ctrl.Updated()
					return addrs,nil
				}
				if len(conns.dialQueue)==0 { break }
				if err:=ctrl.Wait(ctx); err!=nil {
					return nil,err
				}
			}
		}
		// Refill the queue.
		// TODO(gprusak): wrap the whole call into an async mutex, so that there is no race condition on refilling the queue.
		addrsBySender := map[types.NodeID][]NodeAddress{}
		for pexAddrs,ctrl := range p.pexAddrs.Lock() {
			if err:=ctrl.WaitUntil(ctx,func() bool { return len(*pexAddrs)>0 }); err!=nil {
				return nil, err
			}
			// TODO(gprusak): trim pexAddrs to maxIn+maxOut 
			addrsBySender,*pexAddrs = addrsBySender,*pexAddrs
		}
		p.setDialQueue(addrsBySender)
	}
}

func (p *pool[C]) DialFailed(id types.NodeID) {
	for conns,ctrl := range p.conns.Lock() {
		delete(conns.dialing,id)
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
	if info.ID == p.selfID {
		return errors.New("connection to self")
	}
	for conns,ctrl := range p.conns.Lock() {
		_,out := info.DialAddr.Get()
		if out {
			delete(conns.dialing,info.ID)
			if old,ok := conns.out.Set(prio(info.ID),conn); ok { old.Close() }
			for conns.out.Len() > p.maxOut {
				_,lowPrioConn,_ := conns.out.PopMin()
				lowPrioConn.Close()
			}
			ctrl.Updated()
			return nil
		}
		if old,ok := conns.in[info.ID]; ok {
			old.Close()
		} else if len(conns.in)>=p.maxIn {
			return errors.New("too many connections")
		}
		conns.in[info.ID] = conn 
		ctrl.Updated()
	}
	return nil
}

func (p *pool[C]) Disconnected(conn C) {
	info := conn.Info()
	shouldClearPex := false
	for conns,ctrl := range p.conns.Lock() {
		pID := prio(info.ID)
		if old, ok := conns.in[info.ID]; ok && old == conn {
			old.Close()
			delete(conns.in,info.ID)
			// We clear the pex iff conn is inbound and is the only connection
			// from the given peer that we have.
			// This is to prevent pexTable from being flooded by data from outbound connections.
			shouldClearPex = conns.out.GetOpt(pID).IsPresent()
			ctrl.Updated()
		}
		if old, ok := conns.out.Get(pID); ok && old == conn {
			old.Close()
			conns.out.Delete(pID)
			ctrl.Updated()
		}
	}
	if shouldClearPex {
		for pexAddrs := range p.pexAddrs.Lock() {
			delete(*pexAddrs,info.ID)
		}
	}
}

func (p *pool[C]) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Run upgrade limiter.
		s.Spawn(func() error {
			limiter := rate.NewLimiter(p.upgradeRate,1)
			for {
				if err := limiter.Wait(ctx); err!=nil { return err }
				for conns,ctrl := range p.conns.Lock() {
					if err:=ctrl.WaitUntil(ctx,func() bool { return !conns.upgradePermit }); err!=nil { return err }
					conns.upgradePermit = true
					ctrl.Updated()
				}
			}
		})

		if len(p.bootstrapAddrs)>0 {
			// Task feeding bootstrap addresses periodically.
			s.Spawn(func() error {
				const bootstrapInterval = 10 * time.Second
				for {
					// We use selfID as a placeholder to keep bootstrapAddrs and initialQueue.
					// This is not very elegant.
					p.PushPexAddrs(p.selfID, p.bootstrapAddrs)
					if err:=utils.Sleep(ctx,bootstrapInterval); err!=nil {
						return err
					}
				}
			})
		}
		return nil
	})
}


