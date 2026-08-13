package ante_test

import (
	"fmt"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltestutil "github.com/cosmos/cosmos-sdk/x/accesscontrol/testutil"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

type BadAnteDecoratorOne struct{}

func (ad BadAnteDecoratorOne) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	return ctx, fmt.Errorf("some error")
}

func (ad BadAnteDecoratorOne) AnteDeps(txDeps []accesscontrol.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []accesscontrol.AccessOperation, err error) {
	return next(txDeps, tx, txIndex)
}

func (suite *AnteTestSuite) TestEnsureMempoolFees() {
	suite.SetupTest(true) // setup
	suite.txBuilder = suite.clientCtx.TxConfig.NewTxBuilder()

	feeParam := suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(
		sdk.NewDecCoinFromDec("atom", sdk.NewDec(0)),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(0)),
	)
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)

	mfd := ante.NewDeductFeeDecorator(suite.app.AccountKeeper, suite.app.BankKeeper, suite.app.FeeGrantKeeper, suite.app.ParamsKeeper, nil)
	antehandler, _ := sdk.ChainAnteDecorators(sdk.DefaultWrappedAnteDecorator(mfd))

	// keys and addresses
	priv1, _, addr1 := testdata.KeyTestPubAddr()
	coins := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(300)))
	err := simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr1, coins)
	suite.Require().NoError(err)

	// msg and signatures
	msg := testdata.NewTestMsg(addr1)
	feeAmount := testdata.NewTestFeeAmount()
	gasLimit := uint64(15)
	suite.Require().NoError(suite.txBuilder.SetMsgs(msg))
	suite.txBuilder.SetFeeAmount(feeAmount)
	suite.txBuilder.SetGasLimit(gasLimit)

	privs, accNums, accSeqs := []cryptotypes.PrivKey{priv1}, []uint64{0}, []uint64{0}
	tx, err := suite.CreateTestTx(privs, accNums, accSeqs, suite.ctx.ChainID())
	suite.Require().NoError(err)

	// Set high gas price so standard test fee fails
	atomPrice := sdk.NewDecCoinFromDec("usei", sdk.NewDec(20))
	highGasPrice := []sdk.DecCoin{atomPrice}
	suite.ctx = suite.ctx.WithMinGasPrices(highGasPrice)

	// Set IsCheckTx to true
	suite.ctx = suite.ctx.WithIsCheckTx(true)

	// antehandler errors with insufficient fees
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().NotNil(err, "Decorator should have errored on too low fee for local gasPrice")

	// Set IsCheckTx to false
	suite.ctx = suite.ctx.WithIsCheckTx(false)

	// antehandler should not error since we do not check minGasPrice in DeliverTx
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "MempoolFeeDecorator returned error in DeliverTx")

	// Set IsCheckTx back to true for testing sufficient mempool fee
	suite.ctx = suite.ctx.WithIsCheckTx(true)

	atomPrice = sdk.NewDecCoinFromDec("usei", sdk.NewDec(0).Quo(sdk.NewDec(100000)))
	lowGasPrice := []sdk.DecCoin{atomPrice}
	suite.ctx = suite.ctx.WithMinGasPrices(lowGasPrice)

	newCtx, err := antehandler(suite.ctx, tx, false)

	suite.Require().Nil(err, "Decorator should not have errored on fee higher than local gasPrice")
	// Priority is the smallest gas price amount in any denom. Since we have only 1 gas price
	// of 10atom, the priority here is 10.
	suite.Equal(int64(10), newCtx.Priority())
}

func (suite *AnteTestSuite) TestDeductFees() {
	suite.SetupTest(false) // setup
	suite.txBuilder = suite.clientCtx.TxConfig.NewTxBuilder()

	// keys and addresses
	priv1, _, addr1 := testdata.KeyTestPubAddr()

	// msg and signatures
	msg := testdata.NewTestMsg(addr1)
	feeAmount := testdata.NewTestFeeAmount()
	gasLimit := testdata.NewTestGasLimit()
	suite.Require().NoError(suite.txBuilder.SetMsgs(msg))
	suite.txBuilder.SetFeeAmount(feeAmount)
	suite.txBuilder.SetGasLimit(gasLimit)

	privs, accNums, accSeqs := []cryptotypes.PrivKey{priv1}, []uint64{0}, []uint64{0}
	tx, err := suite.CreateTestTx(privs, accNums, accSeqs, suite.ctx.ChainID())
	suite.Require().NoError(err)

	// Set account with insufficient funds
	acc := suite.app.AccountKeeper.NewAccountWithAddress(suite.ctx, addr1)
	suite.app.AccountKeeper.SetAccount(suite.ctx, acc)
	coins := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))
	err = simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr1, coins)
	suite.Require().NoError(err)

	dfd := ante.NewDeductFeeDecorator(suite.app.AccountKeeper, suite.app.BankKeeper, nil, suite.app.ParamsKeeper, nil)
	antehandler, _ := sdk.ChainAnteDecorators(sdk.DefaultWrappedAnteDecorator(dfd))

	_, err = antehandler(suite.ctx, tx, false)

	suite.Require().NotNil(err, "Tx did not error when fee payer had insufficient funds")

	// Set account with sufficient funds
	suite.app.AccountKeeper.SetAccount(suite.ctx, acc)
	err = simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr1, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(200))))
	suite.Require().NoError(err)

	_, err = antehandler(suite.ctx, tx, false)

	suite.Require().Nil(err, "Tx errored after account has been set with sufficient funds")
}

func (suite *AnteTestSuite) TestLazySendToModuleAccount() {
	suite.SetupTest(false) // setup
	suite.txBuilder = suite.clientCtx.TxConfig.NewTxBuilder()

	// keys and addresses
	priv1, _, addr1 := testdata.KeyTestPubAddr()

	// msg and signatures
	msg := testdata.NewTestMsg(addr1)
	feeAmount := testdata.NewTestFeeAmount()
	gasLimit := testdata.NewTestGasLimit()
	suite.Require().NoError(suite.txBuilder.SetMsgs(msg))
	suite.txBuilder.SetFeeAmount(feeAmount)
	suite.txBuilder.SetGasLimit(gasLimit)

	privs, accNums, accSeqs := []cryptotypes.PrivKey{priv1}, []uint64{0}, []uint64{0}
	tx, err := suite.CreateTestTx(privs, accNums, accSeqs, suite.ctx.ChainID())
	suite.Require().NoError(err)

	// Set account with insufficient funds
	acc := suite.app.AccountKeeper.NewAccountWithAddress(suite.ctx, addr1)
	suite.app.AccountKeeper.SetAccount(suite.ctx, acc)
	err = simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr1, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(900))))
	suite.Require().NoError(err)

	feeCollectorAcc := suite.app.AccountKeeper.GetModuleAccount(suite.ctx, types.FeeCollectorName)
	expectedFeeCollectorBalance := suite.app.BankKeeper.GetBalance(suite.ctx, feeCollectorAcc.GetAddress(), "usei")

	dfd := ante.NewDeductFeeDecorator(suite.app.AccountKeeper, suite.app.BankKeeper, nil, suite.app.ParamsKeeper, nil)
	antehandler, _ := sdk.ChainAnteDecorators(dfd)

	// Set account with sufficient funds
	antehandler(suite.ctx, tx, false)
	_, err = antehandler(suite.ctx, tx, false)

	suite.Require().Nil(err, "Tx errored after account has been set with sufficient funds")

	// Fee Collector actual account balance should not have increased
	resultFeeCollectorBalance := suite.app.BankKeeper.GetBalance(suite.ctx, feeCollectorAcc.GetAddress(), "usei")
	suite.Assert().Equal(
		expectedFeeCollectorBalance,
		resultFeeCollectorBalance,
	)

	// Fee Collector actual account balance deposit coins into the fee collector account
	suite.app.BankKeeper.WriteDeferredBalances(suite.ctx)

	depositFeeCollectorBalance := suite.app.BankKeeper.GetBalance(suite.ctx, feeCollectorAcc.GetAddress(), "usei")

	expectedAtomFee := feeAmount.AmountOf("usei")

	suite.Assert().Equal(
		// Called antehandler twice, expect fees to be deducted twice
		expectedFeeCollectorBalance.Add(sdk.NewCoin("usei", expectedAtomFee)).Add(sdk.NewCoin("usei", expectedAtomFee)),
		depositFeeCollectorBalance,
	)
}

func (suite *AnteTestSuite) createTestTxWithGas(msg *testdata.TestMsg, fee, gasLimit uint64, priv cryptotypes.PrivKey, denom string) (sdk.Tx, error) {
	feeAmount := sdk.NewCoins(sdk.NewInt64Coin(denom, int64(fee)))
	suite.Require().NoError(suite.txBuilder.SetMsgs(msg))
	suite.txBuilder.SetFeeAmount(feeAmount)
	suite.txBuilder.SetGasLimit(gasLimit)

	privs, accNums, accSeqs := []cryptotypes.PrivKey{priv}, []uint64{0}, []uint64{0}
	return suite.CreateTestTx(privs, accNums, accSeqs, suite.ctx.ChainID())
}

func (suite *AnteTestSuite) TestGlobalMinimumFees() {
	suite.SetupTest(true) // setup
	suite.txBuilder = suite.clientCtx.TxConfig.NewTxBuilder()
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, paramstypes.DefaultGenesis().GetFeesParams())

	mfd := ante.NewDeductFeeDecorator(suite.app.AccountKeeper, suite.app.BankKeeper, suite.app.FeeGrantKeeper, suite.app.ParamsKeeper, nil)
	antehandler, _ := sdk.ChainAnteDecorators(sdk.DefaultWrappedAnteDecorator(mfd))

	// keys and addresses
	priv1, _, addr1 := testdata.KeyTestPubAddr()
	coins := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(3000000000)))
	err := simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr1, coins)
	suite.Require().NoError(err)

	// msg and signatures
	msg := testdata.NewTestMsg(addr1)
	tx, err := suite.createTestTxWithGas(msg, 1500, 15000, priv1, "usei")
	suite.Require().NoError(err)

	// Global minimum gas price is zero, but transaction fee is non-zero
	feeParam := suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("usei", sdk.NewDec(0)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("usei", sdk.NewDec(1000000000))})
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Global minimum gas price is non-zero, but transaction fee is zero
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("usei", sdk.NewDec(1000000000)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("usei", sdk.NewDec(0))})
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Global minimum gas price is non-zero, and transaction fee is non-zero but less than global minimum gas price
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("usei", sdk.NewDec(100)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("usei", sdk.NewDec(1))})
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Global minimum gas price is non-zero, and transaction fee is non-zero but less than global minimum gas price
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("usei", sdk.NewDec(1)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("usei", sdk.NewDec(100))})
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Global minimum gas price is non-zero, and transaction fee is equal to global minimum gas price
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("usei", sdk.NewDec(50)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("usei", sdk.NewDec(50))})
	// 750000 = 15000 * 50
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1, "usei")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored on equal fee for global gasPrice")

	// Global minimum gas price is non-zero, and transaction fee is greater than global minimum gas price
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("usei", sdk.NewDec(1)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("usei", sdk.NewDec(50))})
	// 750000 = 15000 * 50
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1, "usei")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored on equal fee for global gasPrice")

	// Global minimum gas price is non-zero, and transaction fee is less than global minimum gas price
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("usei", sdk.NewDec(50)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("usei", sdk.NewDec(1))})
	// 750000 = 15000 * 50
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1, "usei")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored on equal fee for global gasPrice")
}

func (suite *AnteTestSuite) TestDeductFeeDependency() {
	suite.SetupTest(false) // setup
	suite.txBuilder = suite.clientCtx.TxConfig.NewTxBuilder()

	// keys and addresses
	priv1, _, addr1 := testdata.KeyTestPubAddr()
	_, _, addr2 := testdata.KeyTestPubAddr()

	// msg and signatures
	msg := testdata.NewTestMsg(addr1)
	feeAmount := testdata.NewTestFeeAmount()
	gasLimit := testdata.NewTestGasLimit()
	suite.Require().NoError(suite.txBuilder.SetMsgs(msg))
	suite.txBuilder.SetFeeGranter(addr2)
	suite.txBuilder.SetFeeAmount(feeAmount)
	suite.txBuilder.SetGasLimit(gasLimit)

	privs, accNums, accSeqs := []cryptotypes.PrivKey{priv1}, []uint64{0}, []uint64{0}
	tx, err := suite.CreateTestTx(privs, accNums, accSeqs, suite.ctx.ChainID())
	suite.Require().NoError(err)

	dfd := ante.NewDeductFeeDecorator(suite.app.AccountKeeper, suite.app.BankKeeper, suite.app.FeeGrantKeeper, suite.app.ParamsKeeper, nil)

	antehandler, decorator := sdk.ChainAnteDecorators(dfd)

	// Set account with sufficient funds
	acc := suite.app.AccountKeeper.NewAccountWithAddress(suite.ctx, addr1)
	suite.app.AccountKeeper.SetAccount(suite.ctx, acc)
	err = simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr1, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(200))))
	suite.Require().NoError(err)

	acc2 := suite.app.AccountKeeper.NewAccountWithAddress(suite.ctx, addr2)
	suite.app.AccountKeeper.SetAccount(suite.ctx, acc2)
	err = simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr2, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(200))))
	suite.Require().NoError(err)
	// create fee grant
	err = suite.app.FeeGrantKeeper.GrantAllowance(suite.ctx, addr2, addr1, &feegrant.BasicAllowance{})
	suite.Require().NoError(err)

	msgValidator := sdkacltypes.NewMsgValidator(acltestutil.TestingStoreKeyToResourceTypePrefixMap)
	suite.ctx = suite.ctx.WithMsgValidator(msgValidator)
	suite.ctx = suite.ctx.WithTxIndex(1)
	ms := suite.ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	suite.ctx = suite.ctx.WithMultiStore(msCache)

	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Tx errored after account has been set with sufficient funds")

	newDeps, err := decorator([]sdkacltypes.AccessOperation{}, tx, 1)
	suite.Require().NoError(err)

	storeAccessOpEvents := msCache.GetEvents()

	missingAccessOps := suite.ctx.MsgValidator().ValidateAccessOperations(newDeps, storeAccessOpEvents)
	suite.Require().Equal(0, len(missingAccessOps))
}

func (suite *AnteTestSuite) TestMultipleGlobalMinimumFees() {
	suite.SetupTest(true) // setup
	suite.txBuilder = suite.clientCtx.TxConfig.NewTxBuilder()
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, paramstypes.DefaultGenesis().GetFeesParams())

	mfd := ante.NewDeductFeeDecorator(suite.app.AccountKeeper, suite.app.BankKeeper, suite.app.FeeGrantKeeper, suite.app.ParamsKeeper, nil)
	antehandler, _ := sdk.ChainAnteDecorators(sdk.DefaultWrappedAnteDecorator(mfd))

	// keys and addresses
	priv1, _, addr1 := testdata.KeyTestPubAddr()
	coins := sdk.NewCoins(sdk.NewCoin("atom", sdk.NewInt(3000000000)), sdk.NewCoin("usei", sdk.NewInt(3000000000)))
	err := simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr1, coins)
	suite.Require().NoError(err)

	// msg and signatures
	msg := testdata.NewTestMsg(addr1)

	// Test case: the fee provided is less than the global minimum gas prices
	feeParam := suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("usei", sdk.NewDec(100)))
	feeParam.AllowedFeeDenoms = []string{"atom"}
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("usei", sdk.NewDec(100))})
	// 750000 < 15000 * 100
	tx, _ := suite.createTestTxWithGas(msg, 750000, 15000, priv1, "usei")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Test case: the fee provided is less than the required minimum gas prices for the specific transaction
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("atom", sdk.NewDec(10)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(100)), sdk.NewDecCoinFromDec("usei", sdk.NewDec(1))})
	// 750000 < 15000 * 100
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1, "atom")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Test case: the fee provided is less than both the global and the transaction-specific minimum gas prices
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("atom", sdk.NewDec(100)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(200)), sdk.NewDecCoinFromDec("usei", sdk.NewDec(1))})
	// 750000 < 15000 * 200
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1, "atom")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Test case: the fee provided in all denominations is less than the global minimum gas prices
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(
		sdk.NewDecCoinFromDec("atom", sdk.NewDec(100)),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(10)),
	)
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{
		sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)), // less than global minimum
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(1)),  // less than global minimum
	})
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1, "atom")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Test case: the fee provided in all denominations is less than the transaction-specific minimum gas prices
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("usei", sdk.NewDec(50)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)), sdk.NewDecCoinFromDec("usei", sdk.NewDec(1))})
	// 750000 = 15000 * 50
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1, "atom")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored on equal fee for global gasPrice")

	// Test case: enough fee based on max local
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("usei", sdk.NewDec(1)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)), sdk.NewDecCoinFromDec("usei", sdk.NewDec(1))})
	// 750000 = 15000 * 50
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1, "usei")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored on equal fee for global gasPrice")

	// Test case: enough fee based on max global
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("usei", sdk.NewDec(50)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(1)), sdk.NewDecCoinFromDec("usei", sdk.NewDec(5))})
	// 750000 = 15000 * 50
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1, "usei")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored on equal fee for global gasPrice")

	// Test case: the fee provided in one of the denominations is equal to the global minimum gas prices
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(
		sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(2)),
	)
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{
		sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(2)), // equal to global minimum
	})
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1, "atom")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored, fee is sufficient in 'usei' denomination")

	// Test case: the fee provided in all denominations is greater than the global minimum gas prices
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(
		sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(2)),
	)
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{
		sdk.NewDecCoinFromDec("atom", sdk.NewDec(100)), // greater than global minimum
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(10)),  // greater than global minimum
	})
	tx, _ = suite.createTestTxWithGas(msg, 1500000, 15000, priv1, "atom")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored, fee is sufficient in all denominations")

	// Test case: fee provided in 'usei' denomination is greater than the global minimum gas prices
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(
		sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(2)),
	)
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{
		sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(10)), // greater than global minimum
	})
	tx, _ = suite.createTestTxWithGas(msg, 1500000, 15000, priv1, "usei")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored, fee is sufficient in 'usei' denomination")

	// Test case: fee provided in 'usei' denomination is less than the global minimum gas prices
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(
		sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(10)),
	)
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{
		sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(5)), // less than global minimum
	})
	tx, _ = suite.createTestTxWithGas(msg, 100000, 15000, priv1, "usei")
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")
}
