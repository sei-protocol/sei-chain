package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// upgradeInfoFileName is the file, under the node's data directory, that
// records the software upgrade a node stopped for.
const upgradeInfoFileName = "upgrade-info.json"

// upgradeInfo is the content of upgrade-info.json, in x/upgrade's format.
type upgradeInfo struct {
	Name   string `json:"name"`
	Height int64  `json:"height"`
	Info   string `json:"info"`
}

// checkEVMOnlyUpgradeStart refuses to start a binary whose upgrade is
// scheduled after the next block the committed state would execute.
func checkEVMOnlyUpgradeStart(manager *bootstrap.GigaStorageManager, upgrades gov.Upgrades) error {
	latest, err := manager.SC().GetLatestVersion()
	if err != nil {
		return fmt.Errorf("read EVM-only state version: %w", err)
	}
	next, ok := utils.SafeCast[uint64](latest + 1)
	if !ok {
		return fmt.Errorf("invalid EVM-only state version %d", latest)
	}
	view := manager.StateDB().OpenView()
	defer view.Close()
	return upgrades.CheckStart(stateViewReader{view}, next)
}

// stateViewReader reads account storage from a Giga state view.
type stateViewReader struct{ view gigatypes.StateView }

func (r stateViewReader) GetState(addr common.Address, key common.Hash) common.Hash {
	return r.view.GetStorage(addr, key)
}

// reportUpgradeNeeded writes upgrade-info.json under rootDir's data directory
// and prints the upgrade message when err stopped the node for a software
// upgrade. It returns err, joined with any failure to write the file.
func reportUpgradeNeeded(rootDir string, err error) error {
	needed, ok := errors.AsType[*gov.UpgradeNeededError](err)
	if !ok {
		return err
	}
	logger.Error(needed.Error())
	// Printed raw because structured log handlers escape the quotes tools such as cosmovisor match.
	fmt.Fprintln(os.Stderr, needed.Error())
	return errors.Join(err, writeUpgradeInfo(filepath.Join(rootDir, "data"), needed.Plan))
}

func writeUpgradeInfo(dataDir string, plan gov.Plan) error {
	height, ok := utils.SafeCast[int64](plan.Height)
	if !ok {
		return fmt.Errorf("upgrade height %d exceeds int64", plan.Height)
	}
	bz, err := json.Marshal(upgradeInfo{Name: plan.Name, Height: height, Info: plan.Info})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dataDir, err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, upgradeInfoFileName), bz, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", upgradeInfoFileName, err)
	}
	return nil
}
