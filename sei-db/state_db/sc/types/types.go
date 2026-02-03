package types

import (
	"io"

	ics23 "github.com/confio/ics23/go"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

type Committer interface {
	Initialize(initialStores []string)

	Commit() (int64, error)

	Version() int64

	GetLatestVersion() (int64, error)

	GetEarliestVersion() (int64, error)

	ApplyChangeSets(cs []*proto.NamedChangeSet) error

	ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error

	WorkingCommitInfo() *proto.CommitInfo

	LastCommitInfo() *proto.CommitInfo

	LoadVersion(targetVersion int64, copyExisting bool) (Committer, error)

	Rollback(targetVersion int64) error

	SetInitialVersion(initialVersion int64) error

	GetChildStoreByName(name string) CommitKVStore

	Importer(version int64) (Importer, error)

	Exporter(version int64) (Exporter, error)

	io.Closer
}

type CommitKVStore interface {
	Get(key []byte) []byte

	Has(key []byte) bool

	Set(key, value []byte)

	Remove(key []byte)

	Version() int64

	RootHash() []byte

	Iterator(start, end []byte, ascending bool) dbm.Iterator

	GetProof(key []byte) *ics23.CommitmentProof

	io.Closer
}

type Importer interface {
	AddModule(name string) error

	AddNode(node *SnapshotNode)

	io.Closer
}

type Exporter interface {
	Next() (interface{}, error)

	io.Closer
}

// SnapshotNode contains import/export node data.
type SnapshotNode struct {
	Key     []byte
	Value   []byte
	Version int64
	Height  int8
}
