package apptesting

import (
	"context"
	"time"

	"github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/rootmulti"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	authsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/teststaking"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/suite"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/app"
)

type KeeperTestHelper struct {
	suite.Suite

	App         *app.App
	Ctx         sdk.Context
	QueryHelper *baseapp.QueryServiceTestHelper
	TestAccs    []sdk.AccAddress
}

// Setup sets up basic environment for suite (App, Ctx, and test accounts)
func (s *KeeperTestHelper) Setup() {
	s.App = app.Setup(s.T(), false, false, false)
	s.Ctx = s.App.NewContext(false, tmtypes.Header{Height: 1, ChainID: "sei-test", Time: time.Now().UTC()})
	s.QueryHelper = &baseapp.QueryServiceTestHelper{
		GRPCQueryRouter: s.App.GRPCQueryRouter(),
		Ctx:             s.Ctx,
	}

	s.TestAccs = CreateRandomAccounts(3)
}

// CreateTestContext creates a test context.
func (s *KeeperTestHelper) CreateTestContext() sdk.Context {
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()

	ms := rootmulti.NewStore(db, log.NewNopLogger())

	return sdk.NewContext(ms, tmtypes.Header{}, false, logger)
}

// CreateTestContext creates a test context.
func (s *KeeperTestHelper) Commit() {
	oldHeight := s.Ctx.BlockHeight()
	oldHeader := s.Ctx.BlockHeader()
	_, err := s.App.Commit(context.Background())
	if err != nil {
		panic(err)
	}
	newHeader := tmtypes.Header{Height: oldHeight + 1, ChainID: oldHeader.ChainID, Time: time.Now().UTC()}
	legacyabci.BeginBlock(s.Ctx, newHeader.Height, []abci.VoteInfo{}, []abci.Misbehavior{}, s.App.BeginBlockKeepers)
	s.Ctx = s.App.GetBaseApp().NewContext(false, newHeader)
}

// FundAcc funds target address with specified amount.
func (s *KeeperTestHelper) FundAcc(acc sdk.AccAddress, amounts sdk.Coins) {
	err := FundAccount(s.App.BankKeeper, s.Ctx, acc, amounts)
	s.Require().NoError(err)
}

// SetupValidator sets up a validator and returns the ValAddress.
func (s *KeeperTestHelper) SetupValidator(bondStatus stakingtypes.BondStatus) sdk.ValAddress {
	valPub := secp256k1.GenPrivKey().PubKey()
	valAddr := sdk.ValAddress(valPub.Address())
	bondDenom := s.App.StakingKeeper.GetParams(s.Ctx).BondDenom
	selfBond := sdk.NewCoins(sdk.Coin{Amount: sdk.NewInt(100), Denom: bondDenom})

	s.FundAcc(sdk.AccAddress(valAddr), selfBond)

	sh := teststaking.NewHelper(s.T(), s.Ctx, s.App.StakingKeeper)
	msg := sh.CreateValidatorMsg(valAddr, valPub, selfBond[0].Amount)
	sh.Handle(msg, true)

	val, found := s.App.StakingKeeper.GetValidator(s.Ctx, valAddr)
	s.Require().True(found)

	val = val.UpdateStatus(bondStatus)
	s.App.StakingKeeper.SetValidator(s.Ctx, val)

	consAddr, err := val.GetConsAddr()
	s.Suite.Require().NoError(err)

	signingInfo := slashingtypes.NewValidatorSigningInfo(
		consAddr,
		s.Ctx.BlockHeight(),
		0,
		time.Unix(0, 0),
		false,
		0,
	)
	s.App.SlashingKeeper.SetValidatorSigningInfo(s.Ctx, consAddr, signingInfo)

	return valAddr
}

// SetupTokenFactory sets up a token module account for the TokenFactoryKeeper.
func (s *KeeperTestHelper) SetupTokenFactory() {
	s.App.TokenFactoryKeeper.CreateModuleAccount(s.Ctx)
}

// EndBlock ends the block.
func (s *KeeperTestHelper) EndBlock() {
	legacyabci.EndBlock(s.Ctx, s.Ctx.BlockHeight(), 0, s.App.EndBlockKeepers)
}

// AllocateRewardsToValidator allocates reward tokens to a distribution module then allocates rewards to the validator address.
func (s *KeeperTestHelper) AllocateRewardsToValidator(valAddr sdk.ValAddress, rewardAmt sdk.Int) {
	validator, found := s.App.StakingKeeper.GetValidator(s.Ctx, valAddr)
	s.Require().True(found)

	// allocate reward tokens to distribution module
	coins := sdk.Coins{sdk.NewCoin(sdk.DefaultBondDenom, rewardAmt)}
	err := FundModuleAccount(s.App.BankKeeper, s.Ctx, distrtypes.ModuleName, coins)
	s.Require().NoError(err)

	// allocate rewards to validator
	s.Ctx = s.Ctx.WithBlockHeight(s.Ctx.BlockHeight() + 1)
	decTokens := sdk.DecCoins{{Denom: sdk.DefaultBondDenom, Amount: sdk.NewDec(20000)}}
	s.App.DistrKeeper.AllocateTokensToValidator(s.Ctx, validator, decTokens)
}

// BuildTx builds a transaction.
func (s *KeeperTestHelper) BuildTx(
	txBuilder client.TxBuilder,
	msgs []sdk.Msg,
	sigV2 signing.SignatureV2,
	memo string,
	txFee sdk.Coins,
	gasLimit uint64,
) authsigning.Tx {
	err := txBuilder.SetMsgs(msgs[0])
	s.Require().NoError(err)

	err = txBuilder.SetSignatures(sigV2)
	s.Require().NoError(err)

	txBuilder.SetMemo(memo)
	txBuilder.SetFeeAmount(txFee)
	txBuilder.SetGasLimit(gasLimit)

	return txBuilder.GetTx()
}

// CreateRandomAccounts is a function return a list of randomly generated AccAddresses
func CreateRandomAccounts(numAccts int) []sdk.AccAddress {
	testAddrs := make([]sdk.AccAddress, numAccts)
	for i := 0; i < numAccts; i++ {
		pk := ed25519.GenerateSecretKey().Public()
		testAddrs[i] = sdk.AccAddress(pk.Address())
	}

	return testAddrs
}

func GenerateTestAddrs() (string, string) {
	pk1 := ed25519.GenerateSecretKey().Public()
	validAddr := sdk.AccAddress(pk1.Address()).String()
	invalidAddr := sdk.AccAddress("invalid").String()
	return validAddr, invalidAddr
}

type bankKeeper interface {
	MintCoins(ctx sdk.Context, moduleName string, amounts sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx sdk.Context, moduleName string, recipient sdk.AccAddress, amounts sdk.Coins) error
	SendCoinsFromModuleToModule(ctx sdk.Context, senderModule string, recipientModule string, amounts sdk.Coins) error
}

func FundAccount(bankKeeper bankKeeper, ctx sdk.Context, addr sdk.AccAddress, amounts sdk.Coins) error {
	if err := bankKeeper.MintCoins(ctx, minttypes.ModuleName, amounts); err != nil {
		return err
	}

	return bankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, addr, amounts)
}

func FundModuleAccount(bankKeeper bankKeeper, ctx sdk.Context, recipientMod string, amounts sdk.Coins) error {
	if err := bankKeeper.MintCoins(ctx, minttypes.ModuleName, amounts); err != nil {
		return err
	}

	return bankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, recipientMod, amounts)
}
