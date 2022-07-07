package types

import (
	"fmt"
	"strings"

	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	ProposalTypeRegisterPairs = "RegisterPairs"
	ProposalTypeUpdateTickSize = "UpdateTickSize"
	ProposalTypeAddAssetMetadata = "AddAssetMetadata"
)

func init() {
	// for routing
	govtypes.RegisterProposalType(ProposalTypeRegisterPairs)
	govtypes.RegisterProposalType(ProposalTypeUpdateTickSize)
	govtypes.RegisterProposalType(ProposalTypeAddAssetMetadata)
	// for marshal and unmarshal
	govtypes.RegisterProposalTypeCodec(&RegisterPairsProposal{}, "dex/RegisterPairsProposal")
	govtypes.RegisterProposalTypeCodec(&UpdateTickSizeProposal{}, "dex/UpdateTickSizeProposal")
	govtypes.RegisterProposalTypeCodec(&AddAssetMetadataProposal{}, "dex/AddAssetMetadataProposal")
}

var _ govtypes.Content = &RegisterPairsProposal{}
var _ govtypes.Content = &UpdateTickSizeProposal{}

func NewRegisterPairsProposal(title, description string, batchContractPair []BatchContractPair) RegisterPairsProposal {
	return RegisterPairsProposal{
		Title:       title,
		Description: description,
		Batchcontractpair:    batchContractPair,
	}
}

func (p *RegisterPairsProposal) GetTitle() string { return p.Title }

func (p *RegisterPairsProposal) GetDescription() string { return p.Description }

func (p *RegisterPairsProposal) ProposalRoute() string { return RouterKey }

func (p *RegisterPairsProposal) ProposalType() string {
	return ProposalTypeRegisterPairs
}

func (p *RegisterPairsProposal) ValidateBasic() error {
	err := govtypes.ValidateAbstract(p)
	return err
}

// TODO: String support for register pair type
func (p RegisterPairsProposal) String() string {
	batchContractPairRecords := ""
	for _, contractPair := range p.Batchcontractpair {
		batchContractPairRecords += contractPair.String()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Register Pairs Proposal:
  Title:       %s
  Description: %s
  Records:     %s
`, p.Title, p.Description, batchContractPairRecords))
	return b.String()
}

// todo might be good to separate to different file when # of governance proposal increases
func NewUpdateTickSizeForPair(title, description string, tickSizeList []TickSize) UpdateTickSizeProposal {
	return UpdateTickSizeProposal {
		Title: title,
		Description: description,
		TickSizeList: tickSizeList,
	}
}

func (p *UpdateTickSizeProposal) GetTitle() string { return p.Title }

func (p *UpdateTickSizeProposal) GetDescription() string { return p.Description }

func (p *UpdateTickSizeProposal) ProposalRoute() string { return RouterKey }

func (p *UpdateTickSizeProposal) ProposalType() string {
	return ProposalTypeRegisterPairs
}

func (p *UpdateTickSizeProposal) ValidateBasic() error {
	err := govtypes.ValidateAbstract(p)
	return err
}

func (p UpdateTickSizeProposal) String() string {
	tickSizeListRecords := ""
	for _, ticksize := range p.TickSizeList {
		tickSizeListRecords += ticksize.String()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Register Pairs Proposal:
  Title:       %s
  Description: %s
  Records:     %s
`, p.Title, p.Description, tickSizeListRecords))
	return b.String()
}

// todo might be good to separate to different file when # of governance proposal increases
func NewAddAssetMetadata(title, description string, assetList []AssetMetadata) AddAssetMetadataProposal {
	return AddAssetMetadataProposal {
		Title: title,
		Description: description,
		AssetList: assetList,
	}
}

func (p *AddAssetMetadataProposal) GetTitle() string { return p.Title }

func (p *AddAssetMetadataProposal) GetDescription() string { return p.Description }

func (p *AddAssetMetadataProposal) ProposalRoute() string { return RouterKey }

func (p *AddAssetMetadataProposal) ProposalType() string {
	return ProposalTypeAddAssetMetadata
}

func (p *AddAssetMetadataProposal) ValidateBasic() error {
	// Verify base denoms specified in proposal are well formed
	for _, asset := range p.AssetList {
		err := sdk.ValidateDenom(asset.Metadata.Base)
		if err != nil {
			return err
		}
	}

	err := govtypes.ValidateAbstract(p)
	return err
}

func (p AddAssetMetadataProposal) String() string {
	assetRecords := ""
	for _, asset := range p.AssetList {
		assetRecords += asset.String()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`Add Asset Metadata Proposal:
  Title:       %s
  Description: %s
  Records:     %s
`, p.Title, p.Description, assetRecords))
	return b.String()
}
