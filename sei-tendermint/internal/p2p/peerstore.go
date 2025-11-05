package p2p

import (
	"errors"
	"fmt"
	"time"
	"context"
	"net"

	"github.com/tendermint/tendermint/libs/log"
	"github.com/gogo/protobuf/proto"
	"github.com/google/orderedcode"
	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	p2pproto "github.com/tendermint/tendermint/proto/tendermint/p2p"
	"github.com/tendermint/tendermint/types"
	"golang.org/x/sync/semaphore"
)

type errBadNetwork struct {error}

// <=1 per ID
// latest successful dial.
type dial struct {
	Address  NodeAddress
	SuccessTime time.Time
}

// peerInfoFromProto converts a Protobuf PeerInfo message to a peerInfo,
// erroring if the data is invalid.
func dialFromProto(msg *p2pproto.PeerInfo) (dial, error) {
	var last utils.Option[dial]
	for _, a := range msg.AddressInfo {
		address, err := ParseNodeAddress(a.Address)
		if err != nil {
			return dial{}, err
		}
		if a.LastDialSuccess == nil { continue }
		if x,ok := last.Get(); !ok || a.LastDialSuccess.After(x.SuccessTime) {
			last = utils.Some(dial{address,*a.LastDialSuccess})
		}
	}
	if x,ok := last.Get(); ok {
		return x, nil
	}
	return dial{}, errors.New("no successful dials")
}

// ToProto converts the peerInfo to p2pproto.PeerInfo for database storage. The
// Protobuf type only contains persisted fields, while ephemeral fields are
// discarded. The returned message may contain pointers to original data, since
// it is expected to be serialized immediately.
func (x dial) ToProto() *p2pproto.PeerInfo {
	return &p2pproto.PeerInfo{
		ID:            string(x.Address.NodeID),
		LastConnected: &x.SuccessTime,
		AddressInfo: []*p2pproto.PeerAddressInfo{{
			Address:      x.Address.String(),
			LastDialSuccess: &x.SuccessTime,
		}},
	}
}

// Database key prefixes.
const (
	prefixPeerInfo int64 = 1
)

// keyPeerInfo generates a peerInfo database key.
func keyPeerInfo(id types.NodeID) []byte {
	key, err := orderedcode.Append(nil, prefixPeerInfo, string(id))
	if err != nil {
		panic(err)
	}
	return key
}

// keyPeerInfoRange generates start/end keys for the entire peerInfo key range.
func keyPeerInfoRange() ([]byte, []byte) {
	start, err := orderedcode.Append(nil, prefixPeerInfo, "")
	if err != nil {
		panic(err)
	}
	end, err := orderedcode.Append(nil, prefixPeerInfo, orderedcode.Infinity)
	if err != nil {
		panic(err)
	}
	return start, end
}


type peerDB struct {
	db dbm.DB
	maxPeers utils.Option[int]
	dials map[types.NodeID]dial
}

func newPeerDB(db dbm.DB, maxPeers utils.Option[int]) (*peerDB, error) {
	dials := map[types.NodeID]dial{}
	start, end := keyPeerInfoRange()
	iter, err := db.Iterator(start, end)
	if err != nil {
		return nil, fmt.Errorf("db.Iterator(): %w", err)
	}
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var msg p2pproto.PeerInfo
		if err := proto.Unmarshal(iter.Value(), &msg); err != nil {
			return nil, fmt.Errorf("invalid peer Protobuf data: %w", err)
		}
		d, err := dialFromProto(&msg)
		if err!=nil {
			// Prune invalid data.
			if err:=db.Delete(iter.Key());err!=nil {
				return nil, fmt.Errorf("failed to delete invalid peer info: %w", err)
			}
			continue
		}
		dials[d.Address.NodeID] = d
	}
	if iter.Error() != nil {
		return nil, iter.Error()
	}
	return &peerDB{db,maxPeers,dials}, nil
}

func (db *peerDB) insert(d dial) error {
	db.dials[d.Address.NodeID] = d
	bz, err := d.ToProto().Marshal()
	if err != nil {
		panic(fmt.Errorf("info.ToProto().Marshal(): %w",err))
	}
	// TODO: prune old entries if over cap.
	return db.db.Set(keyPeerInfo(d.Address.NodeID), bz)
}

func (db *peerDB) remove(id types.NodeID) error {
	delete(db.dials, id)
	return db.db.Delete(keyPeerInfo(id))
}

////////////////////////////////////////////

// peerStore stores information about peers. It is not thread-safe.
type peerStore struct {
	logger log.Logger
	options     *PeerManagerOptions
	metrics *Metrics
	connTracker   *connTracker

	dialSem *semaphore.Weighted
	regularPeers *pool
	persistentPeers *pool
	db utils.Mutex[*peerDB]
}

// newPeerStore creates a new peer store, loading all persisted peers from the
// database into memory.
func newPeerStore(db dbm.DB, options *PeerManagerOptions, metrics *Metrics) (*peerStore, error) {
	// Persistent peers are populated from config.
	persistentPeers := newPool(poolOptions{selfID: options.SelfID})
	for _, addrs := range options.PersistentPeers {
		for _, addr := range addrs {
			if err := addr.Validate(); err != nil {
				return nil, err
			}
			persistentPeers.AddAddr(addr)
		}
	}
	// maxPeers/maxConns is computed to match the semantics of the old impl.
	// TODO: fix this
	maxPeers := options.MaxPeers - len(options.PersistentPeers)
	maxConns := options.MaxConnected - len(options.PersistentPeers)
	// Regular peers are populated from db.
	peerDB, err := newPeerDB(db, options.MaxPeers)
	if err != nil {
		return nil, err
	}
	var regularAddrs []NodeAddress
	for _, d := range peerDB.dials {
		if !options.persistent(d.Address.NodeID) {
			regularAddrs = append(regularAddrs,d.Address)
		}
	}
	regularPeers := newPool(poolOptions{
		selfID: options.SelfID,
		maxPeers: maxPeers,
		maxConns: maxConns,
		maxAddrsPerPeer: utils.Some(10),
	})
	regularPeers.AddAddrs(regularAddrs)

	return &peerStore{
		connTracker: newConnTracker(
			options.getMaxIncomingConnectionAttempts(),
			options.getIncomingConnectionWindow(),
		),
		dialSem: semaphore.NewWeighted(int64(options.numConccurentDials())),
		persistentPeers: persistentPeers,
		regularPeers: regularPeers,
		db: utils.NewMutex(peerDB),
		options: options,
		metrics: metrics,
	}, nil
}


// MaxPeers: 4 + 2*MaxConnected // Storage.
// Round robin through peer addresses when dialing.
// Prune other addresses on successful dial.
// Don't punish for failed dials - anyone can forge anyones address.

func (s *peerStore) AddAddrs(addrs []NodeAddress) error {
	var regularAddrs []NodeAddress
	for _, addr := range addrs {
		if s.options.persistent(addr.NodeID) {
			continue
		}
		if addr.NodeID == s.options.SelfID {
			s.logger.Info("can't add self to peer store, skipping address", "address", addr.String(), "self", s.options.SelfID)
			continue
		}
		if err := addr.Validate(); err != nil {
			return err
		}
		regularAddrs = append(regularAddrs,addr)
	}
	s.regularPeers.AddAddrs(regularAddrs)
	return nil
}

func (s *peerStore) IncBlockSyncs(id types.NodeID) {}
func (m *peerStore) SendUpdate(pu PeerUpdate) {}

// SendError reports a peer misbehavior to the router.
func (s *peerStore) SendError(pe PeerError) {
	s.logger.Error("peer error",
		"peer", pe.NodeID,
		"err", pe.Err,
		"evicting", pe.Fatal,
	)
	if pe.Fatal && !s.options.persistent(pe.NodeID) {
		s.logger.Info("evicting peer", "peer", pe.NodeID, "cause", pe.Err)
		s.regularPeers.Forget(pe.NodeID)
	}
}

func (s *peerStore) Accept(conn *Connection) error {
	id := conn.PeerInfo().NodeID
	if id == s.options.SelfID {
		return fmt.Errorf("rejecting connection from self (%v)", id)
	}
	if s.options.persistent(id) {
		return s.persistentPeers.AddConn(conn)
	}
	return s.regularPeers.AddConn(conn)
}

// Advertise returns a list of peer addresses to advertise to a peer.
func (s *peerStore) Advertise(limit int) []NodeAddress {
	var addrs []NodeAddress

	// advertise ourselves, to let everyone know how to dial us back
	// and enable mutual address discovery
	if addr,ok := s.options.SelfAddress.Get(); ok {
		addrs = append(addrs, addr)
	}

	// TODO: add successful dial tracking to pool.
	// TODO: advertise successful dials from persistent and regular.
	return addrs
}

// Status returns the status for a peer, primarily for testing.
func (s *peerStore) Connected(id types.NodeID) bool {
	if s.options.persistent(id) {
		return s.persistentPeers.Connected(id)
	} else {
		return s.regularPeers.Connected(id)
	}
}

func (s *peerStore) GetBlockSyncPeers() map[types.NodeID]bool {
	return s.options.BlockSyncPeers
}

func (s *peerStore) Run(ctx context.Context, r *Router) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.SpawnNamed("dial-persistent", func() error { return s.dialPeers(ctx, r, s.persistentPeers, math.MaxInt) })
		scope.SpawnNamed("dial-regular", func() error { return s.dialPeers(ctx, r, s.regularPeers) })
		return nil
	})
}

func (s *peerStore) AcceptAndRun(ctx context.Context, r *Router, tcpConn *net.TCPConn) error {
	defer tcpConn.Close()
	s.metrics.NewConnections.With("direction", "in").Add(1)
	incomingAddr := remoteEndpoint(tcpConn).AddrPort
	if err := s.connTracker.AddConn(incomingAddr); err != nil {
		return fmt.Errorf("rate limiting incoming peer %v: %w", incomingAddr, err)
	}
	defer s.connTracker.RemoveConn(incomingAddr)

	if err := r.filterPeersIP(ctx, incomingAddr); err != nil {
		s.logger.Debug("peer filtered by IP", "ip", incomingAddr, "err", err)
		return nil
	}

	conn, err := r.handshakePeer(ctx, tcpConn, utils.None[types.NodeID]())
	if err != nil {
		return fmt.Errorf("r.handshakePeer(): %v: %w", tcpConn, err)
	}
	peerInfo := conn.PeerInfo()
	if err := r.filterPeersID(ctx, peerInfo.NodeID); err != nil {
		s.logger.Debug("peer filtered by node ID", "node", peerInfo.NodeID, "err", err)
		return nil
	}
	if s.options.persistent(peerInfo.NodeID) {
		err = s.persistentPeers.AddConn(conn)
	} else {
		err = s.regularPeers.AddConn(conn)
	}
	if err != nil {
		return fmt.Errorf("failed to accept connection: op=incoming/accepted, peer=%v: %w", peerInfo.NodeID, err)
	}
	return conn.Run(ctx, r)
}

func (s *peerStore) dialStore(addr NodeAddress) error {
	for db := range s.db.Lock() {
		// TODO
	}
}

func (s *peerStore) dialPeers(ctx context.Context, r *Router, pool *pool, maxConns int) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		connSem := semaphore.NewWeighted(int64(maxConns))
		for {
			if err := connSem.Acquire(ctx, 1); err != nil {
				return err
			}
			if err := s.dialSem.Acquire(ctx, 1); err != nil {
				connSem.Release(1)
				return err
			}
			// TODO: separate routine for dialing persistent and regular peers.
			addr,err := pool.DialNext(ctx)
			if err!=nil { return err }
			scope.Spawn(func() error {
				defer connSem.Release(1)
				r.logger.Debug("Going to dial", "peer", addr.NodeID)
				// TODO(gprusak): this symmetric logic for handling duplicate connections is a source of race conditions:
				// if 2 nodes try to establish a connection to each other at the same time, both connections will be dropped.
				// Instead either:
				// * break the symmetry by favoring incoming connection iff my.NodeID > peer.NodeID
				// * keep incoming and outcoming connection pools separate to avoid the collision (recommended)
				conn, err := r.ConnectPeer(ctx, addr)
				pool.DialDone(addr, err)
				s.dialSem.Release(1)
				if err!=nil {
					s.logger.Error("failed to connect to peer", "peer", addr.NodeID, "err", err)
					return nil
				}
				if err:=s.dialStore(addr); err!=nil {
					return fmt.Errorf("s.dialStore(): %w",err)
				}
				if err:=pool.AddConn(conn); err!=nil {
					conn.Close()
					s.logger.Error("failed to add connection to pool", "peer", addr.NodeID, "err", err)
					return nil
				}
				err = conn.Run(ctx, r)
				s.logger.Error("[dial] Run()", "err", err)
				pool.DelConn(conn)
				return nil
			})

			// this jitters the frequency that we call
			// DialNext and prevents us from attempting to
			// create connections too quickly.
			if err := m.options.dialSleep(ctx); err != nil {
				return err
			}
		}
	})
}



