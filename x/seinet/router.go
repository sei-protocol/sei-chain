package seinet

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/seinet/keeper"
	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

// VaultRouter coordinates the Kinmodule proof flow across keeper boundaries.
type VaultRouter struct {
	keeper keeper.Keeper
}

// NewVaultRouter wires a keeper into a router instance.
func NewVaultRouter(k keeper.Keeper) VaultRouter {
	return VaultRouter{keeper: k}
}

// CreateVault creates a vault after validating the caller has a soul sigil hash.
func (vr VaultRouter) CreateVault(ctx sdk.Context, owner sdk.AccAddress, soulSigilHash string) (types.SeiVault, error) {
	if len(soulSigilHash) == 0 {
		return types.SeiVault{}, fmt.Errorf("soul sigil hash required")
	}
	return vr.keeper.CreateVault(ctx, owner, soulSigilHash)
}

// Deposit routes funds into the vault escrow managed by the keeper.
func (vr VaultRouter) Deposit(ctx sdk.Context, vaultID uint64, depositor sdk.AccAddress, amount sdk.Coins) (types.SeiVault, error) {
	return vr.keeper.DepositToVault(ctx, vaultID, depositor, amount)
}

// VaultScannerV2WithSig performs presence routing, executes settlements and enforces royalties.
func (vr VaultRouter) VaultScannerV2WithSig(ctx sdk.Context, vaultID uint64, presenceProof string, withdrawalSig []byte) (types.VaultIntrospection, error) {
	if len(withdrawalSig) == 0 {
		return types.VaultIntrospection{}, fmt.Errorf("withdrawal signature required")
	}

	introspection, err := vr.keeper.IntrospectVault(ctx, vaultID, presenceProof, withdrawalSig)
	if err != nil {
		return types.VaultIntrospection{}, err
	}

	if introspection.PaywordTriggered {
		if _, err := vr.keeper.ExecutePaywordSettlement(ctx, introspection.Vault.Id); err != nil {
			return types.VaultIntrospection{}, err
		}

		if err := vr.keeper.SeiNetEnforceRoyalty(ctx, types.PaywordRoyaltyClauseEnforced); err != nil {
			return types.VaultIntrospection{}, err
		}

		updated, found := vr.keeper.GetVault(ctx, introspection.Vault.Id)
		if found {
			introspection.Vault = updated
		}
	}

	return introspection, nil
}
