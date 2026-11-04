package evmonlyapp

import (
	"maps"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/app/upgradenames"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
)

// releaseMigrations are the handlers, by upgrade name, of the releases that
// change EVM-only state.
var releaseMigrations = map[string]upgrade.Handler{}

// upgradeHandlers returns a handler for every upgrade name this build ships,
// and for testUpgradeName in a build with the giga_test_upgrade tag. A name
// without a releaseMigrations entry gets a nil handler, which changes no
// EVM-only state.
func upgradeHandlers() map[string]upgrade.Handler {
	handlers := map[string]upgrade.Handler{}
	for _, name := range upgradenames.Release() {
		handlers[name] = nil
	}
	maps.Copy(handlers, releaseMigrations)
	if testUpgradeHandler {
		handlers[testUpgradeName] = applyTestUpgrade
	}
	return handlers
}

// testUpgradeName is the upgrade a giga_test_upgrade build can apply. It is
// not a release name.
const testUpgradeName = "vX.Y-test"

// testUpgradeMaxDepositPeriod is gov's max deposit period once the test
// upgrade has applied.
const testUpgradeMaxDepositPeriod = 72 * time.Hour

// applyTestUpgrade sets gov's max deposit period to testUpgradeMaxDepositPeriod.
func applyTestUpgrade(_ precompiles.BlockContext, state precompiles.State) error {
	value := common.BigToHash(big.NewInt(int64(testUpgradeMaxDepositPeriod))).Bytes()
	return chainparams.Apply(state, []chainparams.Change{{Subspace: "gov", Key: "depositparams.max_deposit_period", Value: value}})
}
