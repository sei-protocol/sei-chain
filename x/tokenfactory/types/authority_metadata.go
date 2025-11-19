package types

import (
	seitypes "github.com/sei-protocol/sei-chain/types"
)

func (metadata DenomAuthorityMetadata) Validate() error {
	if metadata.Admin != "" {
		_, err := seitypes.AccAddressFromBech32(metadata.Admin)
		if err != nil {
			return err
		}
	}
	return nil
}
