package feegrant_test

import (
	"fmt"
	"testing"
	"time"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant"
)

func TestBasicFeeValidAllow(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)

	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	badTime := ctx.BlockTime().AddDate(0, 0, -1)
	allowace := &feegrant.BasicAllowance{
		Expiration: &badTime,
	}
	require.Error(t, allowace.ValidateBasic())

	ctx = app.BaseApp.NewContext(false, tmproto.Header{
		Time: time.Now(),
	})
	eth := sdk.NewCoins(sdk.NewInt64Coin("eth", 10))
	atom := sdk.NewCoins(sdk.NewInt64Coin("atom", 555))
	smallAtom := sdk.NewCoins(sdk.NewInt64Coin("atom", 43))
	bigAtom := sdk.NewCoins(sdk.NewInt64Coin("atom", 1000))
	leftAtom := sdk.NewCoins(sdk.NewInt64Coin("atom", 512))
	now := ctx.BlockTime()
	oneHour := now.Add(1 * time.Hour)

	cases := map[string]struct {
		allowance *feegrant.BasicAllowance
		// all other checks are ignored if valid=false
		fee       sdk.Coins
		blockTime time.Time
		valid     bool
		accept    bool
		remove    bool
		remains   sdk.Coins
	}{
		"empty": {
			allowance: &feegrant.BasicAllowance{},
			accept:    true,
		},
		"small fee without expire": {
			allowance: &feegrant.BasicAllowance{
				SpendLimit: atom,
			},
			fee:     smallAtom,
			accept:  true,
			remove:  false,
			remains: leftAtom,
		},
		"all fee without expire": {
			allowance: &feegrant.BasicAllowance{
				SpendLimit: smallAtom,
			},
			fee:    smallAtom,
			accept: true,
			remove: true,
		},
		"wrong fee": {
			allowance: &feegrant.BasicAllowance{
				SpendLimit: smallAtom,
			},
			fee:    eth,
			accept: false,
		},
		"non-expired": {
			allowance: &feegrant.BasicAllowance{
				SpendLimit: atom,
				Expiration: &oneHour,
			},
			valid:     true,
			fee:       smallAtom,
			blockTime: now,
			accept:    true,
			remove:    false,
			remains:   leftAtom,
		},
		"expired": {
			allowance: &feegrant.BasicAllowance{
				SpendLimit: atom,
				Expiration: &now,
			},
			valid:     true,
			fee:       smallAtom,
			blockTime: oneHour,
			accept:    false,
			remove:    true,
		},
		"fee more than allowed": {
			allowance: &feegrant.BasicAllowance{
				SpendLimit: atom,
				Expiration: &oneHour,
			},
			valid:     true,
			fee:       bigAtom,
			blockTime: now,
			accept:    false,
		},
		"with out spend limit": {
			allowance: &feegrant.BasicAllowance{
				Expiration: &oneHour,
			},
			valid:     true,
			fee:       bigAtom,
			blockTime: now,
			accept:    true,
		},
		"expired no spend limit": {
			allowance: &feegrant.BasicAllowance{
				Expiration: &now,
			},
			valid:     true,
			fee:       bigAtom,
			blockTime: oneHour,
			accept:    false,
		},
	}

	for name, stc := range cases {
		tc := stc // to make scopelint happy
		t.Run(name, func(t *testing.T) {
			err := tc.allowance.ValidateBasic()
			require.NoError(t, err)

			ctx := app.BaseApp.NewContext(false, tmproto.Header{}).WithBlockTime(tc.blockTime)

			// now try to deduct
			removed, err := tc.allowance.Accept(ctx, tc.fee, []sdk.Msg{})
			if !tc.accept {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			require.Equal(t, tc.remove, removed)
			if !removed {
				assert.Equal(t, tc.allowance.SpendLimit, tc.remains)
			}
		})
	}
}

// sortedCoins builds a valid, sorted, duplicate-free Coins list of n denoms
// (zero-padded so lexical order matches numeric order).
func sortedCoins(n int) sdk.Coins {
	coins := make(sdk.Coins, n)
	for i := 0; i < n; i++ {
		coins[i] = sdk.NewInt64Coin(fmt.Sprintf("coin%08d", i), 1)
	}
	return coins.Sort()
}

func TestBasicAllowanceMaxDenoms(t *testing.T) {
	atCap := &feegrant.BasicAllowance{SpendLimit: sortedCoins(feegrant.MaxAllowanceDenoms)}
	require.NoError(t, atCap.ValidateBasic())

	overCap := &feegrant.BasicAllowance{SpendLimit: sortedCoins(feegrant.MaxAllowanceDenoms + 1)}
	err := overCap.ValidateBasic()
	require.Error(t, err)
	require.ErrorIs(t, err, feegrant.ErrTooManyDenoms)
}

func TestPeriodicAllowanceMaxDenoms(t *testing.T) {
	atCap := sortedCoins(feegrant.MaxAllowanceDenoms)
	over := sortedCoins(feegrant.MaxAllowanceDenoms + 1)

	// Each case keeps the other allowance fields under cap so that a single
	// over-cap field is the only thing that can trip ErrTooManyDenoms. This
	// isolates the periodic-specific checks; if they only oversized every field
	// at once, the embedded Basic.ValidateBasic() would short-circuit and the
	// periodic checks would never be exercised.
	cases := map[string]*feegrant.PeriodicAllowance{
		"basic spend limit over cap": {
			Basic:            feegrant.BasicAllowance{SpendLimit: over},
			PeriodSpendLimit: atCap,
			PeriodCanSpend:   atCap,
			Period:           time.Hour,
		},
		"period spend limit over cap": {
			Basic:            feegrant.BasicAllowance{SpendLimit: atCap},
			PeriodSpendLimit: over,
			PeriodCanSpend:   atCap,
			Period:           time.Hour,
		},
		"period can spend over cap": {
			Basic:            feegrant.BasicAllowance{SpendLimit: atCap},
			PeriodSpendLimit: atCap,
			PeriodCanSpend:   over,
			Period:           time.Hour,
		},
	}

	for name, allowance := range cases {
		t.Run(name, func(t *testing.T) {
			err := allowance.ValidateBasic()
			require.Error(t, err)
			require.ErrorIs(t, err, feegrant.ErrTooManyDenoms)
		})
	}
}
