package keeper

import (
	"encoding/json"
	"io/ioutil"
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	distributionkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type StakingInitMsg struct {
	Name      string         `json:"name"`
	Symbol    string         `json:"symbol"`
	Decimals  uint8          `json:"decimals"`
	Validator sdk.ValAddress `json:"validator"`
	ExitTax   sdk.Dec        `json:"exit_tax"`
	// MinWithdrawal is uint128 encoded as a string (use sdk.Int?)
	MinWithdrawl string `json:"min_withdrawal"`
}

// StakingHandleMsg is used to encode handle messages
type StakingHandleMsg struct {
	Transfer *transferPayload `json:"transfer,omitempty"`
	Bond     *struct{}        `json:"bond,omitempty"`
	Unbond   *unbondPayload   `json:"unbond,omitempty"`
	Claim    *struct{}        `json:"claim,omitempty"`
	Reinvest *struct{}        `json:"reinvest,omitempty"`
	Change   *ownerPayload    `json:"change_owner,omitempty"`
}

type transferPayload struct {
	Recipient sdk.Address `json:"recipient"`
	// uint128 encoded as string
	Amount string `json:"amount"`
}

type unbondPayload struct {
	// uint128 encoded as string
	Amount string `json:"amount"`
}

// StakingQueryMsg is used to encode query messages
type StakingQueryMsg struct {
	Balance    *addressQuery `json:"balance,omitempty"`
	Claims     *addressQuery `json:"claims,omitempty"`
	TokenInfo  *struct{}     `json:"token_info,omitempty"`
	Investment *struct{}     `json:"investment,omitempty"`
}

type addressQuery struct {
	Address sdk.AccAddress `json:"address"`
}

type BalanceResponse struct {
	Balance string `json:"balance,omitempty"`
}

type ClaimsResponse struct {
	Claims string `json:"claims,omitempty"`
}

type TokenInfoResponse struct {
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals uint8  `json:"decimals"`
}

type InvestmentResponse struct {
	TokenSupply  string         `json:"token_supply"`
	StakedTokens sdk.Coin       `json:"staked_tokens"`
	NominalValue sdk.Dec        `json:"nominal_value"`
	Owner        sdk.AccAddress `json:"owner"`
	Validator    sdk.ValAddress `json:"validator"`
	ExitTax      sdk.Dec        `json:"exit_tax"`
	// MinWithdrawl is uint128 encoded as a string (use sdk.Int?)
	MinWithdrawl string `json:"min_withdrawal"`
}

func TestInitializeStaking(t *testing.T) {
	ctx, k := CreateTestInput(t, false, SupportedFeatures)
	accKeeper, stakingKeeper, keeper, bankKeeper := k.AccountKeeper, k.StakingKeeper, k.ContractKeeper, k.BankKeeper

	valAddr := addValidator(t, ctx, stakingKeeper, k.Faucet, sdk.NewInt64Coin("stake", 1234567))
	ctx = nextBlock(ctx, stakingKeeper)
	v, found := stakingKeeper.GetValidator(ctx, valAddr)
	assert.True(t, found)
	assert.Equal(t, v.GetDelegatorShares(), sdk.NewDec(1234567))

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000), sdk.NewInt64Coin("stake", 500000))
	creator := k.Faucet.NewFundedAccount(ctx, deposit...)

	// upload staking derivates code
	stakingCode, err := ioutil.ReadFile("./testdata/staking.wasm")
	require.NoError(t, err)
	stakingID, err := keeper.Create(ctx, creator, stakingCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), stakingID)

	// register to a valid address
	initMsg := StakingInitMsg{
		Name:         "Staking Derivatives",
		Symbol:       "DRV",
		Decimals:     0,
		Validator:    valAddr,
		ExitTax:      sdk.MustNewDecFromStr("0.10"),
		MinWithdrawl: "100",
	}
	initBz, err := json.Marshal(&initMsg)
	require.NoError(t, err)

	stakingAddr, _, err := k.ContractKeeper.Instantiate(ctx, stakingID, creator, nil, initBz, "staking derivates - DRV", nil)
	require.NoError(t, err)
	require.NotEmpty(t, stakingAddr)

	// nothing spent here
	checkAccount(t, ctx, accKeeper, bankKeeper, creator, deposit)

	// try to register with a validator not on the list and it fails
	_, _, bob := keyPubAddr()
	badInitMsg := StakingInitMsg{
		Name:         "Missing Validator",
		Symbol:       "MISS",
		Decimals:     0,
		Validator:    sdk.ValAddress(bob),
		ExitTax:      sdk.MustNewDecFromStr("0.10"),
		MinWithdrawl: "100",
	}
	badBz, err := json.Marshal(&badInitMsg)
	require.NoError(t, err)

	_, _, err = k.ContractKeeper.Instantiate(ctx, stakingID, creator, nil, badBz, "missing validator", nil)
	require.Error(t, err)

	// no changes to bonding shares
	val, _ := stakingKeeper.GetValidator(ctx, valAddr)
	assert.Equal(t, val.GetDelegatorShares(), sdk.NewDec(1234567))
}

type initInfo struct {
	valAddr      sdk.ValAddress
	creator      sdk.AccAddress
	contractAddr sdk.AccAddress

	ctx            sdk.Context
	accKeeper      authkeeper.AccountKeeper
	stakingKeeper  stakingkeeper.Keeper
	distKeeper     distributionkeeper.Keeper
	wasmKeeper     Keeper
	contractKeeper wasmtypes.ContractOpsKeeper
	bankKeeper     bankkeeper.Keeper
	faucet         *TestFaucet
}

func initializeStaking(t *testing.T) initInfo {
	ctx, k := CreateTestInput(t, false, SupportedFeatures)
	accKeeper, stakingKeeper, keeper, bankKeeper := k.AccountKeeper, k.StakingKeeper, k.WasmKeeper, k.BankKeeper

	valAddr := addValidator(t, ctx, stakingKeeper, k.Faucet, sdk.NewInt64Coin("stake", 1000000))
	ctx = nextBlock(ctx, stakingKeeper)

	// set some baseline - this seems to be needed
	k.DistKeeper.SetValidatorHistoricalRewards(ctx, valAddr, 0, distributiontypes.ValidatorHistoricalRewards{
		CumulativeRewardRatio: sdk.DecCoins{},
		ReferenceCount:        1,
	})

	v, found := stakingKeeper.GetValidator(ctx, valAddr)
	assert.True(t, found)
	assert.Equal(t, v.GetDelegatorShares(), sdk.NewDec(1000000))
	assert.Equal(t, v.Status, stakingtypes.Bonded)

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000), sdk.NewInt64Coin("stake", 500000))
	creator := k.Faucet.NewFundedAccount(ctx, deposit...)

	// upload staking derivates code
	stakingCode, err := ioutil.ReadFile("./testdata/staking.wasm")
	require.NoError(t, err)
	stakingID, err := k.ContractKeeper.Create(ctx, creator, stakingCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), stakingID)

	// register to a valid address
	initMsg := StakingInitMsg{
		Name:         "Staking Derivatives",
		Symbol:       "DRV",
		Decimals:     0,
		Validator:    valAddr,
		ExitTax:      sdk.MustNewDecFromStr("0.10"),
		MinWithdrawl: "100",
	}
	initBz, err := json.Marshal(&initMsg)
	require.NoError(t, err)

	stakingAddr, _, err := k.ContractKeeper.Instantiate(ctx, stakingID, creator, nil, initBz, "staking derivates - DRV", nil)
	require.NoError(t, err)
	require.NotEmpty(t, stakingAddr)

	return initInfo{
		valAddr:        valAddr,
		creator:        creator,
		contractAddr:   stakingAddr,
		ctx:            ctx,
		accKeeper:      accKeeper,
		stakingKeeper:  stakingKeeper,
		wasmKeeper:     *keeper,
		distKeeper:     k.DistKeeper,
		bankKeeper:     bankKeeper,
		contractKeeper: k.ContractKeeper,
		faucet:         k.Faucet,
	}
}

func TestBonding(t *testing.T) {
	initInfo := initializeStaking(t)
	ctx, valAddr, contractAddr := initInfo.ctx, initInfo.valAddr, initInfo.contractAddr
	keeper, stakingKeeper, accKeeper, bankKeeper := initInfo.wasmKeeper, initInfo.stakingKeeper, initInfo.accKeeper, initInfo.bankKeeper

	// initial checks of bonding state
	val, found := stakingKeeper.GetValidator(ctx, valAddr)
	require.True(t, found)
	initPower := val.GetDelegatorShares()

	// bob has 160k, putting 80k into the contract
	full := sdk.NewCoins(sdk.NewInt64Coin("stake", 160000))
	funds := sdk.NewCoins(sdk.NewInt64Coin("stake", 80000))
	bob := initInfo.faucet.NewFundedAccount(ctx, full...)

	// check contract state before
	assertBalance(t, ctx, keeper, contractAddr, bob, "0")
	assertClaims(t, ctx, keeper, contractAddr, bob, "0")
	assertSupply(t, ctx, keeper, contractAddr, "0", sdk.NewInt64Coin("stake", 0))

	bond := StakingHandleMsg{
		Bond: &struct{}{},
	}
	bondBz, err := json.Marshal(bond)
	require.NoError(t, err)
	_, err = initInfo.contractKeeper.Execute(ctx, contractAddr, bob, bondBz, funds)
	require.NoError(t, err)

	// check some account values - the money is on neither account (cuz it is bonded)
	checkAccount(t, ctx, accKeeper, bankKeeper, contractAddr, sdk.Coins{})
	checkAccount(t, ctx, accKeeper, bankKeeper, bob, funds)

	// make sure the proper number of tokens have been bonded
	val, _ = stakingKeeper.GetValidator(ctx, valAddr)
	finalPower := val.GetDelegatorShares()
	assert.Equal(t, sdk.NewInt(80000), finalPower.Sub(initPower).TruncateInt())

	// check the delegation itself
	d, found := stakingKeeper.GetDelegation(ctx, contractAddr, valAddr)
	require.True(t, found)
	assert.Equal(t, d.Shares, sdk.MustNewDecFromStr("80000"))

	// check we have the desired balance
	assertBalance(t, ctx, keeper, contractAddr, bob, "80000")
	assertClaims(t, ctx, keeper, contractAddr, bob, "0")
	assertSupply(t, ctx, keeper, contractAddr, "80000", sdk.NewInt64Coin("stake", 80000))
}

func TestUnbonding(t *testing.T) {
	initInfo := initializeStaking(t)
	ctx, valAddr, contractAddr := initInfo.ctx, initInfo.valAddr, initInfo.contractAddr
	keeper, stakingKeeper, accKeeper, bankKeeper := initInfo.wasmKeeper, initInfo.stakingKeeper, initInfo.accKeeper, initInfo.bankKeeper

	// initial checks of bonding state
	val, found := stakingKeeper.GetValidator(ctx, valAddr)
	require.True(t, found)
	initPower := val.GetDelegatorShares()

	// bob has 160k, putting 80k into the contract
	full := sdk.NewCoins(sdk.NewInt64Coin("stake", 160000))
	funds := sdk.NewCoins(sdk.NewInt64Coin("stake", 80000))
	bob := initInfo.faucet.NewFundedAccount(ctx, full...)

	bond := StakingHandleMsg{
		Bond: &struct{}{},
	}
	bondBz, err := json.Marshal(bond)
	require.NoError(t, err)
	_, err = initInfo.contractKeeper.Execute(ctx, contractAddr, bob, bondBz, funds)
	require.NoError(t, err)

	// update height a bit
	ctx = nextBlock(ctx, stakingKeeper)

	// now unbond 30k - note that 3k (10%) goes to the owner as a tax, 27k unbonded and available as claims
	unbond := StakingHandleMsg{
		Unbond: &unbondPayload{
			Amount: "30000",
		},
	}
	unbondBz, err := json.Marshal(unbond)
	require.NoError(t, err)
	_, err = initInfo.contractKeeper.Execute(ctx, contractAddr, bob, unbondBz, nil)
	require.NoError(t, err)

	// check some account values - the money is on neither account (cuz it is bonded)
	// Note: why is this immediate? just test setup?
	checkAccount(t, ctx, accKeeper, bankKeeper, contractAddr, sdk.Coins{})
	checkAccount(t, ctx, accKeeper, bankKeeper, bob, funds)

	// make sure the proper number of tokens have been bonded (80k - 27k = 53k)
	val, _ = stakingKeeper.GetValidator(ctx, valAddr)
	finalPower := val.GetDelegatorShares()
	assert.Equal(t, sdk.NewInt(53000), finalPower.Sub(initPower).TruncateInt(), finalPower.String())

	// check the delegation itself
	d, found := stakingKeeper.GetDelegation(ctx, contractAddr, valAddr)
	require.True(t, found)
	assert.Equal(t, d.Shares, sdk.MustNewDecFromStr("53000"))

	// check there is unbonding in progress
	un, found := stakingKeeper.GetUnbondingDelegation(ctx, contractAddr, valAddr)
	require.True(t, found)
	require.Equal(t, 1, len(un.Entries))
	assert.Equal(t, "27000", un.Entries[0].Balance.String())

	// check we have the desired balance
	assertBalance(t, ctx, keeper, contractAddr, bob, "50000")
	assertBalance(t, ctx, keeper, contractAddr, initInfo.creator, "3000")
	assertClaims(t, ctx, keeper, contractAddr, bob, "27000")
	assertSupply(t, ctx, keeper, contractAddr, "53000", sdk.NewInt64Coin("stake", 53000))
}

func TestReinvest(t *testing.T) {
	initInfo := initializeStaking(t)
	ctx, valAddr, contractAddr := initInfo.ctx, initInfo.valAddr, initInfo.contractAddr
	keeper, stakingKeeper, accKeeper, bankKeeper := initInfo.wasmKeeper, initInfo.stakingKeeper, initInfo.accKeeper, initInfo.bankKeeper
	distKeeper := initInfo.distKeeper

	// initial checks of bonding state
	val, found := stakingKeeper.GetValidator(ctx, valAddr)
	require.True(t, found)
	initPower := val.GetDelegatorShares()
	assert.Equal(t, val.Tokens, sdk.NewInt(1000000), "%s", val.Tokens)

	// full is 2x funds, 1x goes to the contract, other stays on his wallet
	full := sdk.NewCoins(sdk.NewInt64Coin("stake", 400000))
	funds := sdk.NewCoins(sdk.NewInt64Coin("stake", 200000))
	bob := initInfo.faucet.NewFundedAccount(ctx, full...)

	// we will stake 200k to a validator with 1M self-bond
	// this means we should get 1/6 of the rewards
	bond := StakingHandleMsg{
		Bond: &struct{}{},
	}
	bondBz, err := json.Marshal(bond)
	require.NoError(t, err)
	_, err = initInfo.contractKeeper.Execute(ctx, contractAddr, bob, bondBz, funds)
	require.NoError(t, err)

	// update height a bit to solidify the delegation
	ctx = nextBlock(ctx, stakingKeeper)
	// we get 1/6, our share should be 40k minus 10% commission = 36k
	setValidatorRewards(ctx, stakingKeeper, distKeeper, valAddr, "240000")

	// this should withdraw our outstanding 36k of rewards and reinvest them in the same delegation
	reinvest := StakingHandleMsg{
		Reinvest: &struct{}{},
	}
	reinvestBz, err := json.Marshal(reinvest)
	require.NoError(t, err)
	_, err = initInfo.contractKeeper.Execute(ctx, contractAddr, bob, reinvestBz, nil)
	require.NoError(t, err)

	// check some account values - the money is on neither account (cuz it is bonded)
	// Note: why is this immediate? just test setup?
	checkAccount(t, ctx, accKeeper, bankKeeper, contractAddr, sdk.Coins{})
	checkAccount(t, ctx, accKeeper, bankKeeper, bob, funds)

	// check the delegation itself
	d, found := stakingKeeper.GetDelegation(ctx, contractAddr, valAddr)
	require.True(t, found)
	// we started with 200k and added 36k
	assert.Equal(t, d.Shares, sdk.MustNewDecFromStr("236000"))

	// make sure the proper number of tokens have been bonded (80k + 40k = 120k)
	val, _ = stakingKeeper.GetValidator(ctx, valAddr)
	finalPower := val.GetDelegatorShares()
	assert.Equal(t, sdk.NewInt(236000), finalPower.Sub(initPower).TruncateInt(), finalPower.String())

	// check there is no unbonding in progress
	un, found := stakingKeeper.GetUnbondingDelegation(ctx, contractAddr, valAddr)
	assert.False(t, found, "%#v", un)

	// check we have the desired balance
	assertBalance(t, ctx, keeper, contractAddr, bob, "200000")
	assertBalance(t, ctx, keeper, contractAddr, initInfo.creator, "0")
	assertClaims(t, ctx, keeper, contractAddr, bob, "0")
	assertSupply(t, ctx, keeper, contractAddr, "200000", sdk.NewInt64Coin("stake", 236000))
}

func TestQueryStakingInfo(t *testing.T) {
	// STEP 1: take a lot of setup from TestReinvest so we have non-zero info
	initInfo := initializeStaking(t)
	ctx, valAddr, contractAddr := initInfo.ctx, initInfo.valAddr, initInfo.contractAddr
	keeper, stakingKeeper := initInfo.wasmKeeper, initInfo.stakingKeeper
	distKeeper := initInfo.distKeeper

	// initial checks of bonding state
	val, found := stakingKeeper.GetValidator(ctx, valAddr)
	require.True(t, found)
	assert.Equal(t, sdk.NewInt(1000000), val.Tokens)

	// full is 2x funds, 1x goes to the contract, other stays on his wallet
	full := sdk.NewCoins(sdk.NewInt64Coin("stake", 400000))
	funds := sdk.NewCoins(sdk.NewInt64Coin("stake", 200000))
	bob := initInfo.faucet.NewFundedAccount(ctx, full...)

	// we will stake 200k to a validator with 1M self-bond
	// this means we should get 1/6 of the rewards
	bond := StakingHandleMsg{
		Bond: &struct{}{},
	}
	bondBz, err := json.Marshal(bond)
	require.NoError(t, err)
	_, err = initInfo.contractKeeper.Execute(ctx, contractAddr, bob, bondBz, funds)
	require.NoError(t, err)

	// update height a bit to solidify the delegation
	ctx = nextBlock(ctx, stakingKeeper)
	// we get 1/6, our share should be 40k minus 10% commission = 36k
	setValidatorRewards(ctx, stakingKeeper, distKeeper, valAddr, "240000")

	// see what the current rewards are
	origReward := distKeeper.GetValidatorCurrentRewards(ctx, valAddr)

	// STEP 2: Prepare the mask contract
	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := initInfo.faucet.NewFundedAccount(ctx, deposit...)

	// upload mask code
	maskCode, err := ioutil.ReadFile("./testdata/reflect.wasm")
	require.NoError(t, err)
	maskID, err := initInfo.contractKeeper.Create(ctx, creator, maskCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(2), maskID)

	// creator instantiates a contract and gives it tokens
	maskAddr, _, err := initInfo.contractKeeper.Instantiate(ctx, maskID, creator, nil, []byte("{}"), "mask contract 2", nil)
	require.NoError(t, err)
	require.NotEmpty(t, maskAddr)

	// STEP 3: now, let's reflect some queries.
	// let's get the bonded denom
	reflectBondedQuery := ReflectQueryMsg{Chain: &ChainQuery{Request: &wasmvmtypes.QueryRequest{Staking: &wasmvmtypes.StakingQuery{
		BondedDenom: &struct{}{},
	}}}}
	reflectBondedBin := buildReflectQuery(t, &reflectBondedQuery)
	res, err := keeper.QuerySmart(ctx, maskAddr, reflectBondedBin)
	require.NoError(t, err)
	// first we pull out the data from chain response, before parsing the original response
	var reflectRes ChainResponse
	mustParse(t, res, &reflectRes)
	var bondedRes wasmvmtypes.BondedDenomResponse
	mustParse(t, reflectRes.Data, &bondedRes)
	assert.Equal(t, "stake", bondedRes.Denom)

	// now, let's reflect a smart query into the x/wasm handlers and see if we get the same result
	reflectAllValidatorsQuery := ReflectQueryMsg{Chain: &ChainQuery{Request: &wasmvmtypes.QueryRequest{Staking: &wasmvmtypes.StakingQuery{
		AllValidators: &wasmvmtypes.AllValidatorsQuery{},
	}}}}
	reflectAllValidatorsBin := buildReflectQuery(t, &reflectAllValidatorsQuery)
	res, err = keeper.QuerySmart(ctx, maskAddr, reflectAllValidatorsBin)
	require.NoError(t, err)
	// first we pull out the data from chain response, before parsing the original response
	mustParse(t, res, &reflectRes)
	var allValidatorsRes wasmvmtypes.AllValidatorsResponse
	mustParse(t, reflectRes.Data, &allValidatorsRes)
	require.Len(t, allValidatorsRes.Validators, 1)
	valInfo := allValidatorsRes.Validators[0]
	// Note: this ValAddress not AccAddress, may change with #264
	require.Equal(t, valAddr.String(), valInfo.Address)
	require.Contains(t, valInfo.Commission, "0.100")
	require.Contains(t, valInfo.MaxCommission, "0.200")
	require.Contains(t, valInfo.MaxChangeRate, "0.010")

	// find a validator
	reflectValidatorQuery := ReflectQueryMsg{Chain: &ChainQuery{Request: &wasmvmtypes.QueryRequest{Staking: &wasmvmtypes.StakingQuery{
		Validator: &wasmvmtypes.ValidatorQuery{
			Address: valAddr.String(),
		},
	}}}}
	reflectValidatorBin := buildReflectQuery(t, &reflectValidatorQuery)
	res, err = keeper.QuerySmart(ctx, maskAddr, reflectValidatorBin)
	require.NoError(t, err)
	// first we pull out the data from chain response, before parsing the original response
	mustParse(t, res, &reflectRes)
	var validatorRes wasmvmtypes.ValidatorResponse
	mustParse(t, reflectRes.Data, &validatorRes)
	require.NotNil(t, validatorRes.Validator)
	valInfo = *validatorRes.Validator
	// Note: this ValAddress not AccAddress, may change with #264
	require.Equal(t, valAddr.String(), valInfo.Address)
	require.Contains(t, valInfo.Commission, "0.100")
	require.Contains(t, valInfo.MaxCommission, "0.200")
	require.Contains(t, valInfo.MaxChangeRate, "0.010")

	// missing validator
	noVal := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address())
	reflectNoValidatorQuery := ReflectQueryMsg{Chain: &ChainQuery{Request: &wasmvmtypes.QueryRequest{Staking: &wasmvmtypes.StakingQuery{
		Validator: &wasmvmtypes.ValidatorQuery{
			Address: noVal.String(),
		},
	}}}}
	reflectNoValidatorBin := buildReflectQuery(t, &reflectNoValidatorQuery)
	res, err = keeper.QuerySmart(ctx, maskAddr, reflectNoValidatorBin)
	require.NoError(t, err)
	// first we pull out the data from chain response, before parsing the original response
	mustParse(t, res, &reflectRes)
	var noValidatorRes wasmvmtypes.ValidatorResponse
	mustParse(t, reflectRes.Data, &noValidatorRes)
	require.Nil(t, noValidatorRes.Validator)

	// test to get all my delegations
	reflectAllDelegationsQuery := ReflectQueryMsg{Chain: &ChainQuery{Request: &wasmvmtypes.QueryRequest{Staking: &wasmvmtypes.StakingQuery{
		AllDelegations: &wasmvmtypes.AllDelegationsQuery{
			Delegator: contractAddr.String(),
		},
	}}}}
	reflectAllDelegationsBin := buildReflectQuery(t, &reflectAllDelegationsQuery)
	res, err = keeper.QuerySmart(ctx, maskAddr, reflectAllDelegationsBin)
	require.NoError(t, err)
	// first we pull out the data from chain response, before parsing the original response
	mustParse(t, res, &reflectRes)
	var allDelegationsRes wasmvmtypes.AllDelegationsResponse
	mustParse(t, reflectRes.Data, &allDelegationsRes)
	require.Len(t, allDelegationsRes.Delegations, 1)
	delInfo := allDelegationsRes.Delegations[0]
	// Note: this ValAddress not AccAddress, may change with #264
	require.Equal(t, valAddr.String(), delInfo.Validator)
	// note this is not bob (who staked to the contract), but the contract itself
	require.Equal(t, contractAddr.String(), delInfo.Delegator)
	// this is a different Coin type, with String not BigInt, compare field by field
	require.Equal(t, funds[0].Denom, delInfo.Amount.Denom)
	require.Equal(t, funds[0].Amount.String(), delInfo.Amount.Amount)

	// test to get one delegations
	reflectDelegationQuery := ReflectQueryMsg{Chain: &ChainQuery{Request: &wasmvmtypes.QueryRequest{Staking: &wasmvmtypes.StakingQuery{
		Delegation: &wasmvmtypes.DelegationQuery{
			Validator: valAddr.String(),
			Delegator: contractAddr.String(),
		},
	}}}}
	reflectDelegationBin := buildReflectQuery(t, &reflectDelegationQuery)
	res, err = keeper.QuerySmart(ctx, maskAddr, reflectDelegationBin)
	require.NoError(t, err)
	// first we pull out the data from chain response, before parsing the original response
	mustParse(t, res, &reflectRes)
	var delegationRes wasmvmtypes.DelegationResponse
	mustParse(t, reflectRes.Data, &delegationRes)
	assert.NotEmpty(t, delegationRes.Delegation)
	delInfo2 := delegationRes.Delegation
	// Note: this ValAddress not AccAddress, may change with #264
	require.Equal(t, valAddr.String(), delInfo2.Validator)
	// note this is not bob (who staked to the contract), but the contract itself
	require.Equal(t, contractAddr.String(), delInfo2.Delegator)
	// this is a different Coin type, with String not BigInt, compare field by field
	require.Equal(t, funds[0].Denom, delInfo2.Amount.Denom)
	require.Equal(t, funds[0].Amount.String(), delInfo2.Amount.Amount)

	require.Equal(t, wasmvmtypes.NewCoin(200000, "stake"), delInfo2.CanRedelegate)
	require.Len(t, delInfo2.AccumulatedRewards, 1)
	// see bonding above to see how we calculate 36000 (240000 / 6 - 10% commission)
	require.Equal(t, wasmvmtypes.NewCoin(36000, "stake"), delInfo2.AccumulatedRewards[0])

	// ensure rewards did not change when querying (neither amount nor period)
	finalReward := distKeeper.GetValidatorCurrentRewards(ctx, valAddr)
	require.Equal(t, origReward, finalReward)
}

func TestQueryStakingPlugin(t *testing.T) {
	// STEP 1: take a lot of setup from TestReinvest so we have non-zero info
	initInfo := initializeStaking(t)
	ctx, valAddr, contractAddr := initInfo.ctx, initInfo.valAddr, initInfo.contractAddr
	stakingKeeper := initInfo.stakingKeeper
	distKeeper := initInfo.distKeeper

	// initial checks of bonding state
	val, found := stakingKeeper.GetValidator(ctx, valAddr)
	require.True(t, found)
	assert.Equal(t, sdk.NewInt(1000000), val.Tokens)

	// full is 2x funds, 1x goes to the contract, other stays on his wallet
	full := sdk.NewCoins(sdk.NewInt64Coin("stake", 400000))
	funds := sdk.NewCoins(sdk.NewInt64Coin("stake", 200000))
	bob := initInfo.faucet.NewFundedAccount(ctx, full...)

	// we will stake 200k to a validator with 1M self-bond
	// this means we should get 1/6 of the rewards
	bond := StakingHandleMsg{
		Bond: &struct{}{},
	}
	bondBz, err := json.Marshal(bond)
	require.NoError(t, err)
	_, err = initInfo.contractKeeper.Execute(ctx, contractAddr, bob, bondBz, funds)
	require.NoError(t, err)

	// update height a bit to solidify the delegation
	ctx = nextBlock(ctx, stakingKeeper)
	// we get 1/6, our share should be 40k minus 10% commission = 36k
	setValidatorRewards(ctx, stakingKeeper, distKeeper, valAddr, "240000")

	// see what the current rewards are
	origReward := distKeeper.GetValidatorCurrentRewards(ctx, valAddr)

	// Step 2: Try out the query plugins
	query := wasmvmtypes.StakingQuery{
		Delegation: &wasmvmtypes.DelegationQuery{
			Delegator: contractAddr.String(),
			Validator: valAddr.String(),
		},
	}
	raw, err := StakingQuerier(stakingKeeper, distKeeper)(ctx, &query)
	require.NoError(t, err)
	var res wasmvmtypes.DelegationResponse
	mustParse(t, raw, &res)
	assert.NotEmpty(t, res.Delegation)
	delInfo := res.Delegation
	// Note: this ValAddress not AccAddress, may change with #264
	require.Equal(t, valAddr.String(), delInfo.Validator)
	// note this is not bob (who staked to the contract), but the contract itself
	require.Equal(t, contractAddr.String(), delInfo.Delegator)
	// this is a different Coin type, with String not BigInt, compare field by field
	require.Equal(t, funds[0].Denom, delInfo.Amount.Denom)
	require.Equal(t, funds[0].Amount.String(), delInfo.Amount.Amount)

	require.Equal(t, wasmvmtypes.NewCoin(200000, "stake"), delInfo.CanRedelegate)
	require.Len(t, delInfo.AccumulatedRewards, 1)
	// see bonding above to see how we calculate 36000 (240000 / 6 - 10% commission)
	require.Equal(t, wasmvmtypes.NewCoin(36000, "stake"), delInfo.AccumulatedRewards[0])

	// ensure rewards did not change when querying (neither amount nor period)
	finalReward := distKeeper.GetValidatorCurrentRewards(ctx, valAddr)
	require.Equal(t, origReward, finalReward)
}

// adds a few validators and returns a list of validators that are registered
func addValidator(t *testing.T, ctx sdk.Context, stakingKeeper stakingkeeper.Keeper, faucet *TestFaucet, value sdk.Coin) sdk.ValAddress {
	owner := faucet.NewFundedAccount(ctx, value)

	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	addr := sdk.ValAddress(pubKey.Address())

	pkAny, err := codectypes.NewAnyWithValue(pubKey)
	require.NoError(t, err)
	msg := stakingtypes.MsgCreateValidator{
		Description: types.Description{
			Moniker: "Validator power",
		},
		Commission: types.CommissionRates{
			Rate:          sdk.MustNewDecFromStr("0.1"),
			MaxRate:       sdk.MustNewDecFromStr("0.2"),
			MaxChangeRate: sdk.MustNewDecFromStr("0.01"),
		},
		MinSelfDelegation: sdk.OneInt(),
		DelegatorAddress:  owner.String(),
		ValidatorAddress:  addr.String(),
		Pubkey:            pkAny,
		Value:             value,
	}

	h := staking.NewHandler(stakingKeeper)
	_, err = h(ctx, &msg)
	require.NoError(t, err)
	return addr
}

// this will commit the current set, update the block height and set historic info
// basically, letting two blocks pass
func nextBlock(ctx sdk.Context, stakingKeeper stakingkeeper.Keeper) sdk.Context {
	staking.EndBlocker(ctx, stakingKeeper)
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	staking.BeginBlocker(ctx, stakingKeeper)
	return ctx
}

func setValidatorRewards(ctx sdk.Context, stakingKeeper stakingkeeper.Keeper, distKeeper distributionkeeper.Keeper, valAddr sdk.ValAddress, reward string) {
	// allocate some rewards
	vali := stakingKeeper.Validator(ctx, valAddr)
	amount, err := sdk.NewDecFromStr(reward)
	if err != nil {
		panic(err)
	}
	payout := sdk.DecCoins{{Denom: "stake", Amount: amount}}
	distKeeper.AllocateTokensToValidator(ctx, vali, payout)
}

func assertBalance(t *testing.T, ctx sdk.Context, keeper Keeper, contract sdk.AccAddress, addr sdk.AccAddress, expected string) {
	query := StakingQueryMsg{
		Balance: &addressQuery{
			Address: addr,
		},
	}
	queryBz, err := json.Marshal(query)
	require.NoError(t, err)
	res, err := keeper.QuerySmart(ctx, contract, queryBz)
	require.NoError(t, err)
	var balance BalanceResponse
	err = json.Unmarshal(res, &balance)
	require.NoError(t, err)
	assert.Equal(t, expected, balance.Balance)
}

func assertClaims(t *testing.T, ctx sdk.Context, keeper Keeper, contract sdk.AccAddress, addr sdk.AccAddress, expected string) {
	query := StakingQueryMsg{
		Claims: &addressQuery{
			Address: addr,
		},
	}
	queryBz, err := json.Marshal(query)
	require.NoError(t, err)
	res, err := keeper.QuerySmart(ctx, contract, queryBz)
	require.NoError(t, err)
	var claims ClaimsResponse
	err = json.Unmarshal(res, &claims)
	require.NoError(t, err)
	assert.Equal(t, expected, claims.Claims)
}

func assertSupply(t *testing.T, ctx sdk.Context, keeper Keeper, contract sdk.AccAddress, expectedIssued string, expectedBonded sdk.Coin) {
	query := StakingQueryMsg{Investment: &struct{}{}}
	queryBz, err := json.Marshal(query)
	require.NoError(t, err)
	res, err := keeper.QuerySmart(ctx, contract, queryBz)
	require.NoError(t, err)
	var invest InvestmentResponse
	err = json.Unmarshal(res, &invest)
	require.NoError(t, err)
	assert.Equal(t, expectedIssued, invest.TokenSupply)
	assert.Equal(t, expectedBonded, invest.StakedTokens)
}
