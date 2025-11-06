package p2p

import (
	"fmt"
	"time"
	"slices"

	"github.com/gogo/protobuf/proto"
	"github.com/google/orderedcode"
	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/libs/utils"
	p2pproto "github.com/tendermint/tendermint/proto/tendermint/p2p"
	"github.com/tendermint/tendermint/types"
)

type errBadNetwork struct {error}

// peerInfoFromProto converts a Protobuf PeerInfo message to a peerInfo,
// erroring if the data is invalid.
func connRecordFromProto(msg *p2pproto.PeerInfo) (connRecord, error) {
	if msg.LastConnected==nil {
		return connRecord{}, fmt.Errorf("missing LastConnected")
	}
	if len(msg.AddressInfo)==0 {
		return connRecord{}, fmt.Errorf("missing AddressInfo")
	}
	addr, err := ParseNodeAddress(msg.AddressInfo[0].Address)
	if err!=nil {
		return connRecord{},fmt.Errorf("ParseNodeAddress(): %w",err)
	}
	return connRecord{
		Addr: addr,
		LastConnected: *msg.LastConnected,
	},nil
}

// ToProto converts the peerInfo to p2pproto.PeerInfo for database storage. The
// Protobuf type only contains persisted fields, while ephemeral fields are
// discarded. The returned message may contain pointers to original data, since
// it is expected to be serialized immediately.
func (r connRecord) ToProto() *p2pproto.PeerInfo {
	return &p2pproto.PeerInfo{
		ID:            string(r.Addr.NodeID),
		LastConnected: utils.Alloc(r.LastConnected),
		AddressInfo: []*p2pproto.PeerAddressInfo{{Address: r.Addr.String()}},
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

type connRecord struct {
	Addr NodeAddress
	LastConnected time.Time
}

type peerDB struct {
	db dbm.DB
	records map[types.NodeID]connRecord
}

func newPeerDB(db dbm.DB) (*peerDB, error) {
	records := map[types.NodeID]connRecord{}
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
		r, err := connRecordFromProto(&msg)
		if err!=nil {
			// Prune invalid data.
			if err:=db.Delete(iter.Key());err!=nil {
				return nil, fmt.Errorf("failed to delete invalid peer info: %w", err)
			}
			continue
		}
		records[r.Addr.NodeID] = r
	}
	if iter.Error() != nil {
		return nil, iter.Error()
	}
	return &peerDB{db,records}, nil
}

func (db *peerDB) Insert(addr NodeAddress, lastConnected time.Time) error {
	r := connRecord {
		Addr: addr,
		LastConnected: lastConnected,
	}
	bz, err := r.ToProto().Marshal()
	if err != nil {
		panic(fmt.Errorf("info.ToProto().Marshal(): %w",err))
	}
	if err:=db.db.Set(keyPeerInfo(r.Addr.NodeID), bz); err!=nil {
		return fmt.Errorf("Set(): %w",err)
	}
	db.records[addr.NodeID] = r
	return nil
}

func (db *peerDB) Prune(before time.Time) error {
	var toPrune[] types.NodeID
	for id,r := range db.records {
		if r.LastConnected.Before(before) {
			toPrune = append(toPrune,id)
		}
	}
	for _, id := range toPrune {
		if err:=db.db.Delete(keyPeerInfo(id)); err!=nil {
			return fmt.Errorf("Delete(): %w",err)
		}
	}
	for _, id := range toPrune {
		delete(db.records, id)
	}
	return nil
}

// Snapshot returns the list of addresses in the peerDB
// sorted by LastConnected (more recent go first).
func (db *peerDB) Snapshot() []NodeAddress {
	var addrs []NodeAddress
	for _,r := range db.records {
		addrs = append(addrs,r.Addr)
	}
	slices.SortFunc(addrs, func(a,b NodeAddress) int {
		at := db.records[a.NodeID].LastConnected
		bt := db.records[b.NodeID].LastConnected
		return bt.Compare(at)
	})
	return addrs
}


