package keeper

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
)

const FeeCollectorAddress = "41cc5d9842746c69d689c8379f7f5662b8701393"

func (server msgServer) GetFeeCollectorAddress(ctx sdk.Context) (common.Address, error) {
	moduleAddr := server.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	if evmAddr, ok := server.GetEVMAddress(ctx, moduleAddr); !ok {
		return common.Address{}, errors.New("fee collector's EVM address not found")
	} else {
		return evmAddr, nil
	}
}
