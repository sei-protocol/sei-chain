package keeper_test

import (
	"fmt"
	"math"
	"math/big"

	"github.com/coinbase/kryptology/pkg/core/curves"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

var teg *elgamal.TwistedElGamal

// Tests the InitializeAccount method of the MsgServer.
func (suite *KeeperTestSuite) TestMsgServer_InitializeAccountBasic() {
	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)

	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Test empty request
	req := &types.MsgInitializeAccount{
		FromAddress: testAddr.String(),
		Denom:       DefaultTestDenom,
	}
	_, err := suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error initializing using struct with missing fields")

	// Happy Path
	initializeStruct, _ := types.NewInitializeAccount(testAddr.String(), DefaultTestDenom, *testPk)

	req = types.NewMsgInitializeAccountProto(initializeStruct)
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error initializing valid account state")

	// Check that account exists in storage now
	account, exists := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	suite.Require().True(exists, "Account should exist after successful initialization")

	// Check that account state matches the one submitted.
	initializeStructPubkey := *initializeStruct.Pubkey
	suite.Require().Equal(initializeStructPubkey.ToAffineCompressed(), account.PublicKey.ToAffineCompressed(), "Public keys should match")
	suite.Require().Equal(uint16(0), account.PendingBalanceCreditCounter, "PendingBalanceCreditCounter should be 0")
	suite.Require().Equal(initializeStruct.DecryptableBalance, account.DecryptableAvailableBalance, "DecryptableAvailableBalance should match")
	suite.Require().True(initializeStruct.PendingBalanceLo.C.Equal(account.PendingBalanceLo.C), "PendingBalanceLo.C should match")
	suite.Require().True(initializeStruct.PendingBalanceLo.D.Equal(account.PendingBalanceLo.D), "PendingBalanceLo.D should match")
	suite.Require().True(initializeStruct.PendingBalanceHi.C.Equal(account.PendingBalanceHi.C), "PendingBalanceHi.C should match")
	suite.Require().True(initializeStruct.PendingBalanceHi.D.Equal(account.PendingBalanceHi.D), "PendingBalanceHi.D should match")
	suite.Require().True(initializeStruct.AvailableBalance.C.Equal(account.AvailableBalance.C), "AvailableBalance.C should match")
	suite.Require().True(initializeStruct.AvailableBalance.D.Equal(account.AvailableBalance.D), "AvailableBalance.D should match")

	// Try to initialize the account again - this should produce an error
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().EqualError(err, "account already exists: invalid request")

	// Try to initialize another account for a different denom
	otherDenom := DefaultOtherDenom
	initializeStruct, _ = types.NewInitializeAccount(testAddr.String(), otherDenom, *testPk)
	req = types.NewMsgInitializeAccountProto(initializeStruct)
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error initializing valid account state on a different denom")

	// Check that other account exists in storage as well
	_, exists = suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), otherDenom)
	suite.Require().True(exists, "Account should exist after successful initialization")

	// Check that initial account still exists independently and is unchanged.
	accountAgain, exists := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	suite.Require().True(exists, "Account should still exist")
	suite.Require().Equal(account, accountAgain, "Account should be unchanged")
}

// Tests scenarios in which a user tries to Initialize an account with a pubkey that doesn't match the proofs.
func (suite *KeeperTestSuite) TestMsgServer_InitializeAccountModifyPubkey() {
	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)

	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Test that modifying the PublicKey without modifying the proof fails the PubkeyValidityProof test.
	initializeStruct, _ := types.NewInitializeAccount(testAddr.String(), DefaultTestDenom, *testPk)

	// Modify the pubkey used after.
	otherPk, _ := crypto.GenerateKey()
	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}
	otherKeyPair, _ := utils.GetElGamalKeyPair(*otherPk, DefaultTestDenom)
	initializeStruct.Pubkey = &otherKeyPair.PublicKey

	req := types.NewMsgInitializeAccountProto(initializeStruct)
	_, err := suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error initializing account with mismatched pubkey")
	suite.Require().ErrorContains(err, "invalid public key")

	// Check that account does not exist in storage
	_, exists := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	suite.Require().False(exists, "Account should not exist after failed initialization")

	// Now try modifying the Pubkey Validity Proof as well.
	// This should still throw an error as the ZeroBalanceProofs will fail, since the balances were generated with the original Pubkey.
	otherKeyPairProof, _ := zkproofs.NewPubKeyValidityProof(otherKeyPair.PublicKey, otherKeyPair.PrivateKey)
	initializeStruct.Proofs.PubkeyValidityProof = otherKeyPairProof
	req = types.NewMsgInitializeAccountProto(initializeStruct)
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error initializing account with mismatched pubkey despite valid PubkeyValidityProof")
	suite.Require().ErrorContains(err, "invalid pending balance lo")
}

// Tests scenarios where the client tries to initialize an account with balances that are not zero.
func (suite *KeeperTestSuite) TestMsgServer_InitializeAccountModifyBalances() {
	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)

	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Create a ciphertext on a non zero value.
	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}
	keyPair, _ := utils.GetElGamalKeyPair(*testPk, DefaultTestDenom)
	nonZeroCiphertext, _, _ := teg.Encrypt(keyPair.PublicKey, big.NewInt(100000))

	// Generate a 'ZeroBalanceProof' on the non-zero balance
	// Proof generation should be successful, but the initialization should still fail.
	zeroBalanceProof, err := zkproofs.NewZeroBalanceProof(keyPair, nonZeroCiphertext)
	suite.Require().NoError(err, "Should not have error creating zero balance proof")

	// Test that submitting an initialization request with non-zero balances will fail.
	initializeStruct, err := types.NewInitializeAccount(testAddr.String(), DefaultTestDenom, *testPk)
	suite.Require().NoError(err, "Should not have error creating account state")

	// Modify the available balance. This should fail the zero balance check for the available balance.
	initializeStruct.AvailableBalance = nonZeroCiphertext
	req := types.NewMsgInitializeAccountProto(initializeStruct)
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error initializing account with non-zero available balance")
	suite.Require().ErrorContains(err, "invalid available balance")

	// Try modifying the proof as well.
	initializeStruct.Proofs.ZeroAvailableBalanceProof = zeroBalanceProof
	req = types.NewMsgInitializeAccountProto(initializeStruct)

	// ZeroBalanceProof validation on ZeroAvailableBalance should fail.
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error initializing account with non-zero available balance despite generating a proof on it.")
	suite.Require().ErrorContains(err, "invalid available balance")

	// Repeat tests for PendingAmountLo
	initializeStruct, _ = types.NewInitializeAccount(testAddr.String(), DefaultTestDenom, *testPk)

	// Modify the pending balance lo. This should fail the zero balance check for the pendingBalanceLo.
	initializeStruct.PendingBalanceLo = nonZeroCiphertext
	req = types.NewMsgInitializeAccountProto(initializeStruct)
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error initializing account with non-zero pending balance lo")
	suite.Require().ErrorContains(err, "invalid pending balance lo")

	// Try modifying the proof as well.
	initializeStruct.Proofs.ZeroPendingBalanceLoProof = zeroBalanceProof
	req = types.NewMsgInitializeAccountProto(initializeStruct)

	// ZeroBalanceProof validation on PendingBalanceLo should fail.
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error initializing account with non-zero pending balance lo despite generating a proof on it.")
	suite.Require().ErrorContains(err, "invalid pending balance lo")

	// Repeat tests for PendingAmountHi
	initializeStruct, _ = types.NewInitializeAccount(testAddr.String(), DefaultTestDenom, *testPk)

	// Modify the pending balance hi. This should fail the zero balance check for the pendingBalanceHi.
	initializeStruct.PendingBalanceHi = nonZeroCiphertext
	req = types.NewMsgInitializeAccountProto(initializeStruct)
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error initializing account with non-zero pending balance hi")
	suite.Require().ErrorContains(err, "invalid pending balance hi")

	// Try modifying the proof as well.
	initializeStruct.Proofs.ZeroPendingBalanceHiProof = zeroBalanceProof
	req = types.NewMsgInitializeAccountProto(initializeStruct)

	// ZeroBalanceProof validation on PendingBalanceHi should fail.
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error initializing account with non-zero pending balance hi despite generating a proof on it.")
	suite.Require().ErrorContains(err, "invalid pending balance hi")
}

// Tests scenarios where the client tries to initialize an account on a denom that doesn't exist.
func (suite *KeeperTestSuite) TestMsgServer_InitializeAccountDenomDoesnExist() {
	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)

	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	nonExistentDenom := "nonExistentDenom"

	initialize, err := types.NewInitializeAccount(testAddr.String(), nonExistentDenom, *testPk)
	req := types.NewMsgInitializeAccountProto(initialize)
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)

	// Test that submitting an initialization request for a non-existent denom will fail.
	suite.Require().Error(err, "Should not be able to create denom on non-existent denom")
	suite.Require().ErrorContains(err, "denom does not exist")
}

// Validate alternate scenarios that are technically allowed, but will cause incompatibility with the client.
func (suite *KeeperTestSuite) TestMsgServer_InitializeAccountAlternateHappyPaths() {
	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)

	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Test that tampering with the denom will still lead to a successful initialization.
	// However, since the client generates the keys based on the denom,
	// all the fields will be encrypted with a different PublicKe than the one the client would use.
	// As a result, the client will not be able to use the account.
	initializeStruct, _ := types.NewInitializeAccount(testAddr.String(), DefaultTestDenom, *testPk)

	// Modify the denom
	otherDenom := DefaultOtherDenom
	initializeStruct.Denom = otherDenom
	req := types.NewMsgInitializeAccountProto(initializeStruct)
	_, err := suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error initializing account with different denom")

	_, exists := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), otherDenom)
	suite.Require().True(exists, "Account should exist after successful initialization")

	// Test that modifying the decryptableBalance will still lead to a successful initialization.
	// The decryptable balance is just a convenience feature that allows the user to keep track of their balance (AvailableBalance)
	// Corrupting this field will not affect the account's functionality, but will render it unusable by the client.
	// If the user loses track of their balance, they may be unable to recover their funds from the account since the AvailableBalance may not be decryptable.
	initializeStruct, _ = types.NewInitializeAccount(testAddr.String(), DefaultTestDenom, *testPk)
	initializeStruct.DecryptableBalance = "corrupted"
	req = types.NewMsgInitializeAccountProto(initializeStruct)
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error initializing account despite corrupted decryptable balance")

	_, exists = suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	suite.Require().True(exists, "Account should exist after successful initialization")
}

// Tests that the InitializeAccount method fails when the feature is disabled.
func (suite *KeeperTestSuite) TestMsgServer_InitializeAccountFeatureDisabled() {
	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)

	// Disable the confidential tokens module via params
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})
	params := types.DefaultParams()
	params.EnableCtModule = false
	suite.App.ConfidentialTransfersKeeper.SetParams(suite.Ctx, params)

	initialize, err := types.NewInitializeAccount(testAddr.String(), DefaultTestDenom, *testPk)
	req := types.NewMsgInitializeAccountProto(initialize)
	_, err = suite.msgServer.InitializeAccount(sdk.WrapSDKContext(suite.Ctx), req)

	// Test that submitting an initialization request while module is disabled will fail.
	suite.Require().Error(err, "Should not be able to initialize when feature is disabled")
	suite.Require().ErrorContains(err, "feature is disabled by governance")
}

/// DEPOSIT TESTS

// Tests the Deposit method of the MsgServer.
func (suite *KeeperTestSuite) TestMsgServer_DepositBasic() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}
	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	bankModuleInitialAmount := big.NewInt(1000000000000)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 50, big.NewInt(1000000), big.NewInt(8000), bankModuleInitialAmount)

	// Test empty request
	req := &types.MsgDeposit{
		FromAddress: testAddr.String(),
		Denom:       DefaultTestDenom,
	}
	_, err := suite.msgServer.Deposit(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error depositing without amount")
	suite.Require().ErrorContains(err, "invalid request")

	// Happy path
	depositStruct := types.MsgDeposit{
		FromAddress: testAddr.String(),
		Denom:       DefaultTestDenom,
		Amount:      20000,
	}

	_, err = suite.msgServer.Deposit(sdk.WrapSDKContext(suite.Ctx), &depositStruct)
	suite.Require().NoError(err, "Should not have error depositing with valid request")

	// Check that the account has been updated
	account, _ := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)

	// Check that available balances were not touched by this operation
	suite.Require().Equal(initialState.AvailableBalance.C.ToAffineCompressed(), account.AvailableBalance.C.ToAffineCompressed(), "AvailableBalance.C should not have been touched")
	suite.Require().Equal(initialState.AvailableBalance.D.ToAffineCompressed(), account.AvailableBalance.D.ToAffineCompressed(), "AvailableBalance.D should not have been touched")
	suite.Require().Equal(initialState.DecryptableAvailableBalance, account.DecryptableAvailableBalance, "DecryptableAvailableBalance should not have been touched")

	// Check that pending balance counter increased by 1
	suite.Require().Equal(initialState.PendingBalanceCreditCounter+1, account.PendingBalanceCreditCounter, "PendingBalanceCreditCounter should have increased by 1")

	// Check that pending balances were increased by the deposit amount
	keyPair, _ := utils.GetElGamalKeyPair(*testPk, DefaultTestDenom)
	oldPendingBalancePlaintext, _, _, _ := initialState.GetPendingBalancePlaintext(teg, keyPair)

	newPendingBalancePlaintext, _, _, _ := account.GetPendingBalancePlaintext(teg, keyPair)

	// Check that newPendingBalancePlaintext = oldPendingBalancePlaintext + DepositAmount
	suite.Require().Equal(
		new(big.Int).Add(oldPendingBalancePlaintext, new(big.Int).SetUint64(depositStruct.Amount)).String(),
		newPendingBalancePlaintext.String(),
		"Pending balances should have increased by the deposit amount")

	// Lastly check that the amount in the bank module are changed correctly.
	testAddrBalance := suite.App.BankKeeper.GetBalance(suite.Ctx, testAddr, DefaultTestDenom)
	suite.Require().Equal(new(big.Int).Sub(bankModuleInitialAmount, new(big.Int).SetUint64(depositStruct.Amount)).String(), testAddrBalance.Amount.String(), "Addresses token balance should have decreased by the deposit amount")

	// Check that the amount in the bank module has increased by the deposit amount (Assuming module account balance starts from 0)
	moduleAccount := suite.App.AccountKeeper.GetModuleAccount(suite.Ctx, types.ModuleName)
	moduleBalance := suite.App.BankKeeper.GetBalance(suite.Ctx, moduleAccount.GetAddress(), DefaultTestDenom)
	suite.Require().Equal(depositStruct.Amount, moduleBalance.Amount.Uint64(), "Module account balance should have increased by the deposit amount")

	// Test Large Deposit
	depositStruct = types.MsgDeposit{
		FromAddress: testAddr.String(),
		Denom:       DefaultTestDenom,
		Amount:      math.MaxUint32 + 1,
	}

	_, err = suite.msgServer.Deposit(sdk.WrapSDKContext(suite.Ctx), &depositStruct)
	suite.Require().NoError(err, "Should not have error depositing large amount with valid request")

	// Check that the account has been updated
	updatedAccount, _ := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)

	oldPendingBalancePlaintext = oldPendingBalancePlaintext.Set(newPendingBalancePlaintext)
	newPendingBalancePlaintext, _, _, _ = updatedAccount.GetPendingBalancePlaintext(teg, keyPair)
	suite.Require().Equal(
		new(big.Int).Add(oldPendingBalancePlaintext, new(big.Int).SetUint64(depositStruct.Amount)),
		newPendingBalancePlaintext,
		"Pending balances should have increased by the deposit amount")

	// Check that the amount in the bank module are changed correctly.
	oldTestAddrBalance := testAddrBalance
	testAddrBalance = suite.App.BankKeeper.GetBalance(suite.Ctx, testAddr, DefaultTestDenom)
	suite.Require().Equal(oldTestAddrBalance.Amount.Uint64()-depositStruct.Amount, testAddrBalance.Amount.Uint64(), "Addresses token balance should have decreased by the deposit amount")

	// Check that the amount in the bank module has increased by the deposit amount
	moduleAccount = suite.App.AccountKeeper.GetModuleAccount(suite.Ctx, types.ModuleName)
	oldModuleBalance := moduleBalance
	moduleBalance = suite.App.BankKeeper.GetBalance(suite.Ctx, moduleAccount.GetAddress(), DefaultTestDenom)
	suite.Require().Equal(oldModuleBalance.Amount.Uint64()+depositStruct.Amount, moduleBalance.Amount.Uint64(), "Module account balance should have increased by the deposit amount")
}

// Tests scenario in which the client tries to deposit into an account that has not been initialized.
func (suite *KeeperTestSuite) TestMsgServer_DepositUninitialized() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)
	depositStruct := types.MsgDeposit{
		FromAddress: testAddr.String(),
		Denom:       DefaultTestDenom,
		Amount:      20000,
	}

	// Test depositing into uninitialized account
	_, err := suite.msgServer.Deposit(sdk.WrapSDKContext(suite.Ctx), &depositStruct)
	suite.Require().Error(err, "Should have error depositing into uninitialized account")
}

// Tests scenario in which user has insufficient balances for deposit.
func (suite *KeeperTestSuite) TestMsgServer_DepositInsufficientFunds() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	bankModuleInitialAmount := uint64(1000)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 50, big.NewInt(1000000), big.NewInt(8000), new(big.Int).SetUint64(bankModuleInitialAmount))

	// Create a struct where the deposit amount is greater than the amount of token the user has.
	depositStruct := types.MsgDeposit{
		FromAddress: testAddr.String(),
		Denom:       DefaultTestDenom,
		Amount:      uint64(bankModuleInitialAmount + 1),
	}

	// Test depositing into account with insufficient funds
	_, err := suite.msgServer.Deposit(sdk.WrapSDKContext(suite.Ctx), &depositStruct)
	suite.Require().Error(err, "Should have error depositing more than available balance in bank module")
	suite.Require().ErrorContains(err, "insufficient funds to deposit")

	// Test that account state is untouched
	account, _ := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)

	// Check that pending balances were not touched by this operation
	suite.Require().Equal(initialState.PendingBalanceLo.C.ToAffineCompressed(), account.PendingBalanceLo.C.ToAffineCompressed(), "PendingBalanceLo.C should not have been modified by failed instruction")
	suite.Require().Equal(initialState.PendingBalanceLo.D.ToAffineCompressed(), account.PendingBalanceLo.D.ToAffineCompressed(), "PendingBalanceLo.D should not have been modified by failed instruction")
	suite.Require().Equal(initialState.PendingBalanceHi.C.ToAffineCompressed(), account.PendingBalanceHi.C.ToAffineCompressed(), "PendingBalanceHi.C should not have been modified by failed instruction")
	suite.Require().Equal(initialState.PendingBalanceHi.D.ToAffineCompressed(), account.PendingBalanceHi.D.ToAffineCompressed(), "PendingBalanceHi.D should not have been modified by failed instruction")
}

// Tests scenario in which user tries to deposit a greater than 48 bit number
func (suite *KeeperTestSuite) TestMsgServer_DepositOversizedDeposit() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	bankModuleInitialAmount := uint64(1000)
	_, _ = suite.SetupAccountState(testPk, DefaultTestDenom, 50, big.NewInt(1000000), big.NewInt(8000), new(big.Int).SetUint64(bankModuleInitialAmount))

	// Create a struct where the deposit amount is greater than a 48 bit number
	depositStruct := types.MsgDeposit{
		FromAddress: testAddr.String(),
		Denom:       DefaultTestDenom,
		Amount:      (1 << 48) + 1,
	}

	// Test depositing an amount larger than 48 bits.
	_, err := suite.msgServer.Deposit(sdk.WrapSDKContext(suite.Ctx), &depositStruct)
	suite.Require().Error(err, "Should not be able to deposit an amount larger than 48 bits")
	suite.Require().ErrorContains(err, "exceeded maximum deposit amount of 2^48")
}

// Tests scenario in which user tries to deposit into an amount with too many pending balances
func (suite *KeeperTestSuite) TestMsgServer_DepositTooManyPendingBalances() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)

	// Create an account where the pending balance counter is at the maximum value
	bankModuleInitialAmount := uint64(10000000000)
	suite.SetupAccountState(testPk, DefaultTestDenom, math.MaxUint16, big.NewInt(1000000), big.NewInt(8000), new(big.Int).SetUint64(bankModuleInitialAmount))

	// Create a struct where the deposit amount is greater than a 48 bit number
	depositStruct := types.MsgDeposit{
		FromAddress: testAddr.String(),
		Denom:       DefaultTestDenom,
		Amount:      20000,
	}

	// Test depositing an amount larger than 48 bits.
	_, err := suite.msgServer.Deposit(sdk.WrapSDKContext(suite.Ctx), &depositStruct)
	suite.Require().Error(err, "Should not be able to deposit an amount when pending balance counter is at maximum value")
	suite.Require().ErrorContains(err, "account has too many pending transactions")
}

// Tests that the Deposit method fails when the feature is disabled.
func (suite *KeeperTestSuite) TestMsgServer_DepositFeatureDisabled() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]

	// Generate the address from the private key
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	bankModuleInitialAmount := uint64(1000)
	_, _ = suite.SetupAccountState(testPk, DefaultTestDenom, 50, big.NewInt(1000000), big.NewInt(8000), new(big.Int).SetUint64(bankModuleInitialAmount))

	// Create a struct where the deposit amount is greater than a 48 bit number
	depositStruct := types.MsgDeposit{
		FromAddress: testAddr.String(),
		Denom:       DefaultTestDenom,
		Amount:      100,
	}

	// Disable the confidential tokens module via params
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})
	params := types.DefaultParams()
	params.EnableCtModule = false
	suite.App.ConfidentialTransfersKeeper.SetParams(suite.Ctx, params)

	// Test that submitting an deposit request while module is disabled will fail.
	_, err := suite.msgServer.Deposit(sdk.WrapSDKContext(suite.Ctx), &depositStruct)
	suite.Require().Error(err, "Should not be able to deposit when feature is disabled")
	suite.Require().ErrorContains(err, "feature is disabled by governance")
}

// WITHDRAW TESTS

// Tests the Withdraw method of the MsgServer.
func (suite *KeeperTestSuite) TestMsgServer_WithdrawHappyPath() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Fund the module account
	initialModuleBalance := big.NewInt(1000000000000)
	suite.FundAcc(suite.TestAccs[0], sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewIntFromBigInt(initialModuleBalance))))

	_ = suite.App.BankKeeper.SendCoinsFromAccountToModule(suite.Ctx, suite.TestAccs[0], types.ModuleName, sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewInt(1000000000000))))

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	bankModuleInitialAmount := big.NewInt(1000000000000)
	initialAvailableBalance := big.NewInt(1000)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 50, initialAvailableBalance, big.NewInt(8000), bankModuleInitialAmount)

	// Create a withdraw request
	withdrawAmount := new(big.Int).Div(initialAvailableBalance, big.NewInt(2))
	withdrawStruct, _ := types.NewWithdraw(*testPk, initialState.AvailableBalance, DefaultTestDenom, testAddr.String(), initialState.DecryptableAvailableBalance, withdrawAmount)

	// Execute the withdraw
	req := types.NewMsgWithdrawProto(withdrawStruct)
	_, err := suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error calling valid withdraw")

	// Check that the account has been updated
	account, _ := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)

	// Check that pending balances are left untouched
	suite.Require().Equal(initialState.PendingBalanceLo.C.ToAffineCompressed(), account.PendingBalanceLo.C.ToAffineCompressed(), "PendingBalanceLo.C should not have been modified by withdraw")
	suite.Require().Equal(initialState.PendingBalanceLo.D.ToAffineCompressed(), account.PendingBalanceLo.D.ToAffineCompressed(), "PendingBalanceLo.D should not have been modified by withdraw")
	suite.Require().Equal(initialState.PendingBalanceHi.C.ToAffineCompressed(), account.PendingBalanceHi.C.ToAffineCompressed(), "PendingBalanceHi.C should not have been modified by withdraw")
	suite.Require().Equal(initialState.PendingBalanceHi.D.ToAffineCompressed(), account.PendingBalanceHi.D.ToAffineCompressed(), "PendingBalanceHi.D should not have been modified by withdraw")
	suite.Require().Equal(initialState.PendingBalanceCreditCounter, account.PendingBalanceCreditCounter, "PendingBalanceCreditCounter should not have been modified by withdraw")

	// Check that available balances were updated correctly
	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}
	keyPair, _ := utils.GetElGamalKeyPair(*testPk, DefaultTestDenom)
	oldBalanceDecrypted, _ := teg.DecryptLargeNumber(keyPair.PrivateKey, initialState.AvailableBalance, elgamal.MaxBits32)
	newBalanceDecrypted, _ := teg.DecryptLargeNumber(keyPair.PrivateKey, account.AvailableBalance, elgamal.MaxBits32)
	newTotal := new(big.Int).Sub(oldBalanceDecrypted, withdrawAmount)
	suite.Require().Equal(newTotal, newBalanceDecrypted)

	// Check that the DecryptableAvailableBalances were updated correctly
	suite.Require().Equal(req.DecryptableBalance, account.DecryptableAvailableBalance)

	// Check that account balances were updated properly
	moduleAccount := suite.App.AccountKeeper.GetModuleAccount(suite.Ctx, types.ModuleName)
	moduleBalance := suite.App.BankKeeper.GetBalance(suite.Ctx, moduleAccount.GetAddress(), DefaultTestDenom)
	suite.Require().Equal(new(big.Int).Sub(initialModuleBalance, withdrawAmount).String(), moduleBalance.Amount.String(), "Module account balance should have decreased by the withdraw amount")

	userBalance := suite.App.BankKeeper.GetBalance(suite.Ctx, testAddr, DefaultTestDenom)
	suite.Require().Equal(new(big.Int).Add(bankModuleInitialAmount, withdrawAmount).String(), userBalance.Amount.String(), "User balance should have increased by the withdraw amount")
}

// Test that we cannot perform successive withdraws. The second withdraw struct is invalidated after the first withdraw.
func (suite *KeeperTestSuite) TestMsgServer_WithdrawSuccessive() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Fund the module account
	initialModuleBalance := int64(1000000000000)
	suite.FundAcc(suite.TestAccs[0], sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewInt(initialModuleBalance))))

	_ = suite.App.BankKeeper.SendCoinsFromAccountToModule(suite.Ctx, suite.TestAccs[0], types.ModuleName, sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewInt(1000000000000))))

	// Initialize an account
	initialAvailableBalance := big.NewInt(1000000)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 50, initialAvailableBalance, big.NewInt(8000), big.NewInt(1000000000000))

	// Create a withdraw request with an invalid amount
	withdrawAmount := new(big.Int).Add(initialAvailableBalance, big.NewInt(1))
	_, err := types.NewWithdraw(*testPk, initialState.AvailableBalance, DefaultTestDenom, testAddr.String(), initialState.DecryptableAvailableBalance, withdrawAmount)
	suite.Require().Error(err, "Cannot use client to create withdraw for more than the account balance")
	suite.Require().ErrorContains(err, "insufficient balance")

	// Create two withdraw requests for the entire balance
	withdrawStruct1, _ := types.NewWithdraw(*testPk, initialState.AvailableBalance, DefaultTestDenom, testAddr.String(), initialState.DecryptableAvailableBalance, initialAvailableBalance)
	withdrawStruct2, _ := types.NewWithdraw(*testPk, initialState.AvailableBalance, DefaultTestDenom, testAddr.String(), initialState.DecryptableAvailableBalance, initialAvailableBalance)

	// Execute the first withdraw
	req1 := types.NewMsgWithdrawProto(withdrawStruct1)
	_, err = suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), req1)
	suite.Require().NoError(err, "Should not have error calling first valid withdraw")

	// Execute the second withdraw
	req2 := types.NewMsgWithdrawProto(withdrawStruct2)
	_, err = suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), req2)
	suite.Require().Error(err, "The withdraw should have failed since the withdraw struct is now invalid (has wrong newBalanceCommitment)")
	suite.Require().ErrorContains(err, "ciphertext commitment equality verification failed")
}

// Test that we cannot perform withdraws with an invalid amount.
func (suite *KeeperTestSuite) TestMsgServer_WithdrawInvalidAmount() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Fund the module account
	initialModuleBalance := int64(1000000000000)
	suite.FundAcc(suite.TestAccs[0], sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewInt(initialModuleBalance))))

	_ = suite.App.BankKeeper.SendCoinsFromAccountToModule(suite.Ctx, suite.TestAccs[0], types.ModuleName, sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewInt(1000000000000))))

	// Initialize an account
	initialAvailableBalance := big.NewInt(1000000)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 50, initialAvailableBalance, big.NewInt(8000), big.NewInt(1000000000000))

	// Create a withdraw request
	withdrawStruct, _ := types.NewWithdraw(*testPk, initialState.AvailableBalance, DefaultTestDenom, testAddr.String(), initialState.DecryptableAvailableBalance, initialAvailableBalance)

	// Manually modify the withdraw struct to have an invalid amount (since we can't do that via the client)
	withdrawStruct.Amount = new(big.Int).Add(initialAvailableBalance, big.NewInt(1))

	// Try executing the withdraw. This should fail since the proofs generated before are invalid
	req := types.NewMsgWithdrawProto(withdrawStruct)
	_, err := suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "The withdraw should have failed since the withdraw struct has an invalid amount")
	suite.Require().ErrorContains(err, "ciphertext commitment equality verification failed")

	// Try creating proofs on the new withdraw amount. This should not work since range proofs cannnot be generated on negative numbers.
	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}
	keys, _ := utils.GetElGamalKeyPair(*testPk, DefaultTestDenom)
	newBalanceNegative := new(big.Int).Sub(initialAvailableBalance, withdrawStruct.Amount)

	// NOTE: This should be encrypting a negative number, but this cannot be done using the teg library.
	// This is not important for the test since we just want to test that we cannot create a range proof on a negative number.
	_, randomness, _ := teg.Encrypt(keys.PublicKey, big.NewInt(0))

	_, err = zkproofs.NewRangeProof(128, newBalanceNegative, randomness)
	suite.Require().Error(err, "Cannot create a range proof on a negative number")
}

// Test that we cannot reuse a withdraw struct even if the account has sufficient funds to support it twice.
func (suite *KeeperTestSuite) TestMsgServer_RepeatWithdraw() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Fund the module account
	initialModuleBalance := int64(1000000000000)
	suite.FundAcc(suite.TestAccs[0], sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewInt(initialModuleBalance))))

	_ = suite.App.BankKeeper.SendCoinsFromAccountToModule(suite.Ctx, suite.TestAccs[0], types.ModuleName, sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewInt(1000000000000))))

	// Initialize an account
	initialAvailableBalance := big.NewInt(1000000)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 50, initialAvailableBalance, big.NewInt(8000), big.NewInt(1000000000000))

	// Create a withdraw request
	withdrawAmount := new(big.Int).Div(initialAvailableBalance, big.NewInt(5))
	withdrawStruct, _ := types.NewWithdraw(*testPk, initialState.AvailableBalance, DefaultTestDenom, testAddr.String(), initialState.DecryptableAvailableBalance, withdrawAmount)

	// Execute the first withdraw
	req := types.NewMsgWithdrawProto(withdrawStruct)
	_, err := suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error calling valid withdraw")

	// Execute the same withdraw again
	_, err = suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should not be able to repeat withdraw")
	suite.Require().ErrorContains(err, "ciphertext commitment equality verification failed")
}

// Test the scenario where the decryptable balance was modified and the user tries to withdraw.
func (suite *KeeperTestSuite) TestMsgServer_ModifiedDecryptableBalance() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Fund the module account
	initialModuleBalance := int64(1000000000000)
	suite.FundAcc(suite.TestAccs[0], sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewInt(initialModuleBalance))))

	_ = suite.App.BankKeeper.SendCoinsFromAccountToModule(suite.Ctx, suite.TestAccs[0], types.ModuleName, sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewInt(1000000000000))))

	// Initialize an account
	initialAvailableBalance := big.NewInt(10000)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 50, initialAvailableBalance, big.NewInt(8000), big.NewInt(1000000000000))

	// Create a withdraw request
	withdrawAmount := new(big.Int).Div(initialAvailableBalance, big.NewInt(5))
	withdrawStruct, _ := types.NewWithdraw(*testPk, initialState.AvailableBalance, DefaultTestDenom, testAddr.String(), initialState.DecryptableAvailableBalance, withdrawAmount)

	// Modify the decryptable balance
	aesKey, _ := utils.GetAESKey(*testPk, DefaultTestDenom)
	fraudulentDecryptableBalance, _ := encryption.EncryptAESGCM(big.NewInt(10000000000), aesKey)
	withdrawStruct.DecryptableBalance = fraudulentDecryptableBalance

	// Execute the withdraw
	req := types.NewMsgWithdrawProto(withdrawStruct)
	_, err := suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error calling withdraw despite incorrect decryptable balance submitted")

	// At this point, the decryptable available balance is corrupted.
	// Any withdraw struct we create based on the decryptable balance in the account will be invalid.
	accountState, _ := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	nextWithdrawStruct, err := types.NewWithdraw(*testPk, accountState.AvailableBalance, DefaultTestDenom, testAddr.String(), accountState.DecryptableAvailableBalance, withdrawAmount)
	req = types.NewMsgWithdrawProto(nextWithdrawStruct)
	_, err = suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error withdrawing since withdraw struct is invalid")
	suite.Require().ErrorContains(err, "ciphertext commitment equality verification failed")

	// We can still fix this at this point if we know the correct decryptable balance.
	// This is because the account state is still correct; the decryptable balance is just a convenience feature.
	// The rest of this test shows how to manually create a withdraw struct that will be accepted by the server.

	// First get the actual balance in the account by decrypting the available balance.
	// This will only work if the encrypted value is small enough to be decrypted.
	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}
	keyPair, _ := utils.GetElGamalKeyPair(*testPk, DefaultTestDenom)
	actualBalance, err := teg.DecryptLargeNumber(keyPair.PrivateKey, accountState.AvailableBalance, elgamal.MaxBits32)
	suite.Require().NoError(err, "Should be able to decrypt actual balance since the encrypted value is small")

	// Re-encrypt the actual balance as the current decryptable balance.
	aesEncryptedActualBalance, _ := encryption.EncryptAESGCM(actualBalance, aesKey)

	// Then create the correct struct for the withdraw
	correctedWithdrawStruct, err := types.NewWithdraw(*testPk, accountState.AvailableBalance, DefaultTestDenom, testAddr.String(), aesEncryptedActualBalance, withdrawAmount)

	// Execute the withdraw
	req = types.NewMsgWithdrawProto(correctedWithdrawStruct)
	_, err = suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error calling withdraw despite incorrect decryptable balance submitted")

	// Validate that the number is correct
	accountState, _ = suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	decryptedAvailableBalance, err := teg.DecryptLargeNumber(keyPair.PrivateKey, accountState.AvailableBalance, elgamal.MaxBits32)
	suite.Require().NoError(err, "Should be decryptable")
	newBalance := new(big.Int).Sub(actualBalance, withdrawAmount)
	suite.Require().Equal(decryptedAvailableBalance, newBalance, "New account value should have been updated")

	// Validate that I can create regular transactions with the account again
	nextWithdrawStruct, err = types.NewWithdraw(*testPk, accountState.AvailableBalance, DefaultTestDenom, testAddr.String(), accountState.DecryptableAvailableBalance, big.NewInt(1))
	req = types.NewMsgWithdrawProto(nextWithdrawStruct)
	_, err = suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error withdrawing since decryptable balance is no longer corrupted")
}

// Test that we cannot withdraw when the feature is disabled
func (suite *KeeperTestSuite) TestMsgServer_WithdrawFeatureDisabled() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Fund the module account
	initialModuleBalance := int64(1000000000000)
	suite.FundAcc(suite.TestAccs[0], sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewInt(initialModuleBalance))))

	_ = suite.App.BankKeeper.SendCoinsFromAccountToModule(suite.Ctx, suite.TestAccs[0], types.ModuleName, sdk.NewCoins(sdk.NewCoin(DefaultTestDenom, sdk.NewInt(1000000000000))))

	// Initialize an account
	initialAvailableBalance := big.NewInt(1000000)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 50, initialAvailableBalance, big.NewInt(8000), big.NewInt(1000000000000))

	// Create a withdraw request
	withdrawAmount := new(big.Int).Div(initialAvailableBalance, big.NewInt(5))
	withdrawStruct, _ := types.NewWithdraw(*testPk, initialState.AvailableBalance, DefaultTestDenom, testAddr.String(), initialState.DecryptableAvailableBalance, withdrawAmount)

	// Disable the confidential tokens module via params
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})
	params := types.DefaultParams()
	params.EnableCtModule = false
	suite.App.ConfidentialTransfersKeeper.SetParams(suite.Ctx, params)

	req := types.NewMsgWithdrawProto(withdrawStruct)

	// Test that submitting an withdraw request while module is disabled will fail.
	_, err := suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should not be able to withdraw when feature is disabled")
	suite.Require().ErrorContains(err, "feature is disabled by governance")
}

// CLOSE ACCOUNT TESTS

// Tests the CloseAccount method of the MsgServer.
func (suite *KeeperTestSuite) TestMsgServer_CloseAccountHappyPath() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)
	zeroBigInt := big.NewInt(0)
	// Initialize an account
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 0, zeroBigInt, zeroBigInt, big.NewInt(100))

	// Create a close account request
	closeAccountStruct, err := types.NewCloseAccount(
		*testPk,
		testAddr.String(),
		DefaultTestDenom,
		initialState.PendingBalanceLo,
		initialState.PendingBalanceHi,
		initialState.AvailableBalance)
	suite.Require().NoError(err, "Should not have error creating close account struct")

	// Execute the close account
	req := types.NewMsgCloseAccountProto(closeAccountStruct)
	_, err = suite.msgServer.CloseAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error closing account")

	// Check that the account has been deleted
	_, exists := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	suite.Require().False(exists, "Account should not exist")

	// Try sending the close account instruction again. This should fail now since the account doesn't exist.
	_, err = suite.msgServer.CloseAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error closing account that doesn't exist")
	suite.Require().ErrorContains(err, "account does not exist")
}

func (suite *KeeperTestSuite) TestMsgServer_CloseAccountHasPendingBalance() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Initialize an account that still has pending balances.
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 3, big.NewInt(0), big.NewInt(200), big.NewInt(100))

	// Create a close account request
	closeAccountStruct, _ := types.NewCloseAccount(
		*testPk,
		testAddr.String(),
		DefaultTestDenom,
		initialState.PendingBalanceLo,
		initialState.PendingBalanceHi,
		initialState.AvailableBalance)

	// Execute the close account. This should fail since the account has pending balances on it.
	req := types.NewMsgCloseAccountProto(closeAccountStruct)
	_, err := suite.msgServer.CloseAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error closing account with pending balance")
	suite.Require().ErrorContains(err, "pending balance lo must be 0")

	// Check that the account still exists
	_, exists := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	suite.Require().True(exists, "Account should still exist")

	// Test that applying balances then withdrawing all the available balance results in a successful close account
	applyStruct, _ := types.NewApplyPendingBalance(
		*testPk,
		testAddr.String(),
		DefaultTestDenom,
		initialState.DecryptableAvailableBalance,
		initialState.PendingBalanceCreditCounter,
		initialState.AvailableBalance,
		initialState.PendingBalanceLo,
		initialState.PendingBalanceHi)

	// Apply the pending balances
	applyReq := types.NewMsgApplyPendingBalanceProto(applyStruct)
	_, err = suite.msgServer.ApplyPendingBalance(sdk.WrapSDKContext(suite.Ctx), applyReq)
	suite.Require().NoError(err, "Should have no error applying pending balance")

	account, _ := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	aeskey, _ := utils.GetAESKey(*testPk, DefaultTestDenom)
	availableBalanceAmount, err := encryption.DecryptAESGCM(account.DecryptableAvailableBalance, aeskey)
	suite.Require().NoError(err, "Should be able to decrypt available balance")

	withdrawStruct, _ := types.NewWithdraw(*testPk, account.AvailableBalance, DefaultTestDenom, testAddr.String(), account.DecryptableAvailableBalance, availableBalanceAmount)
	withdrawReq := types.NewMsgWithdrawProto(withdrawStruct)
	suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), withdrawReq)

	account, _ = suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	closeAccountStruct, _ = types.NewCloseAccount(
		*testPk,
		testAddr.String(),
		DefaultTestDenom,
		account.PendingBalanceLo,
		account.PendingBalanceHi,
		account.AvailableBalance)

	// Execute the close account. This should succeed now that all the balances have been withdrawn
	req = types.NewMsgCloseAccountProto(closeAccountStruct)
	_, err = suite.msgServer.CloseAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should have no error closing account with no more balance")
}

// Test the scenario where a close account instruction is applied for an account that still contains available balances.
func (suite *KeeperTestSuite) TestMsgServer_CloseAccountHasAvailableBalance() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	availableBalanceAmount := big.NewInt(900)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 0, availableBalanceAmount, big.NewInt(0), big.NewInt(100))

	// Create a close account request
	closeAccountStruct, _ := types.NewCloseAccount(
		*testPk,
		testAddr.String(),
		DefaultTestDenom,
		initialState.PendingBalanceLo,
		initialState.PendingBalanceHi,
		initialState.AvailableBalance)

	// Execute the close account. This should fail since the account still has available balances.
	req := types.NewMsgCloseAccountProto(closeAccountStruct)
	_, err := suite.msgServer.CloseAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error closing account with available balance")
	suite.Require().ErrorContains(err, "available balance must be 0")

	// Check that the account still exists
	_, exists := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	suite.Require().True(exists, "Account should still exist")
}

// Test that accounts cannot be closed while the feature is disabled
func (suite *KeeperTestSuite) TestMsgServer_CloseAccountFeatureDisabled() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 0, big.NewInt(0), big.NewInt(0), big.NewInt(0))

	// Create a close account request
	closeAccountStruct, _ := types.NewCloseAccount(
		*testPk,
		testAddr.String(),
		DefaultTestDenom,
		initialState.PendingBalanceLo,
		initialState.PendingBalanceHi,
		initialState.AvailableBalance)

	// Disable the confidential tokens module via params
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})
	params := types.DefaultParams()
	params.EnableCtModule = false
	suite.App.ConfidentialTransfersKeeper.SetParams(suite.Ctx, params)

	// Execute the close account. This should fail as the module is disabled.
	req := types.NewMsgCloseAccountProto(closeAccountStruct)
	_, err := suite.msgServer.CloseAccount(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should not be able to close when feature is disabled")
	suite.Require().ErrorContains(err, "feature is disabled by governance")

	// Check that the account still exists
	_, exists := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)
	suite.Require().True(exists, "Account should still exist")
}

// APPLY PENDING BALANCES TESTS

// Tests the ApplyPendingBalance method of the MsgServer.
func (suite *KeeperTestSuite) TestMsgServer_ApplyPendingBalance() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	initialAvailableBalance := big.NewInt(2000)
	initialPendingBalance := big.NewInt(100000)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 10, initialAvailableBalance, initialPendingBalance, big.NewInt(1000))

	// Create an apply pending balance request
	applyPendingBalance, _ := types.NewApplyPendingBalance(
		*testPk,
		testAddr.String(),
		DefaultTestDenom,
		initialState.DecryptableAvailableBalance,
		initialState.PendingBalanceCreditCounter,
		initialState.AvailableBalance,
		initialState.PendingBalanceLo,
		initialState.PendingBalanceHi)

	req := types.NewMsgApplyPendingBalanceProto(applyPendingBalance)
	// Execute the apply pending balance
	_, err := suite.msgServer.ApplyPendingBalance(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error applying pending balance")

	// Check that the account has been updated
	account, _ := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)

	// Decrypt and check balances
	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}
	keyPair, _ := utils.GetElGamalKeyPair(*testPk, DefaultTestDenom)

	expectedNewBalance := new(big.Int).Add(initialPendingBalance, initialAvailableBalance)

	// Check that the balances were correctly added to the available balance.
	actualAvailableBalance, _ := teg.DecryptLargeNumber(keyPair.PrivateKey, account.AvailableBalance, elgamal.MaxBits32)
	suite.Require().Equal(expectedNewBalance, actualAvailableBalance, "Available balance should match")

	aesKey, _ := utils.GetAESKey(*testPk, DefaultTestDenom)
	actualDecryptableAvailableBalance, _ := encryption.DecryptAESGCM(account.DecryptableAvailableBalance, aesKey)
	suite.Require().Equal(expectedNewBalance, actualDecryptableAvailableBalance, "Decryptable available balance should match")

	// Check that the pending balances are set to 0.
	zeroBigInt := big.NewInt(0)
	actualPendingBalanceLo, _ := teg.DecryptLargeNumber(keyPair.PrivateKey, account.PendingBalanceLo, elgamal.MaxBits32)
	suite.Require().Equal(zeroBigInt, actualPendingBalanceLo, "Pending balance lo not 0")

	actualPendingBalanceHi, _ := teg.DecryptLargeNumber(keyPair.PrivateKey, account.PendingBalanceHi, elgamal.MaxBits32)
	suite.Require().Equal(zeroBigInt, actualPendingBalanceHi, "Pending balance hi not 0")

	// Check that the pending balance credit counter is reset to 0.
	suite.Require().Equal(uint16(0), account.PendingBalanceCreditCounter, "Pending balance credit counter should be set to 0 after applying")
}

// Tests the ApplyPendingBalance method of the MsgServer when there is a change to the AvailableBalance between the time the pending balance instruction was created and when it is applied.
func (suite *KeeperTestSuite) TestMsgServer_ApplyPendingBalanceAfterWithdraw() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	initialAvailableBalance := big.NewInt(2000)
	initialPendingBalance := big.NewInt(1000)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 10, initialAvailableBalance, initialPendingBalance, big.NewInt(1000))

	// Create an apply pending balance request
	applyPendingBalance, _ := types.NewApplyPendingBalance(
		*testPk,
		testAddr.String(),
		DefaultTestDenom,
		initialState.DecryptableAvailableBalance,
		initialState.PendingBalanceCreditCounter,
		initialState.AvailableBalance,
		initialState.PendingBalanceLo,
		initialState.PendingBalanceHi)

	req := types.NewMsgApplyPendingBalanceProto(applyPendingBalance)

	// Before the pending balance is applied, a withdrawal is made, changing the available balance in the account.
	withdrawAmount := new(big.Int).Div(initialAvailableBalance, big.NewInt(2))
	withdrawReq, _ := types.NewWithdraw(*testPk, initialState.AvailableBalance, DefaultTestDenom, testAddr.String(), initialState.DecryptableAvailableBalance, withdrawAmount)
	withdrawMsg := types.NewMsgWithdrawProto(withdrawReq)
	_, err := suite.msgServer.Withdraw(sdk.WrapSDKContext(suite.Ctx), withdrawMsg)

	// Now execute the apply pending balance. Checks in the ApplyPendingBalance function should catch this and cause it to fail so we don't wrongly update the new decryptableAvailableBalance
	_, err = suite.msgServer.ApplyPendingBalance(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Application should fail since the available balance has changed since the apply pending balance instruction was created")
	suite.Require().ErrorContains(err, "available balance mismatch")
}

// Tests the ApplyPendingBalance method of the MsgServer when there is a change to the PendingBalance between the time the pending balance instruction was created and when it is applied.
func (suite *KeeperTestSuite) TestMsgServer_ApplyPendingBalanceAfterDeposit() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	initialAvailableBalance := big.NewInt(2000)
	initialPendingBalance := big.NewInt(10)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 10, initialAvailableBalance, initialPendingBalance, big.NewInt(1000))

	// Create an apply pending balance request
	applyPendingBalance, _ := types.NewApplyPendingBalance(
		*testPk,
		testAddr.String(),
		DefaultTestDenom,
		initialState.DecryptableAvailableBalance,
		initialState.PendingBalanceCreditCounter,
		initialState.AvailableBalance,
		initialState.PendingBalanceLo,
		initialState.PendingBalanceHi)

	req := types.NewMsgApplyPendingBalanceProto(applyPendingBalance)

	// Before the pending balance is applied, a deposit is made, changing the pending balance in the account.
	// The same scenario happens when incoming transfers are received.
	depositMsg := &types.MsgDeposit{
		testAddr.String(),
		DefaultTestDenom,
		1000,
	}
	_, err := suite.msgServer.Deposit(sdk.WrapSDKContext(suite.Ctx), depositMsg)

	// Now execute the apply pending balance. Checks in the ApplyPendingBalance function should catch this and cause it to fail so we don't wrongly update the new decryptableAvailableBalance
	_, err = suite.msgServer.ApplyPendingBalance(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Application should fail since the available balance has changed since the pending balance was created")
	suite.Require().ErrorContains(err, "pending balance mismatch")
}

// Tests the ApplyPendingBalance method of the MsgServer on an account with no Pending Balances or doesn't exist. These should both fail.
func (suite *KeeperTestSuite) TestMsgServer_ApplyPendingBalanceNoPendingBalances() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	initialAvailableBalance := big.NewInt(20000000)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 0, initialAvailableBalance, big.NewInt(0), big.NewInt(1000))

	// Create an apply pending balance request
	applyPendingBalance, err := types.NewApplyPendingBalance(
		*testPk,
		testAddr.String(),
		DefaultTestDenom,
		initialState.DecryptableAvailableBalance,
		initialState.PendingBalanceCreditCounter,
		initialState.AvailableBalance,
		initialState.PendingBalanceLo,
		initialState.PendingBalanceHi)

	suite.Require().NoError(err, "Should not have error creating apply pending balance request")

	req := types.NewMsgApplyPendingBalanceProto(applyPendingBalance)

	// Execute the apply pending balance. This should fail since there are no pending balances to apply.
	_, err = suite.msgServer.ApplyPendingBalance(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error applying pending balance on account with no pending balances")

	// Delete the account so we can test running the instruction on an account that doesn't exist.
	suite.App.ConfidentialTransfersKeeper.DeleteAccount(suite.Ctx, testAddr.String(), DefaultTestDenom)

	// Execute the apply pending balance. This should fail since the account doesn't exist.
	_, err = suite.msgServer.ApplyPendingBalance(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error applying pending balance on account that doesn't exist")
	suite.Require().ErrorContains(err, "account does not exist")
}

// Tests that balances cannot be applied while feature is disabled
func (suite *KeeperTestSuite) TestMsgServer_ApplyPendingBalanceFeatureDisabled() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	testPk := suite.PrivKeys[0]
	testAddr := privkeyToAddress(testPk)

	// Initialize an account
	initialAvailableBalance := big.NewInt(2000)
	initialPendingBalance := big.NewInt(10)
	initialState, _ := suite.SetupAccountState(testPk, DefaultTestDenom, 10, initialAvailableBalance, initialPendingBalance, big.NewInt(1000))

	// Create an apply pending balance request
	applyPendingBalance, _ := types.NewApplyPendingBalance(
		*testPk,
		testAddr.String(),
		DefaultTestDenom,
		initialState.DecryptableAvailableBalance,
		initialState.PendingBalanceCreditCounter,
		initialState.AvailableBalance,
		initialState.PendingBalanceLo,
		initialState.PendingBalanceHi)

	req := types.NewMsgApplyPendingBalanceProto(applyPendingBalance)

	// Disable the confidential tokens module via params
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})
	params := types.DefaultParams()
	params.EnableCtModule = false
	suite.App.ConfidentialTransfersKeeper.SetParams(suite.Ctx, params)

	// Execute the apply pending balance. This should have an error as the module is disabled
	_, err := suite.msgServer.ApplyPendingBalance(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should not be able to apply balances when feature is disabled")
	suite.Require().ErrorContains(err, "feature is disabled by governance")
}

// TRANSFER TESTS

func (suite *KeeperTestSuite) TestMsgServer_TransferHappyPath() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Setup the accounts used for the test
	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)
	recipientPk := suite.PrivKeys[1]
	recipientAddr := privkeyToAddress(recipientPk)
	auditorPk := suite.PrivKeys[2]
	auditorAddr := privkeyToAddress(auditorPk)

	// Initialize an account
	initialSenderState, _ := suite.SetupAccountState(senderPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))
	initialRecipientState, _ := suite.SetupAccountState(recipientPk, DefaultTestDenom, 12, big.NewInt(5000), big.NewInt(21000), big.NewInt(201000))
	initialAuditorState, _ := suite.SetupAccountState(auditorPk, DefaultTestDenom, 12, big.NewInt(5000), big.NewInt(21000), big.NewInt(201000))

	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}
	senderKeypair, _ := utils.GetElGamalKeyPair(*senderPk, DefaultTestDenom)

	recipientKeypair, _ := utils.GetElGamalKeyPair(*recipientPk, DefaultTestDenom)

	transferAmount := uint64(500)

	// Happy Path
	auditorsInput := []types.AuditorInput{{auditorAddr.String(), &initialAuditorState.PublicKey}}
	transferStruct, err := types.NewTransfer(
		senderPk,
		senderAddr.String(),
		recipientAddr.String(),
		DefaultTestDenom,
		initialSenderState.DecryptableAvailableBalance,
		initialSenderState.AvailableBalance,
		transferAmount,
		&initialRecipientState.PublicKey,
		auditorsInput)
	suite.Require().NoError(err, "Should not have error creating valid transfer struct")

	req := types.NewMsgTransferProto(transferStruct)
	_, err = suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().NoError(err, "Should not have error calling valid transfer")

	senderAccountState, _ := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, senderAddr.String(), DefaultTestDenom)

	// Pending Balances should not be altered by this instruction
	suite.Require().Equal(initialSenderState.PendingBalanceLo.C.ToAffineCompressed(), senderAccountState.PendingBalanceLo.C.ToAffineCompressed(), "PendingBalanceLo should not have been touched")
	suite.Require().Equal(initialSenderState.PendingBalanceLo.D.ToAffineCompressed(), senderAccountState.PendingBalanceLo.D.ToAffineCompressed(), "PendingBalanceLo should not have been touched")
	suite.Require().Equal(initialSenderState.PendingBalanceHi.C.ToAffineCompressed(), senderAccountState.PendingBalanceHi.C.ToAffineCompressed(), "PendingBalanceHi should not have been touched")
	suite.Require().Equal(initialSenderState.PendingBalanceHi.D.ToAffineCompressed(), senderAccountState.PendingBalanceHi.D.ToAffineCompressed(), "PendingBalanceHi should not have been touched")
	suite.Require().Equal(initialSenderState.PendingBalanceCreditCounter, senderAccountState.PendingBalanceCreditCounter, "PendingBalanceCreditCounter should not have been touched")

	// NonEncryptableBalance and Account metadata should also not be altered by this instruction.
	suite.Require().Equal(initialSenderState.PublicKey.ToAffineCompressed(), senderAccountState.PublicKey.ToAffineCompressed(), "PublicKey should not have been touched")

	// Check that new balance encrypts the sum of oldBalance and withdrawAmount
	transferAmountBigInt := new(big.Int).SetUint64(transferAmount)
	senderOldBalanceDecrypted, _ := teg.DecryptLargeNumber(senderKeypair.PrivateKey, initialSenderState.AvailableBalance, elgamal.MaxBits32)
	senderNewBalanceDecrypted, _ := teg.DecryptLargeNumber(senderKeypair.PrivateKey, senderAccountState.AvailableBalance, elgamal.MaxBits32)
	suite.Require().Equal(new(big.Int).Sub(senderOldBalanceDecrypted, transferAmountBigInt), senderNewBalanceDecrypted, "AvailableBalance of sender should be decreased")

	// Verify that the DecryptableAvailableBalances were updated as well and that they match the available balances.
	senderAesKey, _ := utils.GetAESKey(*senderPk, DefaultTestDenom)
	senderOldDecryptableBalanceDecrypted, _ := encryption.DecryptAESGCM(initialSenderState.DecryptableAvailableBalance, senderAesKey)
	senderNewDecryptableBalanceDecrypted, _ := encryption.DecryptAESGCM(senderAccountState.DecryptableAvailableBalance, senderAesKey)
	suite.Require().Equal(new(big.Int).Sub(senderOldDecryptableBalanceDecrypted, transferAmountBigInt), senderNewDecryptableBalanceDecrypted)
	suite.Require().Equal(senderNewBalanceDecrypted, senderNewDecryptableBalanceDecrypted)

	// On the other hand, available balances of the recipient account should not have been altered
	recipientAccountState, _ := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, recipientAddr.String(), DefaultTestDenom)
	suite.Require().Equal(initialRecipientState.AvailableBalance.C.ToAffineCompressed(), recipientAccountState.AvailableBalance.C.ToAffineCompressed(), "AvailableBalance should not have been touched")
	suite.Require().Equal(initialRecipientState.AvailableBalance.D.ToAffineCompressed(), recipientAccountState.AvailableBalance.D.ToAffineCompressed(), "AvailableBalance should not have been touched")
	suite.Require().Equal(initialRecipientState.DecryptableAvailableBalance, recipientAccountState.DecryptableAvailableBalance, "DecryptableAvailableBalance should not have been touched")

	// NonEncryptableBalance and Account metadata should also not be altered by this instruction.
	suite.Require().Equal(initialRecipientState.PublicKey.ToAffineCompressed(), recipientAccountState.PublicKey.ToAffineCompressed(), "PublicKey should not have been touched")

	// Check that new pending balances of the recipient account have been updated to reflect the change
	suite.Require().Equal(initialRecipientState.PendingBalanceCreditCounter+1, recipientAccountState.PendingBalanceCreditCounter)
	oldRecipientPendingBalance, _, _, _ := initialRecipientState.GetPendingBalancePlaintext(teg, recipientKeypair)
	newRecipientPendingBalance, _, _, _ := recipientAccountState.GetPendingBalancePlaintext(teg, recipientKeypair)

	newTotal := new(big.Int).Add(oldRecipientPendingBalance, transferAmountBigInt)

	suite.Require().Equal(newTotal, newRecipientPendingBalance, "New pending balance should be equal to transfer amount added to old pending balance")
}

func (suite *KeeperTestSuite) TestMsgServer_TransferToMaxPendingRecipient() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Setup the accounts used for the test
	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)
	recipientPk := suite.PrivKeys[1]
	recipientAddr := privkeyToAddress(recipientPk)

	// Initialize the sender account
	initialSenderState, _ := suite.SetupAccountState(senderPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))
	initialRecipientState, _ := suite.SetupAccountState(recipientPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))

	// Initialize the recipient account with max pending balances
	_, _ = suite.SetupAccountState(recipientPk, DefaultTestDenom, math.MaxUint16, big.NewInt(1000000), big.NewInt(100), big.NewInt(500))

	transferAmount := uint64(50)

	// Attempt to transfer to account with max pending balances
	transferStruct, _ := types.NewTransfer(
		senderPk,
		senderAddr.String(),
		recipientAddr.String(),
		DefaultTestDenom,
		initialSenderState.DecryptableAvailableBalance,
		initialSenderState.AvailableBalance,
		transferAmount,
		&initialRecipientState.PublicKey,
		nil,
	)

	req := types.NewMsgTransferProto(transferStruct)
	_, err := suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should not be able to transfer to account with max pending balances")
	suite.Require().ErrorContains(err, "recipient account has too many pending transactions")
}

func (suite *KeeperTestSuite) TestMsgServer_TransferInsufficientBalance() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Setup the accounts used for the test
	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)
	recipientPk := suite.PrivKeys[1]
	recipientAddr := privkeyToAddress(recipientPk)

	// Initialize the sender account
	initialSenderState, _ := suite.SetupAccountState(senderPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))
	// Initialize the recipient account
	recipientAccountState, _ := suite.SetupAccountState(recipientPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))

	senderAesKey, _ := utils.GetAESKey(*senderPk, DefaultTestDenom)

	initialBalance, _ := encryption.DecryptAESGCM(initialSenderState.DecryptableAvailableBalance, senderAesKey)

	// Set transfer amount to greater than the initial balance.
	transferAmount := initialBalance.Uint64() + 1

	// Attempt to create transfer object.
	_, err := types.NewTransfer(
		senderPk,
		senderAddr.String(),
		recipientAddr.String(),
		DefaultTestDenom,
		initialSenderState.DecryptableAvailableBalance,
		initialSenderState.AvailableBalance,
		transferAmount,
		&recipientAccountState.PublicKey,
		nil,
	)

	suite.Require().Error(err, "Should have error creating transfer struct with insufficient balances using the client")
	suite.Require().ErrorContains(err, "insufficient balance")

	// First create a regular transfer with a normal transfer amount
	transferStruct, _ := types.NewTransfer(
		senderPk,
		senderAddr.String(),
		recipientAddr.String(),
		DefaultTestDenom,
		initialSenderState.DecryptableAvailableBalance,
		initialSenderState.AvailableBalance,
		initialBalance.Uint64(),
		&recipientAccountState.PublicKey,
		nil,
	)

	// Substitute the transfer amounts after
	// Split the transfer amount into bottom 16 bits and top 32 bits.
	transferLoBits, transferHiBits, _ := utils.SplitTransferBalance(initialBalance.Uint64())

	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}
	transferLoBigInt := new(big.Int).SetUint64(uint64(transferLoBits))
	transferHiBigInt := new(big.Int).SetUint64(uint64(transferHiBits))
	senderAmountLo, senderLoRandomness, _ := teg.Encrypt(initialSenderState.PublicKey, transferLoBigInt)
	senderAmountHi, senderHiRandomness, _ := teg.Encrypt(initialSenderState.PublicKey, transferHiBigInt)

	recipientAmountLo, recipientLoRandomness, _ := teg.Encrypt(recipientAccountState.PublicKey, transferLoBigInt)
	recipientAmountHi, recipientHiRandomness, _ := teg.Encrypt(recipientAccountState.PublicKey, transferHiBigInt)

	transferStruct.SenderTransferAmountLo = senderAmountLo
	transferStruct.SenderTransferAmountHi = senderAmountHi
	transferStruct.RecipientTransferAmountLo = recipientAmountLo
	transferStruct.RecipientTransferAmountHi = recipientAmountHi

	// Try to execute the modified transfer instruction. This should fail since the balances don't match the proof generated
	req := types.NewMsgTransferProto(transferStruct)
	_, err = suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should have error transferring more than the account balance")
	suite.Require().ErrorContains(err, "failed to verify sender transfer amount lo")

	// Try to modify the proofs as well
	senderLoValidityProof, _ := zkproofs.NewCiphertextValidityProof(&senderLoRandomness, initialSenderState.PublicKey, senderAmountLo, transferLoBigInt)
	senderHiValidityProof, _ := zkproofs.NewCiphertextValidityProof(&senderHiRandomness, initialSenderState.PublicKey, senderAmountHi, transferHiBigInt)
	recipientLoValidityProof, _ := zkproofs.NewCiphertextValidityProof(&recipientLoRandomness, recipientAccountState.PublicKey, recipientAmountLo, transferLoBigInt)
	recipientHiValidityProof, _ := zkproofs.NewCiphertextValidityProof(&recipientHiRandomness, recipientAccountState.PublicKey, recipientAmountHi, transferHiBigInt)

	transferStruct.Proofs.SenderTransferAmountLoValidityProof = senderLoValidityProof
	transferStruct.Proofs.SenderTransferAmountHiValidityProof = senderHiValidityProof
	transferStruct.Proofs.RecipientTransferAmountLoValidityProof = recipientLoValidityProof
	transferStruct.Proofs.RecipientTransferAmountHiValidityProof = recipientHiValidityProof

	// Try to run the bad transfer instruction again.
	// This should still fail since the ciphertext commitment equality proof will catch that the NewBalanceCommitment (0) does not equal account.AvailableBalance - transferAmount (-1)
	// We can also swap NewBalanceCommitment out to be -1 to make the proof pass, but the instruction should still fail since we cannot generate a range proof on -1
	req = types.NewMsgTransferProto(transferStruct)
	_, err = suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should still have error transferring more than the account balance despite modifying the proofs")
	suite.Require().ErrorContains(err, "ciphertext commitment equality verification failed")
}

func (suite *KeeperTestSuite) TestMsgServer_TransferWrongRecipient() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Setup the accounts used for the test
	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)
	recipientPk := suite.PrivKeys[1]
	recipientAddr := privkeyToAddress(recipientPk)
	otherPk := suite.PrivKeys[2]
	otherAddr := privkeyToAddress(otherPk)

	// Initialize the sender account
	initialSenderState, _ := suite.SetupAccountState(senderPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))
	initialRecipientState, _ := suite.SetupAccountState(recipientPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))
	suite.SetupAccountState(otherPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))

	senderAesKey, _ := utils.GetAESKey(*senderPk, DefaultTestDenom)

	initialBalance, _ := encryption.DecryptAESGCM(initialSenderState.DecryptableAvailableBalance, senderAesKey)

	// Set transfer amount to half of the initial balance.
	transferAmount := initialBalance.Uint64() / 2
	transferStruct, _ := types.NewTransfer(
		senderPk,
		senderAddr.String(),
		recipientAddr.String(),
		DefaultTestDenom,
		initialSenderState.DecryptableAvailableBalance,
		initialSenderState.AvailableBalance,
		transferAmount,
		&initialRecipientState.PublicKey,
		nil,
	)

	// Set the transferStruct recipient to the wrong recipient
	transferStruct.ToAddress = otherAddr.String()

	// However, since the balance used to calculate the proofs in the transfer structs are false, the equality proof verification will fail
	req := types.NewMsgTransferProto(transferStruct)
	_, err := suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should fail ciphertext validity proof since we created those ciphertexts using recipient's public key")
	suite.Require().ErrorContains(err, "failed to verify recipient transfer amount lo")
}

func (suite *KeeperTestSuite) TestMsgServer_TransferDifferentAmounts() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}

	// Setup the accounts used for the test
	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)
	recipientPk := suite.PrivKeys[1]
	recipientAddr := privkeyToAddress(recipientPk)

	// Initialize the sender account
	initialSenderState, _ := suite.SetupAccountState(senderPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))
	initialRecipientState, _ := suite.SetupAccountState(recipientPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))

	senderAesKey, _ := utils.GetAESKey(*senderPk, DefaultTestDenom)

	initialBalance, _ := encryption.DecryptAESGCM(initialSenderState.DecryptableAvailableBalance, senderAesKey)

	// Set transfer amount to a fraction of the initial balance.
	transferAmount := initialBalance.Uint64() / 5
	transferStruct, _ := types.NewTransfer(
		senderPk,
		senderAddr.String(),
		recipientAddr.String(),
		DefaultTestDenom,
		initialSenderState.DecryptableAvailableBalance,
		initialSenderState.AvailableBalance,
		transferAmount,
		&initialRecipientState.PublicKey,
		nil,
	)

	// Now we change the transfer amounts encoded with the recipient's keys to attempt to send them more than we lose.
	fakeTransferAmount := transferAmount * 2

	// Split the transfer amount into bottom 16 bits and top 32 bits.
	transferLoBits, transferHiBits, _ := utils.SplitTransferBalance(fakeTransferAmount)
	transferLoBigInt := new(big.Int).SetUint64(uint64(transferLoBits))
	transferHiBigInt := new(big.Int).SetUint64(uint64(transferHiBits))

	// Encrypt the transfer amounts for the recipient
	recipientKeyPair, _ := utils.GetElGamalKeyPair(*recipientPk, DefaultTestDenom)

	encryptedTransferLoBits, loBitsRandomness, _ := teg.Encrypt(recipientKeyPair.PublicKey, transferLoBigInt)

	encryptedTransferHiBits, hiBitsRandomness, _ := teg.Encrypt(recipientKeyPair.PublicKey, transferHiBigInt)

	// Set the transferStruct recipient to the new amounts
	transferStruct.RecipientTransferAmountLo = encryptedTransferLoBits
	transferStruct.RecipientTransferAmountHi = encryptedTransferHiBits

	// Attempt to make the transfer. This call should fail since the ciphertext validity proofs generated are specific to the underlying value and have not been updated.
	req := types.NewMsgTransferProto(transferStruct)
	_, err := suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should fail validity proof since we created those proofs using ciphertexts on the original value.")
	suite.Require().ErrorContains(err, "failed to verify recipient transfer amount lo")

	// Generate the validity proofs of the new amounts
	loBitsValidityProof, _ := zkproofs.NewCiphertextValidityProof(&loBitsRandomness, recipientKeyPair.PublicKey, encryptedTransferLoBits, transferLoBigInt)
	hiBitsValidityProof, _ := zkproofs.NewCiphertextValidityProof(&hiBitsRandomness, recipientKeyPair.PublicKey, encryptedTransferHiBits, transferHiBigInt)

	transferStruct.Proofs.RecipientTransferAmountLoValidityProof = loBitsValidityProof
	transferStruct.Proofs.RecipientTransferAmountHiValidityProof = hiBitsValidityProof

	// However, since the equality proofs are generated on the original recipient transfer amounts, the equality proof verification will fail
	req = types.NewMsgTransferProto(transferStruct)
	_, err = suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should fail equality proof since we created those proofs using ciphertexts on the original value.")
	suite.Require().ErrorContains(err, "ciphertext ciphertext equality verification on transfer amount lo failed")

	// So we attempt to generate new equality proofs for the amounts as well.
	loBitsScalar, _ := curves.ED25519().Scalar.SetBigInt(transferLoBigInt)

	hiBitsScalar, _ := curves.ED25519().Scalar.SetBigInt(transferHiBigInt)

	senderKeyPair, _ := utils.GetElGamalKeyPair(*senderPk, DefaultTestDenom)

	ciphertextEqualityLoProof, err := zkproofs.NewCiphertextCiphertextEqualityProof(senderKeyPair, &recipientKeyPair.PublicKey, transferStruct.SenderTransferAmountLo, &loBitsRandomness, &loBitsScalar)
	suite.Require().NoError(err, "Should have no error generating lo bits equality proof despite mismatch in transfer amounts")

	ciphertextEqualityHiProof, err := zkproofs.NewCiphertextCiphertextEqualityProof(senderKeyPair, &recipientKeyPair.PublicKey, transferStruct.SenderTransferAmountHi, &hiBitsRandomness, &hiBitsScalar)
	suite.Require().NoError(err, "Should have no error generating hi bits equality proof despite mismatch in transfer amounts")

	transferStruct.Proofs.TransferAmountLoEqualityProof = ciphertextEqualityLoProof
	transferStruct.Proofs.TransferAmountHiEqualityProof = ciphertextEqualityHiProof

	// However, the equality proofs should still fail here since the sender and recipient ciphertexts encode different values.
	req = types.NewMsgTransferProto(transferStruct)
	_, err = suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should still fail equality proof since transfer amount ciphertexts encode different values.")
	suite.Require().ErrorContains(err, "ciphertext ciphertext equality verification on transfer amount lo failed")
}

func (suite *KeeperTestSuite) TestMsgServer_TransferFeatureDisabled() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Setup the accounts used for the test
	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)
	recipientPk := suite.PrivKeys[1]
	recipientAddr := privkeyToAddress(recipientPk)
	auditorPk := suite.PrivKeys[2]
	auditorAddr := privkeyToAddress(auditorPk)

	// Initialize an account
	initialSenderState, _ := suite.SetupAccountState(senderPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))
	initialRecipientState, _ := suite.SetupAccountState(recipientPk, DefaultTestDenom, 12, big.NewInt(5000), big.NewInt(21000), big.NewInt(201000))
	initialAuditorState, _ := suite.SetupAccountState(auditorPk, DefaultTestDenom, 12, big.NewInt(5000), big.NewInt(21000), big.NewInt(201000))

	transferAmount := uint64(500)

	// Happy Path
	auditorsInput := []types.AuditorInput{{auditorAddr.String(), &initialAuditorState.PublicKey}}
	transferStruct, err := types.NewTransfer(
		senderPk,
		senderAddr.String(),
		recipientAddr.String(),
		DefaultTestDenom,
		initialSenderState.DecryptableAvailableBalance,
		initialSenderState.AvailableBalance,
		transferAmount,
		&initialRecipientState.PublicKey,
		auditorsInput)
	suite.Require().NoError(err, "Should not have error creating valid transfer struct")

	req := types.NewMsgTransferProto(transferStruct)

	// Disable the confidential tokens module via params
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})
	params := types.DefaultParams()
	params.EnableCtModule = false
	suite.App.ConfidentialTransfersKeeper.SetParams(suite.Ctx, params)

	// Execute the transfer. This should have an error as the module is disabled
	_, err = suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should not be able to transfer when feature is disabled")
	suite.Require().ErrorContains(err, "feature is disabled by governance")
}

func (suite *KeeperTestSuite) TestMsgServer_TransferNegativeAmount() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Setup the accounts used for the test
	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)
	recipientPk := suite.PrivKeys[1]
	recipientAddr := privkeyToAddress(recipientPk)

	// Initialize the sender account
	initialAvailableBalance := big.NewInt(2000)
	initialSenderState, _ := suite.SetupAccountState(senderPk, DefaultTestDenom, 10, initialAvailableBalance, big.NewInt(3000), big.NewInt(1000))
	// Initialize the recipient account
	recipientAccountState, _ := suite.SetupAccountState(recipientPk, DefaultTestDenom, 10, initialAvailableBalance, big.NewInt(3000), big.NewInt(1000))

	senderAesKey, _ := utils.GetAESKey(*senderPk, DefaultTestDenom)

	// Set transfer amount to negative.
	transferAmount := big.NewInt(int64(-100))

	// First create a regular transfer with a normal transfer amount
	transferStruct, _ := types.NewTransfer(
		senderPk,
		senderAddr.String(),
		recipientAddr.String(),
		DefaultTestDenom,
		initialSenderState.DecryptableAvailableBalance,
		initialSenderState.AvailableBalance,
		uint64(0),
		&recipientAccountState.PublicKey,
		nil,
	)

	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}
	// Make the transfer amounts negative
	senderAmountLo, senderRandomness, err := teg.Encrypt(initialSenderState.PublicKey, transferAmount)
	recipientAmountLo, recipientRandomness, err := teg.Encrypt(recipientAccountState.PublicKey, transferAmount)
	transferStruct.SenderTransferAmountLo = senderAmountLo
	transferStruct.RecipientTransferAmountLo = recipientAmountLo

	// Regenerate the proofs
	transferStruct.Proofs.SenderTransferAmountLoValidityProof, _ = zkproofs.NewCiphertextValidityProof(&senderRandomness, initialSenderState.PublicKey, transferStruct.SenderTransferAmountLo, transferAmount)
	transferStruct.Proofs.RecipientTransferAmountLoValidityProof, _ = zkproofs.NewCiphertextValidityProof(&recipientRandomness, recipientAccountState.PublicKey, transferStruct.RecipientTransferAmountLo, transferAmount)

	newBalance := new(big.Int).Sub(initialAvailableBalance, transferAmount)
	remainingCommitment, commitmentRandomness, err := teg.Encrypt(initialSenderState.PublicKey, newBalance)
	transferStruct.RemainingBalanceCommitment = remainingCommitment
	transferStruct.Proofs.RemainingBalanceCommitmentValidityProof, _ = zkproofs.NewCiphertextValidityProof(&commitmentRandomness, initialSenderState.PublicKey, remainingCommitment, newBalance)

	transferStruct.DecryptableBalance, _ = encryption.EncryptAESGCM(newBalance, senderAesKey)

	senderKeyPair, _ := utils.GetElGamalKeyPair(*senderPk, DefaultTestDenom)
	loBitsScalar, _ := curves.ED25519().Scalar.SetBigInt(transferAmount)
	transferStruct.Proofs.TransferAmountLoEqualityProof, err = zkproofs.NewCiphertextCiphertextEqualityProof(senderKeyPair, &recipientAccountState.PublicKey, transferStruct.SenderTransferAmountLo, &recipientRandomness, &loBitsScalar)

	transferStruct.Proofs.RemainingBalanceRangeProof, _ = zkproofs.NewRangeProof(128, newBalance, commitmentRandomness)
	newBalanceCalculated, _ := teg.SubWithLoHi(initialSenderState.AvailableBalance, transferStruct.SenderTransferAmountLo, transferStruct.SenderTransferAmountHi)
	newBalanceScalar, _ := curves.ED25519().Scalar.SetBigInt(newBalance)
	commitmentCiphertextEqualityProof, err := zkproofs.NewCiphertextCommitmentEqualityProof(senderKeyPair, newBalanceCalculated, &commitmentRandomness, &newBalanceScalar)
	transferStruct.Proofs.RemainingBalanceEqualityProof = commitmentCiphertextEqualityProof

	// Try to execute the modified transfer instruction. This should fail since the balances don't match the proof generated
	req := types.NewMsgTransferProto(transferStruct)
	_, err = suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)

	senderAccountState, _ := suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, senderAddr.String(), DefaultTestDenom)

	// Next 2 lines are for debugging. Remove after test passes.
	newAvailableBalance, _ := teg.Decrypt(senderKeyPair.PrivateKey, senderAccountState.AvailableBalance, elgamal.MaxBits32)
	fmt.Print(newAvailableBalance)

	suite.Require().Error(err, "Should have error transferring negative amount")
}

func (suite *KeeperTestSuite) TestMsgServer_TransferOverflowAmount() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Setup the accounts used for the test
	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)
	recipientPk := suite.PrivKeys[1]
	recipientAddr := privkeyToAddress(recipientPk)

	// Initialize the sender account
	initialAvailableBalance := big.NewInt(math.MaxUint32 + math.MaxUint16)
	initialSenderState, _ := suite.SetupAccountState(senderPk, DefaultTestDenom, 10, initialAvailableBalance, big.NewInt(3000), big.NewInt(1000))
	// Initialize the recipient account
	recipientAccountState, _ := suite.SetupAccountState(recipientPk, DefaultTestDenom, 10, initialAvailableBalance, big.NewInt(3000), big.NewInt(1000))

	senderAesKey, _ := utils.GetAESKey(*senderPk, DefaultTestDenom)

	// First create a regular transfer with a normal transfer amount
	transferStruct, _ := types.NewTransfer(
		senderPk,
		senderAddr.String(),
		recipientAddr.String(),
		DefaultTestDenom,
		initialSenderState.DecryptableAvailableBalance,
		initialSenderState.AvailableBalance,
		uint64(0),
		&recipientAccountState.PublicKey,
		nil,
	)

	if teg == nil {
		teg = elgamal.NewTwistedElgamal()
	}
	// Set transfer amount lo to a number larger than 32 bits
	transferAmount := big.NewInt(int64(math.MaxUint32 + 2))

	// Make the transfer amount lo overflow
	senderAmountLo, senderRandomness, err := teg.Encrypt(initialSenderState.PublicKey, transferAmount)
	recipientAmountLo, recipientRandomness, err := teg.Encrypt(recipientAccountState.PublicKey, transferAmount)
	transferStruct.SenderTransferAmountLo = senderAmountLo
	transferStruct.RecipientTransferAmountLo = recipientAmountLo

	// Regenerate the proofs
	transferStruct.Proofs.SenderTransferAmountLoValidityProof, _ = zkproofs.NewCiphertextValidityProof(&senderRandomness, initialSenderState.PublicKey, transferStruct.SenderTransferAmountLo, transferAmount)
	transferStruct.Proofs.RecipientTransferAmountLoValidityProof, _ = zkproofs.NewCiphertextValidityProof(&recipientRandomness, recipientAccountState.PublicKey, transferStruct.RecipientTransferAmountLo, transferAmount)

	newBalance := new(big.Int).Sub(initialAvailableBalance, transferAmount)
	remainingCommitment, commitmentRandomness, err := teg.Encrypt(initialSenderState.PublicKey, newBalance)
	transferStruct.RemainingBalanceCommitment = remainingCommitment
	transferStruct.Proofs.RemainingBalanceCommitmentValidityProof, _ = zkproofs.NewCiphertextValidityProof(&commitmentRandomness, initialSenderState.PublicKey, remainingCommitment, newBalance)

	transferStruct.DecryptableBalance, _ = encryption.EncryptAESGCM(newBalance, senderAesKey)

	senderKeyPair, _ := utils.GetElGamalKeyPair(*senderPk, DefaultTestDenom)
	loBitsScalar, _ := curves.ED25519().Scalar.SetBigInt(transferAmount)
	transferStruct.Proofs.TransferAmountLoEqualityProof, err = zkproofs.NewCiphertextCiphertextEqualityProof(senderKeyPair, &recipientAccountState.PublicKey, transferStruct.SenderTransferAmountLo, &recipientRandomness, &loBitsScalar)

	transferStruct.Proofs.RemainingBalanceRangeProof, _ = zkproofs.NewRangeProof(128, newBalance, commitmentRandomness)
	newBalanceCalculated, _ := teg.SubWithLoHi(initialSenderState.AvailableBalance, transferStruct.SenderTransferAmountLo, transferStruct.SenderTransferAmountHi)
	newBalanceScalar, _ := curves.ED25519().Scalar.SetBigInt(newBalance)
	commitmentCiphertextEqualityProof, err := zkproofs.NewCiphertextCommitmentEqualityProof(senderKeyPair, newBalanceCalculated, &commitmentRandomness, &newBalanceScalar)
	transferStruct.Proofs.RemainingBalanceEqualityProof = commitmentCiphertextEqualityProof

	// Try to execute the modified transfer instruction. This should fail since the balances don't match the proof generated
	req := types.NewMsgTransferProto(transferStruct)
	_, err = suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)

	recipientAccountState, _ = suite.App.ConfidentialTransfersKeeper.GetAccount(suite.Ctx, recipientAddr.String(), DefaultTestDenom)

	// Next 3 lines are for debugging. Remove after test passes.
	recipientKeyPair, _ := utils.GetElGamalKeyPair(*recipientPk, DefaultTestDenom)
	newPendingBalance, err := teg.Decrypt(recipientKeyPair.PrivateKey, recipientAccountState.AvailableBalance, elgamal.MaxBits32)
	fmt.Print(newPendingBalance)

	suite.Require().Error(err, "Should have error transferring overflow amount")
}

func (suite *KeeperTestSuite) TestMsgServer_TransferTooManyAuditors() {
	suite.Ctx = suite.App.BaseApp.NewContext(false, tmproto.Header{})

	// Setup the accounts
	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)
	recipientPk := suite.PrivKeys[1]
	recipientAddr := privkeyToAddress(recipientPk)

	// Initialize sender and recipient accounts
	initialSenderState, _ := suite.SetupAccountState(senderPk, DefaultTestDenom, 10, big.NewInt(2000), big.NewInt(3000), big.NewInt(1000))
	initialRecipientState, _ := suite.SetupAccountState(recipientPk, DefaultTestDenom, 12, big.NewInt(5000), big.NewInt(21000), big.NewInt(201000))

	// Setup 6 auditor accounts (exceeding MaxAuditors)
	var auditors []types.AuditorInput
	for i := 0; i < 8; i++ {
		auditorPk := suite.PrivKeys[i%2]
		auditorAddr := privkeyToAddress(auditorPk)
		auditorState, _ := suite.SetupAccountState(auditorPk, DefaultTestDenom, 12, big.NewInt(5000), big.NewInt(21000), big.NewInt(201000))
		auditors = append(auditors, types.AuditorInput{
			Address: auditorAddr.String(),
			Pubkey:  &auditorState.PublicKey,
		})
	}

	transferAmount := uint64(500)

	// Create transfer with too many auditors
	transferStruct, err := types.NewTransfer(
		senderPk,
		senderAddr.String(),
		recipientAddr.String(),
		DefaultTestDenom,
		initialSenderState.DecryptableAvailableBalance,
		initialSenderState.AvailableBalance,
		transferAmount,
		&initialRecipientState.PublicKey,
		auditors,
	)
	suite.Require().NoError(err, "Should be able to create transfer struct")

	// Execute the transfer
	req := types.NewMsgTransferProto(transferStruct)
	_, err = suite.msgServer.Transfer(sdk.WrapSDKContext(suite.Ctx), req)
	suite.Require().Error(err, "Should fail with too many auditors")
	suite.Require().ErrorContains(err, "maximum number of auditors exceeded")
}
