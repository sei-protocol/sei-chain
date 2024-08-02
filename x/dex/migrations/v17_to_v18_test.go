package migrations_test

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/sei-protocol/sei-chain/app"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestMigrate17to18(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)

	testAddr := sdk.MustAccAddressFromBech32(keepertest.TestAccount)

	testWrapper.FundAcc(testAddr, sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(100000))))
	testWrapper.FundAcc(testAddr, sdk.NewCoins(sdk.NewCoin("ueth", sdk.NewInt(100))))

	dexkeeper, ctx := testWrapper.App.DexKeeper, testWrapper.Ctx
	bal := dexkeeper.BankKeeper.GetBalance(ctx, testAddr, sdk.MustGetBaseDenom())
	require.Equal(t, int64(100000), bal.Amount.Int64())
	// add contract rent
	rentAmt := int64(10000)
	err := dexkeeper.BankKeeper.SendCoins(ctx, testAddr, dexkeeper.AccountKeeper.GetModuleAddress(types.ModuleName), sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(rentAmt))))
	require.NoError(t, err)

	contract := &types.ContractInfoV2{ContractAddr: keepertest.TestContract, Creator: keepertest.TestAccount, RentBalance: uint64(rentAmt)}
	err = dexkeeper.SetContract(ctx, contract)
	require.NoError(t, err)
	// add some balance to the module just bc
	err = dexkeeper.BankKeeper.SendCoins(ctx, testAddr, dexkeeper.AccountKeeper.GetModuleAddress(types.ModuleName), sdk.NewCoins(sdk.NewCoin("ueth", sdk.NewInt(10)), sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(1000))))
	require.NoError(t, err)

	supplyUseiInitial := dexkeeper.BankKeeper.GetSupply(ctx, sdk.MustGetBaseDenom())

	// do migration
	err = migrations.V17ToV18(ctx, dexkeeper)
	require.NoError(t, err)

	// user refunded rent now has 99000usei and 90ueth
	bals := dexkeeper.BankKeeper.GetAllBalances(ctx, testAddr)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("ueth", sdk.NewInt(90)), sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(99000))), bals)
	// feecollector gets 1000usei
	bals = dexkeeper.BankKeeper.GetAllBalances(ctx, dexkeeper.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(1000))), bals)
	// ueth supply decreased due to burn
	supply := dexkeeper.BankKeeper.GetSupply(ctx, "ueth")
	require.Equal(t, sdk.NewInt(90), supply.Amount)
	// usei supply unchanged
	supplyUseiFinal := dexkeeper.BankKeeper.GetSupply(ctx, sdk.MustGetBaseDenom())
	require.Equal(t, supplyUseiInitial, supplyUseiFinal)

}
