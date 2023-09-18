package ethtx

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

func IsEmptyHash(hash string) bool {
	return bytes.Equal(common.HexToHash(hash).Bytes(), common.Hash{}.Bytes())
}

func IsZeroAddress(address string) bool {
	return bytes.Equal(common.HexToAddress(address).Bytes(), common.Address{}.Bytes())
}

func ValidateAddress(address string) error {
	if !common.IsHexAddress(address) {
		return fmt.Errorf(
			"address '%s' is not a valid ethereum hex address",
			address,
		)
	}
	return nil
}

func ValidateNonZeroAddress(address string) error {
	if IsZeroAddress(address) {
		return fmt.Errorf(
			"address '%s' must not be zero",
			address,
		)
	}
	return ValidateAddress(address)
}
