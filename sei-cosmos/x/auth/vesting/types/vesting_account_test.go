package types_test

import (
	"math/big"
	"testing"
	"time"

	tmtime "github.com/sei-protocol/sei-chain/sei-cosmos/std"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
)

const (
	stakeDenom = "usei"
	feeDenom   = "fee"
)

func TestGetVestedCoinsContVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	cva := types.NewContinuousVestingAccount(bacc, origCoins, now.Unix(), endTime.Unix(), nil)

	require.Nil(t, cva.GetVestedCoins(now))
	require.Equal(t, origCoins, cva.GetVestedCoins(endTime))
	require.Equal(t, cs(fee(500), stake(50)), cva.GetVestedCoins(now.Add(12*time.Hour)))
	require.Equal(t, cs(fee(292), stake(29)), cva.GetVestedCoins(now.Add(7*time.Hour)))
	require.Equal(t, cs(fee(708), stake(71)), cva.GetVestedCoins(now.Add(17*time.Hour)))
	require.Equal(t, origCoins, cva.GetVestedCoins(now.Add(48*time.Hour)))
}

func TestGetVestedCoinsContVestingAccNoOverflow(t *testing.T) {
	now, endTime, bacc, _ := vestingFixture()
	hugeBig := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	orig := cs(sdk.NewCoin(stakeDenom, sdk.NewIntFromBigInt(hugeBig)))
	cva := types.NewContinuousVestingAccount(bacc, orig, now.Unix(), endTime.Unix(), nil)

	var vested sdk.Coins
	require.NotPanics(t, func() {
		vested = cva.GetVestedCoins(now.Add(18 * time.Hour)) // 75% elapsed: inside the old panic window
	})
	// 75% of 2^256-1, banker's-rounded, equals floor((2^256-1)*3/4).
	want := new(big.Int).Div(new(big.Int).Mul(hugeBig, big.NewInt(3)), big.NewInt(4))
	require.Equal(t, sdk.NewIntFromBigInt(want), vested.AmountOf(stakeDenom))
	require.Equal(t, orig, cva.GetVestedCoins(endTime))
}

func TestGetVestingCoinsContVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	cva := types.NewContinuousVestingAccount(bacc, origCoins, now.Unix(), endTime.Unix(), nil)

	require.Equal(t, origCoins, cva.GetVestingCoins(now))
	require.Nil(t, cva.GetVestingCoins(endTime))
	require.Equal(t, cs(fee(500), stake(50)), cva.GetVestingCoins(now.Add(12*time.Hour)))
}

func TestSpendableCoinsContVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	cva := types.NewContinuousVestingAccount(bacc, origCoins, now.Unix(), endTime.Unix(), nil)

	require.Equal(t, origCoins, cva.LockedCoins(now))
	require.Equal(t, sdk.NewCoins(), cva.LockedCoins(endTime))
	require.Equal(t, cs(fee(500), stake(50)), cva.LockedCoins(now.Add(12*time.Hour)))
}

func TestTrackDelegationContVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	newCVA := func() *types.ContinuousVestingAccount {
		return types.NewContinuousVestingAccount(bacc, origCoins, now.Unix(), endTime.Unix(), nil)
	}

	// At t=0: nothing vested, so all delegation counts as vesting.
	cva := newCVA()
	cva.TrackDelegation(now, origCoins, origCoins)
	require.Equal(t, origCoins, cva.DelegatedVesting)
	require.Nil(t, cva.DelegatedFree)

	// At t=end: fully vested, so all delegation counts as free.
	cva = newCVA()
	cva.TrackDelegation(endTime, origCoins, origCoins)
	require.Nil(t, cva.DelegatedVesting)
	require.Equal(t, origCoins, cva.DelegatedFree)

	// At t=12h (50% vested): first 50 stake is vesting; second 50 is free.
	cva = newCVA()
	cva.TrackDelegation(now.Add(12*time.Hour), origCoins, cs(stake(50)))
	require.Equal(t, cs(stake(50)), cva.DelegatedVesting)
	require.Nil(t, cva.DelegatedFree)
	cva.TrackDelegation(now.Add(12*time.Hour), origCoins, cs(stake(50)))
	require.Equal(t, cs(stake(50)), cva.DelegatedVesting)
	require.Equal(t, cs(stake(50)), cva.DelegatedFree)

	// Over-delegation panics and leaves state untouched.
	cva = newCVA()
	require.Panics(t, func() {
		cva.TrackDelegation(endTime, origCoins, cs(stake(1000000)))
	})
	require.Nil(t, cva.DelegatedVesting)
	require.Nil(t, cva.DelegatedFree)
}

func TestTrackUndelegationContVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	newCVA := func() *types.ContinuousVestingAccount {
		return types.NewContinuousVestingAccount(bacc, origCoins, now.Unix(), endTime.Unix(), nil)
	}

	// Undelegate everything delegated while fully vesting.
	cva := newCVA()
	cva.TrackDelegation(now, origCoins, origCoins)
	cva.TrackUndelegation(origCoins)
	require.Nil(t, cva.DelegatedFree)
	require.Nil(t, cva.DelegatedVesting)

	// Undelegate everything delegated when fully vested.
	cva = newCVA()
	cva.TrackDelegation(endTime, origCoins, origCoins)
	cva.TrackUndelegation(origCoins)
	require.Nil(t, cva.DelegatedFree)
	require.Nil(t, cva.DelegatedVesting)

	// Zero-amount undelegation panics; state untouched.
	cva = newCVA()
	require.Panics(t, func() {
		cva.TrackUndelegation(cs(stake(0)))
	})
	require.Nil(t, cva.DelegatedFree)
	require.Nil(t, cva.DelegatedVesting)

	// At t=12h: delegate 50 to two validators (one ends up vesting, one free),
	// then undelegate from each with the first having been slashed 50%.
	cva = newCVA()
	cva.TrackDelegation(now.Add(12*time.Hour), origCoins, cs(stake(50)))
	cva.TrackDelegation(now.Add(12*time.Hour), origCoins, cs(stake(50)))

	cva.TrackUndelegation(cs(stake(25))) // slashed validator returns half
	require.Equal(t, cs(stake(25)), cva.DelegatedFree)
	require.Equal(t, cs(stake(50)), cva.DelegatedVesting)

	cva.TrackUndelegation(cs(stake(50))) // healthy validator returns in full
	require.Nil(t, cva.DelegatedFree)
	require.Equal(t, cs(stake(25)), cva.DelegatedVesting)
}

func TestGetVestedCoinsDelVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	dva := types.NewDelayedVestingAccount(bacc, origCoins, endTime.Unix(), nil)

	require.Nil(t, dva.GetVestedCoins(now))
	require.Equal(t, origCoins, dva.GetVestedCoins(endTime))
}

func TestGetVestingCoinsDelVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	dva := types.NewDelayedVestingAccount(bacc, origCoins, endTime.Unix(), nil)

	require.Equal(t, origCoins, dva.GetVestingCoins(now))
	require.Nil(t, dva.GetVestingCoins(endTime))
}

func TestSpendableCoinsDelVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	dva := types.NewDelayedVestingAccount(bacc, origCoins, endTime.Unix(), nil)

	require.True(t, dva.LockedCoins(now).IsEqual(origCoins))
	require.Equal(t, sdk.NewCoins(), dva.LockedCoins(endTime))
	require.True(t, dva.LockedCoins(now.Add(12*time.Hour)).IsEqual(origCoins))

	// Delegating reduces the locked amount.
	delegated := sdk.NewCoins(stake(50))
	dva.TrackDelegation(now.Add(12*time.Hour), origCoins, delegated)
	require.True(t, dva.LockedCoins(now.Add(12*time.Hour)).IsEqual(origCoins.Sub(delegated)))
}

func TestTrackDelegationDelVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	newDVA := func() *types.DelayedVestingAccount {
		return types.NewDelayedVestingAccount(bacc, origCoins, endTime.Unix(), nil)
	}

	// Before maturation: all delegation is vesting.
	dva := newDVA()
	dva.TrackDelegation(now, origCoins, origCoins)
	require.Equal(t, origCoins, dva.DelegatedVesting)
	require.Nil(t, dva.DelegatedFree)

	// After maturation: all delegation is free.
	dva = newDVA()
	dva.TrackDelegation(endTime, origCoins, origCoins)
	require.Nil(t, dva.DelegatedVesting)
	require.Equal(t, origCoins, dva.DelegatedFree)

	// Halfway through: delayed account hasn't vested anything yet, so it's
	// all still vesting.
	dva = newDVA()
	dva.TrackDelegation(now.Add(12*time.Hour), origCoins, origCoins)
	require.Equal(t, origCoins, dva.DelegatedVesting)
	require.Nil(t, dva.DelegatedFree)

	// Over-delegation panics.
	dva = newDVA()
	require.Panics(t, func() {
		dva.TrackDelegation(endTime, origCoins, cs(stake(1000000)))
	})
	require.Nil(t, dva.DelegatedVesting)
	require.Nil(t, dva.DelegatedFree)
}

func TestTrackUndelegationDelVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	newDVA := func() *types.DelayedVestingAccount {
		return types.NewDelayedVestingAccount(bacc, origCoins, endTime.Unix(), nil)
	}

	// Undelegate everything delegated while vesting.
	dva := newDVA()
	dva.TrackDelegation(now, origCoins, origCoins)
	dva.TrackUndelegation(origCoins)
	require.Nil(t, dva.DelegatedFree)
	require.Nil(t, dva.DelegatedVesting)

	// Undelegate everything delegated when vested.
	dva = newDVA()
	dva.TrackDelegation(endTime, origCoins, origCoins)
	dva.TrackUndelegation(origCoins)
	require.Nil(t, dva.DelegatedFree)
	require.Nil(t, dva.DelegatedVesting)

	// Zero-amount undelegation panics.
	dva = newDVA()
	require.Panics(t, func() {
		dva.TrackUndelegation(cs(stake(0)))
	})
	require.Nil(t, dva.DelegatedFree)
	require.Nil(t, dva.DelegatedVesting)

	// At t=12h: nothing has vested yet, so both delegations are vesting.
	// Undelegate with slashing on one validator.
	dva = newDVA()
	dva.TrackDelegation(now.Add(12*time.Hour), origCoins, cs(stake(50)))
	dva.TrackDelegation(now.Add(12*time.Hour), origCoins, cs(stake(50)))

	dva.TrackUndelegation(cs(stake(25))) // slashed
	require.Nil(t, dva.DelegatedFree)
	require.Equal(t, cs(stake(75)), dva.DelegatedVesting)

	dva.TrackUndelegation(cs(stake(50))) // healthy
	require.Nil(t, dva.DelegatedFree)
	require.Equal(t, cs(stake(25)), dva.DelegatedVesting)
}

func TestGetVestedCoinsPeriodicVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	pva := types.NewPeriodicVestingAccount(bacc, origCoins, now.Unix(), defaultPeriods(), nil)

	require.Nil(t, pva.GetVestedCoins(now))
	require.Equal(t, origCoins, pva.GetVestedCoins(endTime))
	require.Nil(t, pva.GetVestedCoins(now.Add(6*time.Hour))) // mid period 1, nothing yet
	require.Equal(t, cs(fee(500), stake(50)), pva.GetVestedCoins(now.Add(12*time.Hour)))
	require.Equal(t, cs(fee(500), stake(50)), pva.GetVestedCoins(now.Add(15*time.Hour))) // mid period 2
	require.Equal(t, cs(fee(750), stake(75)), pva.GetVestedCoins(now.Add(18*time.Hour)))
	require.Equal(t, origCoins, pva.GetVestedCoins(now.Add(48*time.Hour)))
}

func TestGetVestingCoinsPeriodicVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	pva := types.NewPeriodicVestingAccount(bacc, origCoins, now.Unix(), defaultPeriods(), nil)

	require.Equal(t, origCoins, pva.GetVestingCoins(now))
	require.Nil(t, pva.GetVestingCoins(endTime))
	require.Equal(t, cs(fee(500), stake(50)), pva.GetVestingCoins(now.Add(12*time.Hour)))
	require.Equal(t, cs(fee(500), stake(50)), pva.GetVestingCoins(now.Add(15*time.Hour)))
	require.Equal(t, cs(fee(250), stake(25)), pva.GetVestingCoins(now.Add(18*time.Hour)))
	require.Nil(t, pva.GetVestingCoins(now.Add(48*time.Hour)))
}

func TestSpendableCoinsPeriodicVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	pva := types.NewPeriodicVestingAccount(bacc, origCoins, now.Unix(), defaultPeriods(), nil)

	require.Equal(t, origCoins, pva.LockedCoins(now))
	require.Equal(t, sdk.NewCoins(), pva.LockedCoins(endTime))
	require.Equal(t, cs(fee(500), stake(50)), pva.LockedCoins(now.Add(12*time.Hour)))
}

func TestTrackDelegationPeriodicVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	periods := defaultPeriods()
	newPVA := func() *types.PeriodicVestingAccount {
		return types.NewPeriodicVestingAccount(bacc, origCoins, now.Unix(), periods, nil)
	}

	// At t=0: all delegation is vesting.
	pva := newPVA()
	pva.TrackDelegation(now, origCoins, origCoins)
	require.Equal(t, origCoins, pva.DelegatedVesting)
	require.Nil(t, pva.DelegatedFree)

	// At t=end: all delegation is free.
	pva = newPVA()
	pva.TrackDelegation(endTime, origCoins, origCoins)
	require.Nil(t, pva.DelegatedVesting)
	require.Equal(t, origCoins, pva.DelegatedFree)

	// Delegating period[0]'s amount at t=0 is fully vesting.
	pva = newPVA()
	pva.TrackDelegation(now, origCoins, periods[0].Amount)
	require.Equal(t, pva.DelegatedVesting, periods[0].Amount)
	require.Nil(t, pva.DelegatedFree)

	// At t=12h, periods[0] has vested. Delegating periods[0]+periods[1] worth
	// prefers to spend the vesting bucket first: periods[0] is vesting,
	// periods[1] is free.
	pva = newPVA()
	pva.TrackDelegation(now.Add(12*time.Hour), origCoins, periods[0].Amount.Add(periods[1].Amount...))
	require.Equal(t, pva.DelegatedFree, periods[1].Amount)
	require.Equal(t, pva.DelegatedVesting, periods[0].Amount)

	// At t=12h: split 50/50 between vesting and free across two delegations.
	pva = newPVA()
	pva.TrackDelegation(now.Add(12*time.Hour), origCoins, cs(stake(50)))
	require.Equal(t, cs(stake(50)), pva.DelegatedVesting)
	require.Nil(t, pva.DelegatedFree)
	pva.TrackDelegation(now.Add(12*time.Hour), origCoins, cs(stake(50)))
	require.Equal(t, cs(stake(50)), pva.DelegatedVesting)
	require.Equal(t, cs(stake(50)), pva.DelegatedFree)

	// Over-delegation panics.
	pva = newPVA()
	require.Panics(t, func() {
		pva.TrackDelegation(endTime, origCoins, cs(stake(1000000)))
	})
	require.Nil(t, pva.DelegatedVesting)
	require.Nil(t, pva.DelegatedFree)
}

func TestTrackUndelegationPeriodicVestingAcc(t *testing.T) {
	now, endTime, bacc, origCoins := vestingFixture()
	periods := defaultPeriods()
	newPVA := func() *types.PeriodicVestingAccount {
		return types.NewPeriodicVestingAccount(bacc, origCoins, now.Unix(), periods, nil)
	}

	pva := newPVA()
	pva.TrackDelegation(now, origCoins, origCoins)
	pva.TrackUndelegation(origCoins)
	require.Nil(t, pva.DelegatedFree)
	require.Nil(t, pva.DelegatedVesting)

	pva = newPVA()
	pva.TrackDelegation(endTime, origCoins, origCoins)
	pva.TrackUndelegation(origCoins)
	require.Nil(t, pva.DelegatedFree)
	require.Nil(t, pva.DelegatedVesting)

	// Undelegate periods[0]'s worth after fully vested.
	pva = newPVA()
	pva.TrackDelegation(endTime, origCoins, periods[0].Amount)
	pva.TrackUndelegation(periods[0].Amount)
	require.Nil(t, pva.DelegatedFree)
	require.Nil(t, pva.DelegatedVesting)

	pva = newPVA()
	require.Panics(t, func() {
		pva.TrackUndelegation(cs(stake(0)))
	})
	require.Nil(t, pva.DelegatedFree)
	require.Nil(t, pva.DelegatedVesting)

	// At t=12h: delegate 50 to two validators, then undelegate with slashing.
	pva = newPVA()
	pva.TrackDelegation(now.Add(12*time.Hour), origCoins, cs(stake(50)))
	pva.TrackDelegation(now.Add(12*time.Hour), origCoins, cs(stake(50)))

	pva.TrackUndelegation(cs(stake(25))) // slashed
	require.Equal(t, cs(stake(25)), pva.DelegatedFree)
	require.Equal(t, cs(stake(50)), pva.DelegatedVesting)

	pva.TrackUndelegation(cs(stake(50))) // healthy
	require.Nil(t, pva.DelegatedFree)
	require.Equal(t, cs(stake(25)), pva.DelegatedVesting)
}

func TestGetVestedCoinsPermLockedVestingAcc(t *testing.T) {
	now, _, bacc, origCoins := vestingFixture()
	farFuture := now.Add(1000 * 24 * time.Hour)
	plva := types.NewPermanentLockedAccount(bacc, origCoins, nil)

	// Permanently locked: nothing ever vests.
	require.Nil(t, plva.GetVestedCoins(now))
	require.Nil(t, plva.GetVestedCoins(farFuture))
}

func TestGetVestingCoinsPermLockedVestingAcc(t *testing.T) {
	now, _, bacc, origCoins := vestingFixture()
	farFuture := now.Add(1000 * 24 * time.Hour)
	plva := types.NewPermanentLockedAccount(bacc, origCoins, nil)

	// Permanently locked: everything is always vesting.
	require.Equal(t, origCoins, plva.GetVestingCoins(now))
	require.Equal(t, origCoins, plva.GetVestingCoins(farFuture))
}

func TestSpendableCoinsPermLockedVestingAcc(t *testing.T) {
	now, _, bacc, origCoins := vestingFixture()
	farFuture := now.Add(1000 * 24 * time.Hour)
	plva := types.NewPermanentLockedAccount(bacc, origCoins, nil)

	require.True(t, plva.LockedCoins(now).IsEqual(origCoins))
	require.True(t, plva.LockedCoins(farFuture).IsEqual(origCoins))

	// Delegating reduces the locked amount.
	delegated := sdk.NewCoins(stake(50))
	plva.TrackDelegation(now.Add(12*time.Hour), origCoins, delegated)
	require.True(t, plva.LockedCoins(now.Add(12*time.Hour)).IsEqual(origCoins.Sub(delegated)))
}

func TestTrackDelegationPermLockedVestingAcc(t *testing.T) {
	now, _, bacc, origCoins := vestingFixture()
	farFuture := now.Add(1000 * 24 * time.Hour)
	newPLVA := func() *types.PermanentLockedAccount {
		return types.NewPermanentLockedAccount(bacc, origCoins, nil)
	}

	// All delegation is always vesting on a permanent-locked account,
	// regardless of timestamp.
	plva := newPLVA()
	plva.TrackDelegation(now, origCoins, origCoins)
	require.Equal(t, origCoins, plva.DelegatedVesting)
	require.Nil(t, plva.DelegatedFree)

	plva = newPLVA()
	plva.TrackDelegation(farFuture, origCoins, origCoins)
	require.Equal(t, origCoins, plva.DelegatedVesting)
	require.Nil(t, plva.DelegatedFree)

	// Over-delegation panics.
	plva = newPLVA()
	require.Panics(t, func() {
		plva.TrackDelegation(farFuture, origCoins, cs(stake(1000000)))
	})
	require.Nil(t, plva.DelegatedVesting)
	require.Nil(t, plva.DelegatedFree)
}

func TestTrackUndelegationPermLockedVestingAcc(t *testing.T) {
	now, _, bacc, origCoins := vestingFixture()
	farFuture := now.Add(1000 * 24 * time.Hour)
	newPLVA := func() *types.PermanentLockedAccount {
		return types.NewPermanentLockedAccount(bacc, origCoins, nil)
	}

	plva := newPLVA()
	plva.TrackDelegation(now, origCoins, origCoins)
	plva.TrackUndelegation(origCoins)
	require.Nil(t, plva.DelegatedFree)
	require.Nil(t, plva.DelegatedVesting)

	plva = newPLVA()
	plva.TrackDelegation(farFuture, origCoins, origCoins)
	plva.TrackUndelegation(origCoins)
	require.Nil(t, plva.DelegatedFree)
	require.Nil(t, plva.DelegatedVesting)

	plva = newPLVA()
	require.Panics(t, func() {
		plva.TrackUndelegation(cs(stake(0)))
	})
	require.Nil(t, plva.DelegatedFree)
	require.Nil(t, plva.DelegatedVesting)

	// Delegate 50 to two validators, undelegate with slashing on one.
	plva = newPLVA()
	plva.TrackDelegation(now, origCoins, cs(stake(50)))
	plva.TrackDelegation(now, origCoins, cs(stake(50)))

	plva.TrackUndelegation(cs(stake(25))) // slashed
	require.Nil(t, plva.DelegatedFree)
	require.Equal(t, cs(stake(75)), plva.DelegatedVesting)

	plva.TrackUndelegation(cs(stake(50))) // healthy
	require.Nil(t, plva.DelegatedFree)
	require.Equal(t, cs(stake(25)), plva.DelegatedVesting)
}

func TestGenesisAccountValidate(t *testing.T) {
	t.Parallel()
	pubkey := secp256k1.GenPrivKey().PubKey()
	addr := sdk.AccAddress(pubkey.Address())
	baseAcc := authtypes.NewBaseAccount(addr, pubkey, 0, 0)
	initialVesting := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 50))
	baseVesting := types.NewBaseVestingAccount(baseAcc, initialVesting, 100, nil)

	bondCoin := func(amt int64) sdk.Coin { return sdk.NewInt64Coin(sdk.DefaultBondDenom, amt) }
	onePeriod := func(length int64, amt sdk.Coins) types.Periods {
		return types.Periods{{Length: length, Amount: amt}}
	}

	tests := []struct {
		name   string
		acc    authtypes.GenesisAccount
		expErr bool
	}{
		{"valid base account", baseAcc, false},
		{
			"invalid base valid account",
			authtypes.NewBaseAccount(addr, secp256k1.GenPrivKey().PubKey(), 0, 0),
			true,
		},
		{"valid base vesting account", baseVesting, false},
		{
			"valid continuous vesting account",
			types.NewContinuousVestingAccount(baseAcc, initialVesting, 100, 200, nil),
			false,
		},
		{
			"invalid vesting times",
			types.NewContinuousVestingAccount(baseAcc, initialVesting, 1654668078, 1554668078, nil),
			true,
		},
		{
			"valid periodic vesting account",
			types.NewPeriodicVestingAccount(baseAcc, initialVesting, 0, onePeriod(100, sdk.Coins{bondCoin(50)}), nil),
			false,
		},
		{
			"invalid vesting period lengths",
			types.NewPeriodicVestingAccountRaw(baseVesting, 0, onePeriod(50, sdk.Coins{bondCoin(50)})),
			true,
		},
		{
			"invalid vesting period amounts",
			types.NewPeriodicVestingAccountRaw(baseVesting, 0, onePeriod(100, sdk.Coins{bondCoin(25)})),
			true,
		},
		{
			"empty coin amount should fail",
			types.NewPeriodicVestingAccountRaw(baseVesting, 0, onePeriod(100, sdk.Coins{})),
			true,
		},
		{
			"zero-length period should fail",
			types.NewPeriodicVestingAccountRaw(baseVesting, 0, onePeriod(0, sdk.Coins{bondCoin(25)})),
			true,
		},
		{
			"negative coin amount should fail",
			types.NewPeriodicVestingAccountRaw(baseVesting, 0, onePeriod(100, sdk.Coins{{
				Denom:  sdk.DefaultBondDenom,
				Amount: sdk.NewInt(-123),
			}})),
			true,
		},
		{
			"negative-length period should fail",
			types.NewPeriodicVestingAccountRaw(baseVesting, 0, onePeriod(-1, sdk.Coins{bondCoin(25)})),
			true,
		},
		{
			"valid permanent locked vesting account",
			types.NewPermanentLockedAccount(baseAcc, initialVesting, nil),
			false,
		},
		{
			"invalid positive end time for permanently locked vest account",
			&types.PermanentLockedAccount{BaseVestingAccount: baseVesting},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expErr, tt.acc.Validate() != nil)
		})
	}
}

func TestCreateBaseVestingWithAdmin(t *testing.T) {
	pubkey := secp256k1.GenPrivKey().PubKey()
	addr := sdk.AccAddress(pubkey.Address())
	adminAddr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	baseAcc := authtypes.NewBaseAccount(addr, pubkey, 0, 0)
	initialVesting := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 50))
	acc := types.NewBaseVestingAccount(baseAcc, initialVesting, 100, adminAddr)

	bz, err := a.AccountKeeper.MarshalAccount(acc)
	require.NoError(t, err)

	got, err := a.AccountKeeper.UnmarshalAccount(bz)
	require.NoError(t, err)
	require.IsType(t, &types.BaseVestingAccount{}, got)
	require.Equal(t, acc.String(), got.String())
}

// TestVestingAccountMarshal replaces four near-identical Test*Marshal tests.
// Each variant must round-trip through Marshal/Unmarshal preserving identity
// and string form, and must fail to unmarshal from truncated bytes.
func TestVestingAccountMarshal(t *testing.T) {
	baseAcc, origCoins := initBaseAccount()
	endTime := time.Now().Unix()
	baseVesting := types.NewBaseVestingAccount(baseAcc, origCoins, endTime, nil)

	cases := []struct {
		name string
		acc  authtypes.GenesisAccount
	}{
		{"continuous", types.NewContinuousVestingAccountRaw(baseVesting, baseVesting.EndTime)},
		{
			"periodic",
			types.NewPeriodicVestingAccount(baseAcc, origCoins, endTime,
				types.Periods{{Length: 3600, Amount: origCoins}}, nil),
		},
		{"delayed", types.NewDelayedVestingAccount(baseAcc, origCoins, endTime, nil)},
		{"permanent_locked", types.NewPermanentLockedAccount(baseAcc, origCoins, nil)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bz, err := a.AccountKeeper.MarshalAccount(tc.acc)
			require.NoError(t, err)

			got, err := a.AccountKeeper.UnmarshalAccount(bz)
			require.NoError(t, err)
			require.IsType(t, tc.acc, got)
			require.Equal(t, tc.acc.String(), got.String())

			// Truncated bytes must fail to unmarshal.
			_, err = a.AccountKeeper.UnmarshalAccount(bz[:len(bz)/2])
			require.Error(t, err)
		})
	}
}

// stake(n) / fee(n) read much better in dense assertions than
// sdk.NewInt64Coin(stakeDenom, n).
func stake(n int64) sdk.Coin { return sdk.NewInt64Coin(stakeDenom, n) }
func fee(n int64) sdk.Coin   { return sdk.NewInt64Coin(feeDenom, n) }

// cs builds a raw sdk.Coins — equivalent to sdk.Coins{...}, NOT sdk.NewCoins(...).
// Do not switch to sdk.NewCoins: it sorts, validates, and drops zero coins,
// any of which would break tests that assert exact slice equality or that
// pass a zero coin to TrackUndelegation to provoke a panic.
func cs(c ...sdk.Coin) sdk.Coins { return sdk.Coins(c) }

func initBaseAccount() (*authtypes.BaseAccount, sdk.Coins) {
	_, _, addr := testdata.KeyTestPubAddr()
	return authtypes.NewBaseAccountWithAddress(addr), cs(fee(1000), stake(100))
}

// vestingFixture returns the (now, endTime = now+24h, baseAccount, originalVesting)
// tuple shared by every vesting test in this file.
func vestingFixture() (now, endTime time.Time, bacc *authtypes.BaseAccount, orig sdk.Coins) {
	now = tmtime.Now()
	endTime = now.Add(24 * time.Hour)
	bacc, orig = initBaseAccount()
	return
}

// defaultPeriods is the periodic schedule used by every periodic-vesting test:
// 50% vests at +12h, then 25% at +18h, then 25% at +24h.
func defaultPeriods() types.Periods {
	return types.Periods{
		{Length: int64(12 * 60 * 60), Amount: cs(fee(500), stake(50))},
		{Length: int64(6 * 60 * 60), Amount: cs(fee(250), stake(25))},
		{Length: int64(6 * 60 * 60), Amount: cs(fee(250), stake(25))},
	}
}
