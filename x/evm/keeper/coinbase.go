package keeper

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const CoinbaseSeedAddress = "0000000000000000000000000000000000000001"
const CoinbaseNonce = 42

func (k Keeper) GetFeeCollectorAddress(ctx sdk.Context) (common.Address, error) {
	moduleAddr := k.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	if evmAddr, ok := k.GetEVMAddress(ctx, moduleAddr); !ok {
		return common.Address{}, errors.New("fee collector's EVM address not found")
	} else {
		return evmAddr, nil
	}
}

func GetCoinbaseAddress() common.Address {
	return crypto.CreateAddress(common.HexToAddress(CoinbaseSeedAddress), CoinbaseNonce)
}
