package migrations

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

func MigrateCastAddressBalances(ctx sdk.Context, k *keeper.Keeper) (rerr error) {
	k.IterateSeiAddressMapping(ctx, func(evmAddr common.Address, seiAddr sdk.AccAddress) bool {
		castAddr := sdk.AccAddress(evmAddr[:])
		if balances := k.BankKeeper().SpendableCoins(ctx, castAddr); !balances.IsZero() {
			if err := k.BankKeeper().SendCoins(ctx, castAddr, seiAddr, balances); err != nil {
				ctx.Logger().Error(fmt.Sprintf("error migrating balances from cast address to real address for %s due to %s", evmAddr.Hex(), err))
				rerr = err
				return true
			}
		}
		if wei := k.BankKeeper().GetWeiBalance(ctx, castAddr); !wei.IsZero() {
			if err := k.BankKeeper().SendCoinsAndWei(ctx, castAddr, seiAddr, sdk.ZeroInt(), wei); err != nil {
				ctx.Logger().Error(fmt.Sprintf("error migrating wei from cast address to real address for %s due to %s", evmAddr.Hex(), err))
				rerr = err
				return true
			}
		}
		return false
	})
	return
}
