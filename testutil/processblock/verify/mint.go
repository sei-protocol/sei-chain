package verify

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

func MintRelease(t *testing.T, app *processblock.App, f BlockRunnable, _ []signing.Tx) BlockRunnable {
	return func() []uint32 {
		oldMinter := app.MintKeeper.GetMinter(app.Ctx())
		oldEpoch := app.EpochKeeper.GetEpoch(app.Ctx())
		oldSupply := app.BankKeeper.GetSupply(app.Ctx(), "usei")
		res := f()
		// if minter minted, it must be a new epoch, but not the other way around
		newMinter := app.MintKeeper.GetMinter(app.Ctx())
		if newMinter.RemainingMintAmount == oldMinter.RemainingMintAmount {
			return res
		}
		newPoch := app.EpochKeeper.GetEpoch(app.Ctx())
		require.Equal(t, oldEpoch.CurrentEpoch+1, newPoch.CurrentEpoch)
		startDate, err := time.Parse(minttypes.TokenReleaseDateFormat, oldMinter.StartDate)
		if err != nil {
			panic(err)
		}
		endDate, err := time.Parse(minttypes.TokenReleaseDateFormat, oldMinter.EndDate)
		if err != nil {
			panic(err)
		}
		expectedMintedAmount := oldMinter.TotalMintAmount / uint64(endDate.Sub(startDate)/(24*time.Hour))
		require.Equal(t, expectedMintedAmount, oldMinter.RemainingMintAmount-newMinter.RemainingMintAmount)
		newSupply := app.BankKeeper.GetSupply(app.Ctx(), "usei")
		require.Equal(t, expectedMintedAmount, uint64(newSupply.Amount.Int64()-oldSupply.Amount.Int64()))
		return res
	}
}
