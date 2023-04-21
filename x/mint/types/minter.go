package types

import (
	fmt "fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	epochTypes "github.com/sei-protocol/sei-chain/x/epoch/types"
)

// NewMinter returns a new Minter object with the given inflation and annual
// provisions values.
func NewMinter(
	startDate string,
	endDate string,
	denom string,
	totalMintAmount uint64,
) Minter {
	return Minter{
		StartDate:           startDate,
		EndDate:             endDate,
		Denom:               sdk.DefaultBondDenom,
		TotalMintAmount:     totalMintAmount,
		RemainingMintAmount: totalMintAmount,
		LastMintDate:        time.Time{}.Format(TokenReleaseDateFormat),
		LastMintHeight:      0,
		LastMintAmount:      0,
	}
}

// InitialMinter returns an initial Minter object with default values with no previous mints
func InitialMinter() Minter {
	return NewMinter(
		time.Time{}.Format(TokenReleaseDateFormat),
		time.Time{}.Format(TokenReleaseDateFormat),
		sdk.DefaultBondDenom,
		0,
	)
}

// DefaultInitialMinter returns a default initial Minter object for a new chain
// which uses an inflation rate of 0%.
func DefaultInitialMinter() Minter {
	return InitialMinter()
}

// validate minter
func ValidateMinter(minter Minter) error {
	if minter.GetTotalMintAmount() < minter.GetRemainingMintAmount() {
		return fmt.Errorf("total mint amount cannot be less than remaining mint amount")
	}
	endDate := minter.GetEndDateTime()
	startDate := minter.GetStartDateTime()
	if endDate.Before(startDate) {
		return fmt.Errorf("end date must be after start date %s < %s", endDate, startDate)
	}
	return nil
}

func (m Minter) GetLastMintDateTime() time.Time {
	lastMinteDateTime, err := time.Parse(TokenReleaseDateFormat, m.GetLastMintDate())
	if err != nil {
		// This should not happen as the date is validated when the minter is created
		panic(fmt.Errorf("invalid end date for current minter: %s, minter=%s", err, m.String()))
	}
	return lastMinteDateTime
}

func (m Minter) GetStartDateTime() time.Time {
	startDateTime, err := time.Parse(TokenReleaseDateFormat, m.GetStartDate())
	if err != nil {
		// This should not happen as the date is validated when the minter is created
		panic(fmt.Errorf("invalid end date for current minter: %s, minter=%s", err, m.String()))
	}
	return startDateTime
}

func (m Minter) GetEndDateTime() time.Time {
	endDateTime, err := time.Parse(TokenReleaseDateFormat, m.GetEndDate())
	if err != nil {
		// This should not happen as the date is validated when the minter is created
		panic(fmt.Errorf("invalid end date for current minter: %s, minter=%s", err, m.String()))
	}
	return endDateTime
}

func (m Minter) GetLastMintAmountCoin() sdk.Coin {
	return sdk.NewCoin(m.GetDenom(), sdk.NewInt(int64(m.GetLastMintAmount())))
}

func (m Minter) GetReleaseAmountToday() sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(m.GetDenom(), sdk.NewInt(int64(m.getReleaseAmountToday()))))
}

func (m Minter) RecordSuccessfulMint(ctx sdk.Context, epoch epochTypes.Epoch) {
	m.RemainingMintAmount -= m.getReleaseAmountToday()
	m.LastMintDate = epoch.CurrentEpochStartTime.Format(TokenReleaseDateFormat)
	m.LastMintHeight = uint64(epoch.CurrentEpochHeight)
	m.LastMintAmount = m.getReleaseAmountToday()
	metrics.SetCoinsMinted(m.getReleaseAmountToday(), m.GetDenom())

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			EventTypeMint,
			sdk.NewAttribute(AttributeEpochNumber, fmt.Sprintf("%d", epoch.GetCurrentEpoch())),
			sdk.NewAttribute(AttributeKeyEpochProvisions, m.GetLastMintDate()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, m.GetReleaseAmountToday().String()),
		),
	)
}

func (m Minter) getReleaseAmountToday() uint64 {
	numberOfDaysLeft := m.getNumberOfDaysLeft()
	if numberOfDaysLeft == 0 {
		return 0
	}
	return m.GetRemainingMintAmount() / numberOfDaysLeft
}

func (m Minter) getNumberOfDaysLeft() uint64 {
	startDate := m.GetStartDateTime()
	endDate := m.GetEndDateTime()
	return daysBetween(endDate, startDate)
}

func (m Minter) OngoingRelease() bool {
	endDateTime := m.GetEndDateTime()
	return m.GetRemainingMintAmount() > 0 && time.Now().Before(endDateTime)
}

func daysBetween(a, b time.Time) uint64 {
	// Always return a positive value between the dates
	if a.Before(b) {
		a, b = b, a
	}
	duration := a.Sub(b)
	hours := duration.Hours()
	// Rounds down, the last day should be 1 day diff so it will always release any remaining amount
	return uint64(hours / 24)
}
