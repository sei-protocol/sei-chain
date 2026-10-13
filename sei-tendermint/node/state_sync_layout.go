package node

import (
	"errors"
	"fmt"
	"path/filepath"

	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
)

// AutobahnLayout is the on-disk layout of one node's Autobahn state.
type AutobahnLayout struct {
	PersistentStateDir string
	FlatKVDir          string
	EpochsDir          string
	BlockDBDir         string
	ReceiptStoreDir    string
	StateStoreDir      string
	HashVaultDir       string
	// RestoredMarker is the file a state sync restore writes last.
	RestoredMarker string
}

// ResolveAutobahnLayout returns the Autobahn layout of the node configured by cfg,
// resolved the same way node startup resolves it.
func ResolveAutobahnLayout(cfg *config.Config) (AutobahnLayout, error) {
	if cfg.AutobahnConfigFile == "" {
		return AutobahnLayout{}, errors.New("autobahn-config-file is not set")
	}
	fc, err := loadAutobahnFileConfig(cfg.AutobahnConfigFile)
	if err != nil {
		return AutobahnLayout{}, err
	}
	psd, err := resolvePersistentStateDir(cfg.RootDir, fc.PersistentStateDir)
	if err != nil {
		return AutobahnLayout{}, err
	}
	storage, err := seidbconfig.AutobahnStorageConfig(psd)
	if err != nil {
		return AutobahnLayout{}, fmt.Errorf("build Autobahn storage config: %w", err)
	}
	return AutobahnLayout{
		PersistentStateDir: psd,
		FlatKVDir:          storage.FlatKVConfig.DataDir,
		EpochsDir:          filepath.Join(psd, "epochs"),
		BlockDBDir:         filepath.Join(psd, "blockdb"),
		ReceiptStoreDir:    storage.ReceiptDBConfig.DBDirectory,
		StateStoreDir:      storage.SSConfig.EVMDBDirectory,
		HashVaultDir:       filepath.Join(psd, "hashvault"),
		RestoredMarker:     filepath.Join(psd, p2p.StateSyncRestoredMarker),
	}, nil
}
