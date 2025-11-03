package p2p

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"


	"github.com/gogo/protobuf/proto"
	"github.com/google/orderedcode"
	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/libs/utils"
	p2pproto "github.com/tendermint/tendermint/proto/tendermint/p2p"
	"github.com/tendermint/tendermint/types"
)

// peerStore stores information about peers. It is not thread-safe, assuming it
// is only used by PeerManager which handles concurrency control. This allows
// the manager to execute multiple operations atomically via its own mutex.
//
// The entire set of peers is kept in memory, for performance. It is loaded
// from disk on initialization, and any changes are written back to disk
// (without fsync, since we can afford to lose recent writes).
type peerStore struct {
	options *PeerManagerOptions
	db      dbm.DB
	peers   map[types.NodeID]*peerInfo
	dynamicPrivatePeers map[types.NodeID]struct{} // dynamically added private peers
	ranked  utils.Option[[]*peerInfo]

	upgrading           map[types.NodeID]types.NodeID              // peers claimed for upgrade (DialNext → Dialed/DialFail)
	ready               utils.Watch[map[types.NodeID]ChannelIDSet] // ready peers (Ready → Disconnected)

	metrics *Metrics
}

func (s *peerStore) NumConnected() int {
	cnt := 0
	for _,p := range s.peers {
		if p.connected && !s.options.UnconditionalPeers[p.ID] {
			cnt++
		}
	}
	return cnt
}

// newPeerStore creates a new peer store, loading all persisted peers from the
// database into memory.
func newPeerStore(db dbm.DB, options *PeerManagerOptions, metrics *Metrics) (*peerStore, error) {
	peers := map[types.NodeID]*peerInfo{}

	start, end := keyPeerInfoRange()
	iter, err := db.Iterator(start, end)
	if err != nil {
		return nil, fmt.Errorf("db.Iterator(): %w", err)
	}
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		// FIXME: We may want to tolerate failures here, by simply logging
		// the errors and ignoring the faulty peer entries.
		msg := new(p2pproto.PeerInfo)
		if err := proto.Unmarshal(iter.Value(), msg); err != nil {
			return nil, fmt.Errorf("invalid peer Protobuf data: %w", err)
		}
		peer, err := storedPeerInfoFromProto(msg)
		if err != nil {
			return nil, fmt.Errorf("invalid peer data: %w", err)
		}
		peers[peer.ID] = &peerInfo {
			storedPeerInfo: peer,
		}
	}
	if iter.Error() != nil {
		return nil, iter.Error()
	}
	return &peerStore{
		peers: peers,
		db: db,
		options: options,
		metrics: metrics,
		upgrading:           map[types.NodeID]types.NodeID{},
		ready:               utils.NewWatch(map[types.NodeID]ChannelIDSet{}),
	}, nil
}

func (s *peerStore) BanPeer(id types.NodeID) error {
	if peer, ok := s.peers[id]; ok {
		if s.options.PersistentPeers[id] || s.options.BlockSyncPeers[id] {
			// cannot ban peers listed in the config file
			return fmt.Errorf("attempting to ban %s but no-op. Remove the peer from config.toml instead", id)
		}
		peer.MutableScore = 0
		peer.ConsecSuccessfulBlocks = 0
		s.ranked = utils.None[[]*peerInfo]()
	}
	return nil
}

func (s *peerStore) DialFailed(addr NodeAddress) error {
	for from, to := range s.upgrading {
		if to == addr.NodeID {
			delete(s.upgrading, from) // Unmark failed upgrade attempt.
		}
	}

	peer,ok := s.peers[addr.NodeID]
	if !ok { return nil }
	if _, ok := peer.AddressInfo[addr]; !ok { return nil }
	peer.AddressInfo[addr].LastDialFailure = utils.Some(time.Now().UTC())
	peer.AddressInfo[addr].DialFailures++
	peer.ConsecSuccessfulBlocks = 0
	if err:=storePeerInfo(s.db,peer.storedPeerInfo); err!=nil {
		return err
	}
	// We need to invalidate the cache after score changed
	s.ranked = utils.None[[]*peerInfo]()

	if d,ok := s.RetryDelay(addr).Get(); ok {
		// TODO: update next dial time (under watch)
		_ = d
	} else {
		s.Delete(addr.NodeID)
	}
	return nil
}

// Get fetches a peer. The boolean indicates whether the peer existed or not.
// The returned peer info is a copy, and can be mutated at will.
func (s *peerStore) Get(id types.NodeID) (peerInfo, bool) {
	peer, ok := s.peers[id]
	return peer.Copy(), ok
}

func storePeerInfo(db dbm.DB, info storedPeerInfo) error {
	bz, err := info.ToProto().Marshal()
	if err != nil {
		panic(fmt.Errorf("info.ToProto().Marshal(): %w",err))
	}
	return db.Set(keyPeerInfo(info.ID), bz)
}

// Set stores peer data. The input data will be copied, and can safely be reused
// by the caller.
func (s *peerStore) Set(peer peerInfo) error {
	if err := peer.Validate(); err != nil {
		return err
	}
	peer = peer.Copy()

	if err := storePeerInfo(s.db, peer.storedPeerInfo); err!=nil {
		return err
	}

	if current, ok := s.peers[peer.ID]; !ok || current.Score() != peer.Score() {
		// If the peer is new, or its score changes, we invalidate the Ranked() cache.
		s.peers[peer.ID] = &peer
		s.ranked = utils.None[[]*peerInfo]()
	} else {
		// Otherwise, since s.ranked contains pointers to the old data and we
		// want those pointers to remain valid with the new data, we have to
		// update the existing pointer address.
		*current = peer
	}

	return nil
}

// Delete deletes a peer, or does nothing if it does not exist.
func (s *peerStore) Delete(id types.NodeID) error {
	if _, ok := s.peers[id]; !ok {
		return nil
	}
	if err := s.db.Delete(keyPeerInfo(id)); err != nil {
		return err
	}
	delete(s.peers, id)
	s.ranked = utils.None[[]*peerInfo]()
	return nil
}

// List retrieves all peers in an arbitrary order. The returned data is a copy,
// and can be mutated at will.
func (s *peerStore) List() []peerInfo {
	peers := make([]peerInfo, 0, len(s.peers))
	for _, peer := range s.peers {
		peers = append(peers, peer.Copy())
	}
	return peers
}

// Ranked returns a list of peers ordered by score (better peers first). Peers
// with equal scores are returned in an arbitrary order. The returned list must
// not be mutated or accessed concurrently by the caller, since it returns
// pointers to internal peerStore data for performance.
//
// Ranked is used to determine both which peers to dial, which ones to evict,
// and which ones to delete completely.
//
// FIXME: For now, we simply maintain a cache in s.ranked which is invalidated
// by setting it to nil, but if necessary we should use a better data structure
// for this (e.g. a heap or ordered map).
//
// FIXME: The scoring logic is currently very naïve, see peerInfo.Score().
func (s *peerStore) Ranked() []*peerInfo {
	if ranked,ok := s.ranked.Get(); ok {
		return ranked
	}
	ranked := make([]*peerInfo, 0, len(s.peers))
	for _, peer := range s.peers {
		ranked = append(ranked, peer)
	}
	sort.Slice(ranked, func(i, j int) bool {
		// FIXME: If necessary, consider precomputing scores before sorting,
		// to reduce the number of Score() calls.
		if a, b := s.Score(ranked[i].ID), s.Score(ranked[j].ID); a != b {
			return a > b
		}
		// TODO(gprusak): we don't allow ties because tests require deterministic order.
		// If not necessary in prod, then fix the tests instead.
		return ranked[i].ID < ranked[j].ID
	})
	for _, peer := range ranked {
		s.metrics.PeerScore.With("peer_id", string(peer.ID)).Set(float64(int(peer.Score())))
	}
	s.ranked = utils.Some(ranked)
	return ranked
}

// Size returns the number of peers in the peer store.
func (s *peerStore) Size() int {
	// exclude unconditional peers
	cnt := 0
	for id := range s.peers {
		if !s.options.UnconditionalPeers[id] {
			cnt++
		}
	}
	return cnt
}

type storedPeerInfo struct {
	ID                  types.NodeID
	AddressInfo         map[NodeAddress]*peerAddressInfo
	LastConnected       utils.Option[time.Time]
}

// peerInfo contains peer information stored in a peerStore.
type peerInfo struct {
	storedPeerInfo

	// These fields are ephemeral, i.e. not persisted to the database.
	NumOfDisconnections uint64
	ConsecSuccessfulBlocks int64
	BaseScore  PeerScore
	FinalScore PeerScore

	// Connection state.
	dialing bool
	conn utils.Option[*Connection]
}

// newPeerInfo creates a peerInfo for a new peer. Each peer will start with a positive MutableScore.
// If a peer is misbehaving, we will decrease the MutableScore, and it will be ranked down.
func newPeerInfo(id types.NodeID) *peerInfo {
	return &peerInfo{
		storedPeerInfo: storedPeerInfo {
			ID:           id,
			AddressInfo:  map[NodeAddress]*peerAddressInfo{},
		},
		BaseScore: DefaultMutableScore, // Should start with a default value above 0
	}
}

// peerInfoFromProto converts a Protobuf PeerInfo message to a peerInfo,
// erroring if the data is invalid.
func storedPeerInfoFromProto(msg *p2pproto.PeerInfo) (storedPeerInfo, error) {
	p := storedPeerInfo{
		ID:          types.NodeID(msg.ID),
		AddressInfo: map[NodeAddress]*peerAddressInfo{},
	}
	if msg.LastConnected != nil {
		p.LastConnected = utils.Some(*msg.LastConnected)
	}
	for _, a := range msg.AddressInfo {
		addressInfo, err := peerAddressInfoFromProto(a)
		if err != nil {
			return storedPeerInfo{}, err
		}
		p.AddressInfo[addressInfo.Address] = &addressInfo
	}
	return p, p.Validate()
}

// ToProto converts the peerInfo to p2pproto.PeerInfo for database storage. The
// Protobuf type only contains persisted fields, while ephemeral fields are
// discarded. The returned message may contain pointers to original data, since
// it is expected to be serialized immediately.
func (p *storedPeerInfo) ToProto() *p2pproto.PeerInfo {
	msg := &p2pproto.PeerInfo{
		ID:            string(p.ID),
	}
	if t,ok := p.LastConnected.Get(); ok {
		msg.LastConnected = &t
	}
	for _, addressInfo := range p.AddressInfo {
		msg.AddressInfo = append(msg.AddressInfo, addressInfo.ToProto())
	}
	return msg
}

func (s *peerStore) updateScore(id types.NodeID) {
	p,ok := s.peers[id]
	if !ok { return }
	p.FinalScore = s.score(p)
}

// Score calculates a score for the peer. Higher-scored peers will be
// preferred over lower scores.
func (s *peerStore) score(p *peerInfo) PeerScore {
	id := p.ID
	// Use predetermined scores if set
	if score,ok := s.options.PeerScores[id]; ok {
		return score
	}
	if s.options.UnconditionalPeers[id] {
		return PeerScoreUnconditional
	}
	score := int64(p.BaseScore)
	if s.options.PersistentPeers[id] || s.options.BlockSyncPeers[id] {
		return PeerScorePersistent
	}

	// Add points for block sync performance
	score += p.ConsecSuccessfulBlocks / 5

	// Penalize for dial failures with time decay
	for _, addr := range p.AddressInfo {
		if lastDialFailure,ok := addr.LastDialFailure.Get(); ok {
			failureScore := float64(addr.DialFailures) * math.Exp(-0.1*float64(time.Since(lastDialFailure).Hours()))
			score -= int64(failureScore)
		}
	}

	// Penalize for disconnections with time decay
	if lastConnected,ok := p.LastConnected.Get(); ok {
		timeSinceLastDisconnect := time.Since(lastConnected)
		decayFactor := math.Exp(-0.1 * timeSinceLastDisconnect.Hours())
		effectiveDisconnections := int64(float64(p.NumOfDisconnections) * decayFactor)
		score -= effectiveDisconnections / 3
	}
	return PeerScore(max(min(score, int64(MaxPeerScoreNotPersistent)),0))
}

// Validate validates the peer info.
func (p *storedPeerInfo) Validate() error {
	if p.ID == "" {
		return errors.New("no peer ID")
	}
	return nil
}

// peerAddressInfo contains information and statistics about a peer address.
type peerAddressInfo struct {
	Address         NodeAddress
	LastDialSuccess utils.Option[time.Time]
	LastDialFailure utils.Option[time.Time]
	DialFailures    uint32 // since last successful dial
}

// peerAddressInfoFromProto converts a Protobuf PeerAddressInfo message
// to a peerAddressInfo.
func peerAddressInfoFromProto(msg *p2pproto.PeerAddressInfo) (peerAddressInfo, error) {
	address, err := ParseNodeAddress(msg.Address)
	if err != nil {
		return peerAddressInfo{}, fmt.Errorf("invalid address %q: %w", address, err)
	}
	addressInfo := peerAddressInfo{
		Address:      address,
		DialFailures: msg.DialFailures,
	}
	if t:=msg.LastDialSuccess; t != nil {
		addressInfo.LastDialSuccess = utils.Some(*t)
	}
	if t:=msg.LastDialFailure; t != nil {
		addressInfo.LastDialFailure = utils.Some(*t)
	}
	return addressInfo, addressInfo.Validate()
}

// ToProto converts the address into to a Protobuf message for serialization.
func (a *peerAddressInfo) ToProto() *p2pproto.PeerAddressInfo {
	msg := &p2pproto.PeerAddressInfo{
		Address:         a.Address.String(),
		DialFailures:    a.DialFailures,
	}
	if t,ok := a.LastDialSuccess.Get(); ok {
		msg.LastDialSuccess = &t
	}
	if t,ok := a.LastDialFailure.Get(); ok {
		msg.LastDialFailure = &t
	}
	return msg
}

// Validate validates the address info.
func (a *peerAddressInfo) Validate() error {
	return a.Address.Validate()
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

func (s *peerStore) Add(addr NodeAddress) (bool,error) {
	if err := addr.Validate(); err != nil {
		return false, err
	}
	if addr.NodeID == s.options.SelfID {
		m.logger.Info("can't add self to peer store, skipping address", "address", address.String(), "self", m.selfID)
		return false, nil
	}
	if _,ok := s.peers[addr.NodeID]; !ok {
		s.peers[addr.NodeID] = newPeerInfo(addr.NodeID)
	}
	if _, ok := s.peers[addr.NodeID].AddressInfo[addr]; ok {
		return false, nil
	}
	s.peers[addr.NodeID].AddressInfo[addr] = &peerAddressInfo{Address: addr}
	m.logger.Info(fmt.Sprintf("Adding new peer %s with address %s to peer store\n", peer.ID, addr.String()))
	if err := storePeerInfo(s.db, s.peers[addr.NodeID].storedPeerInfo); err!=nil {
		return false, err
	}
	if err := s.prunePeers(); err != nil {
		return true, err
	}
	return true, nil
}

// prunePeers removes low-scored peers from the peer store if it contains more
// than MaxPeers peers. The caller must hold the mutex lock.
func (s *peerStore) prunePeers() error {
	maxPeers,ok := s.options.MaxPeers.Get()
	if !ok || s.Size() <= int(maxPeers) {
		return nil
	}
	ranked := s.Ranked()
	for i := len(ranked) - 1; i >= 0; i-- {
		peerID := ranked[i].ID
		switch {
		case s.Size() <= int(maxPeers):
			return nil
		case m.dialing[peerID]:
		case m.connected[peerID]:
		default:
			if err := s.Delete(peerID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *peerStore) Disconnect(conn *Connection) {
	// Update score and invalidate cache if a peer got disconnected
	if peer, ok := s.peers[conn.PeerInfo().NodeID]; ok {
		if peer.conn==utils.Some(conn) {
			peer.NumOfDisconnections++
			peer.ConsecSuccessfulBlocks = 0
			peer.conn = utils.None[*Connection]()
		}
	}
}

func (s *peerStore) UpdateScore(id types.NodeID, diff int64) {
	if peer,ok := s.peers[id]; ok {
		score := int64(peer.BaseScore)+diff
		score = min(max(score,0),int64(MaxPeerScoreNotPersistent))
		peer.BaseScore = PeerScore(score)
		s.ranked = utils.None[[]*peerInfo]()
	}
}

func (s *peerStore) Addresses(id types.NodeID) []NodeAddress {
	var addrs []NodeAddress
	if peer, ok := s.peers[id]; ok {
		for addr := range peer.AddressInfo {
			addrs = append(addrs, addr)
		}
	}
	return addrs
}

func (s *peerStore) Peers() []types.NodeID {
	var peers []types.NodeID
	for id := range s.peers {
		peers = append(peers,id)
	}
	return peers
}

func (s *peerStore) Scores() map[types.NodeID]PeerScore {
	scores := map[types.NodeID]PeerScore{}
	for id := range s.peers {
		scores[id] = s.Score(id)
	}
	return scores
}

// The caller must hold the mutex lock (for m.rand which is not thread-safe).
func (s *peerStore) RetryDelay(addr NodeAddress) utils.Option[time.Duration] {
	never := utils.None[time.Duration]()
	peer,ok := s.peers[addr.NodeID]
	if !ok { return never }
	info,ok := peer.AddressInfo[addr]
	if !ok { return never }
	if info.DialFailures == 0 {
		return utils.Some(time.Duration(0))
	}
	ro,ok := s.options.Retry.Get()
	if !ok { return never }
	return ro.delay(info.DialFailures, s.options.PersistentPeers[addr.NodeID])
}

func (s *peerStore) IncBlockSyncs(id types.NodeID) {
	if peer, ok := s.peers[id]; ok {
		peer.ConsecSuccessfulBlocks++
		s.ranked = utils.None[[]*peerInfo]()
	}
}

func (s *peerStore) Ready(conn *Connection, dialAddr utils.Option[NodeAddress]) error {
	id := conn.PeerInfo().NodeID
	if id == s.options.SelfID {
		return fmt.Errorf("rejecting connection from self (%v)", id)
	}
	if _,ok := s.peers[id]; !ok {
		s.peers[id] = newPeerInfo(id)
	}
	peer := s.peers[id]
	if peer.conn.IsPresent() {
		return fmt.Errorf("can't accept, peer=%q is already connected", id)
	}
	if !s.options.UnconditionalPeers[id] && s.options.MaxConnected > 0 &&
		s.NumConnected() >= int(s.options.MaxConnected)+int(s.options.MaxConnectedUpgrade) {
		return fmt.Errorf("accepted peer %q failed, already connected to maximum number of peers", peerID)
	}
	now := time.Now().UTC()
	peer.dialing = false
	peer.conn = utils.Some(conn)
	peer.LastConnected = utils.Some(now)
	// reset this to avoid penalizing peers for their past transgressions
	for _,info := range peer.AddressInfo {
		info.DialFailures = 0
		info.LastDialFailure = utils.None[time.Time]()
	}
	if dialAddr,ok := dialAddr.Get(); ok {
		peer.AddressInfo[dialAddr] = &peerAddressInfo{
			Address: dialAddr, LastDialSuccess: utils.Some(now),
		}
	}
	return nil
}

// SendError reports a peer misbehavior to the router.
func (s *peerStore) SendError(pe PeerError) {
	shouldEvict := pe.Fatal
	if maxConnected,ok := s.options.MaxConnected.Get(); ok {
		shouldEvict = shouldEvict || (s.NumConnected() >= int(maxConnected))
	}
	s.logger.Error("peer error",
		"peer", pe.NodeID,
		"err", pe.Err,
		"evicting", shouldEvict,
	)
	if shouldEvict {
		peer := s.peers[pe.NodeID]
		s.logger.Info("evicting peer", "peer", pe.NodeID, "cause", pe.Err)
		peer.Close()
		peer.evicting = true
	} else {
		s.UpdateScore(pe.NodeID,-1)
	}
}


// Advertise returns a list of peer addresses to advertise to a peer.
//
// FIXME: This is fairly naïve and only returns the addresses of the
// highest-ranked peers.
func (s *peerStore) Advertise(limit int) []NodeAddress {
	var addrs []NodeAddress

	// advertise ourselves, to let everyone know how to dial us back
	// and enable mutual address discovery
	if addr,ok := s.options.SelfAddress.Get(); ok {
		addrs = append(addrs, addr)
	}

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

