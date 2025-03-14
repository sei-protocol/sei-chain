package keeper_test

import (
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
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
	pk1, _ := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	pk2, _ := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	addr1 := sdk.AccAddress("addr1")
	addr2 := sdk.AccAddress("addr2")
	testDenom1 := fmt.Sprintf("factory/%s/TEST1", addr1.String())
	testDenom2 := fmt.Sprintf("factory/%s/TEST2", addr2.String())

	ctAcc1 := generateCtAccount(pk1, testDenom1, big.NewInt(1000))
	ctAcc2 := generateCtAccount(pk2, testDenom2, big.NewInt(2000))

	accounts := []types.GenesisCtAccount{
		{
			Key:     []byte(addr1.String() + testDenom1),
			Account: ctAcc1,
		},
		{
			Key:     []byte(addr2.String() + testDenom2),
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

func generateCtAccount(pk *ecdsa.PrivateKey, testDenom string, balance *big.Int) types.CtAccount {
	eg := elgamal.NewTwistedElgamal()
	keyPair, _ := utils.GetElGamalKeyPair(*pk, testDenom)

	aesPK1, _ := utils.GetAESKey(*pk, testDenom)

	amountLo := big.NewInt(100)
	amountHi := big.NewInt(0)

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
