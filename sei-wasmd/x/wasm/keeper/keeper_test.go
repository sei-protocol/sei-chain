package keeper

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"math"
	"testing"
	"time"

	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	wasmvm "github.com/CosmWasm/wasmvm"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/CosmWasm/wasmd/x/wasm/keeper/wasmtesting"
	"github.com/CosmWasm/wasmd/x/wasm/types"
)

// When migrated to go 1.16, embed package should be used instead.
func init() {
	b, err := ioutil.ReadFile("./testdata/hackatom.wasm")
	if err != nil {
		panic(err)
	}
	hackatomWasm = b
}

var hackatomWasm []byte

const SupportedFeatures = "iterator,staking,stargate"

func TestNewKeeper(t *testing.T) {
	_, keepers := CreateTestInput(t, false, SupportedFeatures)
	require.NotNil(t, keepers.ContractKeeper)
}

func TestCreateSuccess(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	em := sdk.NewEventManager()
	contractID, err := keeper.Create(ctx.WithEventManager(em), creator, hackatomWasm, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), contractID)
	// and verify content
	storedCode, err := keepers.WasmKeeper.GetByteCode(ctx, contractID)
	require.NoError(t, err)
	require.Equal(t, hackatomWasm, storedCode)
	// and events emitted
	exp := sdk.Events{sdk.NewEvent("store_code", sdk.NewAttribute("code_id", "1"))}
	assert.Equal(t, exp, em.Events())
}

func TestCreateNilCreatorAddress(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)

	_, err := keepers.ContractKeeper.Create(ctx, nil, hackatomWasm, nil)
	require.Error(t, err, "nil creator is not allowed")
}

func TestCreateNilWasmCode(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	_, err := keepers.ContractKeeper.Create(ctx, creator, nil, nil)
	require.Error(t, err, "nil WASM code is not allowed")
}

func TestCreateInvalidWasmCode(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	_, err := keepers.ContractKeeper.Create(ctx, creator, []byte("potatoes"), nil)
	require.Error(t, err, "potatoes are not valid WASM code")
}

func TestCreateStoresInstantiatePermission(t *testing.T) {
	var (
		deposit                = sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
		myAddr  sdk.AccAddress = bytes.Repeat([]byte{1}, types.SDKAddrLen)
	)

	specs := map[string]struct {
		srcPermission types.AccessType
		expInstConf   types.AccessConfig
	}{
		"default": {
			srcPermission: types.DefaultParams().InstantiateDefaultPermission,
			expInstConf:   types.AllowEverybody,
		},
		"everybody": {
			srcPermission: types.AccessTypeEverybody,
			expInstConf:   types.AllowEverybody,
		},
		"nobody": {
			srcPermission: types.AccessTypeNobody,
			expInstConf:   types.AllowNobody,
		},
		"onlyAddress with matching address": {
			srcPermission: types.AccessTypeOnlyAddress,
			expInstConf:   types.AccessConfig{Permission: types.AccessTypeOnlyAddress, Address: myAddr.String()},
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
			accKeeper, keeper, bankKeeper := keepers.AccountKeeper, keepers.ContractKeeper, keepers.BankKeeper
			keepers.WasmKeeper.SetParams(ctx, types.Params{
				CodeUploadAccess:             types.AllowEverybody,
				InstantiateDefaultPermission: spec.srcPermission,
			})
			fundAccounts(t, ctx, accKeeper, bankKeeper, myAddr, deposit)

			codeID, err := keeper.Create(ctx, myAddr, hackatomWasm, nil)
			require.NoError(t, err)

			codeInfo := keepers.WasmKeeper.GetCodeInfo(ctx, codeID)
			require.NotNil(t, codeInfo)
			assert.True(t, spec.expInstConf.Equals(codeInfo.InstantiateConfig), "got %#v", codeInfo.InstantiateConfig)
		})
	}
}

func TestCreateWithParamPermissions(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)
	otherAddr := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	specs := map[string]struct {
		srcPermission types.AccessConfig
		expError      *sdkerrors.Error
	}{
		"default": {
			srcPermission: types.DefaultUploadAccess,
		},
		"everybody": {
			srcPermission: types.AllowEverybody,
		},
		"nobody": {
			srcPermission: types.AllowNobody,
			expError:      sdkerrors.ErrUnauthorized,
		},
		"onlyAddress with matching address": {
			srcPermission: types.AccessTypeOnlyAddress.With(creator),
		},
		"onlyAddress with non matching address": {
			srcPermission: types.AccessTypeOnlyAddress.With(otherAddr),
			expError:      sdkerrors.ErrUnauthorized,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			params := types.DefaultParams()
			params.CodeUploadAccess = spec.srcPermission
			keepers.WasmKeeper.SetParams(ctx, params)
			_, err := keeper.Create(ctx, creator, hackatomWasm, nil)
			require.True(t, spec.expError.Is(err), err)
			if spec.expError != nil {
				return
			}
		})
	}
}

// ensure that the user cannot set the code instantiate permission to something more permissive
// than the default
func TestEnforceValidPermissionsOnCreate(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.WasmKeeper
	contractKeeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)
	other := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	onlyCreator := types.AccessTypeOnlyAddress.With(creator)
	onlyOther := types.AccessTypeOnlyAddress.With(other)

	specs := map[string]struct {
		defaultPermssion    types.AccessType
		requestedPermission *types.AccessConfig
		// grantedPermission is set iff no error
		grantedPermission types.AccessConfig
		// expError is nil iff the request is allowed
		expError *sdkerrors.Error
	}{
		"override everybody": {
			defaultPermssion:    types.AccessTypeEverybody,
			requestedPermission: &onlyCreator,
			grantedPermission:   onlyCreator,
		},
		"default to everybody": {
			defaultPermssion:    types.AccessTypeEverybody,
			requestedPermission: nil,
			grantedPermission:   types.AccessConfig{Permission: types.AccessTypeEverybody},
		},
		"explicitly set everybody": {
			defaultPermssion:    types.AccessTypeEverybody,
			requestedPermission: &types.AccessConfig{Permission: types.AccessTypeEverybody},
			grantedPermission:   types.AccessConfig{Permission: types.AccessTypeEverybody},
		},
		"cannot override nobody": {
			defaultPermssion:    types.AccessTypeNobody,
			requestedPermission: &onlyCreator,
			expError:            sdkerrors.ErrUnauthorized,
		},
		"default to nobody": {
			defaultPermssion:    types.AccessTypeNobody,
			requestedPermission: nil,
			grantedPermission:   types.AccessConfig{Permission: types.AccessTypeNobody},
		},
		"only defaults to code creator": {
			defaultPermssion:    types.AccessTypeOnlyAddress,
			requestedPermission: nil,
			grantedPermission:   onlyCreator,
		},
		"can explicitly set to code creator": {
			defaultPermssion:    types.AccessTypeOnlyAddress,
			requestedPermission: &onlyCreator,
			grantedPermission:   onlyCreator,
		},
		"cannot override which address in only": {
			defaultPermssion:    types.AccessTypeOnlyAddress,
			requestedPermission: &onlyOther,
			expError:            sdkerrors.ErrUnauthorized,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			params := types.DefaultParams()
			params.InstantiateDefaultPermission = spec.defaultPermssion
			keeper.SetParams(ctx, params)
			codeID, err := contractKeeper.Create(ctx, creator, hackatomWasm, spec.requestedPermission)
			require.True(t, spec.expError.Is(err), err)
			if spec.expError == nil {
				codeInfo := keeper.GetCodeInfo(ctx, codeID)
				require.Equal(t, codeInfo.InstantiateConfig, spec.grantedPermission)
			}
		})
	}
}

func TestCreateDuplicate(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	// create one copy
	contractID, err := keeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), contractID)

	// create second copy
	duplicateID, err := keeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(2), duplicateID)

	// and verify both content is proper
	storedCode, err := keepers.WasmKeeper.GetByteCode(ctx, contractID)
	require.NoError(t, err)
	require.Equal(t, hackatomWasm, storedCode)
	storedCode, err = keepers.WasmKeeper.GetByteCode(ctx, duplicateID)
	require.NoError(t, err)
	require.Equal(t, hackatomWasm, storedCode)
}

func TestCreateWithSimulation(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)

	ctx = ctx.WithBlockHeader(tmproto.Header{Height: 1}).
		WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	// create this once in simulation mode
	contractID, err := keepers.ContractKeeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), contractID)

	// then try to create it in non-simulation mode (should not fail)
	ctx, keepers = CreateTestInput(t, false, SupportedFeatures)
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, 10_000_000))
	creator = keepers.Faucet.NewFundedAccount(ctx, deposit...)
	contractID, err = keepers.ContractKeeper.Create(ctx, creator, hackatomWasm, nil)

	require.NoError(t, err)
	require.Equal(t, uint64(1), contractID)

	// and verify content
	code, err := keepers.WasmKeeper.GetByteCode(ctx, contractID)
	require.NoError(t, err)
	require.Equal(t, code, hackatomWasm)
}

func TestIsSimulationMode(t *testing.T) {
	specs := map[string]struct {
		ctx sdk.Context
		exp bool
	}{
		"genesis block": {
			ctx: sdk.Context{}.WithBlockHeader(tmproto.Header{}).WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)),
			exp: false,
		},
		"any regular block": {
			ctx: sdk.Context{}.WithBlockHeader(tmproto.Header{Height: 1}).WithGasMeter(sdk.NewGasMeter(10000000, 1, 1)),
			exp: false,
		},
		"simulation": {
			ctx: sdk.Context{}.WithBlockHeader(tmproto.Header{Height: 1}).WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)),
			exp: true,
		},
	}
	for msg := range specs {
		t.Run(msg, func(t *testing.T) {
			// assert.Equal(t, spec.exp, isSimulationMode(spec.ctx))
		})
	}
}

func TestCreateWithGzippedPayload(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	wasmCode, err := ioutil.ReadFile("./testdata/hackatom.wasm.gzip")
	require.NoError(t, err, "reading gzipped WASM code")

	contractID, err := keeper.Create(ctx, creator, wasmCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), contractID)
	// and verify content
	storedCode, err := keepers.WasmKeeper.GetByteCode(ctx, contractID)
	require.NoError(t, err)
	require.Equal(t, hackatomWasm, storedCode)
}

func TestInstantiate(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	codeID, err := keeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)

	_, _, bob := keyPubAddr()
	_, _, fred := keyPubAddr()

	initMsg := HackatomExampleInitMsg{
		Verifier:    fred,
		Beneficiary: bob,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)

	gasBefore := ctx.GasMeter().GasConsumed()

	em := sdk.NewEventManager()
	// create with no balance is also legal
	gotContractAddr, _, err := keepers.ContractKeeper.Instantiate(ctx.WithEventManager(em), codeID, creator, nil, initMsgBz, "demo contract 1", nil)
	require.NoError(t, err)
	require.Equal(t, "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr", gotContractAddr.String())

	gasAfter := ctx.GasMeter().GasConsumed()
	if types.EnableGasVerification {
		require.Equal(t, uint64(0x18dde), gasAfter-gasBefore)
	}

	// ensure it is stored properly
	info := keepers.WasmKeeper.GetContractInfo(ctx, gotContractAddr)
	require.NotNil(t, info)
	assert.Equal(t, creator.String(), info.Creator)
	assert.Equal(t, codeID, info.CodeID)
	assert.Equal(t, "demo contract 1", info.Label)

	exp := []types.ContractCodeHistoryEntry{{
		Operation: types.ContractCodeHistoryOperationTypeInit,
		CodeID:    codeID,
		Updated:   types.NewAbsoluteTxPosition(ctx),
		Msg:       initMsgBz,
	}}
	assert.Equal(t, exp, keepers.WasmKeeper.GetContractHistory(ctx, gotContractAddr))

	// and events emitted
	expEvt := sdk.Events{
		sdk.NewEvent("instantiate",
			sdk.NewAttribute("_contract_address", gotContractAddr.String()), sdk.NewAttribute("code_id", "1")),
		sdk.NewEvent("wasm",
			sdk.NewAttribute("_contract_address", gotContractAddr.String()), sdk.NewAttribute("Let the", "hacking begin")),
	}
	assert.Equal(t, expEvt, em.Events())
}

func TestInstantiateWithDeposit(t *testing.T) {
	var (
		bob  = bytes.Repeat([]byte{1}, types.SDKAddrLen)
		fred = bytes.Repeat([]byte{2}, types.SDKAddrLen)

		deposit = sdk.NewCoins(sdk.NewInt64Coin("denom", 100))
		initMsg = HackatomExampleInitMsg{Verifier: fred, Beneficiary: bob}
	)

	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)

	specs := map[string]struct {
		srcActor sdk.AccAddress
		expError bool
		fundAddr bool
	}{
		"address with funds": {
			srcActor: bob,
			fundAddr: true,
		},
		"address without funds": {
			srcActor: bob,
			expError: true,
		},
		"blocked address": {
			srcActor: authtypes.NewModuleAddress(authtypes.FeeCollectorName),
			fundAddr: true,
			expError: false,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
			accKeeper, bankKeeper, keeper := keepers.AccountKeeper, keepers.BankKeeper, keepers.ContractKeeper

			if spec.fundAddr {
				fundAccounts(t, ctx, accKeeper, bankKeeper, spec.srcActor, sdk.NewCoins(sdk.NewInt64Coin("denom", 200)))
			}
			contractID, err := keeper.Create(ctx, spec.srcActor, hackatomWasm, nil)
			require.NoError(t, err)

			// when
			addr, _, err := keepers.ContractKeeper.Instantiate(ctx, contractID, spec.srcActor, nil, initMsgBz, "my label", deposit)
			// then
			if spec.expError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			balances := bankKeeper.GetAllBalances(ctx, addr)
			assert.Equal(t, deposit, balances)
		})
	}
}

func TestInstantiateWithPermissions(t *testing.T) {
	var (
		deposit   = sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
		myAddr    = bytes.Repeat([]byte{1}, types.SDKAddrLen)
		otherAddr = bytes.Repeat([]byte{2}, types.SDKAddrLen)
		anyAddr   = bytes.Repeat([]byte{3}, types.SDKAddrLen)
	)

	initMsg := HackatomExampleInitMsg{
		Verifier:    anyAddr,
		Beneficiary: anyAddr,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)

	specs := map[string]struct {
		srcPermission types.AccessConfig
		srcActor      sdk.AccAddress
		expError      *sdkerrors.Error
	}{
		"default": {
			srcPermission: types.DefaultUploadAccess,
			srcActor:      anyAddr,
		},
		"everybody": {
			srcPermission: types.AllowEverybody,
			srcActor:      anyAddr,
		},
		"nobody": {
			srcPermission: types.AllowNobody,
			srcActor:      myAddr,
			expError:      sdkerrors.ErrUnauthorized,
		},
		"onlyAddress with matching address": {
			srcPermission: types.AccessTypeOnlyAddress.With(myAddr),
			srcActor:      myAddr,
		},
		"onlyAddress with non matching address": {
			srcPermission: types.AccessTypeOnlyAddress.With(otherAddr),
			expError:      sdkerrors.ErrUnauthorized,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
			accKeeper, bankKeeper, keeper := keepers.AccountKeeper, keepers.BankKeeper, keepers.ContractKeeper
			fundAccounts(t, ctx, accKeeper, bankKeeper, spec.srcActor, deposit)

			contractID, err := keeper.Create(ctx, myAddr, hackatomWasm, &spec.srcPermission)
			require.NoError(t, err)

			_, _, err = keepers.ContractKeeper.Instantiate(ctx, contractID, spec.srcActor, nil, initMsgBz, "demo contract 1", nil)
			assert.True(t, spec.expError.Is(err), "got %+v", err)
		})
	}
}

func TestInstantiateWithNonExistingCodeID(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	initMsg := HackatomExampleInitMsg{}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)

	const nonExistingCodeID = 9999
	addr, _, err := keepers.ContractKeeper.Instantiate(ctx, nonExistingCodeID, creator, nil, initMsgBz, "demo contract 2", nil)
	require.True(t, types.ErrNotFound.Is(err), err)
	require.Nil(t, addr)
}

func TestInstantiateWithContractDataResponse(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)

	wasmerMock := &wasmtesting.MockWasmer{
		InstantiateFn: func(codeID wasmvm.Checksum, env wasmvmtypes.Env, info wasmvmtypes.MessageInfo, initMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
			return &wasmvmtypes.Response{Data: []byte("my-response-data")}, 0, nil
		},
		AnalyzeCodeFn: wasmtesting.WithoutIBCAnalyzeFn,
		CreateFn:      wasmtesting.NoOpCreateFn,
	}

	example := StoreRandomContract(t, ctx, keepers, wasmerMock)
	_, data, err := keepers.ContractKeeper.Instantiate(ctx, example.CodeID, example.CreatorAddr, nil, nil, "test", nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("my-response-data"), data)
}

func TestExecute(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	accKeeper, keeper, bankKeeper := keepers.AccountKeeper, keepers.ContractKeeper, keepers.BankKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	topUp := sdk.NewCoins(sdk.NewInt64Coin("denom", 5000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit.Add(deposit...)...)
	fred := keepers.Faucet.NewFundedAccount(ctx, topUp...)

	contractID, err := keeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)

	_, _, bob := keyPubAddr()
	initMsg := HackatomExampleInitMsg{
		Verifier:    fred,
		Beneficiary: bob,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)

	addr, _, err := keepers.ContractKeeper.Instantiate(ctx, contractID, creator, nil, initMsgBz, "demo contract 3", deposit)
	require.NoError(t, err)
	require.Equal(t, "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr", addr.String())

	// ensure bob doesn't exist
	bobAcct := accKeeper.GetAccount(ctx, bob)
	require.Nil(t, bobAcct)

	// ensure funder has reduced balance
	creatorAcct := accKeeper.GetAccount(ctx, creator)
	require.NotNil(t, creatorAcct)
	// we started at 2*deposit, should have spent one above
	assert.Equal(t, deposit, bankKeeper.GetAllBalances(ctx, creatorAcct.GetAddress()))

	// ensure contract has updated balance
	contractAcct := accKeeper.GetAccount(ctx, addr)
	require.NotNil(t, contractAcct)
	assert.Equal(t, deposit, bankKeeper.GetAllBalances(ctx, contractAcct.GetAddress()))

	// unauthorized - trialCtx so we don't change state
	trialCtx := ctx.WithMultiStore(ctx.MultiStore().CacheWrap(keepers.WasmKeeper.storeKey).(sdk.MultiStore))
	res, err := keepers.ContractKeeper.Execute(trialCtx, addr, creator, []byte(`{"release":{}}`), nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, types.ErrExecuteFailed))
	require.Equal(t, "Unauthorized: execute wasm contract failed", err.Error())

	// verifier can execute, and get proper gas amount
	start := time.Now()
	gasBefore := ctx.GasMeter().GasConsumed()
	em := sdk.NewEventManager()
	// when
	res, err = keepers.ContractKeeper.Execute(ctx.WithEventManager(em), addr, fred, []byte(`{"release":{}}`), topUp)
	diff := time.Now().Sub(start)
	require.NoError(t, err)
	require.NotNil(t, res)

	// make sure gas is properly deducted from ctx
	gasAfter := ctx.GasMeter().GasConsumed()
	if types.EnableGasVerification {
		require.Equal(t, uint64(0x17d1c), gasAfter-gasBefore)
	}
	// ensure bob now exists and got both payments released
	bobAcct = accKeeper.GetAccount(ctx, bob)
	require.NotNil(t, bobAcct)
	balance := bankKeeper.GetAllBalances(ctx, bobAcct.GetAddress())
	assert.Equal(t, deposit.Add(topUp...), balance)

	// ensure contract has updated balance
	contractAcct = accKeeper.GetAccount(ctx, addr)
	require.NotNil(t, contractAcct)
	assert.Equal(t, sdk.Coins{}, bankKeeper.GetAllBalances(ctx, contractAcct.GetAddress()))

	// and events emitted
	require.Len(t, em.Events(), 9)
	expEvt := sdk.NewEvent("execute",
		sdk.NewAttribute("_contract_address", addr.String()))
	assert.Equal(t, expEvt, em.Events()[3], prettyEvents(t, em.Events()))

	t.Logf("Duration: %v (%d gas)\n", diff, gasAfter-gasBefore)
}

func TestExecuteWithDeposit(t *testing.T) {
	var (
		bob         = bytes.Repeat([]byte{1}, types.SDKAddrLen)
		fred        = bytes.Repeat([]byte{2}, types.SDKAddrLen)
		blockedAddr = authtypes.NewModuleAddress(distributiontypes.ModuleName)
		deposit     = sdk.NewCoins(sdk.NewInt64Coin("denom", 100))
	)

	specs := map[string]struct {
		srcActor      sdk.AccAddress
		beneficiary   sdk.AccAddress
		newBankParams *banktypes.Params
		expError      bool
		fundAddr      bool
	}{
		"actor with funds": {
			srcActor:    bob,
			fundAddr:    true,
			beneficiary: fred,
		},
		"actor without funds": {
			srcActor:    bob,
			beneficiary: fred,
			expError:    true,
		},
		"blocked address as actor": {
			srcActor:    blockedAddr,
			fundAddr:    true,
			beneficiary: fred,
			expError:    false,
		},
		"coin transfer with all transfers disabled": {
			srcActor:      bob,
			fundAddr:      true,
			beneficiary:   fred,
			newBankParams: &banktypes.Params{DefaultSendEnabled: false},
			expError:      true,
		},
		"coin transfer with transfer denom disabled": {
			srcActor:    bob,
			fundAddr:    true,
			beneficiary: fred,
			newBankParams: &banktypes.Params{
				DefaultSendEnabled: true,
				SendEnabled:        []*banktypes.SendEnabled{{Denom: "denom", Enabled: false}},
			},
			expError: true,
		},
		"blocked address as beneficiary": {
			srcActor:    bob,
			fundAddr:    true,
			beneficiary: blockedAddr,
			expError:    true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
			accKeeper, bankKeeper, keeper := keepers.AccountKeeper, keepers.BankKeeper, keepers.ContractKeeper
			if spec.newBankParams != nil {
				bankKeeper.SetParams(ctx, *spec.newBankParams)
			}
			if spec.fundAddr {
				fundAccounts(t, ctx, accKeeper, bankKeeper, spec.srcActor, sdk.NewCoins(sdk.NewInt64Coin("denom", 200)))
			}
			codeID, err := keeper.Create(ctx, spec.srcActor, hackatomWasm, nil)
			require.NoError(t, err)

			initMsg := HackatomExampleInitMsg{Verifier: spec.srcActor, Beneficiary: spec.beneficiary}
			initMsgBz, err := json.Marshal(initMsg)
			require.NoError(t, err)

			contractAddr, _, err := keepers.ContractKeeper.Instantiate(ctx, codeID, spec.srcActor, nil, initMsgBz, "my label", nil)
			require.NoError(t, err)

			// when
			_, err = keepers.ContractKeeper.Execute(ctx, contractAddr, spec.srcActor, []byte(`{"release":{}}`), deposit)

			// then
			if spec.expError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			balances := bankKeeper.GetAllBalances(ctx, spec.beneficiary)
			assert.Equal(t, deposit, balances)
		})
	}
}

func TestExecuteWithNonExistingAddress(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit.Add(deposit...)...)

	// unauthorized - trialCtx so we don't change state
	nonExistingAddress := RandomAccountAddress(t)
	_, err := keeper.Execute(ctx, nonExistingAddress, creator, []byte(`{}`), nil)
	require.True(t, types.ErrNotFound.Is(err), err)
}

func TestExecuteWithPanic(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	topUp := sdk.NewCoins(sdk.NewInt64Coin("denom", 5000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit.Add(deposit...)...)
	fred := keepers.Faucet.NewFundedAccount(ctx, topUp...)

	contractID, err := keeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)

	_, _, bob := keyPubAddr()
	initMsg := HackatomExampleInitMsg{
		Verifier:    fred,
		Beneficiary: bob,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)

	addr, _, err := keepers.ContractKeeper.Instantiate(ctx, contractID, creator, nil, initMsgBz, "demo contract 4", deposit)
	require.NoError(t, err)

	// let's make sure we get a reasonable error, no panic/crash
	_, err = keepers.ContractKeeper.Execute(ctx, addr, fred, []byte(`{"panic":{}}`), topUp)
	require.Error(t, err)
	require.True(t, errors.Is(err, types.ErrExecuteFailed))
	// test with contains as "Display" implementation of the Wasmer "RuntimeError" is different for Mac and Linux
	assert.Contains(t, err.Error(), "Error calling the VM: Error executing Wasm: Wasmer runtime error: RuntimeError: unreachable")
}

func TestExecuteWithCpuLoop(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	topUp := sdk.NewCoins(sdk.NewInt64Coin("denom", 5000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit.Add(deposit...)...)
	fred := keepers.Faucet.NewFundedAccount(ctx, topUp...)

	contractID, err := keeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)

	_, _, bob := keyPubAddr()
	initMsg := HackatomExampleInitMsg{
		Verifier:    fred,
		Beneficiary: bob,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)

	addr, _, err := keepers.ContractKeeper.Instantiate(ctx, contractID, creator, nil, initMsgBz, "demo contract 5", deposit)
	require.NoError(t, err)

	// make sure we set a limit before calling
	var gasLimit uint64 = 400_000
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, gasLimit))
	require.Equal(t, uint64(0), ctx.GasMeter().GasConsumed())

	// ensure we get an out of gas panic
	defer func() {
		r := recover()
		require.NotNil(t, r)
		_, ok := r.(sdk.ErrorOutOfGas)
		require.True(t, ok, "%v", r)
	}()

	// this should throw out of gas exception (panic)
	_, err = keepers.ContractKeeper.Execute(ctx, addr, fred, []byte(`{"cpu_loop":{}}`), nil)
	require.True(t, false, "We must panic before this line")
}

func TestExecuteWithStorageLoop(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	topUp := sdk.NewCoins(sdk.NewInt64Coin("denom", 5000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit.Add(deposit...)...)
	fred := keepers.Faucet.NewFundedAccount(ctx, topUp...)

	contractID, err := keeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)

	_, _, bob := keyPubAddr()
	initMsg := HackatomExampleInitMsg{
		Verifier:    fred,
		Beneficiary: bob,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)

	addr, _, err := keepers.ContractKeeper.Instantiate(ctx, contractID, creator, nil, initMsgBz, "demo contract 6", deposit)
	require.NoError(t, err)

	// make sure we set a limit before calling
	var gasLimit uint64 = 400_002
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, gasLimit))
	require.Equal(t, uint64(0), ctx.GasMeter().GasConsumed())

	// ensure we get an out of gas panic
	defer func() {
		r := recover()
		require.NotNil(t, r)
		_, ok := r.(sdk.ErrorOutOfGas)
		require.True(t, ok, "%v", r)
	}()

	// this should throw out of gas exception (panic)
	_, err = keepers.ContractKeeper.Execute(ctx, addr, fred, []byte(`{"storage_loop":{}}`), nil)
	require.True(t, false, "We must panic before this line")
}

func TestMigrate(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	topUp := sdk.NewCoins(sdk.NewInt64Coin("denom", 5000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit.Add(deposit...)...)
	fred := keepers.Faucet.NewFundedAccount(ctx, topUp...)

	originalCodeID := StoreHackatomExampleContract(t, ctx, keepers).CodeID
	newCodeID := StoreHackatomExampleContract(t, ctx, keepers).CodeID
	ibcCodeID := StoreIBCReflectContract(t, ctx, keepers).CodeID
	require.NotEqual(t, originalCodeID, newCodeID)

	anyAddr := RandomAccountAddress(t)
	newVerifierAddr := RandomAccountAddress(t)
	initMsgBz := HackatomExampleInitMsg{
		Verifier:    fred,
		Beneficiary: anyAddr,
	}.GetBytes(t)

	migMsg := struct {
		Verifier sdk.AccAddress `json:"verifier"`
	}{Verifier: newVerifierAddr}
	migMsgBz, err := json.Marshal(migMsg)
	require.NoError(t, err)

	specs := map[string]struct {
		admin                sdk.AccAddress
		overrideContractAddr sdk.AccAddress
		caller               sdk.AccAddress
		fromCodeID           uint64
		toCodeID             uint64
		migrateMsg           []byte
		expErr               *sdkerrors.Error
		expVerifier          sdk.AccAddress
		expIBCPort           bool
		initMsg              []byte
	}{
		"all good with same code id": {
			admin:       creator,
			caller:      creator,
			initMsg:     initMsgBz,
			fromCodeID:  originalCodeID,
			toCodeID:    originalCodeID,
			migrateMsg:  migMsgBz,
			expVerifier: newVerifierAddr,
		},
		"all good with different code id": {
			admin:       creator,
			caller:      creator,
			initMsg:     initMsgBz,
			fromCodeID:  originalCodeID,
			toCodeID:    newCodeID,
			migrateMsg:  migMsgBz,
			expVerifier: newVerifierAddr,
		},
		"all good with admin set": {
			admin:       fred,
			caller:      fred,
			initMsg:     initMsgBz,
			fromCodeID:  originalCodeID,
			toCodeID:    newCodeID,
			migrateMsg:  migMsgBz,
			expVerifier: newVerifierAddr,
		},
		"adds IBC port for IBC enabled contracts": {
			admin:       fred,
			caller:      fred,
			initMsg:     initMsgBz,
			fromCodeID:  originalCodeID,
			toCodeID:    ibcCodeID,
			migrateMsg:  []byte(`{}`),
			expIBCPort:  true,
			expVerifier: fred, // not updated
		},
		"prevent migration when admin was not set on instantiate": {
			caller:     creator,
			initMsg:    initMsgBz,
			fromCodeID: originalCodeID,
			toCodeID:   originalCodeID,
			expErr:     sdkerrors.ErrUnauthorized,
		},
		"prevent migration when not sent by admin": {
			caller:     creator,
			admin:      fred,
			initMsg:    initMsgBz,
			fromCodeID: originalCodeID,
			toCodeID:   originalCodeID,
			expErr:     sdkerrors.ErrUnauthorized,
		},
		"fail with non existing code id": {
			admin:      creator,
			caller:     creator,
			initMsg:    initMsgBz,
			fromCodeID: originalCodeID,
			toCodeID:   99999,
			expErr:     sdkerrors.ErrInvalidRequest,
		},
		"fail with non existing contract addr": {
			admin:                creator,
			caller:               creator,
			initMsg:              initMsgBz,
			overrideContractAddr: anyAddr,
			fromCodeID:           originalCodeID,
			toCodeID:             originalCodeID,
			expErr:               sdkerrors.ErrInvalidRequest,
		},
		"fail in contract with invalid migrate msg": {
			admin:      creator,
			caller:     creator,
			initMsg:    initMsgBz,
			fromCodeID: originalCodeID,
			toCodeID:   originalCodeID,
			migrateMsg: bytes.Repeat([]byte{0x1}, 7),
			expErr:     types.ErrMigrationFailed,
		},
		"fail in contract without migrate msg": {
			admin:      creator,
			caller:     creator,
			initMsg:    initMsgBz,
			fromCodeID: originalCodeID,
			toCodeID:   originalCodeID,
			expErr:     types.ErrMigrationFailed,
		},
		"fail when no IBC callbacks": {
			admin:      fred,
			caller:     fred,
			initMsg:    IBCReflectInitMsg{ReflectCodeID: StoreReflectContract(t, ctx, keepers)}.GetBytes(t),
			fromCodeID: ibcCodeID,
			toCodeID:   newCodeID,
			migrateMsg: migMsgBz,
			expErr:     types.ErrMigrationFailed,
		},
	}

	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			// given a contract instance
			ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
			contractAddr, _, err := keepers.ContractKeeper.Instantiate(ctx, spec.fromCodeID, creator, spec.admin, spec.initMsg, "demo contract", nil)
			require.NoError(t, err)
			if spec.overrideContractAddr != nil {
				contractAddr = spec.overrideContractAddr
			}
			// when
			_, err = keeper.Migrate(ctx, contractAddr, spec.caller, spec.toCodeID, spec.migrateMsg)

			// then
			require.True(t, spec.expErr.Is(err), "expected %v but got %+v", spec.expErr, err)
			if spec.expErr != nil {
				return
			}
			cInfo := keepers.WasmKeeper.GetContractInfo(ctx, contractAddr)
			assert.Equal(t, spec.toCodeID, cInfo.CodeID)
			assert.Equal(t, spec.expIBCPort, cInfo.IBCPortID != "", cInfo.IBCPortID)

			expHistory := []types.ContractCodeHistoryEntry{{
				Operation: types.ContractCodeHistoryOperationTypeInit,
				CodeID:    spec.fromCodeID,
				Updated:   types.NewAbsoluteTxPosition(ctx),
				Msg:       initMsgBz,
			}, {
				Operation: types.ContractCodeHistoryOperationTypeMigrate,
				CodeID:    spec.toCodeID,
				Updated:   types.NewAbsoluteTxPosition(ctx),
				Msg:       spec.migrateMsg,
			}}
			assert.Equal(t, expHistory, keepers.WasmKeeper.GetContractHistory(ctx, contractAddr))

			// and verify contract state
			raw := keepers.WasmKeeper.QueryRaw(ctx, contractAddr, []byte("config"))
			var stored map[string]string
			require.NoError(t, json.Unmarshal(raw, &stored))
			require.Contains(t, stored, "verifier")
			require.NoError(t, err)
			assert.Equal(t, spec.expVerifier.String(), stored["verifier"])
		})
	}
}

func TestMigrateReplacesTheSecondIndex(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	example := InstantiateHackatomExampleContract(t, ctx, keepers)

	// then assert a second index exists
	store := ctx.KVStore(keepers.WasmKeeper.storeKey)
	oldContractInfo := keepers.WasmKeeper.GetContractInfo(ctx, example.Contract)
	require.NotNil(t, oldContractInfo)
	createHistoryEntry := types.ContractCodeHistoryEntry{
		CodeID:  example.CodeID,
		Updated: types.NewAbsoluteTxPosition(ctx),
	}
	exists := store.Has(types.GetContractByCreatedSecondaryIndexKey(example.Contract, createHistoryEntry))
	require.True(t, exists)

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1) // increment for different block
	// when do migrate
	newCodeExample := StoreBurnerExampleContract(t, ctx, keepers)
	migMsgBz := BurnerExampleInitMsg{Payout: example.CreatorAddr}.GetBytes(t)
	_, err := keepers.ContractKeeper.Migrate(ctx, example.Contract, example.CreatorAddr, newCodeExample.CodeID, migMsgBz)
	require.NoError(t, err)

	// then the new index exists
	migrateHistoryEntry := types.ContractCodeHistoryEntry{
		CodeID:  newCodeExample.CodeID,
		Updated: types.NewAbsoluteTxPosition(ctx),
	}
	exists = store.Has(types.GetContractByCreatedSecondaryIndexKey(example.Contract, migrateHistoryEntry))
	require.True(t, exists)
	// and the old index was removed
	exists = store.Has(types.GetContractByCreatedSecondaryIndexKey(example.Contract, createHistoryEntry))
	require.False(t, exists)
}

func TestMigrateWithDispatchedMessage(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit.Add(deposit...)...)
	fred := keepers.Faucet.NewFundedAccount(ctx, sdk.NewInt64Coin("denom", 5000))

	burnerCode, err := ioutil.ReadFile("./testdata/burner.wasm")
	require.NoError(t, err)

	originalContractID, err := keeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)
	burnerContractID, err := keeper.Create(ctx, creator, burnerCode, nil)
	require.NoError(t, err)
	require.NotEqual(t, originalContractID, burnerContractID)

	_, _, myPayoutAddr := keyPubAddr()
	initMsg := HackatomExampleInitMsg{
		Verifier:    fred,
		Beneficiary: fred,
	}
	initMsgBz := initMsg.GetBytes(t)

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	contractAddr, _, err := keepers.ContractKeeper.Instantiate(ctx, originalContractID, creator, fred, initMsgBz, "demo contract", deposit)
	require.NoError(t, err)

	migMsgBz := BurnerExampleInitMsg{Payout: myPayoutAddr}.GetBytes(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager()).WithBlockHeight(ctx.BlockHeight() + 1)
	data, err := keeper.Migrate(ctx, contractAddr, fred, burnerContractID, migMsgBz)
	require.NoError(t, err)
	assert.Equal(t, "burnt 1 keys", string(data))
	type dict map[string]interface{}
	expEvents := []dict{
		{
			"Type": "migrate",
			"Attr": []dict{
				{"code_id": "2"},
				{"_contract_address": contractAddr},
			},
		},
		{
			"Type": "wasm",
			"Attr": []dict{
				{"_contract_address": contractAddr},
				{"action": "burn"},
				{"payout": myPayoutAddr},
			},
		},
		{
			"Type": "coin_spent",
			"Attr": []dict{
				{"spender": contractAddr},
				{"amount": "100000denom"},
			},
		},
		{
			"Type": "coin_received",
			"Attr": []dict{
				{"receiver": myPayoutAddr},
				{"amount": "100000denom"},
			},
		},
		{
			"Type": "transfer",
			"Attr": []dict{
				{"recipient": myPayoutAddr},
				{"sender": contractAddr},
				{"amount": "100000denom"},
			},
		},
	}
	expJSONEvts := string(mustMarshal(t, expEvents))
	assert.JSONEq(t, expJSONEvts, prettyEvents(t, ctx.EventManager().Events()), prettyEvents(t, ctx.EventManager().Events()))

	// all persistent data cleared
	m := keepers.WasmKeeper.QueryRaw(ctx, contractAddr, []byte("config"))
	require.Len(t, m, 0)

	// and all deposit tokens sent to myPayoutAddr
	balance := keepers.BankKeeper.GetAllBalances(ctx, myPayoutAddr)
	assert.Equal(t, deposit, balance)
}

func TestIterateContractsByCode(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	k, c := keepers.WasmKeeper, keepers.ContractKeeper
	example1 := InstantiateHackatomExampleContract(t, ctx, keepers)
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	example2 := InstantiateIBCReflectContract(t, ctx, keepers)
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	initMsg := HackatomExampleInitMsg{
		Verifier:    RandomAccountAddress(t),
		Beneficiary: RandomAccountAddress(t),
	}.GetBytes(t)
	contractAddr3, _, err := c.Instantiate(ctx, example1.CodeID, example1.CreatorAddr, nil, initMsg, "foo", nil)
	require.NoError(t, err)
	specs := map[string]struct {
		codeID uint64
		exp    []sdk.AccAddress
	}{
		"multiple results": {
			codeID: example1.CodeID,
			exp:    []sdk.AccAddress{example1.Contract, contractAddr3},
		},
		"single results": {
			codeID: example2.CodeID,
			exp:    []sdk.AccAddress{example2.Contract},
		},
		"empty results": {
			codeID: 99999,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			var gotAddr []sdk.AccAddress
			k.IterateContractsByCode(ctx, spec.codeID, func(address sdk.AccAddress) bool {
				gotAddr = append(gotAddr, address)
				return false
			})
			assert.Equal(t, spec.exp, gotAddr)
		})
	}
}

func TestIterateContractsByCodeWithMigration(t *testing.T) {
	// mock migration so that it does not fail when migrate example1 to example2.codeID
	mockWasmVM := wasmtesting.MockWasmer{MigrateFn: func(codeID wasmvm.Checksum, env wasmvmtypes.Env, migrateMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
		return &wasmvmtypes.Response{}, 1, nil
	}}
	wasmtesting.MakeInstantiable(&mockWasmVM)
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures, WithWasmEngine(&mockWasmVM))
	k, c := keepers.WasmKeeper, keepers.ContractKeeper
	example1 := InstantiateHackatomExampleContract(t, ctx, keepers)
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	example2 := InstantiateIBCReflectContract(t, ctx, keepers)
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	_, err := c.Migrate(ctx, example1.Contract, example1.CreatorAddr, example2.CodeID, []byte("{}"))
	require.NoError(t, err)

	// when
	var gotAddr []sdk.AccAddress
	k.IterateContractsByCode(ctx, example2.CodeID, func(address sdk.AccAddress) bool {
		gotAddr = append(gotAddr, address)
		return false
	})

	// then
	exp := []sdk.AccAddress{example2.Contract, example1.Contract}
	assert.Equal(t, exp, gotAddr)
}

type sudoMsg struct {
	// This is a tongue-in-check demo command. This is not the intended purpose of Sudo.
	// Here we show that some priviledged Go module can make a call that should never be exposed
	// to end users (via Tx/Execute).
	//
	// The contract developer can choose to expose anything to sudo. This functionality is not a true
	// backdoor (it can never be called by end users), but allows the developers of the native blockchain
	// code to make special calls. This can also be used as an authentication mechanism, if you want to expose
	// some callback that only can be triggered by some system module and not faked by external users.
	StealFunds stealFundsMsg `json:"steal_funds"`
}

type stealFundsMsg struct {
	Recipient string            `json:"recipient"`
	Amount    wasmvmtypes.Coins `json:"amount"`
}

func TestSudo(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	accKeeper, keeper, bankKeeper := keepers.AccountKeeper, keepers.ContractKeeper, keepers.BankKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit.Add(deposit...)...)

	contractID, err := keeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)

	_, _, bob := keyPubAddr()
	_, _, fred := keyPubAddr()
	initMsg := HackatomExampleInitMsg{
		Verifier:    fred,
		Beneficiary: bob,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)
	addr, _, err := keepers.ContractKeeper.Instantiate(ctx, contractID, creator, nil, initMsgBz, "demo contract 3", deposit)
	require.NoError(t, err)
	require.Equal(t, "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr", addr.String())

	// the community is broke
	_, _, community := keyPubAddr()
	comAcct := accKeeper.GetAccount(ctx, community)
	require.Nil(t, comAcct)

	// now the community wants to get paid via sudo
	msg := sudoMsg{
		// This is a tongue-in-check demo command. This is not the intended purpose of Sudo.
		// Here we show that some priviledged Go module can make a call that should never be exposed
		// to end users (via Tx/Execute).
		StealFunds: stealFundsMsg{
			Recipient: community.String(),
			Amount:    wasmvmtypes.Coins{wasmvmtypes.NewCoin(76543, "denom")},
		},
	}
	sudoMsg, err := json.Marshal(msg)
	require.NoError(t, err)

	em := sdk.NewEventManager()

	// when
	_, err = keepers.WasmKeeper.Sudo(ctx.WithEventManager(em), addr, sudoMsg)
	require.NoError(t, err)

	// ensure community now exists and got paid
	comAcct = accKeeper.GetAccount(ctx, community)
	require.NotNil(t, comAcct)
	balance := bankKeeper.GetBalance(ctx, comAcct.GetAddress(), "denom")
	assert.Equal(t, sdk.NewInt64Coin("denom", 76543), balance)
	// and events emitted
	require.Len(t, em.Events(), 4, prettyEvents(t, em.Events()))
	expEvt := sdk.NewEvent("sudo",
		sdk.NewAttribute("_contract_address", addr.String()))
	assert.Equal(t, expEvt, em.Events()[0])
}

func prettyEvents(t *testing.T, events sdk.Events) string {
	t.Helper()
	type prettyEvent struct {
		Type string
		Attr []map[string]string
	}

	r := make([]prettyEvent, len(events))
	for i, e := range events {
		attr := make([]map[string]string, len(e.Attributes))
		for j, a := range e.Attributes {
			attr[j] = map[string]string{string(a.Key): string(a.Value)}
		}
		r[i] = prettyEvent{Type: e.Type, Attr: attr}
	}
	return string(mustMarshal(t, r))
}

func mustMarshal(t *testing.T, r interface{}) []byte {
	t.Helper()
	bz, err := json.Marshal(r)
	require.NoError(t, err)
	return bz
}

func TestUpdateContractAdmin(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	topUp := sdk.NewCoins(sdk.NewInt64Coin("denom", 5000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit.Add(deposit...)...)
	fred := keepers.Faucet.NewFundedAccount(ctx, topUp...)

	originalContractID, err := keeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)

	_, _, anyAddr := keyPubAddr()
	initMsg := HackatomExampleInitMsg{
		Verifier:    fred,
		Beneficiary: anyAddr,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)
	specs := map[string]struct {
		instAdmin            sdk.AccAddress
		newAdmin             sdk.AccAddress
		overrideContractAddr sdk.AccAddress
		caller               sdk.AccAddress
		expErr               *sdkerrors.Error
	}{
		"all good with admin set": {
			instAdmin: fred,
			newAdmin:  anyAddr,
			caller:    fred,
		},
		"prevent update when admin was not set on instantiate": {
			caller:   creator,
			newAdmin: fred,
			expErr:   sdkerrors.ErrUnauthorized,
		},
		"prevent updates from non admin address": {
			instAdmin: creator,
			newAdmin:  fred,
			caller:    fred,
			expErr:    sdkerrors.ErrUnauthorized,
		},
		"fail with non existing contract addr": {
			instAdmin:            creator,
			newAdmin:             anyAddr,
			caller:               creator,
			overrideContractAddr: anyAddr,
			expErr:               sdkerrors.ErrInvalidRequest,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			addr, _, err := keepers.ContractKeeper.Instantiate(ctx, originalContractID, creator, spec.instAdmin, initMsgBz, "demo contract", nil)
			require.NoError(t, err)
			if spec.overrideContractAddr != nil {
				addr = spec.overrideContractAddr
			}
			err = keeper.UpdateContractAdmin(ctx, addr, spec.caller, spec.newAdmin)
			require.True(t, spec.expErr.Is(err), "expected %v but got %+v", spec.expErr, err)
			if spec.expErr != nil {
				return
			}
			cInfo := keepers.WasmKeeper.GetContractInfo(ctx, addr)
			assert.Equal(t, spec.newAdmin.String(), cInfo.Admin)
		})
	}
}

func TestClearContractAdmin(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	keeper := keepers.ContractKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	topUp := sdk.NewCoins(sdk.NewInt64Coin("denom", 5000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit.Add(deposit...)...)
	fred := keepers.Faucet.NewFundedAccount(ctx, topUp...)

	originalContractID, err := keeper.Create(ctx, creator, hackatomWasm, nil)
	require.NoError(t, err)

	_, _, anyAddr := keyPubAddr()
	initMsg := HackatomExampleInitMsg{
		Verifier:    fred,
		Beneficiary: anyAddr,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)
	specs := map[string]struct {
		instAdmin            sdk.AccAddress
		overrideContractAddr sdk.AccAddress
		caller               sdk.AccAddress
		expErr               *sdkerrors.Error
	}{
		"all good when called by proper admin": {
			instAdmin: fred,
			caller:    fred,
		},
		"prevent update when admin was not set on instantiate": {
			caller: creator,
			expErr: sdkerrors.ErrUnauthorized,
		},
		"prevent updates from non admin address": {
			instAdmin: creator,
			caller:    fred,
			expErr:    sdkerrors.ErrUnauthorized,
		},
		"fail with non existing contract addr": {
			instAdmin:            creator,
			caller:               creator,
			overrideContractAddr: anyAddr,
			expErr:               sdkerrors.ErrInvalidRequest,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			addr, _, err := keepers.ContractKeeper.Instantiate(ctx, originalContractID, creator, spec.instAdmin, initMsgBz, "demo contract", nil)
			require.NoError(t, err)
			if spec.overrideContractAddr != nil {
				addr = spec.overrideContractAddr
			}
			err = keeper.ClearContractAdmin(ctx, addr, spec.caller)
			require.True(t, spec.expErr.Is(err), "expected %v but got %+v", spec.expErr, err)
			if spec.expErr != nil {
				return
			}
			cInfo := keepers.WasmKeeper.GetContractInfo(ctx, addr)
			assert.Empty(t, cInfo.Admin)
		})
	}
}

func TestPinCode(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	k := keepers.WasmKeeper

	var capturedChecksums []wasmvm.Checksum
	mock := wasmtesting.MockWasmer{PinFn: func(checksum wasmvm.Checksum) error {
		capturedChecksums = append(capturedChecksums, checksum)
		return nil
	}}
	wasmtesting.MakeInstantiable(&mock)
	myCodeID := StoreRandomContract(t, ctx, keepers, &mock).CodeID
	require.Equal(t, uint64(1), myCodeID)
	em := sdk.NewEventManager()

	// when
	gotErr := k.pinCode(ctx.WithEventManager(em), myCodeID)

	// then
	require.NoError(t, gotErr)
	assert.NotEmpty(t, capturedChecksums)
	assert.True(t, k.IsPinnedCode(ctx, myCodeID))

	// and events
	exp := sdk.Events{sdk.NewEvent("pin_code", sdk.NewAttribute("code_id", "1"))}
	assert.Equal(t, exp, em.Events())
}

func TestUnpinCode(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	k := keepers.WasmKeeper

	var capturedChecksums []wasmvm.Checksum
	mock := wasmtesting.MockWasmer{
		PinFn: func(checksum wasmvm.Checksum) error {
			return nil
		},
		UnpinFn: func(checksum wasmvm.Checksum) error {
			capturedChecksums = append(capturedChecksums, checksum)
			return nil
		},
	}
	wasmtesting.MakeInstantiable(&mock)
	myCodeID := StoreRandomContract(t, ctx, keepers, &mock).CodeID
	require.Equal(t, uint64(1), myCodeID)
	err := k.pinCode(ctx, myCodeID)
	require.NoError(t, err)
	em := sdk.NewEventManager()

	// when
	gotErr := k.unpinCode(ctx.WithEventManager(em), myCodeID)

	// then
	require.NoError(t, gotErr)
	assert.NotEmpty(t, capturedChecksums)
	assert.False(t, k.IsPinnedCode(ctx, myCodeID))

	// and events
	exp := sdk.Events{sdk.NewEvent("unpin_code", sdk.NewAttribute("code_id", "1"))}
	assert.Equal(t, exp, em.Events())
}

func TestInitializePinnedCodes(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	k := keepers.WasmKeeper

	var capturedChecksums []wasmvm.Checksum
	mock := wasmtesting.MockWasmer{PinFn: func(checksum wasmvm.Checksum) error {
		capturedChecksums = append(capturedChecksums, checksum)
		return nil
	}}
	wasmtesting.MakeInstantiable(&mock)

	const testItems = 3
	myCodeIDs := make([]uint64, testItems)
	for i := 0; i < testItems; i++ {
		myCodeIDs[i] = StoreRandomContract(t, ctx, keepers, &mock).CodeID
		require.NoError(t, k.pinCode(ctx, myCodeIDs[i]))
	}
	capturedChecksums = nil

	// when
	gotErr := k.InitializePinnedCodes(ctx)

	// then
	require.NoError(t, gotErr)
	require.Len(t, capturedChecksums, testItems)
	for i, c := range myCodeIDs {
		var exp wasmvm.Checksum = k.GetCodeInfo(ctx, c).CodeHash
		assert.Equal(t, exp, capturedChecksums[i])
	}
}

func TestPinnedContractLoops(t *testing.T) {
	var capturedChecksums []wasmvm.Checksum
	mock := wasmtesting.MockWasmer{PinFn: func(checksum wasmvm.Checksum) error {
		capturedChecksums = append(capturedChecksums, checksum)
		return nil
	}}
	wasmtesting.MakeInstantiable(&mock)

	// a pinned contract that calls itself via submessages should terminate with an
	// error at some point
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures, WithWasmEngine(&mock))
	k := keepers.WasmKeeper

	example := SeedNewContractInstance(t, ctx, keepers, &mock)
	require.NoError(t, k.pinCode(ctx, example.CodeID))
	var loops int
	anyMsg := []byte(`{}`)
	mock.ExecuteFn = func(codeID wasmvm.Checksum, env wasmvmtypes.Env, info wasmvmtypes.MessageInfo, executeMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
		loops++
		return &wasmvmtypes.Response{
			Messages: []wasmvmtypes.SubMsg{
				{
					ID:      1,
					ReplyOn: wasmvmtypes.ReplyNever,
					Msg: wasmvmtypes.CosmosMsg{
						Wasm: &wasmvmtypes.WasmMsg{
							Execute: &wasmvmtypes.ExecuteMsg{
								ContractAddr: example.Contract.String(),
								Msg:          anyMsg,
							},
						},
					},
				},
			},
		}, 0, nil
	}
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, 20000))
	require.PanicsWithValue(t, sdk.ErrorOutOfGas{Descriptor: "ReadFlat"}, func() {
		_, err := k.execute(ctx, example.Contract, RandomAccountAddress(t), anyMsg, nil)
		require.NoError(t, err)
	})
	assert.True(t, ctx.GasMeter().IsOutOfGas())
	assert.Greater(t, loops, 2)
}

func TestNewDefaultWasmVMContractResponseHandler(t *testing.T) {
	specs := map[string]struct {
		srcData []byte
		setup   func(m *wasmtesting.MockMsgDispatcher)
		expErr  bool
		expData []byte
		expEvts sdk.Events
	}{
		"submessage overwrites result when set": {
			srcData: []byte("otherData"),
			setup: func(m *wasmtesting.MockMsgDispatcher) {
				m.DispatchSubmessagesFn = func(ctx sdk.Context, contractAddr sdk.AccAddress, ibcPort string, msgs []wasmvmtypes.SubMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) ([]byte, error) {
					return []byte("mySubMsgData"), nil
				}
			},
			expErr:  false,
			expData: []byte("mySubMsgData"),
			expEvts: sdk.Events{},
		},
		"submessage overwrites result when empty": {
			srcData: []byte("otherData"),
			setup: func(m *wasmtesting.MockMsgDispatcher) {
				m.DispatchSubmessagesFn = func(ctx sdk.Context, contractAddr sdk.AccAddress, ibcPort string, msgs []wasmvmtypes.SubMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) ([]byte, error) {
					return []byte(""), nil
				}
			},
			expErr:  false,
			expData: []byte(""),
			expEvts: sdk.Events{},
		},
		"submessage do not overwrite result when nil": {
			srcData: []byte("otherData"),
			setup: func(m *wasmtesting.MockMsgDispatcher) {
				m.DispatchSubmessagesFn = func(ctx sdk.Context, contractAddr sdk.AccAddress, ibcPort string, msgs []wasmvmtypes.SubMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) ([]byte, error) {
					return nil, nil
				}
			},
			expErr:  false,
			expData: []byte("otherData"),
			expEvts: sdk.Events{},
		},
		"submessage error aborts process": {
			setup: func(m *wasmtesting.MockMsgDispatcher) {
				m.DispatchSubmessagesFn = func(ctx sdk.Context, contractAddr sdk.AccAddress, ibcPort string, msgs []wasmvmtypes.SubMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) ([]byte, error) {
					return nil, errors.New("test - ignore")
				}
			},
			expErr: true,
		},
		"message emit non message events": {
			setup: func(m *wasmtesting.MockMsgDispatcher) {
				m.DispatchSubmessagesFn = func(ctx sdk.Context, contractAddr sdk.AccAddress, ibcPort string, msgs []wasmvmtypes.SubMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) ([]byte, error) {
					ctx.EventManager().EmitEvent(sdk.NewEvent("myEvent"))
					return nil, nil
				}
			},
			expEvts: sdk.Events{sdk.NewEvent("myEvent")},
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			var msgs []wasmvmtypes.SubMsg
			var mock wasmtesting.MockMsgDispatcher
			spec.setup(&mock)
			d := NewDefaultWasmVMContractResponseHandler(&mock)
			em := sdk.NewEventManager()

			// when
			gotData, gotErr := d.Handle(sdk.Context{}.WithEventManager(em), RandomAccountAddress(t), "ibc-port", msgs, spec.srcData, wasmvmtypes.MessageInfo{}, types.CodeInfo{})
			if spec.expErr {
				require.Error(t, gotErr)
				return
			}
			require.NoError(t, gotErr)
			assert.Equal(t, spec.expData, gotData)
			assert.Equal(t, spec.expEvts, em.Events())
		})
	}
}

func TestReply(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	k := keepers.WasmKeeper
	var mock wasmtesting.MockWasmer
	wasmtesting.MakeInstantiable(&mock)
	example := SeedNewContractInstance(t, ctx, keepers, &mock)

	specs := map[string]struct {
		replyFn func(codeID wasmvm.Checksum, env wasmvmtypes.Env, reply wasmvmtypes.Reply, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error)
		expData []byte
		expErr  bool
		expEvt  sdk.Events
	}{
		"all good": {
			replyFn: func(codeID wasmvm.Checksum, env wasmvmtypes.Env, reply wasmvmtypes.Reply, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
				return &wasmvmtypes.Response{Data: []byte("foo")}, 1, nil
			},
			expData: []byte("foo"),
			expEvt:  sdk.Events{sdk.NewEvent("reply", sdk.NewAttribute("_contract_address", example.Contract.String()))},
		},
		"with query": {
			replyFn: func(codeID wasmvm.Checksum, env wasmvmtypes.Env, reply wasmvmtypes.Reply, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
				bzRsp, err := querier.Query(wasmvmtypes.QueryRequest{
					Bank: &wasmvmtypes.BankQuery{
						Balance: &wasmvmtypes.BalanceQuery{Address: env.Contract.Address, Denom: "stake"},
					},
				}, 10_000*DefaultGasMultiplier)
				require.NoError(t, err)
				var gotBankRsp wasmvmtypes.BalanceResponse
				require.NoError(t, json.Unmarshal(bzRsp, &gotBankRsp))
				assert.Equal(t, wasmvmtypes.BalanceResponse{Amount: wasmvmtypes.NewCoin(0, "stake")}, gotBankRsp)
				return &wasmvmtypes.Response{Data: []byte("foo")}, 1, nil
			},
			expData: []byte("foo"),
			expEvt:  sdk.Events{sdk.NewEvent("reply", sdk.NewAttribute("_contract_address", example.Contract.String()))},
		},
		"with query error handled": {
			replyFn: func(codeID wasmvm.Checksum, env wasmvmtypes.Env, reply wasmvmtypes.Reply, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
				bzRsp, err := querier.Query(wasmvmtypes.QueryRequest{}, 0)
				require.Error(t, err)
				assert.Nil(t, bzRsp)
				return &wasmvmtypes.Response{Data: []byte("foo")}, 1, nil
			},
			expData: []byte("foo"),
			expEvt:  sdk.Events{sdk.NewEvent("reply", sdk.NewAttribute("_contract_address", example.Contract.String()))},
		},
		"error": {
			replyFn: func(codeID wasmvm.Checksum, env wasmvmtypes.Env, reply wasmvmtypes.Reply, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
				return nil, 1, errors.New("testing")
			},
			expErr: true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			mock.ReplyFn = spec.replyFn
			em := sdk.NewEventManager()
			gotData, gotErr := k.reply(ctx.WithEventManager(em), example.Contract, wasmvmtypes.Reply{})
			if spec.expErr {
				require.Error(t, gotErr)
				return
			}
			require.NoError(t, gotErr)
			assert.Equal(t, spec.expData, gotData)
			assert.Equal(t, spec.expEvt, em.Events())
		})
	}
}

func TestQueryIsolation(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	k := keepers.WasmKeeper
	var mock wasmtesting.MockWasmer
	wasmtesting.MakeInstantiable(&mock)
	example := SeedNewContractInstance(t, ctx, keepers, &mock)
	WithQueryHandlerDecorator(func(other WasmVMQueryHandler) WasmVMQueryHandler {
		return WasmVMQueryHandlerFn(func(ctx sdk.Context, caller sdk.AccAddress, request wasmvmtypes.QueryRequest) ([]byte, error) {
			if request.Custom == nil {
				return other.HandleQuery(ctx, caller, request)
			}
			// here we write to DB which should not be persisted
			ctx.KVStore(k.storeKey).Set([]byte(`set_in_query`), []byte(`this_is_allowed`))
			return nil, nil
		})
	}).apply(k)

	// when
	mock.ReplyFn = func(codeID wasmvm.Checksum, env wasmvmtypes.Env, reply wasmvmtypes.Reply, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
		_, err := querier.Query(wasmvmtypes.QueryRequest{
			Custom: []byte(`{}`),
		}, 10000*DefaultGasMultiplier)
		require.NoError(t, err)
		return &wasmvmtypes.Response{}, 0, nil
	}
	em := sdk.NewEventManager()
	_, gotErr := k.reply(ctx.WithEventManager(em), example.Contract, wasmvmtypes.Reply{})
	require.NoError(t, gotErr)
	assert.Nil(t, ctx.KVStore(k.storeKey).Get([]byte(`set_in_query`)))
}

func TestBuildContractAddress(t *testing.T) {
	specs := map[string]struct {
		srcCodeID     uint64
		srcInstanceID uint64
		expectedAddr  string
	}{
		"initial contract": {
			srcCodeID:     1,
			srcInstanceID: 1,
			expectedAddr:  "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr",
		},
		"demo value": {
			srcCodeID:     1,
			srcInstanceID: 100,
			expectedAddr:  "cosmos1mujpjkwhut9yjw4xueyugc02evfv46y0dtmnz4lh8xxkkdapym9stu5qm8",
		},
		"both below max": {
			srcCodeID:     math.MaxUint32 - 1,
			srcInstanceID: math.MaxUint32 - 1,
		},
		"both at max": {
			srcCodeID:     math.MaxUint32,
			srcInstanceID: math.MaxUint32,
		},
		"codeID > max u32": {
			srcCodeID:     math.MaxUint32 + 1,
			srcInstanceID: 17,
			expectedAddr:  "cosmos1673hrexz4h6s0ft04l96ygq667djzh2nsr335kstjp49x5dk6rpsf5t0le",
		},
		"instanceID > max u32": {
			srcCodeID:     22,
			srcInstanceID: math.MaxUint32 + 1,
			expectedAddr:  "cosmos10q3pgfvmeyy0veekgtqhxujxkhz0vm9zmalqgc7evrhj68q3l62qrdfg4m",
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			gotAddr := BuildContractAddress(spec.srcCodeID, spec.srcInstanceID)
			require.NotNil(t, gotAddr)
			assert.Nil(t, sdk.VerifyAddressFormat(gotAddr))
			if len(spec.expectedAddr) > 0 {
				require.Equal(t, spec.expectedAddr, gotAddr.String())
			}
		})
	}
}
