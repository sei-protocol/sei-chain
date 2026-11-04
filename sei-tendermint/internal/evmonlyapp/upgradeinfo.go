package evmonlyapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
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

// WithUpgradeInfo returns app, writing UpgradeInfoFileName under homeDir's
// data directory and the halt message to stderr when a block halts on a due
// upgrade plan.
func WithUpgradeInfo(app proxy.EVMApplication, homeDir string) proxy.EVMApplication {
	return upgradeInfoApplication{EVMApplication: app, path: filepath.Join(homeDir, "data", UpgradeInfoFileName)}
}

type upgradeInfoApplication struct {
	proxy.EVMApplication
	path string
}

func (a upgradeInfoApplication) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	response, err := a.EVMApplication.FinalizeBlock(ctx, req)
	var needed *upgrade.NeededError
	if errors.As(err, &needed) {
		logger.Error(needed.Error())
		// The raw line, unescaped, is what cosmovisor-style tooling matches.
		fmt.Fprintln(os.Stderr, needed.Error())
		return nil, errors.Join(err, writeUpgradeInfo(a.path, needed))
	}
	return response, err
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
