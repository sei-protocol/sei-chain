package gov

import (
	"encoding/json"
	"errors"
	"fmt"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types/proposal"
)

// Proposal represents the structure for proposal JSON input
type Proposal struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	IsExpedited bool     `json:"is_expedited,omitempty"`
	Deposit     string   `json:"deposit,omitempty"`
	Changes     []Change `json:"changes,omitempty"`
}

type Change struct {
	Subspace string      `json:"subspace"`
	Key      string      `json:"key"`
	Value    interface{} `json:"value"`
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

// ParameterChangeProposalHandler handles parameter change proposals
type ParameterChangeProposalHandler struct{}

// HandleProposal implements ProposalHandler
func (h ParameterChangeProposalHandler) HandleProposal(proposal Proposal) (govtypes.Content, error) {
	if len(proposal.Changes) == 0 {
		return nil, errors.New("at least one parameter change must be specified")
	}

	// Convert changes to ParamChange array
	changes := make([]paramstypes.ParamChange, len(proposal.Changes))
	for i, change := range proposal.Changes {
		// Convert value to string - this is what ParamChange expects
		valueBytes, err := json.Marshal(change.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal parameter value: %w", err)
		}
		changes[i] = paramstypes.ParamChange{
			Subspace: change.Subspace,
			Key:      change.Key,
			Value:    string(valueBytes),
		}
	}

	return paramstypes.NewParameterChangeProposal(
		proposal.Title,
		proposal.Description,
		changes,
		proposal.IsExpedited,
	), nil
}

// Type implements ProposalHandler
func (h ParameterChangeProposalHandler) Type() string {
	return paramstypes.ProposalTypeChange
}

// RegisterProposalHandlers registers all available proposal handlers
func RegisterProposalHandlers() map[string]ProposalHandler {
	proposalHandlers := make(map[string]ProposalHandler)

	// Register the TextProposalHandler
	textHandler := TextProposalHandler{}
	proposalHandlers[textHandler.Type()] = textHandler
	// Default handler for empty type
	proposalHandlers[""] = textHandler

	// Register the ParameterChangeProposalHandler
	paramHandler := ParameterChangeProposalHandler{}
	proposalHandlers[paramHandler.Type()] = paramHandler

	// Additional handlers can be registered here

	return proposalHandlers
}

// GetProposalHandler returns the appropriate handler for a proposal type
func GetProposalHandler(handlers map[string]ProposalHandler, proposalType string) (ProposalHandler, error) {
	handler, ok := handlers[proposalType]
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
	handler, err := GetProposalHandler(p.proposalHandlers, proposal.Type)
	if err != nil {
		// For unsupported types, provide more specific error messages
		switch proposal.Type {
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
	p.proposalHandlers = RegisterProposalHandlers()
}
