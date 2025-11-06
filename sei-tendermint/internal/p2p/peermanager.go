package p2p

import (
	"fmt"
	"time"
	"context"
	"errors"

	"github.com/tendermint/tendermint/libs/utils/im"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/types"
)

const maxAddrsPerPeer = 10

// latest successful dial.
type dial struct {
	Address  NodeAddress
	SuccessTime time.Time
}

type peerAddrs struct {
	lastFail utils.Option[time.Time]
	// Invariants:
	// * fails is a subset of addrs.
	addrs map[NodeAddress]struct{}
	fails map[NodeAddress]time.Time
}

func newPeerAddrs() *peerAddrs {
	return &peerAddrs{
		addrs: map[NodeAddress]struct{}{},
		fails: map[NodeAddress]time.Time{},
	}
}

type peerManagerInner struct {
	options *PeerManagerOptions
	isUnconditional map[types.NodeID]bool
	isPrivate map[types.NodeID]bool
	persistentAddrs map[types.NodeID]*peerAddrs
	// TODO: consider replacing Connection with an interface
	// * comparable
	// * Close()
	// * PeerInfo() types.NodeInfo
	// * DialAddr() utils.Option[NodeAddress]
	// to make it testable without router.
	conns utils.AtomicSend[im.Map[types.NodeID,*Connection]]
	conditionalConns int // counts conditional connections in conns.
	addrs map[types.NodeID]*peerAddrs
	dialing map[types.NodeID]NodeAddress
}

func (i *peerManagerInner) findFailedPeer() (types.NodeID,bool) {
	conns := i.conns.Load()
	for old,pa := range i.addrs {
		if _,ok := i.dialing[old]; ok { continue }
		if _,ok := conns.Get(old); ok { continue }
		if len(pa.fails)<len(pa.addrs) { continue }
		return old,true
	}
	return "",false
}

func (i *peerManagerInner) isPersistent(id types.NodeID) bool {
	_,ok := i.persistentAddrs[id]
	return ok
}

func (i *peerManagerInner) AddAddr(addr NodeAddress) bool {
	id := addr.NodeID
	// Adding persistent peer addrs is only allowed during initialization.
	// This is to make sure that malicious peers won't cause the preconfigured addrs to be dropped.
	if i.isPersistent(id) { return false }
	pa,ok := i.addrs[id]
	// Add new peerAddrs if missing.
	if !ok {
		// Prune some peer if maxPeers limit has been reached.
		if len(i.addrs) == i.options.maxPeers() {
			toPrune,ok := i.findFailedPeer()
			if !ok { return false }
			delete(i.addrs, toPrune)
		}
		pa = newPeerAddrs()
		i.addrs[id] = pa
	}
	// Ignore duplicate address.
	if _,ok := pa.addrs[addr]; ok { return false }
	// Prune any failing address if maxAddrsPerPeer has been reached.
	if len(pa.addrs) == maxAddrsPerPeer {
		var failedAddr utils.Option[NodeAddress]
		for old,_ := range pa.fails {
			failedAddr = utils.Some(old)
			break
		}
		toPrune,ok := failedAddr.Get()
		if !ok { return false }
		delete(pa.addrs, toPrune)
	}
	pa.addrs[addr] = struct{}{}
	return true
}

func before(a, b utils.Option[time.Time]) bool {
	at,ok := a.Get()
	if !ok { return false }
	bt,ok := b.Get()
	if !ok { return true }
	return at.Before(bt)
}

func get[K comparable, V any](m map[K]V, k K) utils.Option[V] {
	if v,ok := m[k]; ok {
		return utils.Some(v)
	}
	return utils.None[V]()
}

func (i *peerManagerInner) TryStartDial(persistentPeer bool) (NodeAddress,bool) {
	// Check concurrent dials limit.
	if len(i.dialing) >= i.options.maxDials() {
		return NodeAddress{},false
	}
	conns := i.conns.Load()
	// Check max connections limit (unless we are dialing a persistent peer).
	if !persistentPeer && len(i.dialing) + conns.Len() >= i.options.maxConns() {
		return NodeAddress{},false
	}
	// Choose peer with the oldest lastFail.
	var bestPeer utils.Option[*peerAddrs]
	addrs := i.addrs
	if persistentPeer {
		addrs = i.persistentAddrs
	}
	for id,peerAddrs := range addrs {
		if i.isPersistent(id) != persistentPeer || len(peerAddrs.addrs)==0 { continue }
		if _,ok := i.dialing[id]; ok { continue }
		if _,ok := conns.Get(id); ok { continue }
		if x,ok := bestPeer.Get(); !ok || before(peerAddrs.lastFail, x.lastFail) {
			bestPeer = utils.Some(peerAddrs)
		}
	}
	// Choose address with the oldest lastFail.
	var best utils.Option[NodeAddress]
	if peer,ok := bestPeer.Get(); ok {
		for addr,_ := range peer.addrs {
			if x,ok := best.Get(); !ok || before(get(peer.fails,addr), get(peer.fails,x)) {
				best = utils.Some(addr)
			}
		}
	}
	if x,ok := best.Get(); ok {
		i.dialing[x.NodeID] = x
	}
	return best.Get()
}

func (i *peerManagerInner) DialFailed(addr NodeAddress) {
	if i.dialing[addr.NodeID] == addr {
		delete(i.dialing, addr.NodeID)
	}
	var peerAddrs *peerAddrs
	var ok bool
	if i.isPersistent(addr.NodeID) {
		peerAddrs,ok = i.persistentAddrs[addr.NodeID]
	} else {
		peerAddrs,ok = i.addrs[addr.NodeID]
	}
	if !ok { return }
	// Record the failure time.
	now := time.Now()
	peerAddrs.lastFail = utils.Some(now)
	if _,ok := peerAddrs.addrs[addr]; ok {
		peerAddrs.fails[addr] = now
	}
}

func (i *peerManagerInner) Drop(id types.NodeID) {
	if !i.isPersistent(id) {
		delete(i.addrs, id)
	}
	if !i.isUnconditional[id] {
		if conn,ok := i.conns.Load().Get(id); ok {
			conn.Close()
		}
	}
}

// Connected registers a new connection.
// If it is an outbound connection the dialing status is cleared (EVEN IF IT RETURNS AN ERROR).
// It atomically checks maximum connection limit and duplicate connections.
// TODO(gprusak): instead of returning an error. Call conn.Close(cause), once it is supported.
func (i *peerManagerInner) Connected(conn *Connection) error {
	if addr,ok := conn.dialAddr.Get(); ok && i.dialing[addr.NodeID] == addr {
		delete(i.dialing, addr.NodeID)
	}
	selfID := i.options.SelfID
	peerID := conn.PeerInfo().NodeID
	if peerID == selfID {
		return errors.New("connection to self")
	}
	conns := i.conns.Load()
	if old,ok := conns.Get(peerID); ok {
		// * allow to override connections in the same direction.
		// * allow inbound conn to override outbound iff peerID > selfID.
		//   This resolves the situation when peers try to connect to each other
		//   at the same time.
		if old.dialAddr.IsPresent() != conn.dialAddr.IsPresent() && (peerID < selfID) != conn.dialAddr.IsPresent() {
			return fmt.Errorf("duplicate connection from peer %q", peerID)
		}
		old.Close()
		i.conns.Store(conns.Set(peerID, conn))
		return nil
	}
	if !i.isUnconditional[peerID] {
		if i.conditionalConns >= i.options.maxConns() {
			conn.Close()
			return errors.New("too many connections")
		}
		i.conditionalConns += 1
	}
	i.conns.Store(conns.Set(peerID, conn))
	return nil
}

func (i *peerManagerInner) Disconnected(conn *Connection) {
	peerID := conn.PeerInfo().NodeID
	conns := i.conns.Load()
	if old, ok := conns.Get(peerID); ok && old == conn {
		if !i.isUnconditional[peerID] {
			i.conditionalConns -= 1
		}
		old.Close()
		i.conns.Store(conns.Delete(peerID))
	}
}

// PeerManager manages connections and addresses of potential peers.
// PeerManager may trigger disconnects by calling conn.Close() in case of conflicting connections.
// Possible lifecycles of the connection are as follows:
// For outbound connections:
// * StartDial() -> [dialing] -> Connected(conn) -> [communicate] -> Disconnected(conn)
// * StartDial() -> [dialing] -> DialFailed(addr)
// For inbound connections:
// * Connected(conn) -> [communicate] -> Disconnected(conn)
// For adding new peer addrs, call AddAddrs().
type PeerManager struct {
	options *PeerManagerOptions
	isBlockSyncPeer map[types.NodeID]bool
	// Receiver of the inner.conns. It is copyable and allows accessing connections
	// without taking lock on inner.
	conns utils.AtomicRecv[im.Map[types.NodeID,*Connection]]
	inner utils.Watch[*peerManagerInner]
}

// PeerUpdatesRecv.
// NOT THREAD-SAFE.
type PeerUpdatesRecv struct {
	recv utils.AtomicRecv[im.Map[types.NodeID,*Connection]]
	last map[types.NodeID]struct{}
}

// PeerUpdate is a peer update event sent via PeerUpdates.
type PeerUpdate struct {
	NodeID   types.NodeID
	Status   PeerStatus
	Channels ChannelIDSet
}

func (s *PeerUpdatesRecv) Recv(ctx context.Context) (PeerUpdate,error) {
	var update PeerUpdate
	_, err := s.recv.Wait(ctx, func(conns im.Map[types.NodeID,*Connection]) bool {
		// Check for disconnected peers.
		for id,_ := range s.last {
			if _,ok := conns.Get(id); !ok {
				delete(s.last, id)
				update = PeerUpdate{
					NodeID: id,
					Status: PeerStatusDown,
				}
				return true
			}
		}
		// Check for connected peers.
		for id,conn := range conns.All() {
			if _,ok := s.last[id]; !ok {
				s.last[id] = struct{}{}
				update = PeerUpdate{
					NodeID: id,
					Status: PeerStatusUp,
					Channels: conn.peerChannels,
				}
				return true
			}
		}
		return false
	})
	return update, err
}

func (m *PeerManager) Subscribe() *PeerUpdatesRecv {
	return &PeerUpdatesRecv{
		recv: m.conns,
		last: map[types.NodeID]struct{}{},
	}
}

func NewPeerManager(options *PeerManagerOptions) (*PeerManager,error) {
	if err:=options.Validate(); err!=nil {
		return nil, err
	}
	inner := &peerManagerInner{
		options: options,
		persistentAddrs: map[types.NodeID]*peerAddrs{},
		isPrivate: map[types.NodeID]bool{},
		isUnconditional: map[types.NodeID]bool{},

		conns: utils.NewAtomicSend(im.NewMap[types.NodeID,*Connection]()),
		addrs: map[types.NodeID]*peerAddrs{},
		dialing: map[types.NodeID]NodeAddress{},
	}
	isBlockSyncPeer := map[types.NodeID]bool{}
	for _,id := range options.PrivatePeers { inner.isPrivate[id] = true }
	for _,id := range options.UnconditionalPeers { inner.isUnconditional[id] = true }
	for _,addr := range options.PersistentPeers {
		inner.isUnconditional[addr.NodeID] = true
		if _,ok := inner.persistentAddrs[addr.NodeID]; !ok {
			inner.persistentAddrs[addr.NodeID] = newPeerAddrs()
		}
		inner.persistentAddrs[addr.NodeID].addrs[addr] = struct{}{}
	}
	for _,addr := range options.BootstrapPeers { inner.AddAddr(addr) }
	for _,id := range options.BlockSyncPeers { isBlockSyncPeer[id] = true }
	return &PeerManager {
		options: options,
		isBlockSyncPeer: isBlockSyncPeer,
		conns: inner.conns.Subscribe(),
		inner: utils.NewWatch(inner),
	},nil
}

func (m *PeerManager) AddAddrs(addrs []NodeAddress) error {
	for inner,ctrl := range m.inner.Lock() {
		for _,addr := range addrs {
			if err:=addr.Validate();err!=nil {
				return err
			}
			if inner.AddAddr(addr) {
				ctrl.Updated()
			}
		}
	}
	return nil
}

func (m *PeerManager) StartDial(ctx context.Context, persistentPeer bool) (NodeAddress,error) {
	for inner,ctrl := range m.inner.Lock() {
		for {
			if addr,ok := inner.TryStartDial(persistentPeer); ok {
				return addr, nil
			}
			if err:=ctrl.Wait(ctx); err!=nil {
				return NodeAddress{}, err
			}
		}
	}
	panic("unreachable")
}

// Release releases the reservation made via Acquire.
func (p *PeerManager) DialFailed(addr NodeAddress) {
	for inner,ctrl := range p.inner.Lock() {
		ctrl.Updated()
		inner.DialFailed(addr)
	}
}

func (m *PeerManager) Connected(conn *Connection) error {
	for inner,ctrl := range m.inner.Lock() {
		ctrl.Updated()
		return inner.Connected(conn)
	}
	panic("unreachable")
}

func (m *PeerManager) Disconnected(conn *Connection) {
	for inner,ctrl := range m.inner.Lock() {
		ctrl.Updated()
		inner.Disconnected(conn)
	}
}

func (m *PeerManager) Drop(id types.NodeID) {
	for inner,ctrl := range m.inner.Lock() {
		ctrl.Updated()
		inner.Drop(id)
	}
	panic("unreachable")
}

func (m *PeerManager) IsBlockSyncPeer(id types.NodeID) bool {
	return len(m.isBlockSyncPeer)==0 || m.isBlockSyncPeer[id]
}

func (m *PeerManager) State(id types.NodeID) string {
	for inner := range m.inner.Lock() {
		if _,ok := inner.conns.Load().Get(id); ok {
			return "ready,connected"
		}
		if _,ok := inner.dialing[id]; ok {
			return "dialing"
		}
	}
	return ""
}
