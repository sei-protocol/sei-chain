package types

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func (metadata DenomAuthorityMetadata) Validate() error {
	if metadata.Admin != "" {
		_, err := sdk.AccAddressFromBech32(metadata.Admin)
		if err != nil {
			return err
		}
	}
	return nil
}
