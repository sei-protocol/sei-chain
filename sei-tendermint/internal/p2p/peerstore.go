package p2p

import (
	"errors"
	"fmt"
	"time"
	"context"

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

// peerStore stores information about successful dials.
type peerStore struct {
	logger log.Logger
	options     *PeerManagerOptions
	metrics *Metrics

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
	regularPeers := newPool(options)

	return &peerStore{
		db: utils.NewMutex(peerDB),
		options: options,
		metrics: metrics,
	}, nil
}

// MaxPeers: 4 + 2*MaxConnected // Storage.
// Round robin through peer addresses when dialing.
// Prune other addresses on successful dial.
// Don't punish for failed dials - anyone can forge anyones address.
