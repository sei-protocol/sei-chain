package common

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
)

type BankKeeper interface {
	SendCoins(sdk.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
	GetBalance(sdk.Context, sdk.AccAddress, string) sdk.Coin
	GetDenomMetaData(ctx sdk.Context, denom string) (banktypes.Metadata, bool)
	GetSupply(ctx sdk.Context, denom string) sdk.Coin
}

type EVMKeeper interface {
	GetSeiAddress(sdk.Context, common.Address) (sdk.AccAddress, bool)
	GetSeiAddressOrDefault(ctx sdk.Context, evmAddress common.Address) sdk.AccAddress
	GetEVMAddress(sdk.Context, sdk.AccAddress) (common.Address, bool)
	GetEVMAddressFromBech32OrDefault(ctx sdk.Context, seiAddress string) common.Address
	GetCodeHash(sdk.Context, common.Address) common.Hash
	WhitelistedCodehashesBankSend(sdk.Context) []string
	IsCodeHashWhitelistedForDelegateCall(ctx sdk.Context, h common.Hash) bool
	IsCodeHashWhitelistedForBankSend(ctx sdk.Context, h common.Hash) bool
	GetPriorityNormalizer(ctx sdk.Context) sdk.Dec
}

type WasmdKeeper interface {
	Instantiate(ctx sdk.Context, codeID uint64, creator, admin sdk.AccAddress, initMsg []byte, label string, deposit sdk.Coins) (sdk.AccAddress, []byte, error)
	Execute(ctx sdk.Context, contractAddress sdk.AccAddress, caller sdk.AccAddress, msg []byte, coins sdk.Coins) ([]byte, error)
}

type WasmdViewKeeper interface {
	QuerySmart(ctx sdk.Context, contractAddr sdk.AccAddress, req []byte) ([]byte, error)
}
