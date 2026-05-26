package migrations

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

// MigrateDelegationCastMapping drops residual direct-cast Sei address mappings
// for EVM addresses whose stored code is an EIP-7702 delegation indicator.
// SetCode no longer registers a cast-address mapping for delegation code, so
// any pre-existing entry of that shape is inconsistent with the current
// invariant and is removed on upgrade.
func MigrateDelegationCastMapping(ctx sdk.Context, k *keeper.Keeper) error {
	type pair struct {
		evmAddr common.Address
		seiAddr sdk.AccAddress
	}
	// Collect first, delete after — IterateSeiAddressMapping holds an open
	// iterator over the same prefix we'd be mutating.
	var toDrop []pair
	k.IterateSeiAddressMapping(ctx, func(evmAddr common.Address, seiAddr sdk.AccAddress) bool {
		if !seiAddr.Equals(sdk.AccAddress(evmAddr[:])) {
			return false
		}
		if _, isDelegation := ethtypes.ParseDelegation(k.GetCode(ctx, evmAddr)); !isDelegation {
			return false
		}
		toDrop = append(toDrop, pair{evmAddr: evmAddr, seiAddr: seiAddr})
		return false
	})
	for _, p := range toDrop {
		k.DeleteAddressMapping(ctx, p.seiAddr, p.evmAddr)
	}
	if len(toDrop) > 0 {
		logger.Info("dropped residual delegation cast mappings", "count", len(toDrop))
	}
	return nil
}
