package bindings

import "github.com/sei-protocol/sei-chain/x/tokenfactory/types"

type SeiTokenFactoryQuery struct {
	// queries the tokenfactory create denom fee whitelist
	CreatorInDenomFeeWhitelist *types.QueryCreatorInDenomFeeWhitelistRequest `json:"creator_in_denom_fee_whitelist,omitempty"`
	GetDenomFeeWhitelist       *types.QueryDenomCreationFeeWhitelistRequest  `json:"get_denom_fee_whitelist,omitempty"`
}
