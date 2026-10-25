package types

import (
	"fmt"
	"strings"

	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

// Names of the retired pointer governance proposals. Pointers can no longer be
// created, so each type below refuses both submission and execution. They remain
// registered because gov keeps a proposal in state once its vote has ended, and a
// content type missing from the registry fails the Any unpack inside
// MustUnmarshalProposal, which panics.
const (
	ProposalTypeAddERCNativePointer   = "AddERCNativePointer"
	ProposalTypeAddERCCW20Pointer     = "AddERCCW20Pointer"
	ProposalTypeAddERCCW721Pointer    = "AddERCCW721Pointer"
	ProposalTypeAddERCCW1155Pointer   = "AddERCCW1155Pointer"
	ProposalTypeAddCWERC20Pointer     = "AddCWERC20Pointer"
	ProposalTypeAddCWERC721Pointer    = "AddCWERC721Pointer"
	ProposalTypeAddCWERC1155Pointer   = "AddCWERC1155Pointer"
	ProposalTypeAddERCNativePointerV2 = "AddERCNativePointerV2"
)

func init() {
	// for routing
	govtypes.RegisterProposalType(ProposalTypeAddERCNativePointer)
	govtypes.RegisterProposalType(ProposalTypeAddERCCW20Pointer)
	govtypes.RegisterProposalType(ProposalTypeAddERCCW721Pointer)
	govtypes.RegisterProposalType(ProposalTypeAddERCCW1155Pointer)
	govtypes.RegisterProposalType(ProposalTypeAddCWERC20Pointer)
	govtypes.RegisterProposalType(ProposalTypeAddCWERC721Pointer)
	govtypes.RegisterProposalType(ProposalTypeAddCWERC1155Pointer)
	govtypes.RegisterProposalType(ProposalTypeAddERCNativePointerV2)

	// for marshal and unmarshal
	govtypes.RegisterProposalTypeCodec(&AddERCNativePointerProposal{}, "evm/AddERCNativePointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddERCCW20PointerProposal{}, "evm/AddERCCW20PointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddERCCW721PointerProposal{}, "evm/AddERCCW721PointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddERCCW1155PointerProposal{}, "evm/AddERCCW1155PointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddCWERC20PointerProposal{}, "evm/AddCWERC20PointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddCWERC721PointerProposal{}, "evm/AddCWERC721PointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddCWERC1155PointerProposal{}, "evm/AddCWERC1155PointerProposal")
	govtypes.RegisterProposalTypeCodec(&AddERCNativePointerProposalV2{}, "evm/AddERCNativePointerProposalV2")
}

func (p *AddERCNativePointerProposal) GetTitle() string { return p.Title }

func (p *AddERCNativePointerProposal) GetDescription() string { return p.Description }

func (*AddERCNativePointerProposal) ProposalRoute() string { return RouterKey }

func (*AddERCNativePointerProposal) ProposalType() string {
	return ProposalTypeAddERCNativePointer
}

func (*AddERCNativePointerProposal) ValidateBasic() error { return ErrPointerProposalDeprecated }

// ValidateProposalSubmission rejects new ERC native pointer proposals.
func (*AddERCNativePointerProposal) ValidateProposalSubmission() error {
	return ErrPointerProposalDeprecated
}

func (p AddERCNativePointerProposal) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, `Add ERC native pointer Proposal:
  Title:       %s
  Description: %s
  Token:       %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Token, p.Pointer, p.Version)
	return b.String()
}

func (p *AddERCCW20PointerProposal) GetTitle() string { return p.Title }

func (p *AddERCCW20PointerProposal) GetDescription() string { return p.Description }

func (*AddERCCW20PointerProposal) ProposalRoute() string { return RouterKey }

func (*AddERCCW20PointerProposal) ProposalType() string {
	return ProposalTypeAddERCCW20Pointer
}

func (*AddERCCW20PointerProposal) ValidateBasic() error { return ErrPointerProposalDeprecated }

// ValidateProposalSubmission rejects new ERC CW20 pointer proposals.
func (*AddERCCW20PointerProposal) ValidateProposalSubmission() error {
	return ErrPointerProposalDeprecated
}

func (p AddERCCW20PointerProposal) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, `Add ERC CW20 pointer Proposal:
  Title:       %s
  Description: %s
  Pointee:     %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Pointee, p.Pointer, p.Version)
	return b.String()
}

func (p *AddERCCW721PointerProposal) GetTitle() string { return p.Title }

func (p *AddERCCW721PointerProposal) GetDescription() string { return p.Description }

func (*AddERCCW721PointerProposal) ProposalRoute() string { return RouterKey }

func (*AddERCCW721PointerProposal) ProposalType() string {
	return ProposalTypeAddERCCW721Pointer
}

func (*AddERCCW721PointerProposal) ValidateBasic() error { return ErrPointerProposalDeprecated }

// ValidateProposalSubmission rejects new ERC CW721 pointer proposals.
func (*AddERCCW721PointerProposal) ValidateProposalSubmission() error {
	return ErrPointerProposalDeprecated
}

func (p AddERCCW721PointerProposal) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, `Add ERC CW721 pointer Proposal:
  Title:       %s
  Description: %s
  Pointee:     %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Pointee, p.Pointer, p.Version)
	return b.String()
}

func (p *AddERCCW1155PointerProposal) GetTitle() string { return p.Title }

func (p *AddERCCW1155PointerProposal) GetDescription() string { return p.Description }

func (*AddERCCW1155PointerProposal) ProposalRoute() string { return RouterKey }

func (*AddERCCW1155PointerProposal) ProposalType() string {
	return ProposalTypeAddERCCW1155Pointer
}

func (*AddERCCW1155PointerProposal) ValidateBasic() error { return ErrPointerProposalDeprecated }

// ValidateProposalSubmission rejects new ERC CW1155 pointer proposals.
func (*AddERCCW1155PointerProposal) ValidateProposalSubmission() error {
	return ErrPointerProposalDeprecated
}

func (p AddERCCW1155PointerProposal) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, `Add ERC CW1155 pointer Proposal:
  Title:       %s
  Description: %s
  Pointee:     %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Pointee, p.Pointer, p.Version)
	return b.String()
}

func (p *AddCWERC20PointerProposal) GetTitle() string { return p.Title }

func (p *AddCWERC20PointerProposal) GetDescription() string { return p.Description }

func (*AddCWERC20PointerProposal) ProposalRoute() string { return RouterKey }

func (*AddCWERC20PointerProposal) ProposalType() string {
	return ProposalTypeAddCWERC20Pointer
}

func (*AddCWERC20PointerProposal) ValidateBasic() error { return ErrPointerProposalDeprecated }

// ValidateProposalSubmission rejects new CW ERC20 pointer proposals.
func (*AddCWERC20PointerProposal) ValidateProposalSubmission() error {
	return ErrPointerProposalDeprecated
}

func (p AddCWERC20PointerProposal) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, `Add CW ERC20 pointer Proposal:
  Title:       %s
  Description: %s
  Pointee:     %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Pointee, p.Pointer, p.Version)
	return b.String()
}

func (p *AddCWERC721PointerProposal) GetTitle() string { return p.Title }

func (p *AddCWERC721PointerProposal) GetDescription() string { return p.Description }

func (*AddCWERC721PointerProposal) ProposalRoute() string { return RouterKey }

func (*AddCWERC721PointerProposal) ProposalType() string {
	return ProposalTypeAddCWERC721Pointer
}

func (*AddCWERC721PointerProposal) ValidateBasic() error { return ErrPointerProposalDeprecated }

// ValidateProposalSubmission rejects new CW ERC721 pointer proposals.
func (*AddCWERC721PointerProposal) ValidateProposalSubmission() error {
	return ErrPointerProposalDeprecated
}

func (p AddCWERC721PointerProposal) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, `Add CW ERC721 pointer Proposal:
  Title:       %s
  Description: %s
  Pointee:     %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Pointee, p.Pointer, p.Version)
	return b.String()
}

func (p *AddCWERC1155PointerProposal) GetTitle() string { return p.Title }

func (p *AddCWERC1155PointerProposal) GetDescription() string { return p.Description }

func (*AddCWERC1155PointerProposal) ProposalRoute() string { return RouterKey }

func (*AddCWERC1155PointerProposal) ProposalType() string {
	return ProposalTypeAddCWERC1155Pointer
}

func (*AddCWERC1155PointerProposal) ValidateBasic() error { return ErrPointerProposalDeprecated }

// ValidateProposalSubmission rejects new CW ERC1155 pointer proposals.
func (*AddCWERC1155PointerProposal) ValidateProposalSubmission() error {
	return ErrPointerProposalDeprecated
}

func (p AddCWERC1155PointerProposal) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, `Add CW ERC1155 pointer Proposal:
  Title:       %s
  Description: %s
  Pointee:     %s
  Pointer:     %s
  Version:     %d
`, p.Title, p.Description, p.Pointee, p.Pointer, p.Version)
	return b.String()
}

func (p *AddERCNativePointerProposalV2) GetTitle() string { return p.Title }

func (p *AddERCNativePointerProposalV2) GetDescription() string { return p.Description }

func (*AddERCNativePointerProposalV2) ProposalRoute() string { return RouterKey }

func (*AddERCNativePointerProposalV2) ProposalType() string {
	return ProposalTypeAddERCNativePointerV2
}

func (*AddERCNativePointerProposalV2) ValidateBasic() error { return ErrPointerProposalDeprecated }

// ValidateProposalSubmission rejects new ERC native pointer proposals.
func (*AddERCNativePointerProposalV2) ValidateProposalSubmission() error {
	return ErrPointerProposalDeprecated
}

func (p AddERCNativePointerProposalV2) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, `Add ERC native pointer Proposal V2:
  Title:       %s
  Description: %s
  Token:       %s
  Name:        %s
  Symbol:      %s
  Decimals:    %d
`, p.Title, p.Description, p.Token, p.Name, p.Symbol, p.Decimals)
	return b.String()
}
