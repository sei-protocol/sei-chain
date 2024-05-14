package common

import (
	"context"

	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	"github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibctypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	"github.com/ethereum/go-ethereum/common"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

type BankKeeper interface {
	SendCoins(sdk.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
	SendCoinsAndWei(ctx sdk.Context, from sdk.AccAddress, to sdk.AccAddress, amt sdk.Int, wei sdk.Int) error
	GetBalance(sdk.Context, sdk.AccAddress, string) sdk.Coin
	GetAllBalances(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins
	GetWeiBalance(ctx sdk.Context, addr sdk.AccAddress) sdk.Int
	GetDenomMetaData(ctx sdk.Context, denom string) (banktypes.Metadata, bool)
	GetSupply(ctx sdk.Context, denom string) sdk.Coin
}

type EVMKeeper interface {
	GetSeiAddress(sdk.Context, common.Address) (sdk.AccAddress, bool)
	GetSeiAddressOrDefault(ctx sdk.Context, evmAddress common.Address) sdk.AccAddress // only used for getting precompile Sei addresses
	GetEVMAddress(sdk.Context, sdk.AccAddress) (common.Address, bool)
	GetCodeHash(sdk.Context, common.Address) common.Hash
	GetPriorityNormalizer(ctx sdk.Context) sdk.Dec
	GetBaseDenom(ctx sdk.Context) string
	SetERC20NativePointer(ctx sdk.Context, token string, addr common.Address) error
	GetERC20NativePointer(ctx sdk.Context, token string) (addr common.Address, version uint16, exists bool)
	SetERC20CW20Pointer(ctx sdk.Context, cw20Address string, addr common.Address) error
	GetERC20CW20Pointer(ctx sdk.Context, cw20Address string) (addr common.Address, version uint16, exists bool)
	SetERC721CW721Pointer(ctx sdk.Context, cw721Address string, addr common.Address) error
	GetERC721CW721Pointer(ctx sdk.Context, cw721Address string) (addr common.Address, version uint16, exists bool)
}

type OracleKeeper interface {
	IterateBaseExchangeRates(ctx sdk.Context, handler func(denom string, exchangeRate oracletypes.OracleExchangeRate) (stop bool))
	CalculateTwaps(ctx sdk.Context, lookbackSeconds uint64) (oracletypes.OracleTwaps, error)
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

type TransferKeeper interface {
	Transfer(goCtx context.Context, msg *ibctypes.MsgTransfer) (*ibctypes.MsgTransferResponse, error)
}

type ClientKeeper interface {
	GetClientState(ctx sdk.Context, clientID string) (exported.ClientState, bool)
	GetClientConsensusState(ctx sdk.Context, clientID string, height exported.Height) (exported.ConsensusState, bool)
}

type ConnectionKeeper interface {
	GetConnection(ctx sdk.Context, connectionID string) (connectiontypes.ConnectionEnd, bool)
}

type ChannelKeeper interface {
	GetChannel(ctx sdk.Context, portID, channelID string) (types.Channel, bool)
}
