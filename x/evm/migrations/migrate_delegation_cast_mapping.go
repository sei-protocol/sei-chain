package migrations

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

// MigrateDelegationCastMapping drops residual direct-cast Sei address mappings
// for EVM addresses whose stored code is either an EIP-7702 delegation
// indicator or empty. A cast-form forward mapping is otherwise only ever
// produced by SetCode's auto-association on a freshly-coded address, and
// SetCode no longer registers one for delegation code (or for code that is
// later cleared back to empty), so any pre-existing entry of that shape is
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
		code := k.GetCode(ctx, evmAddr)
		_, isDelegation := ethtypes.ParseDelegation(code)
		if len(code) > 0 && !isDelegation {
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
