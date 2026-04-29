package ethtx

import (
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

func validateHexAddress(fieldName, value string) error {
	if len(value) >= 2 && value[0] == '0' && (value[1] == 'x' || value[1] == 'X') {
		value = value[2:]
	}
	if len(value) != common.AddressLength*2 {
		return fmt.Errorf("invalid %s: wrong length", fieldName)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("invalid %s", fieldName)
	}
	return nil
}

func validateHexHash(fieldName, value string) error {
	if len(value) >= 2 && value[0] == '0' && (value[1] == 'x' || value[1] == 'X') {
		value = value[2:]
	}
	if len(value) != common.HashLength*2 {
		return fmt.Errorf("invalid %s: wrong length", fieldName)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("invalid %s", fieldName)
	}
	return nil
}

func validateAccessList(accessList AccessList) error {
	for _, tuple := range accessList {
		if err := validateHexAddress("access list address", tuple.Address); err != nil {
			return err
		}
		for _, storageKey := range tuple.StorageKeys {
			if err := validateHexHash("access list storage key", storageKey); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateAuthList(authList AuthList) error {
	for _, auth := range authList {
		if auth.ChainID == nil {
			return fmt.Errorf("auth list chain id cannot be nil")
		}
		if err := validateHexAddress("auth list address", auth.Address); err != nil {
			return err
		}
		if err := validateSignatureValue("auth list v", auth.V, 1); err != nil {
			return err
		}
		if err := validateSignatureValue("auth list r", auth.R, 32); err != nil {
			return err
		}
		if err := validateSignatureValue("auth list s", auth.S, 32); err != nil {
			return err
		}
	}
	return nil
}

func validateSignatureValue(fieldName string, value []byte, maxLen int) error {
	if len(value) > maxLen {
		return fmt.Errorf("invalid %s: too long", fieldName)
	}
	if len(value) > 1 && value[0] == 0 {
		return fmt.Errorf("invalid %s: leading zero", fieldName)
	}
	return nil
}
