package keeper_test

import (
	"crypto/ecdsa"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func (suite *KeeperTestSuite) TestDefaultGenesisState() {
	genesisState := types.DefaultGenesisState()

	app := suite.App
	suite.Ctx = app.BaseApp.NewContext(false, tmproto.Header{})

	suite.App.ConfidentialTransfersKeeper.InitGenesis(suite.Ctx, genesisState)
	exportedGenesis := suite.App.ConfidentialTransfersKeeper.ExportGenesis(suite.Ctx)
	suite.Require().NotNil(exportedGenesis)
	suite.Require().Equal(genesisState, exportedGenesis)
}

func (suite *KeeperTestSuite) TestGenesisExportImportState() {
	pk1, _ := encryption.GenerateKey()
	pk2, _ := encryption.GenerateKey()
	addr1 := sdk.AccAddress("addr1")
	addr2 := sdk.AccAddress("addr2")
	testDenom1 := fmt.Sprintf("factory/%s/TEST1", addr1.String())
	testDenom2 := fmt.Sprintf("factory/%s/TEST2", addr2.String())

	ctAcc1 := generateCtAccount(pk1, testDenom1, 1000)
	ctAcc2 := generateCtAccount(pk2, testDenom2, 2000)

	accounts := []types.GenesisCtAccount{
		{
			Key:     types.GetAccountKey(addr1, testDenom1),
			Account: ctAcc1,
		},
		{
			Key:     types.GetAccountKey(addr2, testDenom2),
			Account: ctAcc2,
		},
	}
	genesisState := types.NewGenesisState(types.DefaultParams(), accounts)
	app := suite.App
	suite.Ctx = app.BaseApp.NewContext(false, tmproto.Header{})

	suite.App.ConfidentialTransfersKeeper.InitGenesis(suite.Ctx, genesisState)
	exportedGenesis := suite.App.ConfidentialTransfersKeeper.ExportGenesis(suite.Ctx)
	suite.Require().NotNil(exportedGenesis)
	suite.Require().Equal(genesisState, exportedGenesis)
}

func generateCtAccount(pk *ecdsa.PrivateKey, testDenom string, balance uint64) types.CtAccount {
	eg := elgamal.NewTwistedElgamal()
	keyPair, _ := eg.KeyGen(*pk, testDenom)

	aesPK1, _ := encryption.GetAESKey(*pk, testDenom)

	amountLo := uint64(100)
	amountHi := uint64(0)

	decryptableBalance, _ := encryption.EncryptAESGCM(balance, aesPK1)

	ciphertextLo, _, _ := eg.Encrypt(keyPair.PublicKey, amountLo)
	ciphertextHi, _, _ := eg.Encrypt(keyPair.PublicKey, amountHi)

	zeroCiphertextAvailable, _, _ := eg.Encrypt(keyPair.PublicKey, amountLo)

	account := &types.Account{
		PublicKey:                   keyPair.PublicKey,
		PendingBalanceLo:            ciphertextLo,
		PendingBalanceHi:            ciphertextHi,
		PendingBalanceCreditCounter: 1,
		AvailableBalance:            zeroCiphertextAvailable,
		DecryptableAvailableBalance: decryptableBalance,
	}

	return *types.NewCtAccount(account)
}
