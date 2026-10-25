package verify

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
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
		expectedMintedAmount := oldMinter.GetReleaseAmountToday(oldEpoch.CurrentEpochStartTime.UTC()).AmountOf("usei").Uint64()
		require.Equal(t, expectedMintedAmount, oldMinter.RemainingMintAmount-newMinter.RemainingMintAmount)
		newSupply := app.BankKeeper.GetSupply(app.Ctx(), "usei")
		require.Equal(t, expectedMintedAmount, uint64(newSupply.Amount.Int64()-oldSupply.Amount.Int64())) //nolint:gosec
		return res
	}
}
