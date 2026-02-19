package epoch_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/epoch"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

func TestNewHandler(t *testing.T) {
	app := app.Setup(t, false, false, false) // Your setup function here
	handler := epoch.NewHandler(app.EpochKeeper)

	// Test unrecognized message type
	testMsg := testdata.NewTestMsg()
	_, err := handler(app.BaseApp.NewContext(false, sdk.Header{}), testMsg)
	require.Error(t, err)

	expectedErrMsg := fmt.Sprintf("unrecognized %s message type", types.ModuleName)
	require.ErrorContains(t, err, expectedErrMsg)
}
