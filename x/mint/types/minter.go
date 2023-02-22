package types

import (
	fmt "fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	epochTypes "github.com/sei-protocol/sei-chain/x/epoch/types"
)

// NewMinter returns a new Minter object with the given inflation and annual
// provisions values.
func NewMinter(lastMintAmount sdk.Dec, lastMintDate string, lastMintHeight int64, denom string) Minter {
	return Minter{
		LastMintAmount: lastMintAmount,
		LastMintDate:   lastMintDate,
		LastMintHeight: lastMintHeight,
		Denom:          denom,
	}
}

// InitialMinter returns an initial Minter object with a given inflation value.
func InitialMinter() Minter {
	return NewMinter(
		sdk.NewDec(0),
		"1970-01-01",
		0,
		sdk.DefaultBondDenom,
	)
}

// DefaultInitialMinter returns a default initial Minter object for a new chain
// which uses an inflation rate of 0%.
func DefaultInitialMinter() Minter {
	return InitialMinter()
}

// validate minter
func ValidateMinter(minter Minter) error {
	return nil
}

func (m Minter) GetLastMintDateTime() time.Time {
	lastTokenReleaseDate, err := time.Parse(TokenReleaseDateFormat, m.GetLastMintDate())
	if err != nil {
		panic(fmt.Errorf("invalid last token release date: %s", err))
	}
	return lastTokenReleaseDate
}

func (m Minter) GetCoin() sdk.Coin {
	return sdk.NewCoin(m.GetDenom(), m.LastMintAmount.TruncateInt())
}

func (m Minter) GetCoins() sdk.Coins {
	return sdk.NewCoins(m.GetCoin())
}

func (m Minter) GetLastMintAmount() sdk.Dec {
	return m.LastMintAmount
}

/*	Returns ScheduledRelease if the date of the block matches the scheduled release date.
 *	You may only schedule one release of tokens on each day, the date must be in
 *  types.TokenReleaseDateFormat.
 */
func GetScheduledTokenRelease(
	epoch epochTypes.Epoch,
	lastTokenReleaseDate time.Time,
	tokenReleaseSchedule []ScheduledTokenRelease,
) *ScheduledTokenRelease {
	blockDateString := epoch.GetCurrentEpochStartTime().Format(TokenReleaseDateFormat)
	lastTokenReleaseDateString := lastTokenReleaseDate.Format(TokenReleaseDateFormat)
	for _, scheduledRelease := range tokenReleaseSchedule {
		scheduledReleaseDate := scheduledRelease.GetDate()
		if blockDateString >= scheduledReleaseDate && scheduledReleaseDate > lastTokenReleaseDateString {
			return &scheduledRelease
		}
	}
	return nil
}
