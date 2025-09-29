package types

import sdk "github.com/cosmos/cosmos-sdk/types"

type VaultKeeper interface {
	Withdraw(ctx sdk.Context, vaultID string, sender string)
}

type SoulSyncKeeper interface {
	VerifyKinProof(ctx sdk.Context, sender string, kinProof string) bool
}

type HoloKeeper interface {
	CheckPresence(ctx sdk.Context, sender string, presence string) bool
}
