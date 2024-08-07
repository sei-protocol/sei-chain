package contract_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestTransferRentFromDexToCollector(t *testing.T) {
	preRents := map[string]uint64{"abc": 100, "def": 50}
	postRents := map[string]uint64{"abc": 70}

	// expected total is (100 - 70) + 50 = 80
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	bankkeeper := testApp.BankKeeper
	err := bankkeeper.MintCoins(ctx, minttypes.ModuleName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100))))
	require.Nil(t, err)
	err = bankkeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, types.ModuleName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100))))
	require.Nil(t, err)
	contract.TransferRentFromDexToCollector(ctx, bankkeeper, preRents, postRents)
	dexBalance := bankkeeper.GetBalance(ctx, testApp.AccountKeeper.GetModuleAddress(types.ModuleName), "usei")
	require.Equal(t, int64(20), dexBalance.Amount.Int64())
	collectorBalance := bankkeeper.GetBalance(ctx, testApp.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName), "usei")
	require.Equal(t, int64(80), collectorBalance.Amount.Int64())
}

func TestOrderMatchingRunnablePanicHandler(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	require.NotPanics(t, func() {
		contract.OrderMatchingRunnable(context.Background(), ctx, nil, nil, types.ContractInfoV2{}, nil)
	})
}
