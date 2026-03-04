package p2p

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/ordered"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// None = -inf (i.e. beginning of time)
func before(a, b utils.Option[time.Time]) bool {
	bt, ok := b.Get()
	if !ok {
		return false
	}
	at, ok := a.Get()
	if !ok {
		return true
	}
	return at.Before(bt)
}

type poolConfig struct {
	selfID types.NodeID
	maxOut utils.Option[int]
	maxIn  utils.Option[int]
	minLifetime time.Duration
	bootstrapAddrs []pNodeAddress
}

type poolDial struct {
	connectedAt utils.Option[time.Time]
}

type poolConns[C peerConn] struct {
	conns map[pNodeID]C
	dial ordered.Map[pNodeID,*poolDial]
}

func (cs *poolConns[C]) Busy(id pNodeID) bool {
	if _,ok := cs.conns[id]; ok { return true }
	if _,ok := cs.dial.Get(id); ok { return true }
	return false
}

type pool[C peerConn] struct {
	poolConfig
	// TODO: include bootstrapAddrs in makeDialQueue()
	// TODO: rename to regularConns
	// TODO: add persistentConns
	conns utils.Watch[*poolConns[C]]
	
	pexAddrs utils.Watch[ordered.Map[pNodeID,[]pNodeAddress]]
	dialQueue utils.Watch[*[][]pNodeAddress]
}

func newPool[C peerConn](cfg poolConfig) *pool[C] {
	return &pool[C]{
		poolConfig: cfg,
		conns: utils.NewWatch(&poolConns[C]{}), 
	}
}

type peerConnInfo struct {
	ID       pNodeID
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
func (p *pool[C]) PushPexAddrs(id pNodeID, addrs []pNodeAddress) {
	for pexAddrs,ctrl := range p.pexAddrs.Lock() {
		pexAddrs.Set(id,addrs)
		if m,ok := p.maxConns.Get(); ok && pexAddrs.Len()>m {
			pexAddrs.PopMax()	
		}
		ctrl.Updated()
	}
}

func (p *pool[C]) Evict(id pNodeID) {
	for conns := range p.conns.Lock() {
		if c, ok := conns.conns[id]; ok {
			c.Close()
		}
	}
}

// TODO: out.GetAt(m)<priority => dial
func (p *pool[C]) SetDialing(id pNodeID) bool {
	maxOutbound := p.getMaxOutbound()
	if maxOutbound==utils.Some(0) { return false }
	// Try to set dialing status.
	for conns,ctrl := range p.conns.Lock() {
		if conns.Busy(id) {
			return false
		}
		if m,ok := maxOutbound.Get(); ok {
			if oldID,oldDial,ok := conns.dial.GetAt(int(m-1)); ok {
				if oldID.priority <= id.priority {
					return false
				}
				if t,ok := oldDial.connectedAt.Get(); !ok || t.Add(p.minLifetime).After(time.Now()) {
					return false
				}
			}
		}
		conns.dial.Set(id,&poolDial{})
		ctrl.Updated()
	}
	return true	
}

func (p *pool[C]) Run(ctx context.Context, initialQueue []pNodeAddress) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		if len(p.bootstrapAddrs)>0 {
			s.Spawn(func() error {
				const bootstrapInterval = 10 * time.Second
				for {
					for pexAddrs,ctrl := range p.pexAddrs.Lock() {
						// We use selfID as a placeholder to keep bootstrapAddrs and initialQueue.
						// This is not very elegant.
						pexAddrs.Set(pNodeID{priority:0,NodeID:p.selfID},p.bootstrapAddrs)
						ctrl.Updated()
					}
					if err:=utils.Sleep(ctx,bootstrapInterval); err!=nil {
						return err
					}
				}
			})
		}
		
		q := map[types.NodeID][]pNodeAddress{}
		for _,addr := range initialQueue {
			id := addr.NodeID
			q[id] = append(q[id],addr)
		}
		for {
			// TODO: sort q by piority
			for dialQueue,ctrl := range p.dialQueue.Lock() {
				// Fill the queue
				*dialQueue = q
				ctrl.Updated()
				// Wait for it to be emptied.
				if err:=ctrl.WaitUntil(ctx,func() bool { return len(*dialQueue)==0 }); err!=nil { return err }
			}

			// Repopulate the queue.
			clear(q)
			for pexAddrs,ctrl := range p.pexAddrs.Lock() {
				if err:=ctrl.WaitUntil(ctx,func() bool { return pexAddrs.Len()>0 }); err!=nil {
					return err
				}
				for _,addrs := range pexAddrs.All() {
					for _,addr := range addrs {
						id := addr.NodeID 
						q[id] = append(q[id],addr)
					}
				}
				pexAddrs.Clear()
			}
		}
	})
}

func getOpt[K comparable, V any](m map[K]V,k K) utils.Option[V] {
	if v,ok := m[k]; ok { return utils.Some(v) }
	return utils.None[V]()
}

func (p *pool[C]) StartDial(ctx context.Context) ([]pNodeAddress, error) {
	return utils.Recv(ctx,p.dialQueue)
}

func (p *pool[C]) Connected(conn C) (err error) {
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	info := conn.Info()
	if info.ID.NodeID == p.selfID {
		return errors.New("connection to self")
	}
	for conns := range p.conns.Lock() {
		_,newIsOutbound := info.DialAddr.Get()
		if newIsOutbound {
			dial,ok := conns.dial.Get(info.ID)
			if !ok {
				return fmt.Errorf("unexpected outbound connection")
			}
			dial.connectedAt = utils.Some(time.Now())
		}
		// If there is already a connection, then it is inbound.
		if old,ok := conns.conns[info.ID]; ok {
			// * allow to override connections in the same direction.
			// * inbound priority > outbound priority <=> peerID > selfID.
			//   This resolves the situation when peers try to connect to each other
			//   at the same time.
			_,oldIsOutbound := conns.dial.Get(info.ID)
			if oldIsOutbound != newIsOutbound && (info.ID.NodeID < p.selfID) != newIsOutbound {
				return fmt.Errorf("duplicate connection from peer %q", info.ID)
			}
			old.Close()
			conns.conns[info.ID] = conn
			if newIsOutbound {
				conns.dial.Delete(info.ID)
			}
			return nil
		}
		// TODO: here we cannot verify the outbound connections limit.
		if m, ok := p.maxConns.Get(); ok && len(conns.conns) >= m {
			return errors.New("too many connections")
		}
		conns.conns[info.ID] = conn 
	}
	return nil
}

func (p *pool[C]) Disconnected(conn C) {
	info := conn.Info()
	for conns := range p.conns.Lock() {
		if old, ok := conns.conns[info.ID]; ok && old == conn {
			old.Close()
			delete(conns.conns,info.ID)
			conns.dial.Delete(info.ID)
		}
	}
}
