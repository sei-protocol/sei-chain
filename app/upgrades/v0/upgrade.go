package v0

import (
	"fmt"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app/upgrades"
)

const (
	UpgradeName = "v0"
)

// HardForkUpgradeHandler defines an example hard fork handler that will be
// executed during BeginBlock at a target height and chain-ID.
type HardForkUpgradeHandler struct {
	TargetHeight  int64
	TargetChainID string
	WasmKeeper    wasm.Keeper
}

func NewHardForkUpgradeHandler(height int64, chainID string, wk wasm.Keeper) upgrades.HardForkHandler {
	return HardForkUpgradeHandler{
		TargetHeight:  height,
		TargetChainID: chainID,
		WasmKeeper:    wk,
	}
}

func (h HardForkUpgradeHandler) GetName() string {
	return UpgradeName
}

func (h HardForkUpgradeHandler) GetTargetChainID() string {
	return h.TargetChainID
}

func (h HardForkUpgradeHandler) GetTargetHeight() int64 {
	return h.TargetHeight
}

func (h HardForkUpgradeHandler) ExecuteHandler(ctx sdk.Context) error {
	govKeeper := wasmkeeper.NewGovPermissionKeeper(h.WasmKeeper)
	return h.migrateGringotts(ctx, govKeeper)
}

func (h HardForkUpgradeHandler) migrateGringotts(ctx sdk.Context, govKeeper *wasmkeeper.PermissionedKeeper) error {
	var (
		contractAddr sdk.AccAddress
		newCodeID    uint64
		msg          []byte
	)

	switch h.TargetChainID {
	case upgrades.ChainIDSeiHardForkTest:
		// TODO: ...

	default:
		return fmt.Errorf("unknown chain ID: %s", h.TargetChainID)
	}

	// Note: Since we're using a GovPermissionKeeper, the caller is not used/required,
	// since the authz policy will automatically allow the migration.
	_, err := govKeeper.Migrate(ctx, contractAddr, sdk.AccAddress{}, newCodeID, msg)
	if err != nil {
		return fmt.Errorf("failed to execute wasm migration: %w", err)
	}

	return nil
}
