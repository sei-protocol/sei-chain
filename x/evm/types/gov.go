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
	ProposalTypeAddERCNativePointer = "AddERCNativePointer"
	ProposalTypeAddERCCW20Pointer   = "AddERCCW20Pointer"
	ProposalTypeAddERCCW721Pointer  = "AddERCCW721Pointer"
)

func init() {
	// for routing
	govtypes.RegisterProposalType(ProposalTypeAddERCNativePointer)
	govtypes.RegisterProposalType(ProposalTypeAddERCCW20Pointer)
	govtypes.RegisterProposalType(ProposalTypeAddERCCW721Pointer)
	// for marshal and unmarshal
	govtypes.RegisterProposalTypeCodec(&AddERCNativePointerProposal{}, "evm/AddERCNativePointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddERCCW20PointerProposal{}, "evm/AddERCCW20PointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddERCCW721PointerProposal{}, "evm/AddERCCW721PointerProposal")
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
