package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/cosmos/cosmos-sdk/client"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	ssconfig "github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/ss"
	seidbtypes "github.com/sei-protocol/sei-db/ss/types"
	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/config"

	"bytes"
	"encoding/hex"
	"strconv"

	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmtypes "github.com/tendermint/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	bam "github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsign "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/sei-protocol/sei-chain/app/legacyabci"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
)

const TestContract = "TEST"
const TestUser = "sei1jdppe6fnj2q7hjsepty5crxtrryzhuqsjrj95y"

type TestTx struct {
	msgs []sdk.Msg
}

func NewTestTx(msgs []sdk.Msg) TestTx {
	return TestTx{msgs: msgs}
}

func (t TestTx) GetMsgs() []sdk.Msg {
	return t.msgs
}

func (t TestTx) ValidateBasic() error {
	return nil
}

func (t TestTx) GetGasEstimate() uint64 {
	return 0
}

type TestAppOpts struct {
	useSc bool
}

func (t TestAppOpts) Get(s string) interface{} {
	if s == "chain-id" {
		return "sei-test"
	}
	if s == FlagSCEnable {
		return t.useSc
	}
	return nil
}

type TestWrapper struct {
	suite.Suite

	App *App
	Ctx sdk.Context
}

func NewTestWrapper(t *testing.T, tm time.Time, valPub cryptotypes.PubKey, enableEVMCustomPrecompiles bool, baseAppOptions ...func(*bam.BaseApp)) *TestWrapper {
	return newTestWrapper(t, tm, valPub, enableEVMCustomPrecompiles, false, baseAppOptions...)
}

func NewTestWrapperWithSc(t *testing.T, tm time.Time, valPub cryptotypes.PubKey, enableEVMCustomPrecompiles bool, baseAppOptions ...func(*bam.BaseApp)) *TestWrapper {
	return newTestWrapper(t, tm, valPub, enableEVMCustomPrecompiles, true, baseAppOptions...)
}

func newTestWrapper(t *testing.T, tm time.Time, valPub cryptotypes.PubKey, enableEVMCustomPrecompiles bool, useSc bool, baseAppOptions ...func(*bam.BaseApp)) *TestWrapper {
	var appPtr *App
	originalHome := DefaultNodeHome
	tempHome := t.TempDir()
	DefaultNodeHome = tempHome
	t.Cleanup(func() {
		DefaultNodeHome = originalHome
	})
	if useSc {
		appPtr = SetupWithSc(t, false, enableEVMCustomPrecompiles, baseAppOptions...)
	} else {
		appPtr = Setup(t, false, enableEVMCustomPrecompiles, false, baseAppOptions...)
	}
	ctx := appPtr.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: tm})
	wrapper := &TestWrapper{
		App: appPtr,
		Ctx: ctx,
	}
	wrapper.SetT(t)
	wrapper.setupValidator(stakingtypes.Unbonded, valPub)
	return wrapper
}

func (s *TestWrapper) FundAcc(acc sdk.AccAddress, amounts sdk.Coins) {
	err := s.App.BankKeeper.MintCoins(s.Ctx, minttypes.ModuleName, amounts)
	s.Require().NoError(err)

	err = s.App.BankKeeper.SendCoinsFromModuleToAccount(s.Ctx, minttypes.ModuleName, acc, amounts)
	s.Require().NoError(err)
}

func (s *TestWrapper) setupValidator(bondStatus stakingtypes.BondStatus, valPub cryptotypes.PubKey) sdk.ValAddress {
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

	legacyabci.BeginBlock(s.Ctx, header.Height, lastCommitInfo.Votes, []abci.Misbehavior{}, s.App.BeginBlockKeepers)
}

func (s *TestWrapper) EndBlock() {
	reqEndBlock := abci.RequestEndBlock{Height: s.Ctx.BlockHeight()}
	s.App.EndBlocker(s.Ctx, reqEndBlock)
}

func setupReceiptStore() (seidbtypes.StateStore, error) {
	// Create a unique temporary directory per test process to avoid Pebble DB lock conflicts
	baseDir := filepath.Join(DefaultNodeHome, "test", "sei-testing")
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp(baseDir, "receipt.db-*")
	if err != nil {
		return nil, err
	}

	ssConfig := ssconfig.DefaultStateStoreConfig()
	ssConfig.KeepRecent = 0 // No min retain blocks in test
	ssConfig.DBDirectory = tempDir
	ssConfig.KeepLastVersion = false
	receiptStore, err := ss.NewStateStore(log.NewNopLogger(), tempDir, ssConfig)
	if err != nil {
		return nil, err
	}
	return receiptStore, nil
}

func SetupWithDefaultHome(isCheckTx bool, enableEVMCustomPrecompiles bool, overrideWasmGasMultiplier bool, baseAppOptions ...func(*bam.BaseApp)) (res *App) {
	encodingConfig := MakeEncodingConfig()
	cdc := encodingConfig.Marshaler

	options := []AppOption{
		func(app *App) {
			receiptStore, err := setupReceiptStore()
			if err != nil {
				panic(fmt.Sprintf("error while creating receipt store: %s", err))
			}
			app.receiptStore = receiptStore
		},
	}
	wasmOpts := EmptyWasmOpts
	if overrideWasmGasMultiplier {
		gasRegisterConfig := wasmkeeper.DefaultGasRegisterConfig()
		gasRegisterConfig.GasMultiplier = 21_000_000
		wasmOpts = []wasm.Option{
			wasmkeeper.WithGasRegister(
				wasmkeeper.NewWasmGasRegister(
					gasRegisterConfig,
				),
			),
		}
	}

	res = New(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		nil,
		true,
		map[int64]bool{},
		DefaultNodeHome,
		1,
		enableEVMCustomPrecompiles,
		config.TestConfig(),
		encodingConfig,
		wasm.EnableAllProposals,
		TestAppOpts{},
		wasmOpts,
		EmptyACLOpts,
		options,
		baseAppOptions...,
	)
	if !isCheckTx {
		genesisState := NewDefaultGenesisState(cdc)
		stateBytes, err := json.MarshalIndent(genesisState, "", " ")
		if err != nil {
			panic(err)
		}

		_, err = res.InitChain(
			context.Background(), &abci.RequestInitChain{
				Validators:      []abci.ValidatorUpdate{},
				ConsensusParams: DefaultConsensusParams,
				AppStateBytes:   stateBytes,
			},
		)
		if err != nil {
			panic(err)
		}
	}

	return res
}

func Setup(t *testing.T, isCheckTx bool, enableEVMCustomPrecompiles bool, overrideWasmGasMultiplier bool, baseAppOptions ...func(*bam.BaseApp)) (res *App) {
	db := dbm.NewMemDB()
	return SetupWithDB(t, db, isCheckTx, enableEVMCustomPrecompiles, overrideWasmGasMultiplier, baseAppOptions...)
}

func SetupWithDB(t *testing.T, db dbm.DB, isCheckTx bool, enableEVMCustomPrecompiles bool, overrideWasmGasMultiplier bool, baseAppOptions ...func(*bam.BaseApp)) (res *App) {
	encodingConfig := MakeEncodingConfig()
	cdc := encodingConfig.Marshaler

	options := []AppOption{
		func(app *App) {
			receiptStore, err := setupReceiptStore()
			if err != nil {
				panic(fmt.Sprintf("error while creating receipt store: %s", err))
			}
			app.receiptStore = receiptStore
		},
	}
	wasmOpts := EmptyWasmOpts
	if overrideWasmGasMultiplier {
		gasRegisterConfig := wasmkeeper.DefaultGasRegisterConfig()
		gasRegisterConfig.GasMultiplier = 21_000_000
		wasmOpts = []wasm.Option{
			wasmkeeper.WithGasRegister(
				wasmkeeper.NewWasmGasRegister(
					gasRegisterConfig,
				),
			),
		}
	}

	res = New(
		log.NewNopLogger(),
		db,
		nil,
		true,
		map[int64]bool{},
		t.TempDir(),
		1,
		enableEVMCustomPrecompiles,
		config.TestConfig(),
		encodingConfig,
		wasm.EnableAllProposals,
		TestAppOpts{},
		wasmOpts,
		EmptyACLOpts,
		options,
		baseAppOptions...,
	)
	if !isCheckTx {
		genesisState := NewDefaultGenesisState(cdc)
		stateBytes, err := json.MarshalIndent(genesisState, "", " ")
		if err != nil {
			panic(err)
		}

		_, err = res.InitChain(
			context.Background(), &abci.RequestInitChain{
				Validators:      []abci.ValidatorUpdate{},
				ConsensusParams: DefaultConsensusParams,
				AppStateBytes:   stateBytes,
			},
		)
		if err != nil {
			panic(err)
		}
	}

	return res
}

func SetupWithSc(t *testing.T, isCheckTx bool, enableEVMCustomPrecompiles bool, baseAppOptions ...func(*bam.BaseApp)) (res *App) {
	db := dbm.NewMemDB()
	encodingConfig := MakeEncodingConfig()
	cdc := encodingConfig.Marshaler

	options := []AppOption{
		func(app *App) {
			receiptStore, err := setupReceiptStore()
			if err != nil {
				panic(fmt.Sprintf("error while creating receipt store: %s", err))
			}
			app.receiptStore = receiptStore
		},
	}

	res = New(
		log.NewNopLogger(),
		db,
		nil,
		true,
		map[int64]bool{},
		t.TempDir(),
		1,
		enableEVMCustomPrecompiles,
		config.TestConfig(),
		encodingConfig,
		wasm.EnableAllProposals,
		TestAppOpts{true},
		EmptyWasmOpts,
		EmptyACLOpts,
		options,
		baseAppOptions...,
	)
	if !isCheckTx {
		genesisState := NewDefaultGenesisState(cdc)
		stateBytes, err := json.MarshalIndent(genesisState, "", " ")
		if err != nil {
			panic(err)
		}

		// TODO: remove once init chain works with SC
		defer func() { _ = recover() }()

		_, err = res.InitChain(
			context.Background(), &abci.RequestInitChain{
				Validators:      []abci.ValidatorUpdate{},
				ConsensusParams: DefaultConsensusParams,
				AppStateBytes:   stateBytes,
			},
		)
		if err != nil {
			panic(err)
		}
	}

	return res
}

func SetupTestingAppWithLevelDb(t *testing.T, isCheckTx bool, enableEVMCustomPrecompiles bool) (*App, func()) {
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
		t.TempDir(),
		5,
		enableEVMCustomPrecompiles,
		nil,
		encodingConfig,
		wasm.EnableAllProposals,
		TestAppOpts{},
		EmptyWasmOpts,
		EmptyACLOpts,
		nil,
	)
	if !isCheckTx {
		genesisState := NewDefaultGenesisState(cdc)
		stateBytes, err := json.MarshalIndent(genesisState, "", " ")
		if err != nil {
			panic(err)
		}

		_, err = app.InitChain(
			context.Background(), &abci.RequestInitChain{
				Validators:      []abci.ValidatorUpdate{},
				ConsensusParams: DefaultConsensusParams,
				AppStateBytes:   stateBytes,
			},
		)
		if err != nil {
			panic(err)
		}
	}

	cleanupFn := func() {
		_ = db.Close()
		err = os.RemoveAll(dir)
		if err != nil {
			panic(err)
		}
	}

	return app, cleanupFn
}

// DefaultConsensusParams defines the default Tendermint consensus params used in
// SimApp testing.
var DefaultConsensusParams = &tmproto.ConsensusParams{
	Block: &tmproto.BlockParams{
		MaxBytes: 200000,
		MaxGas:   100000000,
	},
	Evidence: &tmproto.EvidenceParams{
		MaxAgeNumBlocks: 302400,
		MaxAgeDuration:  504 * time.Hour, // 3 weeks is the max duration
		MaxBytes:        10000,
	},
	Validator: &tmproto.ValidatorParams{
		PubKeyTypes: []string{
			tmtypes.ABCIPubKeyTypeEd25519,
		},
	},
}

func setup(t *testing.T, withGenesis bool, invCheckPeriod uint) (*App, GenesisState) {
	db := dbm.NewMemDB()
	encCdc := MakeEncodingConfig()
	app := New(
		log.NewNopLogger(),
		db,
		nil,
		true,
		map[int64]bool{},
		t.TempDir(),
		1,
		false,
		config.TestConfig(),
		encCdc,
		wasm.EnableAllProposals,
		TestAppOpts{},
		EmptyWasmOpts,
		EmptyACLOpts,
		[]AppOption{},
	)
	if withGenesis {
		return app, NewDefaultGenesisState(encCdc.Marshaler)
	}
	return app, GenesisState{}
}

// SetupWithGenesisValSet initializes a new SimApp with a validator set and genesis accounts
// that also act as delegators. For simplicity, each validator is bonded with a delegation
// of one consensus engine unit (10^6) in the default token of the simapp from first genesis
// account. A Nop logger is set in SimApp.
func SetupWithGenesisValSet(t *testing.T, valSet *tmtypes.ValidatorSet, genAccs []authtypes.GenesisAccount, balances ...banktypes.Balance) *App {
	app, genesisState := setup(t, true, 5)
	// set genesis accounts
	authGenesis := authtypes.NewGenesisState(authtypes.DefaultParams(), genAccs)
	genesisState[authtypes.ModuleName] = app.AppCodec().MustMarshalJSON(authGenesis)

	validators := make([]stakingtypes.Validator, 0, len(valSet.Validators))
	delegations := make([]stakingtypes.Delegation, 0, len(valSet.Validators))

	bondAmt := sdk.NewInt(1000000)

	for _, val := range valSet.Validators {
		pk, err := cryptocodec.FromTmPubKeyInterface(val.PubKey)
		require.NoError(t, err)
		pkAny, err := codectypes.NewAnyWithValue(pk)
		require.NoError(t, err)
		validator := stakingtypes.Validator{
			OperatorAddress:   sdk.ValAddress(val.Address).String(),
			ConsensusPubkey:   pkAny,
			Jailed:            false,
			Status:            stakingtypes.Bonded,
			Tokens:            bondAmt,
			DelegatorShares:   sdk.OneDec(),
			Description:       stakingtypes.Description{},
			UnbondingHeight:   int64(0),
			UnbondingTime:     time.Unix(0, 0).UTC(),
			Commission:        stakingtypes.NewCommission(sdk.ZeroDec(), sdk.ZeroDec(), sdk.ZeroDec()),
			MinSelfDelegation: sdk.ZeroInt(),
		}
		validators = append(validators, validator)
		delegations = append(delegations, stakingtypes.NewDelegation(genAccs[0].GetAddress(), val.Address.Bytes(), sdk.OneDec()))

	}
	// set validators and delegations
	stakingGenesis := stakingtypes.NewGenesisState(stakingtypes.DefaultParams(), validators, delegations)
	genesisState[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)

	totalSupply := sdk.NewCoins()
	for _, b := range balances {
		// add genesis acc tokens and delegated tokens to total supply
		totalSupply = totalSupply.Add(b.Coins.Add(sdk.NewCoin(sdk.DefaultBondDenom, bondAmt))...)
	}

	// add bonded amount to bonded pool module account
	balances = append(balances, banktypes.Balance{
		Address: authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String(),
		Coins:   sdk.Coins{sdk.NewCoin(sdk.DefaultBondDenom, bondAmt)},
	})

	// update total supply
	bankGenesis := banktypes.NewGenesisState(banktypes.DefaultGenesisState().Params, balances, totalSupply, []banktypes.Metadata{}, []banktypes.WeiBalance{})
	genesisState[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)

	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(t, err)

	// init chain will set the validator set and initialize the genesis accounts
	_, _ = app.InitChain(
		context.Background(), &abci.RequestInitChain{
			Validators:      []abci.ValidatorUpdate{},
			ConsensusParams: DefaultConsensusParams,
			AppStateBytes:   stateBytes,
		},
	)

	// commit genesis changes
	_, _ = app.Commit(context.Background())
	_, _ = app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Height:             app.LastBlockHeight() + 1,
		Hash:               app.LastCommitID().Hash,
		NextValidatorsHash: valSet.Hash(),
	})

	return app
}

// SetupWithGenesisAccounts initializes a new SimApp with the provided genesis
// accounts and possible balances.
func SetupWithGenesisAccounts(t *testing.T, genAccs []authtypes.GenesisAccount, balances ...banktypes.Balance) *App {
	app, genesisState := setup(t, true, 0)
	authGenesis := authtypes.NewGenesisState(authtypes.DefaultParams(), genAccs)
	genesisState[authtypes.ModuleName] = app.AppCodec().MustMarshalJSON(authGenesis)

	totalSupply := sdk.NewCoins()
	for _, b := range balances {
		totalSupply = totalSupply.Add(b.Coins...)
	}

	bankGenesis := banktypes.NewGenesisState(banktypes.DefaultGenesisState().Params, balances, totalSupply, []banktypes.Metadata{}, []banktypes.WeiBalance{})
	genesisState[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)

	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	if err != nil {
		panic(err)
	}

	_, _ = app.InitChain(
		context.Background(), &abci.RequestInitChain{
			Validators:      []abci.ValidatorUpdate{},
			ConsensusParams: DefaultConsensusParams,
			AppStateBytes:   stateBytes,
		},
	)

	_, _ = app.Commit(context.Background())
	_, _ = app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: app.LastBlockHeight() + 1})

	return app
}

type GenerateAccountStrategy func(int) []sdk.AccAddress

// createRandomAccounts is a strategy used by addTestAddrs() in order to generated addresses in random order.
func createRandomAccounts(accNum int) []sdk.AccAddress {
	testAddrs := make([]sdk.AccAddress, accNum)
	for i := 0; i < accNum; i++ {
		pk := ed25519.GenPrivKey().PubKey()
		testAddrs[i] = sdk.AccAddress(pk.Address())
	}

	return testAddrs
}

// createIncrementalAccounts is a strategy used by addTestAddrs() in order to generated addresses in ascending order.
func createIncrementalAccounts(accNum int) []sdk.AccAddress {
	var addresses []sdk.AccAddress
	var buffer bytes.Buffer

	// start at 100 so we can make up to 999 test addresses with valid test addresses
	for i := 100; i < (accNum + 100); i++ {
		numString := strconv.Itoa(i)
		buffer.WriteString("A58856F0FD53BF058B4909A21AEC019107BA6") // base address string

		buffer.WriteString(numString) // adding on final two digits to make addresses unique
		res, _ := sdk.AccAddressFromHex(buffer.String())
		bech := res.String()
		addr, _ := TestAddr(buffer.String(), bech)

		addresses = append(addresses, addr)
		buffer.Reset()
	}

	return addresses
}

// AddTestAddrsFromPubKeys adds the addresses into the SimApp providing only the public keys.
func AddTestAddrsFromPubKeys(app *App, ctx sdk.Context, pubKeys []cryptotypes.PubKey, accAmt sdk.Int) {
	initCoins := sdk.NewCoins(sdk.NewCoin(app.StakingKeeper.BondDenom(ctx), accAmt))

	for _, pk := range pubKeys {
		initAccountWithCoins(app, ctx, sdk.AccAddress(pk.Address()), initCoins)
	}
}

// AddTestAddrs constructs and returns accNum amount of accounts with an
// initial balance of accAmt in random order
func AddTestAddrs(app *App, ctx sdk.Context, accNum int, accAmt sdk.Int) []sdk.AccAddress {
	return addTestAddrs(app, ctx, accNum, accAmt, createRandomAccounts)
}

// AddTestAddrs constructs and returns accNum amount of accounts with an
// initial balance of accAmt in random order
func AddTestAddrsIncremental(app *App, ctx sdk.Context, accNum int, accAmt sdk.Int) []sdk.AccAddress {
	return addTestAddrs(app, ctx, accNum, accAmt, createIncrementalAccounts)
}

func addTestAddrs(app *App, ctx sdk.Context, accNum int, accAmt sdk.Int, strategy GenerateAccountStrategy) []sdk.AccAddress {
	testAddrs := strategy(accNum)

	initCoins := sdk.NewCoins(sdk.NewCoin(app.StakingKeeper.BondDenom(ctx), accAmt))

	for _, addr := range testAddrs {
		initAccountWithCoins(app, ctx, addr, initCoins)
	}

	return testAddrs
}

func initAccountWithCoins(app *App, ctx sdk.Context, addr sdk.AccAddress, coins sdk.Coins) {
	err := app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins)
	if err != nil {
		panic(err)
	}

	err = app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, addr, coins)
	if err != nil {
		panic(err)
	}
}

// ConvertAddrsToValAddrs converts the provided addresses to ValAddress.
func ConvertAddrsToValAddrs(addrs []sdk.AccAddress) []sdk.ValAddress {
	valAddrs := make([]sdk.ValAddress, len(addrs))

	for i, addr := range addrs {
		valAddrs[i] = sdk.ValAddress(addr)
	}

	return valAddrs
}

func TestAddr(addr string, bech string) (sdk.AccAddress, error) {
	res, err := sdk.AccAddressFromHex(addr)
	if err != nil {
		return nil, err
	}
	bechexpected := res.String()
	if bech != bechexpected {
		return nil, fmt.Errorf("bech encoding doesn't match reference")
	}

	bechres, err := sdk.AccAddressFromBech32(bech)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(bechres, res) {
		return nil, err
	}

	return res, nil
}

func GenTx(gen client.TxConfig, msgs []sdk.Msg, feeAmt sdk.Coins, gas uint64, chainID string, accNums, accSeqs []uint64, priv ...cryptotypes.PrivKey) (sdk.Tx, error) {
	sigs := make([]signing.SignatureV2, len(priv))

	// create a random length memo
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	memo := simulation.RandStringOfLength(r, simulation.RandIntBetween(r, 0, 100))

	signMode := gen.SignModeHandler().DefaultMode()

	// 1st round: set SignatureV2 with empty signatures, to set correct
	// signer infos.
	for i, p := range priv {
		sigs[i] = signing.SignatureV2{
			PubKey: p.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode: signMode,
			},
			Sequence: accSeqs[i],
		}
	}

	tx := gen.NewTxBuilder()
	err := tx.SetMsgs(msgs...)
	if err != nil {
		return nil, err
	}
	err = tx.SetSignatures(sigs...)
	if err != nil {
		return nil, err
	}
	tx.SetMemo(memo)
	tx.SetFeeAmount(feeAmt)
	tx.SetGasLimit(gas)

	// 2nd round: once all signer infos are set, every signer can sign.
	for i, p := range priv {
		signerData := authsign.SignerData{
			ChainID:       chainID,
			AccountNumber: accNums[i],
			Sequence:      accSeqs[i],
		}
		signBytes, err := gen.SignModeHandler().GetSignBytes(signMode, signerData, tx.GetTx())
		if err != nil {
			panic(err)
		}
		sig, err := p.Sign(signBytes)
		if err != nil {
			panic(err)
		}
		sigs[i].Data.(*signing.SingleSignatureData).Signature = sig
		err = tx.SetSignatures(sigs...)
		if err != nil {
			panic(err)
		}
	}

	return tx.GetTx(), nil
}

func SignCheckDeliver(
	t *testing.T, txCfg client.TxConfig, app *bam.BaseApp, header tmproto.Header, msgs []sdk.Msg,
	chainID string, accNums, accSeqs []uint64, expSimPass, expPass bool, priv ...cryptotypes.PrivKey,
) (sdk.GasInfo, *sdk.Result, error) {

	tx, err := GenTx(
		txCfg,
		msgs,
		sdk.Coins{sdk.NewInt64Coin(sdk.DefaultBondDenom, 0)},
		DefaultGenTxGas,
		chainID,
		accNums,
		accSeqs,
		priv...,
	)
	require.NoError(t, err)
	txBytes, err := txCfg.TxEncoder()(tx)
	require.Nil(t, err)

	// Must simulate now as CheckTx doesn't run Msgs anymore
	_, res, err := app.Simulate(txBytes)

	if expSimPass {
		require.NoError(t, err)
		require.NotNil(t, res)
	} else {
		require.Error(t, err)
		require.Nil(t, res)
	}

	// Simulate a sending a transaction and committing a block
	_, _ = app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: header.Height})
	gInfo, res, err := app.Deliver(txCfg.TxEncoder(), tx)

	if expPass {
		require.NoError(t, err)
		require.NotNil(t, res)
	} else {
		require.Error(t, err)
		require.Nil(t, res)
	}

	_, _ = app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: header.Height})
	_, _ = app.Commit(context.Background())

	return gInfo, res, err
}

func GenSequenceOfTxs(txGen client.TxConfig, msgs []sdk.Msg, accNums []uint64, initSeqNums []uint64, numToGenerate int, priv ...cryptotypes.PrivKey) ([]sdk.Tx, error) {
	txs := make([]sdk.Tx, numToGenerate)
	var err error
	for i := 0; i < numToGenerate; i++ {
		txs[i], err = GenTx(
			txGen,
			msgs,
			sdk.Coins{sdk.NewInt64Coin(sdk.DefaultBondDenom, 0)},
			DefaultGenTxGas,
			"",
			accNums,
			initSeqNums,
			priv...,
		)
		if err != nil {
			break
		}
		incrementAllSequenceNumbers(initSeqNums)
	}

	return txs, err
}

func incrementAllSequenceNumbers(initSeqNums []uint64) {
	for i := 0; i < len(initSeqNums); i++ {
		initSeqNums[i]++
	}
}

// CheckBalance checks the balance of an account.
func CheckBalance(t *testing.T, app *App, addr sdk.AccAddress, balances sdk.Coins) {
	ctxCheck := app.NewContext(true, tmproto.Header{})
	require.True(t, balances.IsEqual(app.BankKeeper.GetAllBalances(ctxCheck, addr)))
}

// CreateTestPubKeys returns a total of numPubKeys public keys in ascending order.
func CreateTestPubKeys(numPubKeys int) []cryptotypes.PubKey {
	var publicKeys []cryptotypes.PubKey
	var buffer bytes.Buffer

	// start at 10 to avoid changing 1 to 01, 2 to 02, etc
	for i := 100; i < (numPubKeys + 100); i++ {
		numString := strconv.Itoa(i)
		buffer.WriteString("0B485CFC0EECC619440448436F8FC9DF40566F2369E72400281454CB552AF") // base pubkey string
		buffer.WriteString(numString)                                                       // adding on final two digits to make pubkeys unique
		publicKeys = append(publicKeys, NewPubKeyFromHex(buffer.String()))
		buffer.Reset()
	}

	return publicKeys
}

// NewPubKeyFromHex returns a PubKey from a hex string.
func NewPubKeyFromHex(pk string) (res cryptotypes.PubKey) {
	pkBytes, err := hex.DecodeString(pk)
	if err != nil {
		panic(err)
	}
	if len(pkBytes) != ed25519.PubKeySize {
		panic(errors.Wrap(errors.ErrInvalidPubKey, "invalid pubkey size"))
	}
	return &ed25519.PubKey{Key: pkBytes}
}

const DefaultGenTxGas = 10000000
