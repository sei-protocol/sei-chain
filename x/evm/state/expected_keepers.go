package state

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	"github.com/ethereum/go-ethereum/common"
)

type EVMKeeper interface {
	PrefixStore(sdk.Context, []byte) sdk.KVStore
	PurgePrefix(sdk.Context, []byte)
	GetSeiAddress(sdk.Context, common.Address) (sdk.AccAddress, bool)
	BankKeeper() bankkeeper.Keeper
	GetBaseDenom(sdk.Context) string
	DeleteAddressMapping(sdk.Context, sdk.AccAddress, common.Address)
	GetBalance(sdk.Context, common.Address) uint64
	SetOrDeleteBalance(sdk.Context, common.Address, uint64)
	GetModuleBalance(sdk.Context) *big.Int
	AccountKeeper() *authkeeper.AccountKeeper
}
