package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

const (
	ProposalTypeAddAssetMetadata = "AddAssetMetadata"
)

func init() {
	// for routing
	govtypes.RegisterProposalType(ProposalTypeAddAssetMetadata)
	// for marshal and unmarshal
	govtypes.RegisterProposalTypeCodec(&AddAssetMetadataProposal{}, "dex/AddAssetMetadataProposal")
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
