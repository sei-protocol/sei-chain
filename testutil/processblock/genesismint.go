package processblock

import (
	"time"

	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
)

func (a *App) NewMinter(amount uint64) {
	// UTC calendar dates so DaysBetween(blockTime, end) matches the
	// formatted start/end regardless of local timezone.
	today := time.Now().UTC()
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	a.MintKeeper.SetMinter(a.Ctx(), minttypes.Minter{
		StartDate:           start.Format(minttypes.TokenReleaseDateFormat),
		EndDate:             start.AddDate(0, 0, 2).Format(minttypes.TokenReleaseDateFormat),
		Denom:               "usei",
		TotalMintAmount:     amount,
		RemainingMintAmount: amount,
	})
}
