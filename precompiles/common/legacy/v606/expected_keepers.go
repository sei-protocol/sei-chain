package v606

import (
	"context"
	"math/big"

	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	"github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibctypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/utils"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

type BankKeeper interface {
	SendCoins(sdk.Context, seitypes.AccAddress, seitypes.AccAddress, sdk.Coins) error
	SendCoinsAndWei(ctx sdk.Context, from seitypes.AccAddress, to seitypes.AccAddress, amt sdk.Int, wei sdk.Int) error
	GetBalance(sdk.Context, seitypes.AccAddress, string) sdk.Coin
	GetAllBalances(ctx sdk.Context, addr seitypes.AccAddress) sdk.Coins
	GetWeiBalance(ctx sdk.Context, addr seitypes.AccAddress) sdk.Int
	GetDenomMetaData(ctx sdk.Context, denom string) (banktypes.Metadata, bool)
	GetSupply(ctx sdk.Context, denom string) sdk.Coin
	LockedCoins(ctx sdk.Context, addr seitypes.AccAddress) sdk.Coins
	SpendableCoins(ctx sdk.Context, addr seitypes.AccAddress) sdk.Coins
}

type BankMsgServer interface {
	Send(goCtx context.Context, msg *banktypes.MsgSend) (*banktypes.MsgSendResponse, error)
}

type EVMKeeper interface {
	GetSeiAddress(sdk.Context, common.Address) (seitypes.AccAddress, bool)
	GetSeiAddressOrDefault(ctx sdk.Context, evmAddress common.Address) seitypes.AccAddress // only used for getting precompile Sei addresses
	GetEVMAddress(sdk.Context, seitypes.AccAddress) (common.Address, bool)
	GetEVMAddressOrDefault(sdk.Context, seitypes.AccAddress) common.Address
	SetAddressMapping(sdk.Context, seitypes.AccAddress, common.Address)
	GetCodeHash(sdk.Context, common.Address) common.Hash
	GetCode(ctx sdk.Context, addr common.Address) []byte
	GetPriorityNormalizer(ctx sdk.Context) sdk.Dec
	GetPriorityNormalizerPre580(ctx sdk.Context) sdk.Dec
	GetBaseDenom(ctx sdk.Context) string
	GetBalance(ctx sdk.Context, addr seitypes.AccAddress) *big.Int
	SetERC20NativePointer(ctx sdk.Context, token string, addr common.Address) error
	GetERC20NativePointer(ctx sdk.Context, token string) (addr common.Address, version uint16, exists bool)
	SetERC20CW20Pointer(ctx sdk.Context, cw20Address string, addr common.Address) error
	GetERC20CW20Pointer(ctx sdk.Context, cw20Address string) (addr common.Address, version uint16, exists bool)
	SetERC721CW721Pointer(ctx sdk.Context, cw721Address string, addr common.Address) error
	GetERC721CW721Pointer(ctx sdk.Context, cw721Address string) (addr common.Address, version uint16, exists bool)
	SetERC1155CW1155Pointer(ctx sdk.Context, cw1155Address string, addr common.Address) error
	GetERC1155CW1155Pointer(ctx sdk.Context, cw1155Address string) (addr common.Address, version uint16, exists bool)
	SetCode(ctx sdk.Context, addr common.Address, code []byte)
	UpsertERCNativePointer(
		ctx sdk.Context, evm *vm.EVM, token string, metadata utils.ERCMetadata,
	) (contractAddr common.Address, err error)
	UpsertERCCW20Pointer(
		ctx sdk.Context, evm *vm.EVM, cw20Addr string, metadata utils.ERCMetadata,
	) (contractAddr common.Address, err error)
	UpsertERCCW721Pointer(
		ctx sdk.Context, evm *vm.EVM, cw721Addr string, metadata utils.ERCMetadata,
	) (contractAddr common.Address, err error)
	UpsertERCCW1155Pointer(
		ctx sdk.Context, evm *vm.EVM, cw1155Addr string, metadata utils.ERCMetadata,
	) (contractAddr common.Address, err error)
	GetEVMGasLimitFromCtx(ctx sdk.Context) uint64
	GetCosmosGasLimitFromEVMGas(ctx sdk.Context, evmGas uint64) uint64
}

type AccountKeeper interface {
	GetAccount(ctx sdk.Context, addr seitypes.AccAddress) authtypes.AccountI
	HasAccount(ctx sdk.Context, addr seitypes.AccAddress) bool
	SetAccount(ctx sdk.Context, acc authtypes.AccountI)
	RemoveAccount(ctx sdk.Context, acc authtypes.AccountI)
	NewAccountWithAddress(ctx sdk.Context, addr seitypes.AccAddress) authtypes.AccountI
	GetParams(ctx sdk.Context) (params authtypes.Params)
}

type OracleKeeper interface {
	IterateBaseExchangeRates(ctx sdk.Context, handler func(denom string, exchangeRate oracletypes.OracleExchangeRate) (stop bool))
	CalculateTwaps(ctx sdk.Context, lookbackSeconds uint64) (oracletypes.OracleTwaps, error)
}

type WasmdKeeper interface {
	Instantiate(ctx sdk.Context, codeID uint64, creator, admin seitypes.AccAddress, initMsg []byte, label string, deposit sdk.Coins) (seitypes.AccAddress, []byte, error)
	Execute(ctx sdk.Context, contractAddress seitypes.AccAddress, caller seitypes.AccAddress, msg []byte, coins sdk.Coins) ([]byte, error)
}

type WasmdViewKeeper interface {
	QuerySmartSafe(ctx sdk.Context, contractAddr seitypes.AccAddress, req []byte) ([]byte, error)
}

type StakingKeeper interface {
	Delegate(goCtx context.Context, msg *stakingtypes.MsgDelegate) (*stakingtypes.MsgDelegateResponse, error)
	BeginRedelegate(goCtx context.Context, msg *stakingtypes.MsgBeginRedelegate) (*stakingtypes.MsgBeginRedelegateResponse, error)
	Undelegate(goCtx context.Context, msg *stakingtypes.MsgUndelegate) (*stakingtypes.MsgUndelegateResponse, error)
}

type StakingQuerier interface {
	Delegation(c context.Context, req *stakingtypes.QueryDelegationRequest) (*stakingtypes.QueryDelegationResponse, error)
}

type GovKeeper interface {
	AddVote(ctx sdk.Context, proposalID uint64, voterAddr seitypes.AccAddress, options govtypes.WeightedVoteOptions) error
	AddDeposit(ctx sdk.Context, proposalID uint64, depositorAddr seitypes.AccAddress, depositAmount sdk.Coins) (bool, error)
}

type DistributionKeeper interface {
	SetWithdrawAddr(ctx sdk.Context, delegatorAddr seitypes.AccAddress, withdrawAddr seitypes.AccAddress) error
	WithdrawDelegationRewards(ctx sdk.Context, delAddr seitypes.AccAddress, valAddr seitypes.ValAddress) (sdk.Coins, error)
	DelegationTotalRewards(c context.Context, req *distrtypes.QueryDelegationTotalRewardsRequest) (*distrtypes.QueryDelegationTotalRewardsResponse, error)
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
