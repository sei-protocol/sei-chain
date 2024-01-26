package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k Keeper) InitGenesis(ctx sdk.Context, genState types.GenesisState) {
	moduleAcc := authtypes.NewEmptyModuleAccount(types.ModuleName, authtypes.Minter, authtypes.Burner)
	k.accountKeeper.SetModuleAccount(ctx, moduleAcc)

	k.SetParams(ctx, types.DefaultParams())

	seiAddrFc := k.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName) // feeCollector == coinbase
	k.SetAddressMapping(ctx, seiAddrFc, GetCoinbaseAddress())

	for _, addr := range genState.AddressAssociations {
		k.SetAddressMapping(ctx, sdk.MustAccAddressFromBech32(addr.SeiAddress), common.HexToAddress(addr.EthAddress))
	}
}
