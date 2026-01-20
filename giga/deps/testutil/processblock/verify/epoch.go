package verify

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/stretchr/testify/require"
)

func Epoch(t *testing.T, app *processblock.App, f BlockRunnable, _ []signing.Tx) BlockRunnable {
	return func() []uint32 {
		oldEpoch := app.EpochKeeper.GetEpoch(app.Ctx())
		res := f()
		if app.Ctx().BlockTime().Sub(oldEpoch.CurrentEpochStartTime) > oldEpoch.EpochDuration {
			newPoch := app.EpochKeeper.GetEpoch(app.Ctx())
			require.Equal(t, oldEpoch.CurrentEpoch+1, newPoch.CurrentEpoch)
		}
		return res
	}
}
