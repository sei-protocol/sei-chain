package gov

import (
	"errors"
	"fmt"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types/proposal"
)

// The Proposal represents the structure for proposal JSON input
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

type ParameterChangeProposalHandler struct{}

func (h ParameterChangeProposalHandler) HandleProposal(proposal Proposal) (govtypes.Content, error) {
	return paramstypes.NewParameterChangeProposal(
		proposal.Title,
		proposal.Description,
		[]paramstypes.ParamChange{},
		proposal.IsExpedited), nil
}

func (h ParameterChangeProposalHandler) Type() string {
	return paramstypes.ProposalTypeChange
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
