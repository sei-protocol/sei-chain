package gov

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	VoteMethod           = "vote"
	VoteWeightedMethod   = "voteWeighted"
	DepositMethod        = "deposit"
	SubmitProposalMethod = "submitProposal"
)

// Query method names. VoteQueryMethod and DepositQueryMethod are ABI overloads
// of the vote/deposit transaction methods; the go-ethereum abi package resolves
// the name conflicts by appending "0" to the later-declared method, so the
// resolved names below depend on abi.json declaration order.
const (
	ProposalQueryMethod    = "proposal"
	ProposalsQueryMethod   = "proposals"
	VoteQueryMethod        = "vote0"
	VotesQueryMethod       = "votes"
	ParamsQueryMethod      = "params"
	DepositQueryMethod     = "deposit0"
	DepositsQueryMethod    = "deposits"
	TallyResultQueryMethod = "tallyResult"
)

const (
	GovAddress = "0x0000000000000000000000000000000000001006"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	govMsgServer     utils.GovMsgServer
	govQuerier       utils.GovQuerier
	evmKeeper        utils.EVMKeeper
	bankKeeper       utils.BankKeeper
	cdc              codec.Codec
	address          common.Address
	proposalHandlers map[string]ProposalHandler

	VoteID           []byte
	VoteWeightedID   []byte
	DepositID        []byte
	SubmitProposalID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.Precompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		govMsgServer: keepers.GovMS(),
		govQuerier:   keepers.GovQ(),
		evmKeeper:    keepers.EVMK(),
		bankKeeper:   keepers.BankK(),
		cdc:          keepers.Codec(),
		address:      common.HexToAddress(GovAddress),
	}

	// Register proposal handlers
	p.registerProposalHandlers()

	// Register method IDs
	for name, m := range newAbi.Methods {
		switch name {
		case VoteMethod:
			p.VoteID = m.ID
		case DepositMethod:
			p.DepositID = m.ID
		case SubmitProposalMethod:
			p.SubmitProposalID = m.ID
		case VoteWeightedMethod:
			p.VoteWeightedID = m.ID
		}
	}

	// Create the precompile
	return pcommon.NewPrecompile(newAbi, p, p.address, "gov"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	if bytes.Equal(method.ID, p.VoteID) || bytes.Equal(method.ID, p.VoteWeightedID) {
		return 30000
	} else if bytes.Equal(method.ID, p.DepositID) {
		return 30000
	} else if bytes.Equal(method.ID, p.SubmitProposalID) {
		return 50000
	} else if !p.IsTransaction(method.Name) {
		// query methods are charged the default read gas cost
		return pcommon.DefaultGasCost(input, false)
	}

	// This should never happen since this is going to fail during Run
	return pcommon.UnknownMethodCallGas
}

// IsTransaction returns true for methods that mutate state. All gov query
// methods are views.
func (p PrecompileExecutor) IsTransaction(method string) bool {
	switch method {
	case VoteMethod, VoteWeightedMethod, DepositMethod, SubmitProposalMethod:
		return true
	default:
		return false
	}
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) (bz []byte, err error) {
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, errors.New("cannot delegatecall gov")
	}

	switch method.Name {
	case ProposalQueryMethod:
		return p.proposalQuery(ctx, method, args, value)
	case ProposalsQueryMethod:
		return p.proposalsQuery(ctx, method, args, value)
	case VoteQueryMethod:
		return p.voteQuery(ctx, method, args, value)
	case VotesQueryMethod:
		return p.votesQuery(ctx, method, args, value)
	case ParamsQueryMethod:
		return p.paramsQuery(ctx, method, args, value)
	case DepositQueryMethod:
		return p.depositQuery(ctx, method, args, value)
	case DepositsQueryMethod:
		return p.depositsQuery(ctx, method, args, value)
	case TallyResultQueryMethod:
		return p.tallyResultQuery(ctx, method, args, value)
	}

	if readOnly {
		return nil, errors.New("cannot call gov precompile from staticcall")
	}

	switch method.Name {
	case VoteMethod:
		return p.vote(ctx, method, caller, args, value)
	case VoteWeightedMethod:
		return p.voteWeighted(ctx, method, caller, args, value)
	case DepositMethod:
		return p.deposit(ctx, method, caller, args, value, hooks, evm)
	case SubmitProposalMethod:
		return p.submitProposal(ctx, method, caller, args, value, hooks, evm)
	}
	return
}

func (p PrecompileExecutor) vote(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	voter, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, types.NewAssociationMissingErr(caller.Hex())
	}
	proposalID := args[0].(uint64)
	voteOption := args[1].(int32)

	msg := govtypes.NewMsgVote(voter, proposalID, govtypes.VoteOption(voteOption))
	err := msg.ValidateBasic()
	if err != nil {
		return nil, err
	}

	goCtx := sdk.WrapSDKContext(ctx)
	_, err = p.govMsgServer.Vote(goCtx, msg)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p PrecompileExecutor) voteWeighted(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	voter, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, types.NewAssociationMissingErr(caller.Hex())
	}
	proposalID := args[0].(uint64)

	// args[1] is the struct array for weighted vote options
	// The ABI decoder gives us the actual struct slice
	weightedOptionsStruct := args[1].([]struct {
		Option int32  `json:"option"`
		Weight string `json:"weight"`
	})

	maxOptions := 4
	if len(weightedOptionsStruct) > maxOptions {
		return nil, fmt.Errorf("too many vote options provided: maximum allowed is %d", maxOptions)
	}

	// Convert to WeightedVoteOptions
	voteOptions := make([]govtypes.WeightedVoteOption, len(weightedOptionsStruct))
	for i, optionStruct := range weightedOptionsStruct {
		// Parse weight as decimal
		weight, err := sdk.NewDecFromStr(optionStruct.Weight)
		if err != nil {
			return nil, fmt.Errorf("invalid weight format: %w", err)
		}

		voteOptions[i] = govtypes.WeightedVoteOption{
			Option: govtypes.VoteOption(optionStruct.Option),
			Weight: weight,
		}
	}

	msg := govtypes.NewMsgVoteWeighted(voter, proposalID, voteOptions)
	err := msg.ValidateBasic()
	if err != nil {
		return nil, err
	}

	goCtx := sdk.WrapSDKContext(ctx)
	_, err = p.govMsgServer.VoteWeighted(goCtx, msg)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p PrecompileExecutor) deposit(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, hooks *tracing.Hooks, evm *vm.EVM) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	depositor, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, types.NewAssociationMissingErr(caller.Hex())
	}
	proposalID := args[0].(uint64)
	if value == nil || value.Sign() == 0 {
		return nil, errors.New("set `value` field to non-zero to deposit fund")
	}
	coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), depositor, value, p.bankKeeper, p.evmKeeper, hooks, evm.GetDepth())
	if err != nil {
		return nil, err
	}

	msg := govtypes.NewMsgDeposit(depositor, proposalID, sdk.NewCoins(coin))
	err = msg.ValidateBasic()
	if err != nil {
		return nil, err
	}

	goCtx := sdk.WrapSDKContext(ctx)
	_, err = p.govMsgServer.Deposit(goCtx, msg)
	if err != nil {
		return nil, err
	}

	return method.Outputs.Pack(true)
}

func (p PrecompileExecutor) submitProposal(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, hooks *tracing.Hooks, evm *vm.EVM) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}

	proposer, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, types.NewAssociationMissingErr(caller.Hex())
	}

	// Parse the proposal JSON
	proposalJSON := args[0].(string)
	var proposal Proposal
	if err := json.Unmarshal([]byte(proposalJSON), &proposal); err != nil {
		return nil, fmt.Errorf("failed to parse proposal JSON: %w", err)
	}

	initialDeposit, err := pcommon.HandlePaymentUsei(
		ctx,
		p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address),
		proposer,
		value,
		p.bankKeeper,
		p.evmKeeper,
		hooks,
		evm.GetDepth())

	if err != nil {
		return nil, err
	}

	// Create the proposal content using the handler system
	content, err := p.createProposalContent(ctx, proposal)
	if err != nil {
		return nil, err
	}

	// Create the MsgSubmitProposal
	msg, err :=
		govtypes.NewMsgSubmitProposalWithExpedite(content, sdk.NewCoins(initialDeposit), proposer, proposal.IsExpedited)
	if err != nil {
		return nil, err
	}

	// Validate the Msg
	err = msg.ValidateBasic()
	if err != nil {
		return nil, err
	}

	// Create a MsgServer context
	goCtx := sdk.WrapSDKContext(ctx)

	// Submit the proposal using the MsgServer
	res, err := p.govMsgServer.SubmitProposal(goCtx, msg)
	if err != nil {
		return nil, err
	}

	// Return the proposal ID
	return method.Outputs.Pack(res.ProposalId)
}

// Coin mirrors the abi.json Coin tuple. Field order must match the tuple
// component order exactly.
type Coin struct {
	Amount *big.Int
	Denom  string
}

// TallyResultData mirrors the abi.json TallyResultData tuple.
type TallyResultData struct {
	Yes        string
	Abstain    string
	No         string
	NoWithVeto string
}

// WeightedVoteOptionData mirrors the abi.json WeightedVoteOptionData tuple.
type WeightedVoteOptionData struct {
	Option int32
	Weight string
}

// ProposalData mirrors the abi.json ProposalData tuple. Content contains the
// proposal content (a protobuf Any) marshaled to JSON.
type ProposalData struct {
	Id               uint64 //nolint:revive,stylecheck // must match abi component name "id"
	Status           int32
	FinalTallyResult TallyResultData
	SubmitTime       int64
	DepositEndTime   int64
	TotalDeposit     []Coin
	VotingStartTime  int64
	VotingEndTime    int64
	IsExpedited      bool
	Content          []byte
}

// VoteData mirrors the abi.json VoteData tuple.
type VoteData struct {
	ProposalId uint64 //nolint:revive,stylecheck // must match abi component name "proposalId"
	Voter      string
	Options    []WeightedVoteOptionData
}

// DepositData mirrors the abi.json DepositData tuple.
type DepositData struct {
	ProposalId uint64 //nolint:revive,stylecheck // must match abi component name "proposalId"
	Depositor  string
	Amount     []Coin
}

// GovParams mirrors the abi.json GovParams tuple. Durations are expressed in
// seconds.
type GovParams struct {
	VotingPeriod          uint64
	ExpeditedVotingPeriod uint64
	MinDeposit            []Coin
	MaxDepositPeriod      uint64
	MinExpeditedDeposit   []Coin
	Quorum                string
	Threshold             string
	VetoThreshold         string
	ExpeditedQuorum       string
	ExpeditedThreshold    string
}

func (p PrecompileExecutor) proposalQuery(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	req := &govtypes.QueryProposalRequest{ProposalId: args[0].(uint64)}
	resp, err := p.govQuerier.Proposal(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, err
	}
	proposal, err := p.convertProposal(resp.Proposal)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(proposal)
}

func (p PrecompileExecutor) proposalsQuery(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := pcommon.ValidateArgsLength(args, 4); err != nil {
		return nil, err
	}
	voter, err := p.optionalBech32FromArg(ctx, args[1])
	if err != nil {
		return nil, err
	}
	depositor, err := p.optionalBech32FromArg(ctx, args[2])
	if err != nil {
		return nil, err
	}
	req := &govtypes.QueryProposalsRequest{
		ProposalStatus: govtypes.ProposalStatus(args[0].(int32)),
		Voter:          voter,
		Depositor:      depositor,
		Pagination: &query.PageRequest{
			Key: args[3].([]byte),
		},
	}
	resp, err := p.govQuerier.Proposals(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, err
	}
	proposals := make([]ProposalData, len(resp.Proposals))
	for i, proposal := range resp.Proposals {
		converted, err := p.convertProposal(proposal)
		if err != nil {
			return nil, err
		}
		proposals[i] = converted
	}
	var nextKey []byte
	if resp.Pagination != nil {
		nextKey = resp.Pagination.NextKey
	}
	return method.Outputs.Pack(proposals, nextKey)
}

func (p PrecompileExecutor) voteQuery(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	voter, err := pcommon.GetSeiAddressFromArg(ctx, args[1], p.evmKeeper)
	if err != nil {
		return nil, err
	}
	req := &govtypes.QueryVoteRequest{
		ProposalId: args[0].(uint64),
		Voter:      voter.String(),
	}
	resp, err := p.govQuerier.Vote(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(convertVote(resp.Vote))
}

func (p PrecompileExecutor) votesQuery(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	req := &govtypes.QueryVotesRequest{
		ProposalId: args[0].(uint64),
		Pagination: &query.PageRequest{
			Key: args[1].([]byte),
		},
	}
	resp, err := p.govQuerier.Votes(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, err
	}
	votes := make([]VoteData, len(resp.Votes))
	for i, vote := range resp.Votes {
		votes[i] = convertVote(vote)
	}
	var nextKey []byte
	if resp.Pagination != nil {
		nextKey = resp.Pagination.NextKey
	}
	return method.Outputs.Pack(votes, nextKey)
}

func (p PrecompileExecutor) paramsQuery(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, err
	}
	goCtx := sdk.WrapSDKContext(ctx)
	votingResp, err := p.govQuerier.Params(goCtx, &govtypes.QueryParamsRequest{ParamsType: govtypes.ParamVoting})
	if err != nil {
		return nil, err
	}
	depositResp, err := p.govQuerier.Params(goCtx, &govtypes.QueryParamsRequest{ParamsType: govtypes.ParamDeposit})
	if err != nil {
		return nil, err
	}
	tallyResp, err := p.govQuerier.Params(goCtx, &govtypes.QueryParamsRequest{ParamsType: govtypes.ParamTallying})
	if err != nil {
		return nil, err
	}
	params := GovParams{
		VotingPeriod:          uint64(votingResp.VotingParams.VotingPeriod.Seconds()),
		ExpeditedVotingPeriod: uint64(votingResp.VotingParams.ExpeditedVotingPeriod.Seconds()),
		MinDeposit:            convertCoins(depositResp.DepositParams.MinDeposit),
		MaxDepositPeriod:      uint64(depositResp.DepositParams.MaxDepositPeriod.Seconds()),
		MinExpeditedDeposit:   convertCoins(depositResp.DepositParams.MinExpeditedDeposit),
		Quorum:                tallyResp.TallyParams.Quorum.String(),
		Threshold:             tallyResp.TallyParams.Threshold.String(),
		VetoThreshold:         tallyResp.TallyParams.VetoThreshold.String(),
		ExpeditedQuorum:       tallyResp.TallyParams.ExpeditedQuorum.String(),
		ExpeditedThreshold:    tallyResp.TallyParams.ExpeditedThreshold.String(),
	}
	return method.Outputs.Pack(params)
}

func (p PrecompileExecutor) depositQuery(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	depositor, err := pcommon.GetSeiAddressFromArg(ctx, args[1], p.evmKeeper)
	if err != nil {
		return nil, err
	}
	req := &govtypes.QueryDepositRequest{
		ProposalId: args[0].(uint64),
		Depositor:  depositor.String(),
	}
	resp, err := p.govQuerier.Deposit(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(convertDeposit(resp.Deposit))
}

func (p PrecompileExecutor) depositsQuery(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	req := &govtypes.QueryDepositsRequest{
		ProposalId: args[0].(uint64),
		Pagination: &query.PageRequest{
			Key: args[1].([]byte),
		},
	}
	resp, err := p.govQuerier.Deposits(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, err
	}
	deposits := make([]DepositData, len(resp.Deposits))
	for i, deposit := range resp.Deposits {
		deposits[i] = convertDeposit(deposit)
	}
	var nextKey []byte
	if resp.Pagination != nil {
		nextKey = resp.Pagination.NextKey
	}
	return method.Outputs.Pack(deposits, nextKey)
}

func (p PrecompileExecutor) tallyResultQuery(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	req := &govtypes.QueryTallyResultRequest{ProposalId: args[0].(uint64)}
	resp, err := p.govQuerier.TallyResult(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(convertTallyResult(resp.Tally))
}

// optionalBech32FromArg resolves an EVM address argument to its associated Sei
// address in bech32 form. The zero address means "no filter" and resolves to
// an empty string.
func (p PrecompileExecutor) optionalBech32FromArg(ctx sdk.Context, arg interface{}) (string, error) {
	if arg.(common.Address) == (common.Address{}) {
		return "", nil
	}
	seiAddr, err := pcommon.GetSeiAddressFromArg(ctx, arg, p.evmKeeper)
	if err != nil {
		return "", err
	}
	return seiAddr.String(), nil
}

func (p PrecompileExecutor) convertProposal(proposal govtypes.Proposal) (ProposalData, error) {
	content := []byte{}
	if proposal.Content != nil {
		bz, err := p.cdc.MarshalAsJSON(proposal.Content)
		if err != nil {
			return ProposalData{}, err
		}
		content = bz
	}
	return ProposalData{
		Id:               proposal.ProposalId,
		Status:           int32(proposal.Status),
		FinalTallyResult: convertTallyResult(proposal.FinalTallyResult),
		SubmitTime:       proposal.SubmitTime.Unix(),
		DepositEndTime:   proposal.DepositEndTime.Unix(),
		TotalDeposit:     convertCoins(proposal.TotalDeposit),
		VotingStartTime:  proposal.VotingStartTime.Unix(),
		VotingEndTime:    proposal.VotingEndTime.Unix(),
		IsExpedited:      proposal.IsExpedited,
		Content:          content,
	}, nil
}

func convertTallyResult(tally govtypes.TallyResult) TallyResultData {
	return TallyResultData{
		Yes:        tally.Yes.String(),
		Abstain:    tally.Abstain.String(),
		No:         tally.No.String(),
		NoWithVeto: tally.NoWithVeto.String(),
	}
}

func convertVote(vote govtypes.Vote) VoteData {
	options := make([]WeightedVoteOptionData, len(vote.Options))
	for i, option := range vote.Options {
		options[i] = WeightedVoteOptionData{
			Option: int32(option.Option),
			Weight: option.Weight.String(),
		}
	}
	return VoteData{
		ProposalId: vote.ProposalId,
		Voter:      vote.Voter,
		Options:    options,
	}
}

func convertDeposit(deposit govtypes.Deposit) DepositData {
	return DepositData{
		ProposalId: deposit.ProposalId,
		Depositor:  deposit.Depositor,
		Amount:     convertCoins(deposit.Amount),
	}
}

func convertCoins(coins sdk.Coins) []Coin {
	converted := make([]Coin, len(coins))
	for i, coin := range coins {
		converted[i] = Coin{
			Amount: coin.Amount.BigInt(),
			Denom:  coin.Denom,
		}
	}
	return converted
}
