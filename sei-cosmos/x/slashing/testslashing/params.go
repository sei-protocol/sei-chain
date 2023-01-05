package testslashing

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/slashing/types"
)

// TestParams construct default slashing params for tests.
// Have to change these parameters for tests
// lest the tests take forever
func TestParams() types.Params {
	params := types.DefaultParams()
	params.SignedBlocksWindow = 1000
	params.DowntimeJailDuration = 60 * 60
	params.MinSignedPerWindow = sdk.NewDecWithPrec(5, 1)

	return params
}
