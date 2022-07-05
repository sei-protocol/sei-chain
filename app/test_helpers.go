package app

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/CosmWasm/wasmd/x/wasm"
	crptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

const TEST_CONTRACT = "TEST"

type TestWrapper struct {
	suite.Suite

	App *App
	Ctx sdk.Context
}

func NewTestWrapper(t *testing.T, tm time.Time, valPub crptotypes.PubKey) *TestWrapper {
	appPtr := Setup(false)
	wrapper := &TestWrapper{
		App: appPtr,
		Ctx: appPtr.BaseApp.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: tm}),
	}
	wrapper.SetT(t)
	wrapper.setupValidator(stakingtypes.Bonded, valPub)
	return wrapper
}

func (s *TestWrapper) FundAcc(acc sdk.AccAddress, amounts sdk.Coins) {
	err := s.App.BankKeeper.MintCoins(s.Ctx, minttypes.ModuleName, amounts)
	s.Require().NoError(err)

	err = s.App.BankKeeper.SendCoinsFromModuleToAccount(s.Ctx, minttypes.ModuleName, acc, amounts)
	s.Require().NoError(err)
}

func (s *TestWrapper) setupValidator(bondStatus stakingtypes.BondStatus, valPub crptotypes.PubKey) sdk.ValAddress {
	valAddr := sdk.ValAddress(valPub.Address())
	bondDenom := s.App.StakingKeeper.GetParams(s.Ctx).BondDenom
	selfBond := sdk.NewCoins(sdk.Coin{Amount: sdk.NewInt(100), Denom: bondDenom})

	s.FundAcc(sdk.AccAddress(valAddr), selfBond)

	sh := teststaking.NewHelper(s.Suite.T(), s.Ctx, s.App.StakingKeeper)
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

func (s *TestWrapper) BeginBlock() {
	var proposer sdk.ValAddress

	validators := s.App.StakingKeeper.GetAllValidators(s.Ctx)
	s.Require().Equal(1, len(validators))

	valAddrFancy, err := validators[0].GetConsAddr()
	s.Require().NoError(err)
	proposer = valAddrFancy.Bytes()

	validator, found := s.App.StakingKeeper.GetValidator(s.Ctx, proposer)
	s.Assert().True(found)

	valConsAddr, err := validator.GetConsAddr()

	s.Require().NoError(err)

	valAddr := valConsAddr.Bytes()

	newBlockTime := s.Ctx.BlockTime().Add(2 * time.Second)

	header := tmproto.Header{Height: s.Ctx.BlockHeight() + 1, Time: newBlockTime}
	newCtx := s.Ctx.WithBlockTime(newBlockTime).WithBlockHeight(s.Ctx.BlockHeight() + 1)
	s.Ctx = newCtx
	lastCommitInfo := abci.LastCommitInfo{
		Votes: []abci.VoteInfo{{
			Validator:       abci.Validator{Address: valAddr, Power: 1000},
			SignedLastBlock: true,
		}},
	}
	reqBeginBlock := abci.RequestBeginBlock{Header: header, LastCommitInfo: lastCommitInfo}

	s.App.BeginBlocker(s.Ctx, reqBeginBlock)
}

func (s *TestWrapper) EndBlock() {
	reqEndBlock := abci.RequestEndBlock{Height: s.Ctx.BlockHeight()}
	s.App.EndBlocker(s.Ctx, reqEndBlock)
}

type EmptyAppOptions struct{}

func (ao EmptyAppOptions) Get(o string) interface{} {
	return nil
}

func Setup(isCheckTx bool) *App {
	db := dbm.NewMemDB()
	encodingConfig := MakeEncodingConfig()
	cdc := encodingConfig.Marshaler
	app := New(
		log.NewNopLogger(),
		db,
		nil,
		true,
		map[int64]bool{},
		DefaultNodeHome,
		5,
		encodingConfig,
		wasm.EnableAllProposals,
		EmptyAppOptions{},
		EmptyWasmOpts,
	)
	if !isCheckTx {
		genesisState := NewDefaultGenesisState(cdc)
		stateBytes, err := json.MarshalIndent(genesisState, "", " ")
		if err != nil {
			panic(err)
		}

		app.InitChain(
			abci.RequestInitChain{
				Validators:      []abci.ValidatorUpdate{},
				ConsensusParams: simapp.DefaultConsensusParams,
				AppStateBytes:   stateBytes,
			},
		)
	}

	return app
}

func SetupTestingAppWithLevelDb(isCheckTx bool) (*App, func()) {
	dir := "sei_testing"
	db, err := sdk.NewLevelDB("sei_leveldb_testing", dir)
	if err != nil {
		panic(err)
	}
	encodingConfig := MakeEncodingConfig()
	cdc := encodingConfig.Marshaler
	app := New(
		log.NewNopLogger(),
		db,
		nil,
		true,
		map[int64]bool{},
		DefaultNodeHome,
		5,
		encodingConfig,
		wasm.EnableAllProposals,
		EmptyAppOptions{},
		EmptyWasmOpts,
	)
	if !isCheckTx {
		genesisState := NewDefaultGenesisState(cdc)
		stateBytes, err := json.MarshalIndent(genesisState, "", " ")
		if err != nil {
			panic(err)
		}

		app.InitChain(
			abci.RequestInitChain{
				Validators:      []abci.ValidatorUpdate{},
				ConsensusParams: simapp.DefaultConsensusParams,
				AppStateBytes:   stateBytes,
			},
		)
	}

	cleanupFn := func() {
		db.Close()
		err = os.RemoveAll(dir)
		if err != nil {
			panic(err)
		}
	}

	return app, cleanupFn
}
