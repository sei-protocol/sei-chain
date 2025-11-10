package types

import (
	"io"

	"github.com/sei-protocol/sei-db/proto"
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

	GetTreeByName(name string) Tree

	Importer(version int64) (Importer, error)

	Exporter(version int64) (Exporter, error)

	io.Closer
}
