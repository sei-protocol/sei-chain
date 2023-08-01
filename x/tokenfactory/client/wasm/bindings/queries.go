package bindings

import "github.com/sei-protocol/sei-chain/x/tokenfactory/types"

type SeiTokenFactoryQuery struct {
	// queries the tokenfactory authority metadata
	GetDenomAuthorityMetadata *types.QueryDenomAuthorityMetadataRequest `json:"get_denom_authority_metadata,omitempty"`
	// queries the tokenfactory denoms from a creator
	GetDenomsFromCreator *types.QueryDenomsFromCreatorRequest `json:"get_denoms_from_creator,omitempty"`
}
