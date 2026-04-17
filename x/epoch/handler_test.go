package epoch_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"

	"github.com/sei-protocol/sei-chain/app"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/epoch"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

func TestNewHandler(t *testing.T) {
	app := app.Setup(t, false, false, false) // Your setup function here
	handler := epoch.NewHandler(app.EpochKeeper)

	// Test unrecognized message type
	testMsg := testdata.NewTestMsg()
	_, err := handler(app.BaseApp.NewContext(false, tmproto.Header{}), testMsg)
	require.Error(t, err)

	expectedErrMsg := fmt.Sprintf("unrecognized %s message type", types.ModuleName)
	require.ErrorContains(t, err, expectedErrMsg)
}
