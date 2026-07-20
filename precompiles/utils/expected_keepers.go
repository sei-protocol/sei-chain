package utils

import (
	"context"
	"math/big"

	connectiontypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/types"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/exported"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	evidencetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	paramsproposal "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	ibctypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
	clienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	"github.com/sei-protocol/sei-chain/utils"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

type Keepers interface {
	BankK() BankKeeper
	BankMS() BankMsgServer
	BankQ() BankQuerier
	EVMK() EVMKeeper
	AccountK() AccountKeeper
	AuthQ() AuthQuerier
	AuthzQ() AuthzQuerier
	OracleK() OracleKeeper
	WasmdK() WasmdKeeper
	WasmdVK() WasmdViewKeeper
	StakingK() StakingKeeper
	StakingQ() StakingQuerier
	GovK() GovKeeper
	GovMS() GovMsgServer
	GovQ() GovQuerier
	DistributionK() DistributionKeeper
	DistributionQ() DistributionQuerier
	EvidenceQ() EvidenceQuerier
	FeegrantQ() FeegrantQuerier
	MintQ() MintQuerier
	ParamsQ() ParamsQuerier
	SlashingQ() SlashingQuerier
	UpgradeQ() UpgradeQuerier
	TransferK() TransferKeeper
	ClientK() ClientKeeper
	ConnectionK() ConnectionKeeper
	ChannelK() ChannelKeeper
	TxConfig() client.TxConfig
	Codec() codec.Codec
}

type EmptyKeepers struct{}

func (ek *EmptyKeepers) BankK() BankKeeper                 { return nil }
func (ek *EmptyKeepers) BankMS() BankMsgServer             { return nil }
func (ek *EmptyKeepers) BankQ() BankQuerier                { return nil }
func (ek *EmptyKeepers) EVMK() EVMKeeper                   { return nil }
func (ek *EmptyKeepers) AccountK() AccountKeeper           { return nil }
func (ek *EmptyKeepers) AuthQ() AuthQuerier                { return nil }
func (ek *EmptyKeepers) AuthzQ() AuthzQuerier              { return nil }
func (ek *EmptyKeepers) OracleK() OracleKeeper             { return nil }
func (ek *EmptyKeepers) WasmdK() WasmdKeeper               { return nil }
func (ek *EmptyKeepers) WasmdVK() WasmdViewKeeper          { return nil }
func (ek *EmptyKeepers) StakingK() StakingKeeper           { return nil }
func (ek *EmptyKeepers) StakingQ() StakingQuerier          { return nil }
func (ek *EmptyKeepers) GovK() GovKeeper                   { return nil }
func (ek *EmptyKeepers) GovMS() GovMsgServer               { return nil }
func (ek *EmptyKeepers) GovQ() GovQuerier                  { return nil }
func (ek *EmptyKeepers) DistributionK() DistributionKeeper { return nil }
func (ek *EmptyKeepers) DistributionQ() DistributionQuerier {
	return nil
}
func (ek *EmptyKeepers) EvidenceQ() EvidenceQuerier    { return nil }
func (ek *EmptyKeepers) FeegrantQ() FeegrantQuerier    { return nil }
func (ek *EmptyKeepers) MintQ() MintQuerier            { return nil }
func (ek *EmptyKeepers) ParamsQ() ParamsQuerier        { return nil }
func (ek *EmptyKeepers) SlashingQ() SlashingQuerier    { return nil }
func (ek *EmptyKeepers) UpgradeQ() UpgradeQuerier      { return nil }
func (ek *EmptyKeepers) TransferK() TransferKeeper     { return nil }
func (ek *EmptyKeepers) ClientK() ClientKeeper         { return nil }
func (ek *EmptyKeepers) ConnectionK() ConnectionKeeper { return nil }
func (ek *EmptyKeepers) ChannelK() ChannelKeeper       { return nil }
func (ek *EmptyKeepers) TxConfig() client.TxConfig     { return nil }
func (ek *EmptyKeepers) Codec() codec.Codec            { return nil }

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
	Validators(c context.Context, req *stakingtypes.QueryValidatorsRequest) (*stakingtypes.QueryValidatorsResponse, error)
	Validator(c context.Context, req *stakingtypes.QueryValidatorRequest) (*stakingtypes.QueryValidatorResponse, error)
	ValidatorDelegations(c context.Context, req *stakingtypes.QueryValidatorDelegationsRequest) (*stakingtypes.QueryValidatorDelegationsResponse, error)
	ValidatorUnbondingDelegations(c context.Context, req *stakingtypes.QueryValidatorUnbondingDelegationsRequest) (*stakingtypes.QueryValidatorUnbondingDelegationsResponse, error)
	UnbondingDelegation(c context.Context, req *stakingtypes.QueryUnbondingDelegationRequest) (*stakingtypes.QueryUnbondingDelegationResponse, error)
	DelegatorDelegations(c context.Context, req *stakingtypes.QueryDelegatorDelegationsRequest) (*stakingtypes.QueryDelegatorDelegationsResponse, error)
	DelegatorValidator(c context.Context, req *stakingtypes.QueryDelegatorValidatorRequest) (*stakingtypes.QueryDelegatorValidatorResponse, error)
	DelegatorUnbondingDelegations(c context.Context, req *stakingtypes.QueryDelegatorUnbondingDelegationsRequest) (*stakingtypes.QueryDelegatorUnbondingDelegationsResponse, error)
	Redelegations(c context.Context, req *stakingtypes.QueryRedelegationsRequest) (*stakingtypes.QueryRedelegationsResponse, error)
	DelegatorValidators(c context.Context, req *stakingtypes.QueryDelegatorValidatorsRequest) (*stakingtypes.QueryDelegatorValidatorsResponse, error)
	HistoricalInfo(c context.Context, req *stakingtypes.QueryHistoricalInfoRequest) (*stakingtypes.QueryHistoricalInfoResponse, error)
	Pool(c context.Context, req *stakingtypes.QueryPoolRequest) (*stakingtypes.QueryPoolResponse, error)
	Params(c context.Context, req *stakingtypes.QueryParamsRequest) (*stakingtypes.QueryParamsResponse, error)
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
	GetDelegatorWithdrawAddr(ctx sdk.Context, delAddr sdk.AccAddress) sdk.AccAddress
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

type BankQuerier interface {
	SpendableBalances(ctx context.Context, req *banktypes.QuerySpendableBalancesRequest) (*banktypes.QuerySpendableBalancesResponse, error)
	TotalSupply(ctx context.Context, req *banktypes.QueryTotalSupplyRequest) (*banktypes.QueryTotalSupplyResponse, error)
	Params(ctx context.Context, req *banktypes.QueryParamsRequest) (*banktypes.QueryParamsResponse, error)
	DenomMetadata(c context.Context, req *banktypes.QueryDenomMetadataRequest) (*banktypes.QueryDenomMetadataResponse, error)
	DenomsMetadata(c context.Context, req *banktypes.QueryDenomsMetadataRequest) (*banktypes.QueryDenomsMetadataResponse, error)
}

type AuthQuerier interface {
	Accounts(c context.Context, req *authtypes.QueryAccountsRequest) (*authtypes.QueryAccountsResponse, error)
	Account(c context.Context, req *authtypes.QueryAccountRequest) (*authtypes.QueryAccountResponse, error)
	Params(c context.Context, req *authtypes.QueryParamsRequest) (*authtypes.QueryParamsResponse, error)
	NextAccountNumber(c context.Context, req *authtypes.QueryNextAccountNumberRequest) (*authtypes.QueryNextAccountNumberResponse, error)
}

type AuthzQuerier interface {
	Grants(c context.Context, req *authz.QueryGrantsRequest) (*authz.QueryGrantsResponse, error)
	GranterGrants(c context.Context, req *authz.QueryGranterGrantsRequest) (*authz.QueryGranterGrantsResponse, error)
	GranteeGrants(c context.Context, req *authz.QueryGranteeGrantsRequest) (*authz.QueryGranteeGrantsResponse, error)
}

type GovQuerier interface {
	Proposal(c context.Context, req *govtypes.QueryProposalRequest) (*govtypes.QueryProposalResponse, error)
	Proposals(c context.Context, req *govtypes.QueryProposalsRequest) (*govtypes.QueryProposalsResponse, error)
	Vote(c context.Context, req *govtypes.QueryVoteRequest) (*govtypes.QueryVoteResponse, error)
	Votes(c context.Context, req *govtypes.QueryVotesRequest) (*govtypes.QueryVotesResponse, error)
	Params(c context.Context, req *govtypes.QueryParamsRequest) (*govtypes.QueryParamsResponse, error)
	Deposit(c context.Context, req *govtypes.QueryDepositRequest) (*govtypes.QueryDepositResponse, error)
	Deposits(c context.Context, req *govtypes.QueryDepositsRequest) (*govtypes.QueryDepositsResponse, error)
	TallyResult(c context.Context, req *govtypes.QueryTallyResultRequest) (*govtypes.QueryTallyResultResponse, error)
}

type DistributionQuerier interface {
	Params(c context.Context, req *distrtypes.QueryParamsRequest) (*distrtypes.QueryParamsResponse, error)
	ValidatorOutstandingRewards(c context.Context, req *distrtypes.QueryValidatorOutstandingRewardsRequest) (*distrtypes.QueryValidatorOutstandingRewardsResponse, error)
	ValidatorCommission(c context.Context, req *distrtypes.QueryValidatorCommissionRequest) (*distrtypes.QueryValidatorCommissionResponse, error)
	ValidatorSlashes(c context.Context, req *distrtypes.QueryValidatorSlashesRequest) (*distrtypes.QueryValidatorSlashesResponse, error)
	DelegationRewards(c context.Context, req *distrtypes.QueryDelegationRewardsRequest) (*distrtypes.QueryDelegationRewardsResponse, error)
	DelegatorValidators(c context.Context, req *distrtypes.QueryDelegatorValidatorsRequest) (*distrtypes.QueryDelegatorValidatorsResponse, error)
	DelegatorWithdrawAddress(c context.Context, req *distrtypes.QueryDelegatorWithdrawAddressRequest) (*distrtypes.QueryDelegatorWithdrawAddressResponse, error)
	CommunityPool(c context.Context, req *distrtypes.QueryCommunityPoolRequest) (*distrtypes.QueryCommunityPoolResponse, error)
}

type EvidenceQuerier interface {
	Evidence(c context.Context, req *evidencetypes.QueryEvidenceRequest) (*evidencetypes.QueryEvidenceResponse, error)
	AllEvidence(c context.Context, req *evidencetypes.QueryAllEvidenceRequest) (*evidencetypes.QueryAllEvidenceResponse, error)
}

type FeegrantQuerier interface {
	Allowance(c context.Context, req *feegrant.QueryAllowanceRequest) (*feegrant.QueryAllowanceResponse, error)
	Allowances(c context.Context, req *feegrant.QueryAllowancesRequest) (*feegrant.QueryAllowancesResponse, error)
	AllowancesByGranter(c context.Context, req *feegrant.QueryAllowancesByGranterRequest) (*feegrant.QueryAllowancesByGranterResponse, error)
}

type MintQuerier interface {
	Params(c context.Context, req *minttypes.QueryParamsRequest) (*minttypes.QueryParamsResponse, error)
	Minter(c context.Context, req *minttypes.QueryMinterRequest) (*minttypes.QueryMinterResponse, error)
}

type ParamsQuerier interface {
	Params(c context.Context, req *paramsproposal.QueryParamsRequest) (*paramsproposal.QueryParamsResponse, error)
}

type SlashingQuerier interface {
	Params(c context.Context, req *slashingtypes.QueryParamsRequest) (*slashingtypes.QueryParamsResponse, error)
	SigningInfo(c context.Context, req *slashingtypes.QuerySigningInfoRequest) (*slashingtypes.QuerySigningInfoResponse, error)
	SigningInfos(c context.Context, req *slashingtypes.QuerySigningInfosRequest) (*slashingtypes.QuerySigningInfosResponse, error)
}

type UpgradeQuerier interface {
	CurrentPlan(c context.Context, req *upgradetypes.QueryCurrentPlanRequest) (*upgradetypes.QueryCurrentPlanResponse, error)
	AppliedPlan(c context.Context, req *upgradetypes.QueryAppliedPlanRequest) (*upgradetypes.QueryAppliedPlanResponse, error)
	// The request type is marked deprecated upstream but is still the wire
	// type of the Query/UpgradedConsensusState rpc.
	UpgradedConsensusState(c context.Context, req *upgradetypes.QueryUpgradedConsensusStateRequest) (*upgradetypes.QueryUpgradedConsensusStateResponse, error) //nolint:staticcheck
	ModuleVersions(c context.Context, req *upgradetypes.QueryModuleVersionsRequest) (*upgradetypes.QueryModuleVersionsResponse, error)
}
