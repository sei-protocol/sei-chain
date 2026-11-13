package node

import (
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// AutobahnLayout lists the on-disk paths of a node's Autobahn state that state
// sync reads, writes or requires to be empty. It does not list the validator
// consensus files under PersistentStateDir (inner_a.pb, inner_b.pb, blocks/
// and commitqcs/).
type AutobahnLayout struct {
	PersistentStateDir string
	FlatKVDir          string
	BlockDBDirs        []string
	ReceiptStoreDir    string
	StateStoreDir      string
	AppHashVaultDir    string
	RestoreMarkerFile  string
	// RestoreProofFile is the state sync proof that restore installs for the seed to read.
	RestoreProofFile string
	// StateSyncProofDir holds the state sync proof files that a running node writes next to its
	// FlatKV checkpoints.
	StateSyncProofDir string
	EpochRegistryDir  string
}

// ResolveAutobahnLayout returns the Autobahn layout of the node that conf, storage and
// genDoc configure, with the paths node startup opens. It creates the persistent state dir
// if it does not exist.
func ResolveAutobahnLayout(conf *config.Config, storage gigaconfig.StorageConfig, genDoc *types.GenesisDoc) (AutobahnLayout, error) {
	fc, _, err := loadAutobahnCommittee(conf.AutobahnConfigFile)
	if err != nil {
		return AutobahnLayout{}, err
	}
	psd, storageConfig, err := autobahnStorageConfig(conf.RootDir, conf.Mode, fc, storage, genDoc)
	if err != nil {
		return AutobahnLayout{}, err
	}
	return AutobahnLayout{
		PersistentStateDir: psd,
		FlatKVDir:          storageConfig.FlatKVConfig.DataDir,
		BlockDBDirs:        storageConfig.BlockDBConfig.Litt.Paths,
		ReceiptStoreDir:    storageConfig.ReceiptDBConfig.DBDirectory,
		StateStoreDir:      storageConfig.SSConfig.EVMDBDirectory,
		AppHashVaultDir:    storageConfig.AppHashBuilderConfig.HashVaultConfig.Path,
		RestoreMarkerFile:  p2p.RestoreMarkerFile(psd),
		RestoreProofFile:   p2p.RestoreStateSyncProofFile(psd),
		StateSyncProofDir:  dbutils.GetStateSyncProofPath(psd),
		EpochRegistryDir:   epoch.SnapshotDir(psd),
	}, nil
}
