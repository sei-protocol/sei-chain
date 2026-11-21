package node

import (
	"fmt"
	"os"
	"path/filepath"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const importStagingDirName = ".state-sync-import"

// AutobahnLayout lists the paths under a node's Autobahn persistent state dir that state
// sync reads and writes. The node creates every other store under PersistentStateDir on
// start.
type AutobahnLayout struct {
	PersistentStateDir string
	FlatKVDir          string
	RestoreMarkerFile  string
	RestoreProofFile   string
	StateSyncProofDir  string
	// ImportStagingDir holds the files of a state sync import until it installs them.
	ImportStagingDir string
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
	return autobahnLayout(psd), nil
}

// autobahnLayout returns the Autobahn layout of the persistent state dir psd.
func autobahnLayout(psd string) AutobahnLayout {
	return AutobahnLayout{
		PersistentStateDir: psd,
		FlatKVDir:          dbutils.GetFlatKVPath(psd),
		RestoreMarkerFile:  p2p.RestoreMarkerFile(psd),
		RestoreProofFile:   p2p.RestoreProofFile(psd),
		StateSyncProofDir:  dbutils.GetStateSyncProofPath(psd),
		ImportStagingDir:   filepath.Join(psd, importStagingDirName),
	}
}

// RemoveImportStaging removes the staging dir of a state sync import that stopped before its
// cleanup. The caller must hold the writer lock of the FlatKV root of layout.
func RemoveImportStaging(layout AutobahnLayout) error {
	if err := os.RemoveAll(layout.ImportStagingDir); err != nil {
		return fmt.Errorf("remove the staging dir of a state sync import: %w", err)
	}
	return nil
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
