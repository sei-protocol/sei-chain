package types

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	epochTypes "github.com/sei-protocol/sei-chain/x/epoch/types"
)

// NewMinter returns a new Minter object with the given inflation and annual
// provisions values.
func NewMinter(epochProvisions sdk.Dec) Minter {
	return Minter{
		EpochProvisions: epochProvisions,
	}
}

// InitialMinter returns an initial Minter object with a given inflation value.
func InitialMinter() Minter {
	return NewMinter(
		sdk.NewDec(0),
	)
}

// DefaultInitialMinter returns a default initial Minter object for a new chain
// which uses an inflation rate of 13%.
func DefaultInitialMinter() Minter {
	return InitialMinter()
}

// validate minter
func ValidateMinter(minter Minter) error {
	return nil
}

// EpochProvision returns the provisions for a block based on the epoch
// provisions rate.
func (m Minter) EpochProvision(params Params) sdk.Coin {
	provisionAmt := m.EpochProvisions
	return sdk.NewCoin(params.MintDenom, provisionAmt.TruncateInt())
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
		println(blockDateString)
		println(scheduledReleaseDate)
		println("  ")
		if blockDateString >= scheduledReleaseDate  && scheduledReleaseDate != lastTokenReleaseDateString {
			return &scheduledRelease
		}
	}
	return nil
}
