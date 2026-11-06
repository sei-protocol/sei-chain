package node

import (
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
)

// AutobahnLayout lists the paths under a node's Autobahn persistent state dir that state
// sync reads and writes. The node creates every other store under PersistentStateDir on
// start.
type AutobahnLayout struct {
	PersistentStateDir string
	FlatKVDir          string
	RestoreMarkerFile  string
}

// ResolveAutobahnLayout returns the Autobahn layout of the node that conf configures, with
// the paths node startup opens. It creates the persistent state dir if it does not exist.
func ResolveAutobahnLayout(conf *config.Config) (AutobahnLayout, error) {
	fc, _, err := loadAutobahnCommittee(conf.AutobahnConfigFile)
	if err != nil {
		return AutobahnLayout{}, err
	}
	psd, err := resolvePersistentStateDir(conf.RootDir, fc.PersistentStateDir)
	if err != nil {
		return AutobahnLayout{}, err
	}
	return AutobahnLayout{
		PersistentStateDir: psd,
		FlatKVDir:          dbutils.GetFlatKVPath(psd),
		RestoreMarkerFile:  p2p.RestoreMarkerFile(psd),
	}, nil
}
