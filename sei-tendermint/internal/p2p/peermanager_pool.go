package p2p

import (
	"context"
	"errors"
	"time"
	"crypto/sha256"
	"crypto/rand"
	"encoding/binary"
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

type pool[C peerConn] struct {
	withPriority func(types.NodeID) pNodeID
	cfg *poolConfig
	inner utils.Watch[*poolInner[C]]
}

func newPool[C peerConn](cfg *poolConfig) *pool[C] {
	h := sha256.New()
	var seed [32]byte
	utils.OrPanic1(rand.Read(seed[:]))
	utils.OrPanic1(h.Write(seed[:]))
	return &pool[C]{
		cfg: cfg,
		inner: utils.NewWatch(&poolInner[C]{}), 
		withPriority: func(id types.NodeID) pNodeID {
			return pNodeID{
				priority: binary.LittleEndian.Uint64(h.Sum([]byte(id)[:8])),
				NodeID: id,
			}
		},
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

func (p *pool[C]) Get(id types.NodeID) []C {
	var out []C
	for inner := range p.inner.Lock() {
		if conn, ok := inner.in[id]; ok { out = append(out,conn) }
		if conn, ok := inner.out.Get(p.withPriority(id)); ok { out = append(out,conn) }
	}
	return out
}

func (p *pool[C]) SetDialQueue(addrs []NodeAddress) {
	// Regroup addresses by ID.
	byID := map[types.NodeID]map[NodeAddress]struct{}{}
	for _,addr := range addrs {
		if _,ok := byID[addr.NodeID]; !ok {
			byID[addr.NodeID] = map[NodeAddress]struct{}{}
		}
		byID[addr.NodeID][addr] = struct{}{}
	}
	// Sort by priority.
	q := make([]dialQueueEntry,0,len(byID))
	for id,addrSet := range byID {
		addrs := make([]NodeAddress,0,len(addrSet))
		for addr,_ := range addrSet { addrs = append(addrs, addr) }
		q = append(q,dialQueueEntry{p.withPriority(id),addrs})
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
			if old,ok := inner.out.Set(p.withPriority(info.ID),conn); ok { old.Close() }
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
		pID := p.withPriority(info.ID)
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

func (p *pool[C]) State(id types.NodeID) string {
	for inner := range p.inner.Lock() {
		if _,ok := inner.dialing[id]; ok {
			return "dialing"
		}
		_,in := inner.in[id]
		_,out := inner.out.Get(p.withPriority(id))
		if in || out {
			return "ready,connected"
		}
	}
	return ""
}
