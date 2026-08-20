package staking

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// commissionUpdateMinInterval is the minimum number of seconds between two
// commission rate changes, matching Cosmos Commission.ValidateNewRate (24h).
const commissionUpdateMinInterval = int64(24 * 60 * 60)

var (
	oneRat = big.NewRat(1, 1)

	errCommissionNegative        = errors.New("commission rate cannot be negative")
	errCommissionHuge            = errors.New("commission max rate cannot be greater than 100%")
	errCommissionGTMaxRate       = errors.New("commission rate cannot be greater than the max rate")
	errCommissionChangeNegative  = errors.New("commission max change rate cannot be negative")
	errCommissionChangeGTMaxRate = errors.New("commission max change rate cannot be greater than the max rate")
	errCommissionLTMinRate       = errors.New("commission rate cannot be less than the min rate")
	errCommissionGTMaxChange     = errors.New("commission rate change cannot be greater than the max change rate")
	errCommissionUpdateTime      = errors.New("commission cannot be changed more than once in 24h")
)

// parseRate parses a decimal commission rate string. Unlike a bare big.Rat
// parse it rejects fraction ("1/3") and scientific ("1e2") forms so the input
// space matches Cosmos sdk.Dec strings.
func parseRate(value string, name string) (*big.Rat, error) {
	if value == "" || strings.ContainsAny(value, "/eE") {
		return nil, fmt.Errorf("invalid %s", name)
	}
	rate, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("invalid %s", name)
	}
	return rate, nil
}

// validateInitialCommission mirrors CommissionRates.Validate plus the
// MinCommissionRate floor the staking msg server enforces on create.
func validateInitialCommission(rateStr, maxRateStr, maxChangeStr, minRateStr string) error {
	rate, err := parseRate(rateStr, "commission rate")
	if err != nil {
		return err
	}
	maxRate, err := parseRate(maxRateStr, "commission max rate")
	if err != nil {
		return err
	}
	maxChange, err := parseRate(maxChangeStr, "commission max change rate")
	if err != nil {
		return err
	}
	minRate, err := parseRate(minRateStr, "min commission rate")
	if err != nil {
		return err
	}
	switch {
	case maxRate.Sign() < 0:
		return errCommissionNegative
	case maxRate.Cmp(oneRat) > 0:
		return errCommissionHuge
	case rate.Sign() < 0:
		return errCommissionNegative
	case rate.Cmp(maxRate) > 0:
		return errCommissionGTMaxRate
	case maxChange.Sign() < 0:
		return errCommissionChangeNegative
	case maxChange.Cmp(maxRate) > 0:
		return errCommissionChangeGTMaxRate
	case rate.Cmp(minRate) < 0:
		return errCommissionLTMinRate
	}
	return nil
}

// validateCommissionUpdate mirrors Commission.ValidateNewRate plus the
// MinCommissionRate floor UpdateValidatorCommission enforces on edit.
func validateCommissionUpdate(validator Validator, newRateStr, minRateStr string, blockTime uint64) error {
	newRate, err := parseRate(newRateStr, "commission rate")
	if err != nil {
		return err
	}
	oldRate, err := parseRate(validator.CommissionRate, "commission rate")
	if err != nil {
		return err
	}
	maxRate, err := parseRate(validator.CommissionMaxRate, "commission max rate")
	if err != nil {
		return err
	}
	maxChange, err := parseRate(validator.CommissionMaxChangeRate, "commission max change rate")
	if err != nil {
		return err
	}
	minRate, err := parseRate(minRateStr, "min commission rate")
	if err != nil {
		return err
	}
	if saturatingInt64FromUint64(blockTime)-validator.CommissionUpdateTime < commissionUpdateMinInterval {
		return errCommissionUpdateTime
	}
	switch {
	case newRate.Sign() < 0:
		return errCommissionNegative
	case newRate.Cmp(maxRate) > 0:
		return errCommissionGTMaxRate
	case new(big.Rat).Sub(newRate, oldRate).Cmp(maxChange) > 0:
		return errCommissionGTMaxChange
	case newRate.Cmp(minRate) < 0:
		return errCommissionLTMinRate
	}
	return nil
}
