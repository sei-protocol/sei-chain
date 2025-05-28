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
	GetSeiAddress(ctx sdk.Context, evmAddr common.Address) (sdk.AccAddress, bool)
}

// The Proposal represents the structure for proposal JSON input
type Proposal struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
	IsExpedited bool   `json:"is_expedited,omitempty"`
	Deposit     string `json:"deposit,omitempty"`
	// Optional fields for specific proposal types
	Plan               *SoftwareUpgradePlan `json:"plan,omitempty"`
	CommunityPoolSpend *CommunityPoolSpend  `json:"community_pool_spend,omitempty"`
	Changes            []Change             `json:"changes,omitempty"` // For parameter changes and other generic changes
}

// SoftwareUpgradePlan represents the plan for a software upgrade proposal
type SoftwareUpgradePlan struct {
	Name   string `json:"name"`
	Height int64  `json:"height"`
	Info   string `json:"info,omitempty"`
}

// CommunityPoolSpend represents the parameters for a community pool spend proposal
type CommunityPoolSpend struct {
	Recipient string `json:"recipient"` // Ethereum address of the recipient
	Amount    string `json:"amount"`    // Amount in the format "1000000usei"
}

type Change struct {
	Subspace string      `json:"subspace"`
	Key      string      `json:"key"`
	Value    interface{} `json:"value"`
}

// ResourceDependencyMapping defines the structure for mapping a resource to its dependencies
// and access control settings
type ResourceDependencyMapping struct {
	Resource string `json:"resource"`

	// Dependencies is a list of message keys that this resource depends on
	// Each dependency will be mapped to a MessageDependencyMapping
	Dependencies []string `json:"dependencies"`

	// AccessOps defines the sequence of access operations required for this resource
	// Must end with AccessType_COMMIT
	// Examples:
	// - [UNKNOWN, COMMIT] for synchronous operations
	// - [READ, COMMIT] for read-only operations
	AccessOps []AccessOperation `json:"access_ops"`

	// DynamicEnabled determines if dynamic access control is enabled for this mapping
	// When true, allows for runtime modification of access control rules
	DynamicEnabled bool `json:"dynamic_enabled"`
}

// AccessOperation defines a single access control operation
type AccessOperation struct {
	// ResourceType specifies the type of resource being accessed
	// Examples:
	// - ANY: Any resource type
	// - KV_STORE: Key-value store
	// - BANK: Bank operations
	ResourceType string `json:"resource_type"`

	// AccessType specifies the type of access being performed
	// Examples:
	// - READ: Read access
	// - WRITE: Write access
	// - COMMIT: Commit access (must be the last operation)
	AccessType string `json:"access_type"`

	// IdentifierTemplate specifies the template for resource identification
	// Examples:
	// - "*" for any identifier
	// - "account/{address}" for specific account
	IdentifierTemplate string `json:"identifier_template"`
}

// ProposalHandler defines an interface for handling different proposal types. ProposalHandler implementations to be
// added and registered in below
type ProposalHandler interface {
	// HandleProposal creates a Content object from the proposal input
	HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error)
	// Type returns the proposal type this handler can process
	Type() string
}

type TextProposalHandler struct{}

func (h TextProposalHandler) HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error) {
	return govtypes.NewTextProposal(proposal.Title, proposal.Description, proposal.IsExpedited), nil
}

func (h TextProposalHandler) Type() string {
	return govtypes.ProposalTypeText
}

type ParameterChangeProposalHandler struct{}

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

func (h ParameterChangeProposalHandler) Type() string {
	return paramstypes.ProposalTypeChange
}

type SoftwareUpgradeProposalHandler struct{}

func (h SoftwareUpgradeProposalHandler) HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error) {
	if proposal.Plan == nil {
		return nil, errors.New("upgrade plan must be specified")
	}

	if proposal.Plan.Height == 0 {
		return nil, errors.New("upgrade height must be specified")
	}
	if proposal.Plan.Name == "" {
		return nil, errors.New("upgrade name must be specified")
	}

	return upgradetypes.NewSoftwareUpgradeProposal(
		proposal.Title,
		proposal.Description,
		upgradetypes.Plan{
			Name:   proposal.Plan.Name,
			Height: proposal.Plan.Height,
			Info:   proposal.Plan.Info,
		},
	), nil
}

func (h SoftwareUpgradeProposalHandler) Type() string {
	return upgradetypes.ProposalTypeSoftwareUpgrade
}

type CancelSoftwareUpgradeProposalHandler struct{}

func (h CancelSoftwareUpgradeProposalHandler) HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error) {
	// Cancel software upgrade proposals don't need any additional parameters
	// They just need title and description which are already validated in createProposalContent
	return upgradetypes.NewCancelSoftwareUpgradeProposal(
		proposal.Title,
		proposal.Description,
	), nil
}

func (h CancelSoftwareUpgradeProposalHandler) Type() string {
	return upgradetypes.ProposalTypeCancelSoftwareUpgrade
}

type CommunityPoolSpendProposalHandler struct {
	evmKeeper EVMKeeper
}

func (h CommunityPoolSpendProposalHandler) HandleProposal(ctx sdk.Context, proposal Proposal) (govtypes.Content, error) {
	if proposal.CommunityPoolSpend == nil {
		return nil, errors.New("community pool spend parameters must be specified")
	}

	// Validate that the recipient is a valid Ethereum address
	if !common.IsHexAddress(proposal.CommunityPoolSpend.Recipient) {
		return nil, fmt.Errorf("invalid ethereum address format")
	}

	// Parse the amount
	amount, err := sdk.ParseCoinsNormalized(proposal.CommunityPoolSpend.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount format: %w", err)
	}

	if amount.IsZero() {
		return nil, errors.New("amount must be greater than zero")
	}

	// Convert Ethereum address to Sei address using the EVM keeper
	ethAddr := common.HexToAddress(proposal.CommunityPoolSpend.Recipient)
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

func (h CommunityPoolSpendProposalHandler) Type() string {
	return distrtypes.ProposalTypeCommunityPoolSpend
}

type UpdateResourceDependencyMappingProposalHandler struct {
	evmKeeper EVMKeeper
}

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
