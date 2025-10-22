package utils

import (
	"context"
	"math/big"

	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	"github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibctypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/utils"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

type Keepers interface {
	BankK() BankKeeper
	BankMS() BankMsgServer
	EVMK() EVMKeeper
	AccountK() AccountKeeper
	OracleK() OracleKeeper
	WasmdK() WasmdKeeper
	WasmdVK() WasmdViewKeeper
	StakingK() StakingKeeper
	StakingQ() StakingQuerier
	GovK() GovKeeper
	GovMS() GovMsgServer
	DistributionK() DistributionKeeper
	TransferK() TransferKeeper
	ClientK() ClientKeeper
	ConnectionK() ConnectionKeeper
	ChannelK() ChannelKeeper
	TxConfig() client.TxConfig
}

type EmptyKeepers struct{}

func (ek *EmptyKeepers) BankK() BankKeeper                 { return nil }
func (ek *EmptyKeepers) BankMS() BankMsgServer             { return nil }
func (ek *EmptyKeepers) EVMK() EVMKeeper                   { return nil }
func (ek *EmptyKeepers) AccountK() AccountKeeper           { return nil }
func (ek *EmptyKeepers) OracleK() OracleKeeper             { return nil }
func (ek *EmptyKeepers) WasmdK() WasmdKeeper               { return nil }
func (ek *EmptyKeepers) WasmdVK() WasmdViewKeeper          { return nil }
func (ek *EmptyKeepers) StakingK() StakingKeeper           { return nil }
func (ek *EmptyKeepers) StakingQ() StakingQuerier          { return nil }
func (ek *EmptyKeepers) GovK() GovKeeper                   { return nil }
func (ek *EmptyKeepers) GovMS() GovMsgServer               { return nil }
func (ek *EmptyKeepers) DistributionK() DistributionKeeper { return nil }
func (ek *EmptyKeepers) TransferK() TransferKeeper         { return nil }
func (ek *EmptyKeepers) ClientK() ClientKeeper             { return nil }
func (ek *EmptyKeepers) ConnectionK() ConnectionKeeper     { return nil }
func (ek *EmptyKeepers) ChannelK() ChannelKeeper           { return nil }
func (ek *EmptyKeepers) TxConfig() client.TxConfig         { return nil }

type BankKeeper interface {
	SendCoins(sdk.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
	SendCoinsAndWei(ctx sdk.Context, from sdk.AccAddress, to sdk.AccAddress, amt sdk.Int, wei sdk.Int) error
	GetBalance(sdk.Context, sdk.AccAddress, string) sdk.Coin
	GetAllBalances(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins
	GetWeiBalance(ctx sdk.Context, addr sdk.AccAddress) sdk.Int
	GetDenomMetaData(ctx sdk.Context, denom string) (banktypes.Metadata, bool)
	GetSupply(ctx sdk.Context, denom string) sdk.Coin
	LockedCoins(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins
	SpendableCoins(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins
}

type BankMsgServer interface {
	Send(goCtx context.Context, msg *banktypes.MsgSend) (*banktypes.MsgSendResponse, error)
}

type EVMKeeper interface {
	GetSeiAddress(sdk.Context, common.Address) (sdk.AccAddress, bool)
	GetSeiAddressOrDefault(ctx sdk.Context, evmAddress common.Address) sdk.AccAddress // only used for getting precompile Sei addresses
	GetEVMAddress(sdk.Context, sdk.AccAddress) (common.Address, bool)
	GetEVMAddressOrDefault(sdk.Context, sdk.AccAddress) common.Address
	SetAddressMapping(sdk.Context, sdk.AccAddress, common.Address)
	GetCodeHash(sdk.Context, common.Address) common.Hash
	GetCode(ctx sdk.Context, addr common.Address) []byte
	GetPriorityNormalizer(ctx sdk.Context) sdk.Dec
	GetBaseDenom(ctx sdk.Context) string
	GetBalance(ctx sdk.Context, addr sdk.AccAddress) *big.Int
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
	GetAccount(ctx sdk.Context, addr sdk.AccAddress) authtypes.AccountI
	HasAccount(ctx sdk.Context, addr sdk.AccAddress) bool
	SetAccount(ctx sdk.Context, acc authtypes.AccountI)
	RemoveAccount(ctx sdk.Context, acc authtypes.AccountI)
	NewAccountWithAddress(ctx sdk.Context, addr sdk.AccAddress) authtypes.AccountI
	GetParams(ctx sdk.Context) (params authtypes.Params)
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
	QuerySmartSafe(ctx sdk.Context, contractAddr sdk.AccAddress, req []byte) ([]byte, error)
	QuerySmart(ctx sdk.Context, contractAddr sdk.AccAddress, req []byte) ([]byte, error)
}

type StakingKeeper interface {
	Delegate(goCtx context.Context, msg *stakingtypes.MsgDelegate) (*stakingtypes.MsgDelegateResponse, error)
	BeginRedelegate(goCtx context.Context, msg *stakingtypes.MsgBeginRedelegate) (*stakingtypes.MsgBeginRedelegateResponse, error)
	Undelegate(goCtx context.Context, msg *stakingtypes.MsgUndelegate) (*stakingtypes.MsgUndelegateResponse, error)
	CreateValidator(goCtx context.Context, msg *stakingtypes.MsgCreateValidator) (*stakingtypes.MsgCreateValidatorResponse, error)
	EditValidator(goCtx context.Context, msg *stakingtypes.MsgEditValidator) (*stakingtypes.MsgEditValidatorResponse, error)
}

type StakingQuerier interface {
	Delegation(c context.Context, req *stakingtypes.QueryDelegationRequest) (*stakingtypes.QueryDelegationResponse, error)
}

type GovKeeper interface {
	AddVote(ctx sdk.Context, proposalID uint64, voterAddr sdk.AccAddress, options govtypes.WeightedVoteOptions) error
	AddDeposit(ctx sdk.Context, proposalID uint64, depositorAddr sdk.AccAddress, depositAmount sdk.Coins) (bool, error)
}

type GovMsgServer interface {
	Vote(goCtx context.Context, msg *govtypes.MsgVote) (*govtypes.MsgVoteResponse, error)
	VoteWeighted(goCtx context.Context, msg *govtypes.MsgVoteWeighted) (*govtypes.MsgVoteWeightedResponse, error)
	Deposit(goCtx context.Context, msg *govtypes.MsgDeposit) (*govtypes.MsgDepositResponse, error)
	SubmitProposal(goCtx context.Context, msg *govtypes.MsgSubmitProposal) (*govtypes.MsgSubmitProposalResponse, error)
}

type DistributionKeeper interface {
	SetWithdrawAddr(ctx sdk.Context, delegatorAddr sdk.AccAddress, withdrawAddr sdk.AccAddress) error
	WithdrawDelegationRewards(ctx sdk.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) (sdk.Coins, error)
	WithdrawValidatorCommission(ctx sdk.Context, valAddr sdk.ValAddress) (sdk.Coins, error)
	DelegationTotalRewards(c context.Context, req *distrtypes.QueryDelegationTotalRewardsRequest) (*distrtypes.QueryDelegationTotalRewardsResponse, error)
}

type TransferKeeper interface {
	Transfer(goCtx context.Context, msg *ibctypes.MsgTransfer) (*ibctypes.MsgTransferResponse, error)
	SendTransfer(
		ctx sdk.Context,
		sourcePort,
		sourceChannel string,
		token sdk.Coin,
		sender sdk.AccAddress,
		receiver string,
		timeoutHeight clienttypes.Height,
		timeoutTimestamp uint64,
	) error
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
