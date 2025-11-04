package p2p

import (
	"errors"
	"fmt"
	"time"

	"github.com/tendermint/tendermint/libs/log"
	"github.com/gogo/protobuf/proto"
	"github.com/google/orderedcode"
	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/libs/utils"
	p2pproto "github.com/tendermint/tendermint/proto/tendermint/p2p"
	"github.com/tendermint/tendermint/types"
)

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
	// dynamically added private peers
	dynamicPrivatePeers utils.Mutex[map[types.NodeID]struct{}]

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
		persistentPeers: persistentPeers,
		regularPeers: regularPeers,
		db: utils.NewMutex(peerDB),
		options: options,
		metrics: metrics,
	}, nil
}


// Delete deletes a peer, or does nothing if it does not exist.
func (s *peerStore) Delete(id types.NodeID) error {
	if s.options.persistent(id) { return nil }
	s.regularPeers.Forget(id)
	return s.db.remove(id)
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
	if pe.Fatal {
		s.logger.Info("evicting peer", "peer", pe.NodeID, "cause", pe.Err)
		s.Delete(pe.NodeID)
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

	// TODO: advertise successfully dialed peers.
	for _, peer := range s.Ranked() {
		for _, info := range peer.AddressInfo {
			if len(addrs) >= limit { return addrs }
			// only add non-private NodeIDs
			if _, ok := s.options.PrivatePeers[peer.ID]; ok { continue }
			if _, ok := s.dynamicPrivatePeers[peer.ID]; ok { continue }
			addrs = append(addrs, info.Address)
		}
	}
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

// PeerRatio returns the ratio of peer addresses stored to the maximum size.
func (m *peerStore) PeerRatio() float64 {
	// TODO: combine persistent and regular peers
	for s := range m.store.Lock() {
		maxPeers,ok := s.options.MaxPeers.Get()
		if !ok { return 0 }
		return float64(s.Size()) / float64(maxPeers)
	}
	panic("unreachable")
}

func (s *peerStore) GetBlockSyncPeers() map[types.NodeID]bool {
	return s.options.BlockSyncPeers
}

func (s *peerStore) AddPrivatePeer(id types.NodeID) {
	s.dynamicPrivatePeers[id] = struct{}{}
}



