package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/kinvault/types"
)

type Keeper struct {
	vaultKeeper    types.VaultKeeper
	soulsyncKeeper types.SoulSyncKeeper
	holoKeeper     types.HoloKeeper
}

func NewKeeper(vaultKeeper types.VaultKeeper, soulsyncKeeper types.SoulSyncKeeper, holoKeeper types.HoloKeeper) Keeper {
	return Keeper{
		vaultKeeper:    vaultKeeper,
		soulsyncKeeper: soulsyncKeeper,
		holoKeeper:     holoKeeper,
	}
}

func (k Keeper) Withdraw(ctx sdk.Context, vaultID, sender string) {
	k.vaultKeeper.Withdraw(ctx, vaultID, sender)
}

func (k Keeper) HasValidKinProof(ctx sdk.Context, sender, kinProof string) bool {
	return k.soulsyncKeeper.VerifyKinProof(ctx, sender, kinProof)
}

func (k Keeper) HasValidHoloPresence(ctx sdk.Context, sender, holoPresence string) bool {
	return k.holoKeeper.CheckPresence(ctx, sender, holoPresence)
}
