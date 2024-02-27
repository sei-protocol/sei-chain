package common

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
)

type BankKeeper interface {
	SendCoins(sdk.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
	SendCoinsAndWei(ctx sdk.Context, from sdk.AccAddress, to sdk.AccAddress, amt sdk.Int, wei sdk.Int) error
	GetBalance(sdk.Context, sdk.AccAddress, string) sdk.Coin
	GetWeiBalance(ctx sdk.Context, addr sdk.AccAddress) sdk.Int
	GetDenomMetaData(ctx sdk.Context, denom string) (banktypes.Metadata, bool)
	GetSupply(ctx sdk.Context, denom string) sdk.Coin
}

type EVMKeeper interface {
	GetSeiAddress(sdk.Context, common.Address) (sdk.AccAddress, bool)
	GetSeiAddressOrDefault(ctx sdk.Context, evmAddress common.Address) sdk.AccAddress
	GetEVMAddress(sdk.Context, sdk.AccAddress) (common.Address, bool)
	GetEVMAddressFromBech32OrDefault(ctx sdk.Context, seiAddress string) common.Address
	GetCodeHash(sdk.Context, common.Address) common.Hash
	IsCodeHashWhitelistedForDelegateCall(ctx sdk.Context, h common.Hash) bool
	IsCodeHashWhitelistedForBankSend(ctx sdk.Context, h common.Hash) bool
	GetPriorityNormalizer(ctx sdk.Context) sdk.Dec
	GetBaseDenom(ctx sdk.Context) string
}

type WasmdKeeper interface {
	Instantiate(ctx sdk.Context, codeID uint64, creator, admin sdk.AccAddress, initMsg []byte, label string, deposit sdk.Coins) (sdk.AccAddress, []byte, error)
	Execute(ctx sdk.Context, contractAddress sdk.AccAddress, caller sdk.AccAddress, msg []byte, coins sdk.Coins) ([]byte, error)
}

type WasmdViewKeeper interface {
	QuerySmart(ctx sdk.Context, contractAddr sdk.AccAddress, req []byte) ([]byte, error)
}

type StakingKeeper interface {
	Delegate(goCtx context.Context, msg *stakingtypes.MsgDelegate) (*stakingtypes.MsgDelegateResponse, error)
	BeginRedelegate(goCtx context.Context, msg *stakingtypes.MsgBeginRedelegate) (*stakingtypes.MsgBeginRedelegateResponse, error)
	Undelegate(goCtx context.Context, msg *stakingtypes.MsgUndelegate) (*stakingtypes.MsgUndelegateResponse, error)
}

type GovKeeper interface {
	AddVote(ctx sdk.Context, proposalID uint64, voterAddr sdk.AccAddress, options govtypes.WeightedVoteOptions) error
	AddDeposit(ctx sdk.Context, proposalID uint64, depositorAddr sdk.AccAddress, depositAmount sdk.Coins) (bool, error)
}

type DistributionKeeper interface {
	SetWithdrawAddr(ctx sdk.Context, delegatorAddr sdk.AccAddress, withdrawAddr sdk.AccAddress) error
	WithdrawDelegationRewards(ctx sdk.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) (sdk.Coins, error)
}
