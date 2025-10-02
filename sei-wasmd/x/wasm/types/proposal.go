package types

import (
	"encoding/base64"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type ProposalType string

const (
	ProposalTypeStoreCode               ProposalType = "StoreCode"
	ProposalTypeInstantiateContract     ProposalType = "InstantiateContract"
	ProposalTypeMigrateContract         ProposalType = "MigrateContract"
	ProposalTypeSudoContract            ProposalType = "SudoContract"
	ProposalTypeExecuteContract         ProposalType = "ExecuteContract"
	ProposalTypeUpdateAdmin             ProposalType = "UpdateAdmin"
	ProposalTypeClearAdmin              ProposalType = "ClearAdmin"
	ProposalTypePinCodes                ProposalType = "PinCodes"
	ProposalTypeUnpinCodes              ProposalType = "UnpinCodes"
	ProposalTypeUpdateInstantiateConfig ProposalType = "UpdateInstantiateConfig"
)

// DisableAllProposals contains no wasm gov types.
var DisableAllProposals []ProposalType

// EnableAllProposals contains all wasm gov types as keys.
var EnableAllProposals = []ProposalType{
	ProposalTypeStoreCode,
	ProposalTypeInstantiateContract,
	ProposalTypeMigrateContract,
	ProposalTypeSudoContract,
	ProposalTypeExecuteContract,
	ProposalTypeUpdateAdmin,
	ProposalTypeClearAdmin,
	ProposalTypePinCodes,
	ProposalTypeUnpinCodes,
	ProposalTypeUpdateInstantiateConfig,
}

// ConvertToProposals maps each key to a ProposalType and returns a typed list.
// If any string is not a valid type (in this file), then return an error
func ConvertToProposals(keys []string) ([]ProposalType, error) {
	valid := make(map[string]bool, len(EnableAllProposals))
	for _, key := range EnableAllProposals {
		valid[string(key)] = true
	}

	proposals := make([]ProposalType, len(keys))
	for i, key := range keys {
		if _, ok := valid[key]; !ok {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "'%s' is not a valid ProposalType", key)
		}
		proposals[i] = ProposalType(key)
	}
	return proposals, nil
}

func init() { // register new content types with the sdk
	govtypes.RegisterProposalType(string(ProposalTypeStoreCode))
	govtypes.RegisterProposalType(string(ProposalTypeInstantiateContract))
	govtypes.RegisterProposalType(string(ProposalTypeMigrateContract))
	govtypes.RegisterProposalType(string(ProposalTypeSudoContract))
	govtypes.RegisterProposalType(string(ProposalTypeExecuteContract))
	govtypes.RegisterProposalType(string(ProposalTypeUpdateAdmin))
	govtypes.RegisterProposalType(string(ProposalTypeClearAdmin))
	govtypes.RegisterProposalType(string(ProposalTypePinCodes))
	govtypes.RegisterProposalType(string(ProposalTypeUnpinCodes))
	govtypes.RegisterProposalType(string(ProposalTypeUpdateInstantiateConfig))
	govtypes.RegisterProposalTypeCodec(&StoreCodeProposal{}, "wasm/StoreCodeProposal")
	govtypes.RegisterProposalTypeCodec(&InstantiateContractProposal{}, "wasm/InstantiateContractProposal")
	govtypes.RegisterProposalTypeCodec(&MigrateContractProposal{}, "wasm/MigrateContractProposal")
	govtypes.RegisterProposalTypeCodec(&SudoContractProposal{}, "wasm/SudoContractProposal")
	govtypes.RegisterProposalTypeCodec(&ExecuteContractProposal{}, "wasm/ExecuteContractProposal")
	govtypes.RegisterProposalTypeCodec(&UpdateAdminProposal{}, "wasm/UpdateAdminProposal")
	govtypes.RegisterProposalTypeCodec(&ClearAdminProposal{}, "wasm/ClearAdminProposal")
	govtypes.RegisterProposalTypeCodec(&PinCodesProposal{}, "wasm/PinCodesProposal")
	govtypes.RegisterProposalTypeCodec(&UnpinCodesProposal{}, "wasm/UnpinCodesProposal")
	govtypes.RegisterProposalTypeCodec(&UpdateInstantiateConfigProposal{}, "wasm/UpdateInstantiateConfigProposal")
}

// ProposalRoute returns the routing key of a parameter change proposal.
func (p StoreCodeProposal) ProposalRoute() string { return RouterKey }

// GetTitle returns the title of the proposal
func (p *StoreCodeProposal) GetTitle() string { return p.Title }

// GetDescription returns the human readable description of the proposal
func (p StoreCodeProposal) GetDescription() string { return p.Description }

// ProposalType returns the type
func (p StoreCodeProposal) ProposalType() string { return string(ProposalTypeStoreCode) }

// ValidateBasic validates the proposal
func (p StoreCodeProposal) ValidateBasic() error {
	if err := validateProposalCommons(p.Title, p.Description); err != nil {
		return err
	}
	if _, err := sdk.AccAddressFromBech32(p.RunAs); err != nil {
		return sdkerrors.Wrap(err, "run as")
	}

	if err := validateWasmCode(p.WASMByteCode); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "code bytes %s", err.Error())
	}

	if p.InstantiatePermission != nil {
		if err := p.InstantiatePermission.ValidateBasic(); err != nil {
			return sdkerrors.Wrap(err, "instantiate permission")
		}
	}
	return nil
}

// String implements the Stringer interface.
func (p StoreCodeProposal) String() string {
	return fmt.Sprintf(`Store Code Proposal:
  Title:       %s
  Description: %s
  Run as:      %s
  WasmCode:    %X
`, p.Title, p.Description, p.RunAs, p.WASMByteCode)
}

// MarshalYAML pretty prints the wasm byte code
func (p StoreCodeProposal) MarshalYAML() (interface{}, error) {
	return struct {
		Title                 string        `yaml:"title"`
		Description           string        `yaml:"description"`
		RunAs                 string        `yaml:"run_as"`
		WASMByteCode          string        `yaml:"wasm_byte_code"`
		InstantiatePermission *AccessConfig `yaml:"instantiate_permission"`
	}{
		Title:                 p.Title,
		Description:           p.Description,
		RunAs:                 p.RunAs,
		WASMByteCode:          base64.StdEncoding.EncodeToString(p.WASMByteCode),
		InstantiatePermission: p.InstantiatePermission,
	}, nil
}

// ProposalRoute returns the routing key of a parameter change proposal.
func (p InstantiateContractProposal) ProposalRoute() string { return RouterKey }

// GetTitle returns the title of the proposal
func (p *InstantiateContractProposal) GetTitle() string { return p.Title }

// GetDescription returns the human readable description of the proposal
func (p InstantiateContractProposal) GetDescription() string { return p.Description }

// ProposalType returns the type
func (p InstantiateContractProposal) ProposalType() string {
	return string(ProposalTypeInstantiateContract)
}

// ValidateBasic validates the proposal
func (p InstantiateContractProposal) ValidateBasic() error {
	if err := validateProposalCommons(p.Title, p.Description); err != nil {
		return err
	}
	if _, err := sdk.AccAddressFromBech32(p.RunAs); err != nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "run as")
	}

	if p.CodeID == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "code id is required")
	}

	if err := validateLabel(p.Label); err != nil {
		return err
	}

	if !p.Funds.IsValid() {
		return sdkerrors.ErrInvalidCoins
	}

	if len(p.Admin) != 0 {
		if _, err := sdk.AccAddressFromBech32(p.Admin); err != nil {
			return err
		}
	}
	if err := p.Msg.ValidateBasic(); err != nil {
		return sdkerrors.Wrap(err, "payload msg")
	}
	return nil
}

// String implements the Stringer interface.
func (p InstantiateContractProposal) String() string {
	return fmt.Sprintf(`Instantiate Code Proposal:
  Title:       %s
  Description: %s
  Run as:      %s
  Admin:       %s
  Code id:     %d
  Label:       %s
  Msg:         %q
  Funds:       %s
`, p.Title, p.Description, p.RunAs, p.Admin, p.CodeID, p.Label, p.Msg, p.Funds)
}

// MarshalYAML pretty prints the init message
func (p InstantiateContractProposal) MarshalYAML() (interface{}, error) {
	return struct {
		Title       string    `yaml:"title"`
		Description string    `yaml:"description"`
		RunAs       string    `yaml:"run_as"`
		Admin       string    `yaml:"admin"`
		CodeID      uint64    `yaml:"code_id"`
		Label       string    `yaml:"label"`
		Msg         string    `yaml:"msg"`
		Funds       sdk.Coins `yaml:"funds"`
	}{
		Title:       p.Title,
		Description: p.Description,
		RunAs:       p.RunAs,
		Admin:       p.Admin,
		CodeID:      p.CodeID,
		Label:       p.Label,
		Msg:         string(p.Msg),
		Funds:       p.Funds,
	}, nil
}

// ProposalRoute returns the routing key of a parameter change proposal.
func (p MigrateContractProposal) ProposalRoute() string { return RouterKey }

// GetTitle returns the title of the proposal
func (p *MigrateContractProposal) GetTitle() string { return p.Title }

// GetDescription returns the human readable description of the proposal
func (p MigrateContractProposal) GetDescription() string { return p.Description }

// ProposalType returns the type
func (p MigrateContractProposal) ProposalType() string { return string(ProposalTypeMigrateContract) }

// ValidateBasic validates the proposal
func (p MigrateContractProposal) ValidateBasic() error {
	if err := validateProposalCommons(p.Title, p.Description); err != nil {
		return err
	}
	if p.CodeID == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "code_id is required")
	}
	if _, err := sdk.AccAddressFromBech32(p.Contract); err != nil {
		return sdkerrors.Wrap(err, "contract")
	}
	if err := p.Msg.ValidateBasic(); err != nil {
		return sdkerrors.Wrap(err, "payload msg")
	}
	return nil
}

// String implements the Stringer interface.
func (p MigrateContractProposal) String() string {
	return fmt.Sprintf(`Migrate Contract Proposal:
  Title:       %s
  Description: %s
  Contract:    %s
  Code id:     %d
  Msg:         %q
`, p.Title, p.Description, p.Contract, p.CodeID, p.Msg)
}

// MarshalYAML pretty prints the migrate message
func (p MigrateContractProposal) MarshalYAML() (interface{}, error) {
	return struct {
		Title       string `yaml:"title"`
		Description string `yaml:"description"`
		Contract    string `yaml:"contract"`
		CodeID      uint64 `yaml:"code_id"`
		Msg         string `yaml:"msg"`
	}{
		Title:       p.Title,
		Description: p.Description,
		Contract:    p.Contract,
		CodeID:      p.CodeID,
		Msg:         string(p.Msg),
	}, nil
}

// ProposalRoute returns the routing key of a parameter change proposal.
func (p SudoContractProposal) ProposalRoute() string { return RouterKey }

// GetTitle returns the title of the proposal
func (p *SudoContractProposal) GetTitle() string { return p.Title }

// GetDescription returns the human readable description of the proposal
func (p SudoContractProposal) GetDescription() string { return p.Description }

// ProposalType returns the type
func (p SudoContractProposal) ProposalType() string { return string(ProposalTypeSudoContract) }

// ValidateBasic validates the proposal
func (p SudoContractProposal) ValidateBasic() error {
	if err := validateProposalCommons(p.Title, p.Description); err != nil {
		return err
	}
	if _, err := sdk.AccAddressFromBech32(p.Contract); err != nil {
		return sdkerrors.Wrap(err, "contract")
	}
	if err := p.Msg.ValidateBasic(); err != nil {
		return sdkerrors.Wrap(err, "payload msg")
	}
	return nil
}

// String implements the Stringer interface.
func (p SudoContractProposal) String() string {
	return fmt.Sprintf(`Migrate Contract Proposal:
  Title:       %s
  Description: %s
  Contract:    %s
  Msg:         %q
`, p.Title, p.Description, p.Contract, p.Msg)
}

// MarshalYAML pretty prints the migrate message
func (p SudoContractProposal) MarshalYAML() (interface{}, error) {
	return struct {
		Title       string `yaml:"title"`
		Description string `yaml:"description"`
		Contract    string `yaml:"contract"`
		Msg         string `yaml:"msg"`
	}{
		Title:       p.Title,
		Description: p.Description,
		Contract:    p.Contract,
		Msg:         string(p.Msg),
	}, nil
}

// ProposalRoute returns the routing key of a parameter change proposal.
func (p ExecuteContractProposal) ProposalRoute() string { return RouterKey }

// GetTitle returns the title of the proposal
func (p *ExecuteContractProposal) GetTitle() string { return p.Title }

// GetDescription returns the human readable description of the proposal
func (p ExecuteContractProposal) GetDescription() string { return p.Description }

// ProposalType returns the type
func (p ExecuteContractProposal) ProposalType() string { return string(ProposalTypeExecuteContract) }

// ValidateBasic validates the proposal
func (p ExecuteContractProposal) ValidateBasic() error {
	if err := validateProposalCommons(p.Title, p.Description); err != nil {
		return err
	}
	if _, err := sdk.AccAddressFromBech32(p.Contract); err != nil {
		return sdkerrors.Wrap(err, "contract")
	}
	if _, err := sdk.AccAddressFromBech32(p.RunAs); err != nil {
		return sdkerrors.Wrap(err, "run as")
	}
	if !p.Funds.IsValid() {
		return sdkerrors.ErrInvalidCoins
	}
	if err := p.Msg.ValidateBasic(); err != nil {
		return sdkerrors.Wrap(err, "payload msg")
	}
	return nil
}

// String implements the Stringer interface.
func (p ExecuteContractProposal) String() string {
	return fmt.Sprintf(`Migrate Contract Proposal:
  Title:       %s
  Description: %s
  Contract:    %s
  Run as:      %s
  Msg:         %q
  Funds:       %s
`, p.Title, p.Description, p.Contract, p.RunAs, p.Msg, p.Funds)
}

// MarshalYAML pretty prints the migrate message
func (p ExecuteContractProposal) MarshalYAML() (interface{}, error) {
	return struct {
		Title       string    `yaml:"title"`
		Description string    `yaml:"description"`
		Contract    string    `yaml:"contract"`
		Msg         string    `yaml:"msg"`
		RunAs       string    `yaml:"run_as"`
		Funds       sdk.Coins `yaml:"funds"`
	}{
		Title:       p.Title,
		Description: p.Description,
		Contract:    p.Contract,
		Msg:         string(p.Msg),
		RunAs:       p.RunAs,
		Funds:       p.Funds,
	}, nil
}

// ProposalRoute returns the routing key of a parameter change proposal.
func (p UpdateAdminProposal) ProposalRoute() string { return RouterKey }

// GetTitle returns the title of the proposal
func (p *UpdateAdminProposal) GetTitle() string { return p.Title }

// GetDescription returns the human readable description of the proposal
func (p UpdateAdminProposal) GetDescription() string { return p.Description }

// ProposalType returns the type
func (p UpdateAdminProposal) ProposalType() string { return string(ProposalTypeUpdateAdmin) }

// ValidateBasic validates the proposal
func (p UpdateAdminProposal) ValidateBasic() error {
	if err := validateProposalCommons(p.Title, p.Description); err != nil {
		return err
	}
	if _, err := sdk.AccAddressFromBech32(p.Contract); err != nil {
		return sdkerrors.Wrap(err, "contract")
	}
	if _, err := sdk.AccAddressFromBech32(p.NewAdmin); err != nil {
		return sdkerrors.Wrap(err, "new admin")
	}
	return nil
}

// String implements the Stringer interface.
func (p UpdateAdminProposal) String() string {
	return fmt.Sprintf(`Update Contract Admin Proposal:
  Title:       %s
  Description: %s
  Contract:    %s
  New Admin:   %s
`, p.Title, p.Description, p.Contract, p.NewAdmin)
}

// ProposalRoute returns the routing key of a parameter change proposal.
func (p ClearAdminProposal) ProposalRoute() string { return RouterKey }

// GetTitle returns the title of the proposal
func (p *ClearAdminProposal) GetTitle() string { return p.Title }

// GetDescription returns the human readable description of the proposal
func (p ClearAdminProposal) GetDescription() string { return p.Description }

// ProposalType returns the type
func (p ClearAdminProposal) ProposalType() string { return string(ProposalTypeClearAdmin) }

// ValidateBasic validates the proposal
func (p ClearAdminProposal) ValidateBasic() error {
	if err := validateProposalCommons(p.Title, p.Description); err != nil {
		return err
	}
	if _, err := sdk.AccAddressFromBech32(p.Contract); err != nil {
		return sdkerrors.Wrap(err, "contract")
	}
	return nil
}

// String implements the Stringer interface.
func (p ClearAdminProposal) String() string {
	return fmt.Sprintf(`Clear Contract Admin Proposal:
  Title:       %s
  Description: %s
  Contract:    %s
`, p.Title, p.Description, p.Contract)
}

// ProposalRoute returns the routing key of a parameter change proposal.
func (p PinCodesProposal) ProposalRoute() string { return RouterKey }

// GetTitle returns the title of the proposal
func (p *PinCodesProposal) GetTitle() string { return p.Title }

// GetDescription returns the human readable description of the proposal
func (p PinCodesProposal) GetDescription() string { return p.Description }

// ProposalType returns the type
func (p PinCodesProposal) ProposalType() string { return string(ProposalTypePinCodes) }

// ValidateBasic validates the proposal
func (p PinCodesProposal) ValidateBasic() error {
	if err := validateProposalCommons(p.Title, p.Description); err != nil {
		return err
	}
	if len(p.CodeIDs) == 0 {
		return sdkerrors.Wrap(ErrEmpty, "code ids")
	}
	return nil
}

// String implements the Stringer interface.
func (p PinCodesProposal) String() string {
	return fmt.Sprintf(`Pin Wasm Codes Proposal:
  Title:       %s
  Description: %s
  Codes:       %v
`, p.Title, p.Description, p.CodeIDs)
}

// ProposalRoute returns the routing key of a parameter change proposal.
func (p UnpinCodesProposal) ProposalRoute() string { return RouterKey }

// GetTitle returns the title of the proposal
func (p *UnpinCodesProposal) GetTitle() string { return p.Title }

// GetDescription returns the human readable description of the proposal
func (p UnpinCodesProposal) GetDescription() string { return p.Description }

// ProposalType returns the type
func (p UnpinCodesProposal) ProposalType() string { return string(ProposalTypeUnpinCodes) }

// ValidateBasic validates the proposal
func (p UnpinCodesProposal) ValidateBasic() error {
	if err := validateProposalCommons(p.Title, p.Description); err != nil {
		return err
	}
	if len(p.CodeIDs) == 0 {
		return sdkerrors.Wrap(ErrEmpty, "code ids")
	}
	return nil
}

// String implements the Stringer interface.
func (p UnpinCodesProposal) String() string {
	return fmt.Sprintf(`Unpin Wasm Codes Proposal:
  Title:       %s
  Description: %s
  Codes:       %v
`, p.Title, p.Description, p.CodeIDs)
}

func validateProposalCommons(title, description string) error {
	if strings.TrimSpace(title) != title {
		return sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "proposal title must not start/end with white spaces")
	}
	if len(title) == 0 {
		return sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "proposal title cannot be blank")
	}
	if len(title) > govtypes.MaxTitleLength {
		return sdkerrors.Wrapf(govtypes.ErrInvalidProposalContent, "proposal title is longer than max length of %d", govtypes.MaxTitleLength)
	}
	if strings.TrimSpace(description) != description {
		return sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "proposal description must not start/end with white spaces")
	}
	if len(description) == 0 {
		return sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "proposal description cannot be blank")
	}
	if len(description) > govtypes.MaxDescriptionLength {
		return sdkerrors.Wrapf(govtypes.ErrInvalidProposalContent, "proposal description is longer than max length of %d", govtypes.MaxDescriptionLength)
	}
	return nil
}

// ProposalRoute returns the routing key of a parameter change proposal.
func (p UpdateInstantiateConfigProposal) ProposalRoute() string { return RouterKey }

// GetTitle returns the title of the proposal
func (p *UpdateInstantiateConfigProposal) GetTitle() string { return p.Title }

// GetDescription returns the human readable description of the proposal
func (p UpdateInstantiateConfigProposal) GetDescription() string { return p.Description }

// ProposalType returns the type
func (p UpdateInstantiateConfigProposal) ProposalType() string {
	return string(ProposalTypeUpdateInstantiateConfig)
}

// ValidateBasic validates the proposal
func (p UpdateInstantiateConfigProposal) ValidateBasic() error {
	if err := validateProposalCommons(p.Title, p.Description); err != nil {
		return err
	}
	if len(p.AccessConfigUpdates) == 0 {
		return sdkerrors.Wrap(ErrEmpty, "code updates")
	}
	dedup := make(map[uint64]bool)
	for _, codeUpdate := range p.AccessConfigUpdates {
		_, found := dedup[codeUpdate.CodeID]
		if found {
			return sdkerrors.Wrapf(ErrDuplicate, "duplicate code: %d", codeUpdate.CodeID)
		}
		if err := codeUpdate.InstantiatePermission.ValidateBasic(); err != nil {
			return sdkerrors.Wrap(err, "instantiate permission")
		}
		dedup[codeUpdate.CodeID] = true
	}
	return nil
}

// String implements the Stringer interface.
func (p UpdateInstantiateConfigProposal) String() string {
	return fmt.Sprintf(`Update Instantiate Config Proposal:
  Title:       %s
  Description: %s
  AccessConfigUpdates: %v
`, p.Title, p.Description, p.AccessConfigUpdates)
}

// String implements the Stringer interface.
func (c AccessConfigUpdate) String() string {
	return fmt.Sprintf(`AccessConfigUpdate:
  CodeID:       %d
  AccessConfig: %v
`, c.CodeID, c.InstantiatePermission)
}
