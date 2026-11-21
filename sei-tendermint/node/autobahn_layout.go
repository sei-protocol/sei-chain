package node

import (
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// AutobahnLayout lists the paths under a node's Autobahn persistent state dir that state
// sync reads and writes. The node creates every other store under PersistentStateDir on
// start.
type AutobahnLayout struct {
	PersistentStateDir string
	FlatKVDir          string
	RestoreMarkerFile  string
	RestoreProofFile   string
	StateSyncProofDir  string
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
		RestoreProofFile:   p2p.RestoreProofFile(psd),
		StateSyncProofDir:  dbutils.GetStateSyncProofPath(psd),
	}, nil
}

// FlatKVConfig returns the FlatKV config that a node in mode, with the [giga.storage] settings
// storage and the genesis genDoc, opens the FlatKV store of layout with.
func FlatKVConfig(
	layout AutobahnLayout,
	mode string,
	storage gigaconfig.StorageConfig,
	genDoc *types.GenesisDoc,
) (*flatkvconfig.Config, error) {
	storageConfig, err := buildGigaStorageConfig(layout.PersistentStateDir, mode, storage, genDoc)
	if err != nil {
		return nil, err
	}
	return storageConfig.FlatKVConfig, nil
}
