package config

import (
	"os"
	"path/filepath"

	dbm "github.com/tendermint/tm-db"
)

// DBContext specifies config information for loading a new DB.
type DBContext struct {
	ID     string
	Config *Config
}

// DBProvider takes a DBContext and returns an instantiated DB.
type DBProvider func(*DBContext) (dbm.DB, error)

// DefaultDBProvider returns a database using the DBBackend and DBDir
// specified in the Config. It routes each DB to its appropriate
// subdirectory under the data folder, with backward compatibility
// for existing nodes that have data in the legacy flat layout.
func DefaultDBProvider(ctx *DBContext) (dbm.DB, error) {
	dbType := dbm.BackendType(ctx.Config.DBBackend)
	dbDir := ResolveDBDir(ctx.ID, ctx.Config.DBDir())
	return dbm.NewDB(ctx.ID, dbType, dbDir)
}

// dbSubDir returns the new subdirectory for a given DB identifier.
func dbSubDir(dbID string) string {
	switch dbID {
	case "blockstore", "tx_index", "state", "evidence", "peerstore":
		return "tendermint"
	default:
		return ""
	}
}

// ResolveDBDir returns the directory in which the given DB should be opened.
// If legacy data exists directly under baseDir (e.g. baseDir/blockstore.db),
// baseDir is returned for backward compatibility. Otherwise the new
// subdirectory layout (e.g. baseDir/ledger) is used.
func ResolveDBDir(dbID string, baseDir string) string {
	subDir := dbSubDir(dbID)
	if subDir == "" {
		return baseDir
	}
	legacyPath := filepath.Join(baseDir, dbID+".db")
	if pathExists(legacyPath) {
		return baseDir
	}
	return filepath.Join(baseDir, subDir)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
