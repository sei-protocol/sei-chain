package staking

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

func stakingValue(value *big.Int) (*big.Int, error) {
	if value == nil || value.Sign() == 0 {
		return nil, errors.New("set `value` field to non-zero to send delegate fund")
	}
	if value.Sign() < 0 {
		return nil, errors.New("staking value cannot be negative")
	}
	usei, remainder := new(big.Int).QuoRem(value, useiToSwei, new(big.Int))
	if remainder.Sign() != 0 {
		return nil, fmt.Errorf("selected precompile function does not allow payment with non-zero wei remainder: received %s", value)
	}
	if usei.Sign() == 0 {
		return nil, errors.New("staking value is below one usei")
	}
	return usei, nil
}

func validateWritable(ctx *precompiles.Context) error {
	if ctx.ReadOnly {
		return errReadOnly
	}
	return nil
}

func normalizeValidatorAddress(validatorAddress string) string {
	if common.IsHexAddress(validatorAddress) {
		return common.HexToAddress(validatorAddress).Hex()
	}
	return validatorAddress
}

func statusMatches(filter string, status int32) bool {
	if filter == "" {
		return true
	}
	switch strings.ToUpper(filter) {
	case "BOND_STATUS_UNSPECIFIED":
		return status == 0
	case "BOND_STATUS_UNBONDED":
		return status == 1
	case "BOND_STATUS_UNBONDING":
		return status == 2
	case "BOND_STATUS_BONDED":
		return status == 3
	default:
		parsed, err := strconv.ParseInt(filter, 10, 32)
		return err == nil && int32(parsed) == status //nolint:gosec // parsed is limited to 32 bits.
	}
}

func isTransaction(method string) bool {
	switch method {
	case DelegateMethod, RedelegateMethod, UndelegateMethod, CreateValidatorMethod, EditValidatorMethod:
		return true
	default:
		return false
	}
}
