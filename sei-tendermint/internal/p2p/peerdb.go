package p2p

import (
	"cmp"
	"fmt"
	"iter"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/google/btree"
	"github.com/google/orderedcode"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	p2pproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// peerInfoFromProto converts a Protobuf PeerInfo message to a peerInfo,
// erroring if the data is invalid.
func peerDBRowFromProto(msg *p2pproto.PeerInfo) (peerDBRow, error) {
	if msg.LastConnected == nil {
		return peerDBRow{}, fmt.Errorf("missing LastConnected")
	}
	if len(msg.AddressInfo) == 0 {
		return peerDBRow{}, fmt.Errorf("missing AddressInfo")
	}
	addr, err := ParseNodeAddress(msg.AddressInfo[0].Address)
	if err != nil {
		return peerDBRow{}, fmt.Errorf("ParseNodeAddress(): %w", err)
	}
	return peerDBRow{
		Addr:          addr,
		LastConnected: *msg.LastConnected,
	}, nil
}

func peerDBRowFromBytes(buf []byte) (peerDBRow, error) {
	var msg p2pproto.PeerInfo
	if err := proto.Unmarshal(buf, &msg); err != nil {
		return peerDBRow{}, fmt.Errorf("invalid peer Protobuf data: %w", err)
	}
	row, err := peerDBRowFromProto(&msg)
	if err != nil {
		return peerDBRow{}, err
	}
	if err := row.Addr.Validate(); err != nil {
		return peerDBRow{}, err
	}
	return row, nil
}

// ToProto converts the peerInfo to p2pproto.PeerInfo for database storage. The
// Protobuf type only contains persisted fields, while ephemeral fields are
// discarded. The returned message may contain pointers to original data, since
// it is expected to be serialized immediately.
func (r peerDBRow) ToProto() *p2pproto.PeerInfo {
	return &p2pproto.PeerInfo{
		ID:            string(r.Addr.NodeID),
		LastConnected: utils.Alloc(r.LastConnected),
		AddressInfo:   []*p2pproto.PeerAddressInfo{{Address: r.Addr.String()}},
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

type peerDBRow struct {
	LastConnected time.Time
	Addr          NodeAddress
}

func (a peerDBRow) Compare(b peerDBRow) int {
	return cmp.Or(
		a.LastConnected.Compare(b.LastConnected),
		cmp.Compare(a.Addr.NodeID, b.Addr.NodeID),
	)
}

type peerDB struct {
	db              dbm.DB
	maxRows         int
	byNodeID        map[types.NodeID]peerDBRow
	byLastConnected *btree.BTreeG[peerDBRow]
}

func newPeerDB(db dbm.DB, maxRows int) (*peerDB, error) {
	byNodeID := map[types.NodeID]peerDBRow{}
	start, end := keyPeerInfoRange()
	iter, err := db.Iterator(start, end)
	if err != nil {
		return nil, fmt.Errorf("db.Iterator(): %w", err)
	}
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		r, err := peerDBRowFromBytes(iter.Value())
		if err != nil {
			// Prune invalid data.
			if err := db.Delete(iter.Key()); err != nil {
				return nil, fmt.Errorf("failed to delete invalid peer info: %w", err)
			}
			continue
		}
		byNodeID[r.Addr.NodeID] = r
	}
	byLastConnected := btree.NewG(2, func(a, b peerDBRow) bool { return a.Compare(b) < 0 })
	for _, r := range byNodeID {
		byLastConnected.ReplaceOrInsert(r)
	}
	if iter.Error() != nil {
		return nil, iter.Error()
	}
	peerDB := &peerDB{db, maxRows, byNodeID, byLastConnected}
	if err := peerDB.truncate(); err != nil {
		return nil, err
	}
	return peerDB, nil
}

func (db *peerDB) Close() {
	_ = db.db.Close()
}

func (db *peerDB) All() iter.Seq[NodeAddress] {
	return func(yield func(addr NodeAddress) bool) {
		db.byLastConnected.Descend(func(r peerDBRow) bool { return yield(r.Addr) })
	}
}

func (db *peerDB) Insert(addr NodeAddress, lastConnected time.Time) error {
	old, oldOk := db.byNodeID[addr.NodeID]
	if oldOk && !old.LastConnected.Before(lastConnected) {
		return nil
	}
	r := peerDBRow{lastConnected, addr}
	bz, err := r.ToProto().Marshal()
	if err != nil {
		panic(fmt.Errorf("info.ToProto().Marshal(): %w", err))
	}
	if err := db.db.Set(keyPeerInfo(r.Addr.NodeID), bz); err != nil {
		return fmt.Errorf("Set(): %w", err)
	}
	if oldOk {
		db.byLastConnected.Delete(old)
	}
	db.byNodeID[addr.NodeID] = r
	db.byLastConnected.ReplaceOrInsert(r)
	if err := db.truncate(); err != nil {
		return err
	}
	return nil
}

func (db *peerDB) truncate() error {
	var toPrune []types.NodeID
	db.byLastConnected.Ascend(func(r peerDBRow) bool {
		if len(db.byNodeID)-len(toPrune) <= db.maxRows {
			return false
		}
		toPrune = append(toPrune, r.Addr.NodeID)
		return true
	})
	for _, id := range toPrune {
		if err := db.db.Delete(keyPeerInfo(id)); err != nil {
			return fmt.Errorf("Delete(): %w", err)
		}
	}
	for _, id := range toPrune {
		db.byLastConnected.Delete(db.byNodeID[id])
		delete(db.byNodeID, id)
	}
	return nil
}
