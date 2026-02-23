package state

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	bankkeeper "github.com/sei-protocol/sei-chain/giga/deps/xbank/keeper"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	upgradekeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"
)

type EVMKeeper interface {
	PrefixStore(sdk.Context, []byte) sdk.KVStore
	PurgePrefix(sdk.Context, []byte)
	GetSeiAddress(sdk.Context, common.Address) (sdk.AccAddress, bool)
	GetSeiAddressOrDefault(ctx sdk.Context, evmAddress common.Address) sdk.AccAddress
	BankKeeper() bankkeeper.Keeper
	GetBaseDenom(sdk.Context) string
	DeleteAddressMapping(sdk.Context, sdk.AccAddress, common.Address)
	GetCode(sdk.Context, common.Address) []byte
	SetCode(sdk.Context, common.Address, []byte)
	GetCodeHash(sdk.Context, common.Address) common.Hash
	GetCodeSize(sdk.Context, common.Address) int
	GetState(sdk.Context, common.Address, common.Hash) common.Hash
	SetState(sdk.Context, common.Address, common.Hash, common.Hash)
	AccountKeeper() *authkeeper.AccountKeeper
	GetFeeCollectorAddress(sdk.Context) (common.Address, error)
	GetNonce(sdk.Context, common.Address) uint64
	SetNonce(sdk.Context, common.Address, uint64)
	GetBalance(ctx sdk.Context, addr sdk.AccAddress) *big.Int
	UpgradeKeeper() *upgradekeeper.Keeper
}
