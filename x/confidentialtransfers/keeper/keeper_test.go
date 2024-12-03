package keeper_test

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/keeper"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/stretchr/testify/suite"
)

const DefaultTestDenom = "factory/creator/test"
const DefaultOtherDenom = "factory/creator/other"

type KeeperTestSuite struct {
	apptesting.KeeperTestHelper

	queryClient types.QueryClient
	msgServer   types.MsgServer
	// defaultDenom is on the suite, as it depends on the creator test address.
	defaultDenom string

	PrivKeys []*ecdsa.PrivateKey
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) SetupTest() {
	suite.Setup()

	suite.queryClient = types.NewQueryClient(suite.QueryHelper)
	suite.msgServer = keeper.NewMsgServerImpl(suite.App.ConfidentialTransfersKeeper)

	// TODO: remove this once the app initializes confidentialtransfers keeper
	suite.App.ConfidentialTransfersKeeper = keeper.NewKeeper(
		suite.App.AppCodec(),
		suite.App.GetKey(types.StoreKey),
		suite.App.GetSubspace(types.ModuleName),
		suite.App.AccountKeeper,
		suite.App.BankKeeper,
	)
	suite.msgServer = keeper.NewMsgServerImpl(suite.App.ConfidentialTransfersKeeper)
	suite.PrivKeys = apptesting.CreateRandomAccountKeys(3)
	suite.App.TokenFactoryKeeper.CreateDenom(suite.Ctx, "creator", "test")
	suite.App.TokenFactoryKeeper.CreateDenom(suite.Ctx, "creator", "other")
}

func (suite *KeeperTestSuite) SetupAccount() {
	suite.queryClient = types.NewQueryClient(suite.QueryHelper)
	// TODO: remove this once the app initializes confidentialtransfers keeper
	suite.App.ConfidentialTransfersKeeper = keeper.NewKeeper(
		suite.App.AppCodec(),
		suite.App.GetKey(types.StoreKey),
		suite.App.GetSubspace(types.ModuleName),
		suite.App.AccountKeeper,
		suite.App.BankKeeper,
	)
	suite.msgServer = keeper.NewMsgServerImpl(suite.App.ConfidentialTransfersKeeper)
	suite.PrivKeys = apptesting.CreateRandomAccountKeys(4)
}

func (suite *KeeperTestSuite) SetupAccountState(privateKey *ecdsa.PrivateKey, denom string, pendingBalanceCreditCounter uint16, initialAvailableBalance, initialPendingBalance, bankAmount *big.Int) (types.Account, error) {
	aesKey, err := encryption.GetAESKey(*privateKey, denom)
	if err != nil {
		return types.Account{}, err
	}

	teg := elgamal.NewTwistedElgamal()
	keypair, err := teg.KeyGen(*privateKey, denom)
	if err != nil {
		return types.Account{}, err
	}

	availableBalance := new(big.Int).Set(initialAvailableBalance)

	// The maximum pending balance is the max Uint32 + max Uint48 (0xFFFFFFFF + 0xFFFFFFFFFFFF = 0x10000FFFFFFFE).
	// If amount is below 64 bits, just split into top 48 bits and bottom 16 bits for simplicity. Otherwise, assume Hi bits are full and lo bits can make up the remainder.
	// Extract the bottom 16 bits (rightmost 16 bits)
	pendingBalanceHi := new(big.Int)
	pendingBalanceLo := new(big.Int)
	if initialPendingBalance.Cmp(new(big.Int).SetUint64(0xFFFFFFFFFFFFFFFF)) == 1 {
		pendingBalanceHi = pendingBalanceHi.SetUint64(0xFFFFFFFFFFFF)
		pendingBalanceLo = pendingBalanceLo.Sub(initialPendingBalance, new(big.Int).SetUint64(0xFFFFFFFFFFFFFFFF))
	} else {
		pendingBalance := initialPendingBalance.Uint64()
		pendingBalanceLo = pendingBalanceLo.SetUint64(pendingBalance & 0xFFFF)
		pendingBalanceHi = pendingBalanceHi.SetUint64((pendingBalance >> 16) & 0xFFFFFFFFFFFF)
	}

	availableBalanceCipherText, _, err := teg.Encrypt(keypair.PublicKey, availableBalance)
	if err != nil {
		return types.Account{}, err
	}

	pendingBalanceLoCipherText, _, err := teg.Encrypt(keypair.PublicKey, pendingBalanceLo)
	if err != nil {
		return types.Account{}, err
	}

	pendingBalanceHiCipherText, _, err := teg.Encrypt(keypair.PublicKey, pendingBalanceHi)
	if err != nil {
		return types.Account{}, err
	}

	decryptableAvailableBalance, err := encryption.EncryptAESGCM(availableBalance, aesKey)
	if err != nil {
		return types.Account{}, err
	}

	initialAccountState := types.Account{
		PublicKey:                   keypair.PublicKey,
		PendingBalanceLo:            pendingBalanceLoCipherText,
		PendingBalanceHi:            pendingBalanceHiCipherText,
		PendingBalanceCreditCounter: pendingBalanceCreditCounter,
		AvailableBalance:            availableBalanceCipherText,
		DecryptableAvailableBalance: decryptableAvailableBalance,
	}

	addr := privkeyToAddress(privateKey)
	err = suite.App.ConfidentialTransfersKeeper.SetAccount(suite.Ctx, addr.String(), denom, initialAccountState)
	if err != nil {
		return types.Account{}, err
	}

	bankModuleTokens := sdk.NewCoins(sdk.Coin{Amount: sdk.NewIntFromBigInt(bankAmount), Denom: denom})

	suite.FundAcc(addr, bankModuleTokens)

	return initialAccountState, nil
}

func privkeyToAddress(privateKey *ecdsa.PrivateKey) sdk.AccAddress {
	publicKeyBytes := crypto.FromECDSAPub(&privateKey.PublicKey)
	testAddr := sdk.AccAddress(crypto.Keccak256(publicKeyBytes[1:])[12:])
	return testAddr
}
