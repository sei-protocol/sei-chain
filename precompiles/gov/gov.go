package gov

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	VoteMethod           = "vote"
	DepositMethod        = "deposit"
	SubmitProposalMethod = "submitProposal"
)

const (
	GovAddress = "0x0000000000000000000000000000000000001006"
)

// Proposal represents the structure for proposal JSON input
type Proposal struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
	IsExpedited bool   `json:"is_expedited,omitempty"`
	Deposit     string `json:"deposit,omitempty"`
}

// ProposalHandler defines an interface for handling different proposal types
type ProposalHandler interface {
	// HandleProposal creates a Content object from the proposal input
	HandleProposal(proposal Proposal) (govtypes.Content, error)
	// Type returns the proposal type this handler can process
	Type() string
}

// TextProposalHandler handles basic text proposals
type TextProposalHandler struct{}

// HandleProposal implements ProposalHandler
func (h TextProposalHandler) HandleProposal(proposal Proposal) (govtypes.Content, error) {
	return govtypes.NewTextProposal(proposal.Title, proposal.Description, proposal.IsExpedited), nil
}

// Type implements ProposalHandler
func (h TextProposalHandler) Type() string {
	return govtypes.ProposalTypeText
}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	govKeeper        pcommon.GovKeeper
	govMsgServer     pcommon.GovMsgServer
	evmKeeper        pcommon.EVMKeeper
	bankKeeper       pcommon.BankKeeper
	address          common.Address
	proposalHandlers map[string]ProposalHandler

	VoteID           []byte
	DepositID        []byte
	SubmitProposalID []byte
}

func NewPrecompile(govKeeper pcommon.GovKeeper, govMsgServer pcommon.GovMsgServer, evmKeeper pcommon.EVMKeeper, bankKeeper pcommon.BankKeeper) (*pcommon.Precompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		govKeeper:    govKeeper,
		govMsgServer: govMsgServer,
		evmKeeper:    evmKeeper,
		bankKeeper:   bankKeeper,
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
		}
	}

	// Create the precompile
	return pcommon.NewPrecompile(newAbi, p, p.address, "gov"), nil
}

// registerProposalHandlers registers all available proposal handlers
func (p *PrecompileExecutor) registerProposalHandlers() {
	p.proposalHandlers = make(map[string]ProposalHandler)

	// Register the TextProposalHandler
	textHandler := TextProposalHandler{}
	p.proposalHandlers[textHandler.Type()] = textHandler
	// Default handler for empty type
	p.proposalHandlers[""] = textHandler

	// Additional handlers can be registered here
}

// getProposalHandler returns the appropriate handler for a proposal type
func (p *PrecompileExecutor) getProposalHandler(proposalType string) (ProposalHandler, error) {
	handler, ok := p.proposalHandlers[proposalType]
	if !ok {
		return nil, fmt.Errorf("unsupported proposal type: %s", proposalType)
	}
	return handler, nil
}

// createProposalContent creates the appropriate content for a proposal based on its type
func (p PrecompileExecutor) createProposalContent(proposal Proposal) (govtypes.Content, error) {
	// Validate required fields
	if proposal.Title == "" {
		return nil, errors.New("proposal title cannot be empty")
	}
	if proposal.Description == "" {
		return nil, errors.New("proposal description cannot be empty")
	}

	// Get the appropriate handler for this proposal type
	handler, err := p.getProposalHandler(proposal.Type)
	if err != nil {
		// For unsupported types, provide more specific error messages
		switch proposal.Type {
		case "ParameterChange":
			return nil, fmt.Errorf("parameter change proposals are not supported yet via precompile")
		case "SoftwareUpgrade":
			return nil, fmt.Errorf("software upgrade proposals are not supported yet via precompile")
		case "CancelSoftwareUpgrade":
			return nil, fmt.Errorf("cancel software upgrade proposals are not supported yet via precompile")
		case "CommunityPoolSpend":
			return nil, fmt.Errorf("community pool spend proposals are not supported yet via precompile")
		case "UpdateResourceDependencyMapping":
			return nil, fmt.Errorf("update resource dependency mapping proposals are not supported yet via precompile")
		case "UpdateWasmDependencyMapping":
			return nil, fmt.Errorf("update wasm dependency mapping proposals are not supported yet via precompile")
		// WASM module proposal types
		case "StoreCode", "InstantiateContract", "MigrateContract", "SudoContract",
			"ExecuteContract", "UpdateAdmin", "ClearAdmin", "PinCodes", "UnpinCodes",
			"UpdateInstantiateConfig":
			return nil, fmt.Errorf("%s proposals are not supported yet via precompile", proposal.Type)
		// IBC module proposal types
		case "ClientUpdate", "IBCUpgrade":
			return nil, fmt.Errorf("%s proposals are not supported yet via precompile", proposal.Type)
		default:
			return nil, err
		}
	}

	// Use the handler to create the appropriate content
	return handler.HandleProposal(proposal)
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	if bytes.Equal(method.ID, p.VoteID) {
		return 30000
	} else if bytes.Equal(method.ID, p.DepositID) {
		return 30000
	} else if bytes.Equal(method.ID, p.SubmitProposalID) {
		return 50000
	}

	// This should never happen since this is going to fail during Run
	return pcommon.UnknownMethodCallGas
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) (bz []byte, err error) {
	if readOnly {
		return nil, errors.New("cannot call gov precompile from staticcall")
	}
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, errors.New("cannot delegatecall gov")
	}

	switch method.Name {
	case VoteMethod:
		return p.vote(ctx, method, caller, args, value)
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
	err := p.govKeeper.AddVote(ctx, proposalID, voter, govtypes.NewNonSplitVoteOption(govtypes.VoteOption(voteOption)))
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
	res, err := p.govKeeper.AddDeposit(ctx, proposalID, depositor, sdk.NewCoins(coin))
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(res)
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

	// Create the proposal content using the handler system
	content, err := p.createProposalContent(proposal)
	if err != nil {
		return nil, err
	}

	// Initialize deposit amount
	var initialDeposit sdk.Coins
	if value != nil && value.Sign() > 0 {
		// Convert the value (in wei) to usei
		coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), proposer, value, p.bankKeeper, p.evmKeeper, hooks, evm.GetDepth())
		if err != nil {
			return nil, err
		}
		initialDeposit = sdk.NewCoins(coin)
	}

	// Create the MsgSubmitProposal
	msg, err := govtypes.NewMsgSubmitProposalWithExpedite(content, initialDeposit, proposer, proposal.IsExpedited)
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
