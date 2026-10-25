package migrationpause

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/app/migration"
	"github.com/sei-protocol/sei-chain/app/upgrades"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
)

const upgradeName = "pause-state-migration"

// HardForkHandler sets the state migration rate to zero at a target height.
type HardForkHandler struct {
	targetHeight  int64
	targetChainID string
	paramsKeeper  paramskeeper.Keeper
}

// NewHardForkHandler returns a handler for pausing state migration on one chain.
func NewHardForkHandler(
	height int64,
	chainID string,
	paramsKeeper paramskeeper.Keeper,
) upgrades.HardForkHandler {
	return HardForkHandler{
		targetHeight:  height,
		targetChainID: chainID,
		paramsKeeper:  paramsKeeper,
	}
}

func (h HardForkHandler) GetName() string {
	return fmt.Sprintf("%s-%d", upgradeName, h.targetHeight)
}

func (h HardForkHandler) GetTargetChainID() string {
	return h.targetChainID
}

func (h HardForkHandler) GetTargetHeight() int64 {
	return h.targetHeight
}

func (h HardForkHandler) ExecuteHandler(ctx sdk.Context) error {
	subspace, ok := h.paramsKeeper.GetSubspace(migration.SubspaceName)
	if !ok {
		return fmt.Errorf("migration parameter subspace is not registered")
	}
	subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, uint64(0))
	return nil
}

var _ upgrades.HardForkHandler = HardForkHandler{}
