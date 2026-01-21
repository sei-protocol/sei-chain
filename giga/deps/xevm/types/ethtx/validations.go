package ethtx

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// address is validate if it is a hex string (case insensitive)
// of length 40. It may optionally have a '0x' or '0X' prefix.
func ValidateAddress(address string) error {
	if !common.IsHexAddress(address) {
		return fmt.Errorf(
			"address '%s' is not a valid ethereum hex address",
			address,
		)
	}
	return nil
}
