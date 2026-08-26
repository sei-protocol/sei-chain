package util

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

func ParseAmount(s string) (*big.Int, error) {
	if s == "" {
		return new(big.Int), nil
	}
	amount, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("invalid integer amount %q", s)
	}
	return amount, nil
}

func CloneBig(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(v)
}

func ValidateNonPayable(value *big.Int) error {
	if value != nil && value.Sign() != 0 {
		return errors.New("sending funds to a non-payable function")
	}
	return nil
}

func ValidateArgsLength(args []interface{}, length int) error {
	if len(args) != length {
		return fmt.Errorf("expected %d arguments but got %d", length, len(args))
	}
	return nil
}

func ValidatePositiveAmount(amount *big.Int, name string) error {
	if amount == nil || amount.Sign() <= 0 {
		return fmt.Errorf("%s must be a positive integer", name)
	}
	return nil
}

func SaturatingCompletionTime(blockTime uint64, offset uint64) int64 {
	if blockTime > uint64(1<<63-1) || offset > uint64(1<<63-1)-blockTime {
		return int64(1<<63 - 1)
	}
	return int64(blockTime + offset) //nolint:gosec // bounded above.
}

func AddressString(addr common.Address) string {
	return addr.Hex()
}
