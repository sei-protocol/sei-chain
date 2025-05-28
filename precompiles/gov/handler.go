package gov

import (
	"encoding/json"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/ethereum/go-ethereum/common"
)

// EVMKeeper defines the interface for EVM keeper operations
type EVMKeeper interface {
	GetSeiAddress(ctx sdk.Context, ethAddr common.Address) (sdk.AccAddress, bool)
}

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
	HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error)
	// Type returns the proposal type this handler can process
	Type() string
}

// TextProposalHandler handles basic text proposals
type TextProposalHandler struct{}

// HandleProposal implements ProposalHandler
func (h TextProposalHandler) HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error) {
	return govtypes.NewTextProposal(proposal.Title, proposal.Description, proposal.IsExpedited), nil
}

// Type implements ProposalHandler
func (h TextProposalHandler) Type() string {
	return govtypes.ProposalTypeText
}

// ParameterChangeProposalHandler handles parameter change proposals
type ParameterChangeProposalHandler struct{}

// HandleProposal implements ProposalHandler
func (h ParameterChangeProposalHandler) HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error) {
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

// SoftwareUpgradeProposalHandler handles software upgrade proposals
type SoftwareUpgradeProposalHandler struct{}

// HandleProposal implements ProposalHandler
func (h SoftwareUpgradeProposalHandler) HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error) {
	if len(proposal.Changes) == 0 {
		return nil, errors.New("at least one upgrade change must be specified")
	}

	// Get the upgrade height from changes
	var height int64
	var name string
	var info string
	for _, change := range proposal.Changes {
		switch change.Key {
		case "height":
			heightFloat, ok := change.Value.(float64)
			if !ok {
				return nil, fmt.Errorf("height must be a number")
			}
			height = int64(heightFloat)
		case "name":
			nameStr, ok := change.Value.(string)
			if !ok {
				return nil, fmt.Errorf("name must be a string")
			}
			name = nameStr
		case "info":
			infoStr, ok := change.Value.(string)
			if !ok {
				return nil, fmt.Errorf("info must be a string")
			}
			info = infoStr
		}
	}

	if height == 0 {
		return nil, errors.New("upgrade height must be specified")
	}
	if name == "" {
		return nil, errors.New("upgrade name must be specified")
	}

	return upgradetypes.NewSoftwareUpgradeProposal(
		proposal.Title,
		proposal.Description,
		upgradetypes.Plan{
			Name:   name,
			Height: height,
			Info:   info,
		},
	), nil
}

// Type implements ProposalHandler
func (h SoftwareUpgradeProposalHandler) Type() string {
	return upgradetypes.ProposalTypeSoftwareUpgrade
}

// CancelSoftwareUpgradeProposalHandler handles cancel software upgrade proposals
type CancelSoftwareUpgradeProposalHandler struct{}

// HandleProposal implements ProposalHandler
func (h CancelSoftwareUpgradeProposalHandler) HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error) {
	// Cancel software upgrade proposals don't need any additional parameters
	// They just need title and description which are already validated in createProposalContent
	return upgradetypes.NewCancelSoftwareUpgradeProposal(
		proposal.Title,
		proposal.Description,
	), nil
}

// Type implements ProposalHandler
func (h CancelSoftwareUpgradeProposalHandler) Type() string {
	return upgradetypes.ProposalTypeCancelSoftwareUpgrade
}

// CommunityPoolSpendProposalHandler handles community pool spend proposals
type CommunityPoolSpendProposalHandler struct {
	evmKeeper EVMKeeper
}

// HandleProposal implements ProposalHandler
func (h CommunityPoolSpendProposalHandler) HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error) {
	if len(proposal.Changes) == 0 {
		return nil, errors.New("at least one spend change must be specified")
	}

	// Get the recipient and amount from changes
	var recipient string
	var amount sdk.Coins
	for _, change := range proposal.Changes {
		switch change.Key {
		case "recipient":
			recipientStr, ok := change.Value.(string)
			if !ok {
				return nil, fmt.Errorf("recipient must be a string")
			}
			// Validate that the recipient is a valid Ethereum address
			if !common.IsHexAddress(recipientStr) {
				return nil, fmt.Errorf("invalid ethereum address format")
			}
			recipient = recipientStr
		case "amount":
			amountStr, ok := change.Value.(string)
			if !ok {
				return nil, fmt.Errorf("amount must be a string")
			}
			var err error
			amount, err = sdk.ParseCoinsNormalized(amountStr)
			if err != nil {
				return nil, fmt.Errorf("invalid amount format: %w", err)
			}
		}
	}

	if recipient == "" {
		return nil, errors.New("recipient address must be specified")
	}
	if amount.IsZero() {
		return nil, errors.New("amount must be greater than zero")
	}

	// Convert Ethereum address to Sei address using the EVM keeper
	ethAddr := common.HexToAddress(recipient)
	seiAddr, found := h.evmKeeper.GetSeiAddress(ctx, ethAddr)
	if !found {
		return nil, fmt.Errorf("no sei address found for ethereum address %s", ethAddr.Hex())
	}

	return distrtypes.NewCommunityPoolSpendProposal(
		proposal.Title,
		proposal.Description,
		seiAddr,
		amount,
	), nil
}

// Type implements ProposalHandler
func (h CommunityPoolSpendProposalHandler) Type() string {
	return distrtypes.ProposalTypeCommunityPoolSpend
}

// UpdateResourceDependencyMappingProposalHandler handles resource dependency mapping proposals
type UpdateResourceDependencyMappingProposalHandler struct {
	evmKeeper EVMKeeper
}

// HandleProposal implements ProposalHandler
func (h UpdateResourceDependencyMappingProposalHandler) HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error) {
	if len(proposal.Changes) == 0 {
		return nil, errors.New("at least one resource dependency mapping must be specified")
	}

	// Get the resource and dependencies from changes
	var resource string
	var dependencies []string
	for _, change := range proposal.Changes {
		switch change.Key {
		case "resource":
			resourceStr, ok := change.Value.(string)
			if !ok {
				return nil, fmt.Errorf("resource must be a string")
			}
			resource = resourceStr
		case "dependencies":
			deps, ok := change.Value.([]interface{})
			if !ok {
				return nil, fmt.Errorf("dependencies must be an array")
			}
			dependencies = make([]string, len(deps))
			for i, dep := range deps {
				depStr, ok := dep.(string)
				if !ok {
					return nil, fmt.Errorf("dependency must be a string")
				}
				dependencies[i] = depStr
			}
		}
	}

	if resource == "" {
		return nil, errors.New("resource must be specified")
	}
	if len(dependencies) == 0 {
		return nil, errors.New("at least one dependency must be specified")
	}

	// Build a MessageDependencyMapping for the resource and dependencies
	// For demonstration, use SynchronousMessageDependencyMapping for each dependency
	mappings := make([]accesscontrol.MessageDependencyMapping, len(dependencies))
	for i, dep := range dependencies {
		mappings[i] = acltypes.SynchronousMessageDependencyMapping(acltypes.MessageKey(dep))
	}

	return acltypes.NewMsgUpdateResourceDependencyMappingProposal(
		proposal.Title,
		proposal.Description,
		mappings,
	), nil
}

// Type implements ProposalHandler
func (h UpdateResourceDependencyMappingProposalHandler) Type() string {
	return acltypes.ProposalUpdateResourceDependencyMapping
}

// RegisterProposalHandlers registers all available proposal handlers
func RegisterProposalHandlers(evmKeeper EVMKeeper) map[string]ProposalHandler {
	proposalHandlers := make(map[string]ProposalHandler)

	// Register the TextProposalHandler
	textHandler := TextProposalHandler{}
	proposalHandlers[textHandler.Type()] = textHandler
	// Default handler for empty type
	proposalHandlers[""] = textHandler

	// Register the ParameterChangeProposalHandler
	paramHandler := ParameterChangeProposalHandler{}
	proposalHandlers[paramHandler.Type()] = paramHandler

	// Register the SoftwareUpgradeProposalHandler
	upgradeHandler := SoftwareUpgradeProposalHandler{}
	proposalHandlers[upgradeHandler.Type()] = upgradeHandler

	// Register the CancelSoftwareUpgradeProposalHandler
	cancelUpgradeHandler := CancelSoftwareUpgradeProposalHandler{}
	proposalHandlers[cancelUpgradeHandler.Type()] = cancelUpgradeHandler

	// Register the CommunityPoolSpendProposalHandler
	communityPoolSpendHandler := CommunityPoolSpendProposalHandler{evmKeeper: evmKeeper}
	proposalHandlers[communityPoolSpendHandler.Type()] = communityPoolSpendHandler

	// Register the UpdateResourceDependencyMappingProposalHandler
	resourceDependencyHandler := UpdateResourceDependencyMappingProposalHandler{evmKeeper: evmKeeper}
	proposalHandlers[resourceDependencyHandler.Type()] = resourceDependencyHandler

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
func (p PrecompileExecutor) createProposalContent(ctx sdk.Context, proposal Proposal) (govtypes.Content, error) {
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
		// WASM module proposal types
		case "UpdateWasmDependencyMapping", "StoreCode", "InstantiateContract", "MigrateContract", "SudoContract",
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
	return handler.HandleProposal(ctx, proposal)
}

// registerProposalHandlers registers all available proposal handlers
func (p *PrecompileExecutor) registerProposalHandlers() {
	p.proposalHandlers = RegisterProposalHandlers(p.evmKeeper)
}
