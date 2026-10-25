package testutil

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

// Type URLs of the account types the removed vesting module registered.
const (
	LegacyBaseVestingAccountTypeURL       = "/cosmos.vesting.v1beta1.BaseVestingAccount"
	LegacyContinuousVestingAccountTypeURL = "/cosmos.vesting.v1beta1.ContinuousVestingAccount"
	LegacyDelayedVestingAccountTypeURL    = "/cosmos.vesting.v1beta1.DelayedVestingAccount"
	LegacyPeriodicVestingAccountTypeURL   = "/cosmos.vesting.v1beta1.PeriodicVestingAccount"
	LegacyPermanentLockedAccountTypeURL   = "/cosmos.vesting.v1beta1.PermanentLockedAccount"
)

// LegacyVestingAccountTypeURLs lists the type URL of every account type the
// removed vesting module registered.
var LegacyVestingAccountTypeURLs = []string{
	LegacyBaseVestingAccountTypeURL,
	LegacyContinuousVestingAccountTypeURL,
	LegacyDelayedVestingAccountTypeURL,
	LegacyPeriodicVestingAccountTypeURL,
	LegacyPermanentLockedAccountTypeURL,
}

// LegacyVestingAccount is an account of one of the removed vesting module's
// types, which Encode writes the way that module's codec did.
type LegacyVestingAccount struct {
	TypeURL          string
	Base             *authtypes.BaseAccount
	OriginalVesting  sdk.Coins
	DelegatedFree    sdk.Coins
	DelegatedVesting sdk.Coins
	EndTime          int64
	Admin            string
	CancelledTime    int64
	// StartTime is encoded for continuous and periodic vesting accounts.
	StartTime int64
	// Periods is encoded for periodic vesting accounts.
	Periods []LegacyVestingPeriod
}

// LegacyVestingPeriod is one period of a periodic vesting account.
type LegacyVestingPeriod struct {
	Length int64
	Amount sdk.Coins
}

// Encode returns the google.protobuf.Any the account store held for the account.
func (a LegacyVestingAccount) Encode() ([]byte, error) {
	base, err := a.Base.Marshal()
	if err != nil {
		return nil, err
	}
	baseVesting := appendLengthDelimited(nil, 1, base)
	for _, field := range []struct {
		num   protowire.Number
		coins sdk.Coins
	}{{2, a.OriginalVesting}, {3, a.DelegatedFree}, {4, a.DelegatedVesting}} {
		if baseVesting, err = appendCoins(baseVesting, field.num, field.coins); err != nil {
			return nil, err
		}
	}
	baseVesting = appendInt64(baseVesting, 5, a.EndTime)
	if a.Admin != "" {
		baseVesting = appendLengthDelimited(baseVesting, 6, []byte(a.Admin))
	}
	baseVesting = appendInt64(baseVesting, 7, a.CancelledTime)

	var value []byte
	switch a.TypeURL {
	case LegacyBaseVestingAccountTypeURL:
		value = baseVesting
	case LegacyDelayedVestingAccountTypeURL, LegacyPermanentLockedAccountTypeURL:
		value = appendLengthDelimited(nil, 1, baseVesting)
	case LegacyContinuousVestingAccountTypeURL:
		value = appendInt64(appendLengthDelimited(nil, 1, baseVesting), 2, a.StartTime)
	case LegacyPeriodicVestingAccountTypeURL:
		value = appendInt64(appendLengthDelimited(nil, 1, baseVesting), 2, a.StartTime)
		for _, period := range a.Periods {
			encoded, err := appendCoins(appendInt64(nil, 1, period.Length), 2, period.Amount)
			if err != nil {
				return nil, err
			}
			value = appendLengthDelimited(value, 3, encoded)
		}
	default:
		return nil, fmt.Errorf("%q is not a legacy vesting account type", a.TypeURL)
	}
	return appendLengthDelimited(appendLengthDelimited(nil, 1, []byte(a.TypeURL)), 2, value), nil
}

func appendLengthDelimited(b []byte, num protowire.Number, payload []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, payload)
}

func appendInt64(b []byte, num protowire.Number, v int64) []byte {
	if v == 0 {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.VarintType)
	return protowire.AppendVarint(b, uint64(v)) //nolint:gosec // proto int64 is the two's complement varint
}

func appendCoins(b []byte, num protowire.Number, coins sdk.Coins) ([]byte, error) {
	for _, coin := range coins {
		encoded, err := coin.Marshal()
		if err != nil {
			return nil, err
		}
		b = appendLengthDelimited(b, num, encoded)
	}
	return b, nil
}
