package node

import (
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
)

// AutobahnLayout lists the on-disk paths of a node's Autobahn state that state sync reads, writes or
// requires to be empty. It does not list the validator consensus files under PersistentStateDir
// (inner_a.pb, inner_b.pb, blocks/ and commitqcs/).
type AutobahnLayout struct {
	PersistentStateDir string
	FlatKVDir          string
	EpochRegistryFiles []string
	BlockDBDirs        []string
	ReceiptStoreDir    string
	StateStoreDir      string
	HashVaultDir       string
	RestoreMarkerFile  string
}

// ResolveAutobahnLayout returns the Autobahn layout of the node that conf and storage
// configure, with the paths node startup opens. It creates the persistent state dir
// if it does not exist.
func ResolveAutobahnLayout(conf *config.Config, storage gigaconfig.StorageConfig) (AutobahnLayout, error) {
	fc, _, err := loadAutobahnCommittee(conf.AutobahnConfigFile)
	if err != nil {
		return AutobahnLayout{}, err
	}
	psd, storageConfig, err := autobahnStorageConfig(conf.RootDir, conf.Mode, fc, storage)
	if err != nil {
		return AutobahnLayout{}, err
	}
	return AutobahnLayout{
		PersistentStateDir: psd,
		FlatKVDir:          storageConfig.FlatKVConfig.DataDir,
		EpochRegistryFiles: epoch.SnapshotFiles(psd),
		BlockDBDirs:        storageConfig.BlockDBConfig.Litt.Paths,
		ReceiptStoreDir:    storageConfig.ReceiptDBConfig.DBDirectory,
		StateStoreDir:      storageConfig.SSConfig.EVMDBDirectory,
		HashVaultDir:       p2p.HashVaultDir(psd),
		RestoreMarkerFile:  p2p.RestoreMarkerFile(psd),
	}, nil
}
