package evmonlyapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
)

// UpgradeInfoFileName is the file, under a node's data directory, that names
// the upgrade a halted node needs.
const UpgradeInfoFileName = "upgrade-info.json"

// upgradeInfo is the content of UpgradeInfoFileName, as V2's upgrade module
// writes it.
type upgradeInfo struct {
	Name   string `json:"name,omitempty"`
	Height int64  `json:"height,omitempty"` // the block the node halted at
	Info   string `json:"info,omitempty"`
}

// upgradeInfoPath returns where a node with home directory homeDir writes
// UpgradeInfoFileName.
func upgradeInfoPath(homeDir string) string {
	return filepath.Join(homeDir, "data", UpgradeInfoFileName)
}

// haltForUpgrade writes UpgradeInfoFileName for needed, when the app has a
// path for it, then logs the halt and prints it to stderr. It returns err,
// joined with the write's error when the write fails, in which case nothing is
// printed.
func (a *evmOnlyApplication) haltForUpgrade(needed *upgrade.NeededError, err error) error {
	if path, ok := a.upgradeInfoPath.Get(); ok {
		if writeErr := writeUpgradeInfo(path, needed); writeErr != nil {
			return errors.Join(err, writeErr)
		}
	}
	logger.Error(needed.Error())
	// The raw line, unescaped, is what cosmovisor-style tooling matches.
	fmt.Fprintln(os.Stderr, needed.Error())
	return err
}

// writeUpgradeInfo writes needed's plan, and the block it halted at, to path.
func writeUpgradeInfo(path string, needed *upgrade.NeededError) error {
	bz, err := json.Marshal(upgradeInfo{
		Name:   needed.Plan.Name,
		Height: int64(needed.Block), //nolint:gosec // block numbers fit an int64
		Info:   needed.Plan.Info,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("write %s: %w", UpgradeInfoFileName, err)
	}
	if err := os.WriteFile(path, bz, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", UpgradeInfoFileName, err)
	}
	return nil
}
