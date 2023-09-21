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
		Denom:               denom,
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
	return validateMintDenom(minter.Denom)
}

func (m *Minter) GetLastMintDateTime() time.Time {
	lastMinteDateTime, err := time.Parse(TokenReleaseDateFormat, m.GetLastMintDate())
	if err != nil {
		// This should not happen as the date is validated when the minter is created
		panic(fmt.Errorf("invalid end date for current minter: %s, minter=%s", err, m.String()))
	}
	return lastMinteDateTime.UTC()
}

func (m *Minter) GetStartDateTime() time.Time {
	startDateTime, err := time.Parse(TokenReleaseDateFormat, m.GetStartDate())
	if err != nil {
		// This should not happen as the date is validated when the minter is created
		panic(fmt.Errorf("invalid end date for current minter: %s, minter=%s", err, m.String()))
	}
	return startDateTime.UTC()
}

func (m *Minter) GetEndDateTime() time.Time {
	endDateTime, err := time.Parse(TokenReleaseDateFormat, m.GetEndDate())
	if err != nil {
		// This should not happen as the date is validated when the minter is created
		panic(fmt.Errorf("invalid end date for current minter: %s, minter=%s", err, m.String()))
	}
	return endDateTime.UTC()
}

func (m Minter) GetLastMintAmountCoin() sdk.Coin {
	return sdk.NewCoin(m.GetDenom(), sdk.NewInt(int64(m.GetLastMintAmount())))
}

func (m *Minter) GetReleaseAmountToday(currentTime time.Time) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(m.GetDenom(), sdk.NewInt(int64(m.getReleaseAmountToday(currentTime.UTC())))))
}

func (m *Minter) RecordSuccessfulMint(ctx sdk.Context, epoch epochTypes.Epoch, mintedAmount uint64) {
	m.RemainingMintAmount -= mintedAmount
	m.LastMintDate = epoch.CurrentEpochStartTime.Format(TokenReleaseDateFormat)
	m.LastMintHeight = uint64(epoch.CurrentEpochHeight)
	m.LastMintAmount = mintedAmount
	metrics.SetCoinsMinted(mintedAmount, m.GetDenom())
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			EventTypeMint,
			sdk.NewAttribute(AttributeMintEpoch, fmt.Sprintf("%d", epoch.GetCurrentEpoch())),
			sdk.NewAttribute(AttribtueMintDate, m.GetLastMintDate()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, fmt.Sprintf("%d", mintedAmount)),
		),
	)
}

func (m *Minter) getReleaseAmountToday(currentTime time.Time) uint64 {
	// Not yet started or already minted today
	if currentTime.Before(m.GetStartDateTime()) || currentTime.Format(TokenReleaseDateFormat) == m.GetLastMintDate() {
		return 0
	}

	// if it's already past the end date then release the remaining amount likely caused by outage
	numberOfDaysLeft := m.GetNumberOfDaysLeft(currentTime)
	if currentTime.After(m.GetEndDateTime()) || numberOfDaysLeft == 0 {
		return m.GetRemainingMintAmount()
	}

	return m.GetRemainingMintAmount() / numberOfDaysLeft
}

func (m *Minter) GetNumberOfDaysLeft(currentTime time.Time) uint64 {
	// If the last mint date is after the start date then use the last mint date as there's an ongoing release
	daysBetween := DaysBetween(currentTime, m.GetEndDateTime())
	return daysBetween
}

func (m *Minter) OngoingRelease() bool {
	return m.GetRemainingMintAmount() != 0
}

func DaysBetween(a, b time.Time) uint64 {
	// Convert both times to UTC before comparing
	aYear, aMonth, aDay := a.UTC().Date()
	a = time.Date(aYear, aMonth, aDay, 0, 0, 0, 0, time.UTC)
	bYear, bMonth, bDay := b.UTC().Date()
	b = time.Date(bYear, bMonth, bDay, 0, 0, 0, 0, time.UTC)

	// Always return a positive value between the dates
	if a.Before(b) {
		a, b = b, a
	}
	hours := a.Sub(b).Hours()
	return uint64(hours / 24)
}
