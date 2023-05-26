package ante_test

import (
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

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
	coins := sdk.NewCoins(sdk.NewCoin("atom", sdk.NewInt(300)))
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
	atomPrice := sdk.NewDecCoinFromDec("atom", sdk.NewDec(20))
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

	atomPrice = sdk.NewDecCoinFromDec("atom", sdk.NewDec(0).Quo(sdk.NewDec(100000)))
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
	coins := sdk.NewCoins(sdk.NewCoin("atom", sdk.NewInt(10)))
	err = simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr1, coins)
	suite.Require().NoError(err)

	dfd := ante.NewDeductFeeDecorator(suite.app.AccountKeeper, suite.app.BankKeeper, nil, suite.app.ParamsKeeper, nil)
	antehandler, _ := sdk.ChainAnteDecorators(sdk.DefaultWrappedAnteDecorator(dfd))

	_, err = antehandler(suite.ctx, tx, false)

	suite.Require().NotNil(err, "Tx did not error when fee payer had insufficient funds")

	// Set account with sufficient funds
	suite.app.AccountKeeper.SetAccount(suite.ctx, acc)
	err = simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr1, sdk.NewCoins(sdk.NewCoin("atom", sdk.NewInt(200))))
	suite.Require().NoError(err)

	_, err = antehandler(suite.ctx, tx, false)

	suite.Require().Nil(err, "Tx errored after account has been set with sufficient funds")
}

func (suite *AnteTestSuite) TestLazySendToModuleAccoutn() {
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
	err = simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr1, sdk.NewCoins(sdk.NewCoin("atom", sdk.NewInt(300))))
	suite.Require().NoError(err)

	feeCollectorAcc := suite.app.AccountKeeper.GetModuleAccount(suite.ctx, types.FeeCollectorName)
	expectedFeeCollectorBalance := suite.app.BankKeeper.GetBalance(suite.ctx, feeCollectorAcc.GetAddress(), "atom")

	dfd := ante.NewDeductFeeDecorator(suite.app.AccountKeeper, suite.app.BankKeeper, nil, suite.app.ParamsKeeper, nil)
	antehandler, _ := sdk.ChainAnteDecorators(sdk.DefaultWrappedAnteDecorator(dfd))

	// Set account with sufficient funds
	antehandler(suite.ctx, tx, false)
	_, err = antehandler(suite.ctx, tx, false)

	suite.Require().Nil(err, "Tx errored after account has been set with sufficient funds")

	// Fee Collector actual account balance should not have increased
	resultFeeCollectorBalance := suite.app.BankKeeper.GetBalance(suite.ctx, feeCollectorAcc.GetAddress(), "atom")
	suite.Assert().Equal(
		expectedFeeCollectorBalance,
		resultFeeCollectorBalance,
	)

	// Fee Collector actual account balance deposit coins into the fee collector account
	suite.app.BankKeeper.WriteDeferredDepositsToModuleAccounts(suite.ctx)

	depositFeeCollectorBalance := suite.app.BankKeeper.GetBalance(suite.ctx, feeCollectorAcc.GetAddress(), "atom")

	expectedAtomFee := feeAmount.AmountOf("atom")

	suite.Assert().Equal(
		// Called antehandler twice, expect fees to be deducted twice
		expectedFeeCollectorBalance.Add(sdk.NewCoin("atom", expectedAtomFee)).Add(sdk.NewCoin("atom", expectedAtomFee)),
		depositFeeCollectorBalance,
	)
}

func (suite *AnteTestSuite) createTestTxWithGas(msg *testdata.TestMsg, fee, gasLimit uint64, priv cryptotypes.PrivKey) (sdk.Tx, error) {
	feeAmount := sdk.NewCoins(sdk.NewInt64Coin("atom", int64(fee)))
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
	coins := sdk.NewCoins(sdk.NewCoin("atom", sdk.NewInt(3000000000)))
	err := simapp.FundAccount(suite.app.BankKeeper, suite.ctx, addr1, coins)
	suite.Require().NoError(err)

	// msg and signatures
	msg := testdata.NewTestMsg(addr1)
	tx, err := suite.createTestTxWithGas(msg, 1500, 15000, priv1)
	suite.Require().NoError(err)

	// Global minimum gas price is zero, but transaction fee is non-zero
	feeParam := suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("atom", sdk.NewDec(0)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(1000000000))})
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Global minimum gas price is non-zero, but transaction fee is zero
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("atom", sdk.NewDec(1000000000)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(0))})
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Global minimum gas price is non-zero, and transaction fee is non-zero but less than global minimum gas price
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("atom", sdk.NewDec(100)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(1))})
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Global minimum gas price is non-zero, and transaction fee is non-zero but less than global minimum gas price
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("atom", sdk.NewDec(1)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(100))})
	_, err = antehandler(suite.ctx, tx, false)
	suite.Assert().ErrorContains(err, "insufficient fees")

	// Global minimum gas price is non-zero, and transaction fee is equal to global minimum gas price
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(50))})
	// 750000 = 15000 * 50
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1)
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored on equal fee for global gasPrice")

	// Global minimum gas price is non-zero, and transaction fee is greater than global minimum gas price
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("atom", sdk.NewDec(1)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(50))})
	// 750000 = 15000 * 50
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1)
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored on equal fee for global gasPrice")

	// Global minimum gas price is non-zero, and transaction fee is less than global minimum gas price
	feeParam = suite.app.ParamsKeeper.GetFeesParams(suite.ctx)
	feeParam.GlobalMinimumGasPrices = sdk.NewDecCoins(sdk.NewDecCoinFromDec("atom", sdk.NewDec(50)))
	suite.app.ParamsKeeper.SetFeesParams(suite.ctx, feeParam)
	suite.ctx = suite.ctx.WithMinGasPrices([]sdk.DecCoin{sdk.NewDecCoinFromDec("atom", sdk.NewDec(1))})
	// 750000 = 15000 * 50
	tx, _ = suite.createTestTxWithGas(msg, 750000, 15000, priv1)
	_, err = antehandler(suite.ctx, tx, false)
	suite.Require().Nil(err, "Decorator should not have errored on equal fee for global gasPrice")
}
