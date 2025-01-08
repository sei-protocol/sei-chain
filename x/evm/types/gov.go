package types

import (
	"errors"
	"fmt"
	"math"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/common"
)

const (
	ProposalTypeAddERCNativePointer   = "AddERCNativePointer"
	ProposalTypeAddERCCW20Pointer     = "AddERCCW20Pointer"
	ProposalTypeAddERCCW721Pointer    = "AddERCCW721Pointer"
	ProposalTypeAddCWERC20Pointer     = "AddCWERC20Pointer"
	ProposalTypeAddCWERC721Pointer    = "AddCWERC721Pointer"
	ProposalTypeAddERCNativePointerV2 = "AddERCNativePointerV2"
)

func init() {
	// for routing
	govtypes.RegisterProposalType(ProposalTypeAddERCNativePointer)
	govtypes.RegisterProposalType(ProposalTypeAddERCCW20Pointer)
	govtypes.RegisterProposalType(ProposalTypeAddERCCW721Pointer)
	govtypes.RegisterProposalType(ProposalTypeAddCWERC20Pointer)
	govtypes.RegisterProposalType(ProposalTypeAddCWERC721Pointer)
	govtypes.RegisterProposalType(ProposalTypeAddERCNativePointerV2)

	// for marshal and unmarshal
	govtypes.RegisterProposalTypeCodec(&AddERCNativePointerProposal{}, "evm/AddERCNativePointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddERCCW20PointerProposal{}, "evm/AddERCCW20PointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddERCCW721PointerProposal{}, "evm/AddERCCW721PointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddCWERC20PointerProposal{}, "evm/AddCWERC20PointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddCWERC721PointerProposal{}, "evm/AddCWERC721PointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddERCNativePointerProposalV2{}, "evm/AddCWERC721PointerProposalV2")
}

func (p *AddERCNativePointerProposal) GetTitle() string { return p.Title }

func (p *AddERCNativePointerProposal) GetDescription() string { return p.Description }

func (p *AddERCNativePointerProposal) ProposalRoute() string { return RouterKey }

func (p *AddERCNativePointerProposal) ProposalType() string {
	return ProposalTypeAddERCNativePointer
}

func (p *AddERCNativePointerProposal) ValidateBasic() error {
	if p.Pointer != "" && !common.IsHexAddress(p.Pointer) {
		return errors.New("pointer address must be either empty or a valid hex-encoded string")
	}

	if p.Version > math.MaxUint16 {
		return errors.New("pointer version must be <= 65535")
	}

	return govtypes.ValidateAbstract(p)
}

func (p AddERCNativePointerProposal) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Add ERC native pointer Proposal:
  Title:       %s
  Description: %s
  Token:       %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Token, p.Pointer, p.Version))
	return b.String()
}

func (p *AddERCCW20PointerProposal) GetTitle() string { return p.Title }

func (p *AddERCCW20PointerProposal) GetDescription() string { return p.Description }

func (p *AddERCCW20PointerProposal) ProposalRoute() string { return RouterKey }

func (p *AddERCCW20PointerProposal) ProposalType() string {
	return ProposalTypeAddERCCW20Pointer
}

func (p *AddERCCW20PointerProposal) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(p.Pointee); err != nil {
		return err
	}

	if p.Pointer != "" && !common.IsHexAddress(p.Pointer) {
		return errors.New("pointer address must be either empty or a valid hex-encoded string")
	}

	if p.Version > math.MaxUint16 {
		return errors.New("pointer version must be <= 65535")
	}

	return govtypes.ValidateAbstract(p)
}

func (p AddERCCW20PointerProposal) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Add ERC CW20 pointer Proposal:
  Title:       %s
  Description: %s
  Pointee:     %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Pointee, p.Pointer, p.Version))
	return b.String()
}

func (p *AddERCCW721PointerProposal) GetTitle() string { return p.Title }

func (p *AddERCCW721PointerProposal) GetDescription() string { return p.Description }

func (p *AddERCCW721PointerProposal) ProposalRoute() string { return RouterKey }

func (p *AddERCCW721PointerProposal) ProposalType() string {
	return ProposalTypeAddERCCW721Pointer
}

func (p *AddERCCW721PointerProposal) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(p.Pointee); err != nil {
		return err
	}

	if p.Pointer != "" && !common.IsHexAddress(p.Pointer) {
		return errors.New("pointer address must be either empty or a valid hex-encoded string")
	}

	if p.Version > math.MaxUint16 {
		return errors.New("pointer version must be <= 65535")
	}

	return govtypes.ValidateAbstract(p)
}

func (p AddERCCW721PointerProposal) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Add ERC CW721 pointer Proposal:
  Title:       %s
  Description: %s
  Pointee:     %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Pointee, p.Pointer, p.Version))
	return b.String()
}

func (p *AddCWERC20PointerProposal) GetTitle() string { return p.Title }

func (p *AddCWERC20PointerProposal) GetDescription() string { return p.Description }

func (p *AddCWERC20PointerProposal) ProposalRoute() string { return RouterKey }

func (p *AddCWERC20PointerProposal) ProposalType() string {
	return ProposalTypeAddCWERC20Pointer
}

func (p *AddCWERC20PointerProposal) ValidateBasic() error {
	if p.Pointer != "" {
		if _, err := sdk.AccAddressFromBech32(p.Pointer); err != nil {
			return err
		}
	}
	if !common.IsHexAddress(p.Pointee) {
		return errors.New("pointee address must be either empty or a valid hex-encoded string")
	}

	if p.Version > math.MaxUint16 {
		return errors.New("pointer version must be <= 65535")
	}

	return govtypes.ValidateAbstract(p)
}

func (p AddCWERC20PointerProposal) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Add CW ERC20 pointer Proposal:
  Title:       %s
  Description: %s
  Pointee:     %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Pointee, p.Pointer, p.Version))
	return b.String()
}

func (p *AddCWERC721PointerProposal) GetTitle() string { return p.Title }

func (p *AddCWERC721PointerProposal) GetDescription() string { return p.Description }

func (p *AddCWERC721PointerProposal) ProposalRoute() string { return RouterKey }

func (p *AddCWERC721PointerProposal) ProposalType() string {
	return ProposalTypeAddCWERC721Pointer
}

func (p *AddCWERC721PointerProposal) ValidateBasic() error {
	if p.Pointer != "" {
		if _, err := sdk.AccAddressFromBech32(p.Pointer); err != nil {
			return err
		}
	}
	if !common.IsHexAddress(p.Pointee) {
		return errors.New("pointee address must be either empty or a valid hex-encoded string")
	}

	if p.Version > math.MaxUint16 {
		return errors.New("pointer version must be <= 65535")
	}

	return govtypes.ValidateAbstract(p)
}

func (p AddCWERC721PointerProposal) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Add CW ERC721 pointer Proposal:
  Title:       %s
  Description: %s
  Pointee:     %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Pointee, p.Pointer, p.Version))
	return b.String()
}

func (p *AddERCNativePointerProposalV2) GetTitle() string { return p.Title }

func (p *AddERCNativePointerProposalV2) GetDescription() string { return p.Description }

func (p *AddERCNativePointerProposalV2) ProposalRoute() string { return RouterKey }

func (p *AddERCNativePointerProposalV2) ProposalType() string {
	return ProposalTypeAddERCNativePointerV2
}

func (p *AddERCNativePointerProposalV2) ValidateBasic() error {
	if p.Decimals > math.MaxUint8 {
		return errors.New("pointer version must be <= 255")
	}

	return govtypes.ValidateAbstract(p)
}

func (p AddERCNativePointerProposalV2) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Add ERC native pointer Proposal V2:
  Title:       %s
  Description: %s
  Token:       %s
  Name:        %s
  Symbol:      %s
  Decimals:    %d
`, p.Title, p.Description, p.Token, p.Name, p.Symbol, p.Decimals))
	return b.String()
}
