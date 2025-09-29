package keeper

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/store"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	distributionkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proto/tendermint/crypto"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	"github.com/CosmWasm/wasmd/x/wasm/types"
	wasmTypes "github.com/CosmWasm/wasmd/x/wasm/types"
)

const firstCodeID = 1

func TestGenesisExportImport(t *testing.T) {
	wasmKeeper, srcCtx, srcStoreKeys := setupKeeper(t)
	contractKeeper := NewGovPermissionKeeper(wasmKeeper)

	wasmCode, err := ioutil.ReadFile("./testdata/hackatom.wasm")
	require.NoError(t, err)

	// store some test data
	f := fuzz.New().Funcs(ModelFuzzers...)

	wasmKeeper.SetParams(srcCtx, types.DefaultParams())

	for i := 0; i < 25; i++ {
		var (
			codeInfo          types.CodeInfo
			contract          types.ContractInfo
			stateModels       []types.Model
			history           []types.ContractCodeHistoryEntry
			pinned            bool
			contractExtension bool
		)
		f.Fuzz(&codeInfo)
		f.Fuzz(&contract)
		f.Fuzz(&stateModels)
		f.NilChance(0).Fuzz(&history)
		f.Fuzz(&pinned)
		f.Fuzz(&contractExtension)

		creatorAddr, err := sdk.AccAddressFromBech32(codeInfo.Creator)
		require.NoError(t, err)
		codeID, err := contractKeeper.Create(srcCtx, creatorAddr, wasmCode, &codeInfo.InstantiateConfig)
		require.NoError(t, err)
		if pinned {
			contractKeeper.PinCode(srcCtx, codeID)
		}
		if contractExtension {
			anyTime := time.Now().UTC()
			var nestedType govtypes.TextProposal
			f.NilChance(0).Fuzz(&nestedType)
			myExtension, err := govtypes.NewProposal(&nestedType, 1, anyTime, anyTime, false)
			require.NoError(t, err)
			contract.SetExtension(&myExtension)
		}

		contract.CodeID = codeID
		contractAddr := wasmKeeper.generateContractAddress(srcCtx, codeID)
		wasmKeeper.storeContractInfo(srcCtx, contractAddr, &contract)
		wasmKeeper.appendToContractHistory(srcCtx, contractAddr, history...)
		wasmKeeper.importContractState(srcCtx, contractAddr, stateModels)
	}
	var wasmParams types.Params
	f.NilChance(0).Fuzz(&wasmParams)
	wasmKeeper.SetParams(srcCtx, wasmParams)

	// export
	exportedState := ExportGenesis(srcCtx, wasmKeeper)
	// order should not matter
	rand.Shuffle(len(exportedState.Codes), func(i, j int) {
		exportedState.Codes[i], exportedState.Codes[j] = exportedState.Codes[j], exportedState.Codes[i]
	})
	rand.Shuffle(len(exportedState.Contracts), func(i, j int) {
		exportedState.Contracts[i], exportedState.Contracts[j] = exportedState.Contracts[j], exportedState.Contracts[i]
	})
	rand.Shuffle(len(exportedState.Sequences), func(i, j int) {
		exportedState.Sequences[i], exportedState.Sequences[j] = exportedState.Sequences[j], exportedState.Sequences[i]
	})
	exportedGenesis, err := wasmKeeper.cdc.MarshalJSON(exportedState)
	require.NoError(t, err)

	// setup new instances
	dstKeeper, dstCtx, dstStoreKeys := setupKeeper(t)

	// reset contract code index in source DB for comparison with dest DB
	wasmKeeper.IterateContractInfo(srcCtx, func(address sdk.AccAddress, info wasmTypes.ContractInfo) bool {
		wasmKeeper.removeFromContractCodeSecondaryIndex(srcCtx, address, wasmKeeper.getLastContractHistoryEntry(srcCtx, address))
		prefixStore := prefix.NewStore(srcCtx.KVStore(wasmKeeper.storeKey), types.GetContractCodeHistoryElementPrefix(address))
		iter := prefixStore.Iterator(nil, nil)

		for ; iter.Valid(); iter.Next() {
			prefixStore.Delete(iter.Key())
		}
		x := &info
		newHistory := x.ResetFromGenesis(dstCtx)
		wasmKeeper.storeContractInfo(srcCtx, address, x)
		wasmKeeper.addToContractCodeSecondaryIndex(srcCtx, address, newHistory)
		wasmKeeper.appendToContractHistory(srcCtx, address, newHistory)
		iter.Close()
		return false
	})

	// re-import
	var importState wasmTypes.GenesisState
	err = dstKeeper.cdc.UnmarshalJSON(exportedGenesis, &importState)
	require.NoError(t, err)
	InitGenesis(dstCtx, dstKeeper, importState, &StakingKeeperMock{}, TestHandler(contractKeeper))

	// compare whole DB
	for j := range srcStoreKeys {
		srcIT := srcCtx.KVStore(srcStoreKeys[j]).Iterator(nil, nil)
		dstIT := dstCtx.KVStore(dstStoreKeys[j]).Iterator(nil, nil)

		for i := 0; srcIT.Valid(); i++ {
			require.True(t, dstIT.Valid(), "[%s] destination DB has less elements than source. Missing: %x", srcStoreKeys[j].Name(), srcIT.Key())
			require.Equal(t, srcIT.Key(), dstIT.Key(), i)
			require.Equal(t, srcIT.Value(), dstIT.Value(), "[%s] element (%d): %X", srcStoreKeys[j].Name(), i, srcIT.Key())
			dstIT.Next()
			srcIT.Next()
		}
		if !assert.False(t, dstIT.Valid()) {
			t.Fatalf("dest Iterator still has key :%X", dstIT.Key())
		}
		srcIT.Close()
		dstIT.Close()
	}
}

func TestGenesisInit(t *testing.T) {
	wasmCode, err := ioutil.ReadFile("./testdata/hackatom.wasm")
	require.NoError(t, err)

	myCodeInfo := wasmTypes.CodeInfoFixture(wasmTypes.WithSHA256CodeHash(wasmCode))
	specs := map[string]struct {
		src            types.GenesisState
		stakingMock    StakingKeeperMock
		msgHandlerMock MockMsgHandler
		expSuccess     bool
	}{
		"happy path: code info correct": {
			src: types.GenesisState{
				Codes: []types.Code{{
					CodeID:    firstCodeID,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				}},
				Sequences: []types.Sequence{
					{IDKey: types.KeyLastCodeID, Value: 2},
					{IDKey: types.KeyLastInstanceID, Value: 1},
				},
				Params: types.DefaultParams(),
			},
			expSuccess: true,
		},
		"happy path: code ids can contain gaps": {
			src: types.GenesisState{
				Codes: []types.Code{{
					CodeID:    firstCodeID,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				}, {
					CodeID:    3,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				}},
				Sequences: []types.Sequence{
					{IDKey: types.KeyLastCodeID, Value: 10},
					{IDKey: types.KeyLastInstanceID, Value: 1},
				},
				Params: types.DefaultParams(),
			},
			expSuccess: true,
		},
		"happy path: code order does not matter": {
			src: types.GenesisState{
				Codes: []types.Code{{
					CodeID:    2,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				}, {
					CodeID:    firstCodeID,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				}},
				Contracts: nil,
				Sequences: []types.Sequence{
					{IDKey: types.KeyLastCodeID, Value: 3},
					{IDKey: types.KeyLastInstanceID, Value: 1},
				},
				Params: types.DefaultParams(),
			},
			expSuccess: true,
		},
		"prevent code hash mismatch": {src: types.GenesisState{
			Codes: []types.Code{{
				CodeID:    firstCodeID,
				CodeInfo:  wasmTypes.CodeInfoFixture(func(i *wasmTypes.CodeInfo) { i.CodeHash = make([]byte, sha256.Size) }),
				CodeBytes: wasmCode,
			}},
			Params: types.DefaultParams(),
		}},
		"prevent duplicate codeIDs": {src: types.GenesisState{
			Codes: []types.Code{
				{
					CodeID:    firstCodeID,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				},
				{
					CodeID:    firstCodeID,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				},
			},
			Params: types.DefaultParams(),
		}},
		"codes with same checksum can be pinned": {
			src: types.GenesisState{
				Codes: []types.Code{
					{
						CodeID:    firstCodeID,
						CodeInfo:  myCodeInfo,
						CodeBytes: wasmCode,
						Pinned:    true,
					},
					{
						CodeID:    2,
						CodeInfo:  myCodeInfo,
						CodeBytes: wasmCode,
						Pinned:    true,
					},
				},
				Params: types.DefaultParams(),
			},
		},
		"happy path: code id in info and contract do match": {
			src: types.GenesisState{
				Codes: []types.Code{{
					CodeID:    firstCodeID,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				}},
				Contracts: []types.Contract{
					{
						ContractAddress: BuildContractAddress(1, 1).String(),
						ContractInfo:    types.ContractInfoFixture(func(c *wasmTypes.ContractInfo) { c.CodeID = 1 }, types.OnlyGenesisFields),
					},
				},
				Sequences: []types.Sequence{
					{IDKey: types.KeyLastCodeID, Value: 2},
					{IDKey: types.KeyLastInstanceID, Value: 2},
				},
				Params: types.DefaultParams(),
			},
			expSuccess: true,
		},
		"happy path: code info with two contracts": {
			src: types.GenesisState{
				Codes: []types.Code{{
					CodeID:    firstCodeID,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				}},
				Contracts: []types.Contract{
					{
						ContractAddress: BuildContractAddress(1, 1).String(),
						ContractInfo:    types.ContractInfoFixture(func(c *wasmTypes.ContractInfo) { c.CodeID = 1 }, types.OnlyGenesisFields),
					}, {
						ContractAddress: BuildContractAddress(1, 2).String(),
						ContractInfo:    types.ContractInfoFixture(func(c *wasmTypes.ContractInfo) { c.CodeID = 1 }, types.OnlyGenesisFields),
					},
				},
				Sequences: []types.Sequence{
					{IDKey: types.KeyLastCodeID, Value: 2},
					{IDKey: types.KeyLastInstanceID, Value: 3},
				},
				Params: types.DefaultParams(),
			},
			expSuccess: true,
		},
		"prevent contracts that points to non existing codeID": {
			src: types.GenesisState{
				Contracts: []types.Contract{
					{
						ContractAddress: BuildContractAddress(1, 1).String(),
						ContractInfo:    types.ContractInfoFixture(func(c *wasmTypes.ContractInfo) { c.CodeID = 1 }, types.OnlyGenesisFields),
					},
				},
				Params: types.DefaultParams(),
			},
		},
		"prevent duplicate contract address": {
			src: types.GenesisState{
				Codes: []types.Code{{
					CodeID:    firstCodeID,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				}},
				Contracts: []types.Contract{
					{
						ContractAddress: BuildContractAddress(1, 1).String(),
						ContractInfo:    types.ContractInfoFixture(func(c *wasmTypes.ContractInfo) { c.CodeID = 1 }, types.OnlyGenesisFields),
					}, {
						ContractAddress: BuildContractAddress(1, 1).String(),
						ContractInfo:    types.ContractInfoFixture(func(c *wasmTypes.ContractInfo) { c.CodeID = 1 }, types.OnlyGenesisFields),
					},
				},
				Params: types.DefaultParams(),
			},
		},
		"prevent duplicate contract model keys": {
			src: types.GenesisState{
				Codes: []types.Code{{
					CodeID:    firstCodeID,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				}},
				Contracts: []types.Contract{
					{
						ContractAddress: BuildContractAddress(1, 1).String(),
						ContractInfo:    types.ContractInfoFixture(func(c *wasmTypes.ContractInfo) { c.CodeID = 1 }, types.OnlyGenesisFields),
						ContractState: []types.Model{
							{
								Key:   []byte{0x1},
								Value: []byte("foo"),
							},
							{
								Key:   []byte{0x1},
								Value: []byte("bar"),
							},
						},
					},
				},
				Params: types.DefaultParams(),
			},
		},
		"prevent duplicate sequences": {
			src: types.GenesisState{
				Sequences: []types.Sequence{
					{IDKey: []byte("foo"), Value: 1},
					{IDKey: []byte("foo"), Value: 9999},
				},
				Params: types.DefaultParams(),
			},
		},
		"prevent code id seq init value == max codeID used": {
			src: types.GenesisState{
				Codes: []types.Code{{
					CodeID:    2,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				}},
				Sequences: []types.Sequence{
					{IDKey: types.KeyLastCodeID, Value: 1},
				},
				Params: types.DefaultParams(),
			},
		},
		"prevent contract id seq init value == count contracts": {
			src: types.GenesisState{
				Codes: []types.Code{{
					CodeID:    firstCodeID,
					CodeInfo:  myCodeInfo,
					CodeBytes: wasmCode,
				}},
				Contracts: []types.Contract{
					{
						ContractAddress: BuildContractAddress(1, 1).String(),
						ContractInfo:    types.ContractInfoFixture(func(c *wasmTypes.ContractInfo) { c.CodeID = 1 }, types.OnlyGenesisFields),
					},
				},
				Sequences: []types.Sequence{
					{IDKey: types.KeyLastCodeID, Value: 2},
					{IDKey: types.KeyLastInstanceID, Value: 1},
				},
				Params: types.DefaultParams(),
			},
		},
		"validator set update called for any genesis messages": {
			src: wasmTypes.GenesisState{
				GenMsgs: []types.GenesisState_GenMsgs{
					{Sum: &types.GenesisState_GenMsgs_StoreCode{
						StoreCode: types.MsgStoreCodeFixture(),
					}},
				},
				Params: types.DefaultParams(),
			},
			stakingMock: StakingKeeperMock{expCalls: 1, validatorUpdate: []abci.ValidatorUpdate{
				{
					PubKey: crypto.PublicKey{Sum: &crypto.PublicKey_Ed25519{
						Ed25519: []byte("a valid key"),
					}},
					Power: 100,
				},
			}},
			msgHandlerMock: MockMsgHandler{expCalls: 1, expMsg: types.MsgStoreCodeFixture()},
			expSuccess:     true,
		},
		"validator set update not called on genesis msg handler errors": {
			src: wasmTypes.GenesisState{
				GenMsgs: []types.GenesisState_GenMsgs{
					{Sum: &types.GenesisState_GenMsgs_StoreCode{
						StoreCode: types.MsgStoreCodeFixture(),
					}},
				},
				Params: types.DefaultParams(),
			},
			msgHandlerMock: MockMsgHandler{expCalls: 1, err: errors.New("test error response")},
			stakingMock:    StakingKeeperMock{expCalls: 0},
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			keeper, ctx, _ := setupKeeper(t)

			require.NoError(t, types.ValidateGenesis(spec.src))
			gotValidatorSet, gotErr := InitGenesis(ctx, keeper, spec.src, &spec.stakingMock, spec.msgHandlerMock.Handle)
			if !spec.expSuccess {
				require.Error(t, gotErr)
				return
			}
			require.NoError(t, gotErr)
			spec.msgHandlerMock.verifyCalls(t)
			spec.stakingMock.verifyCalls(t)
			assert.Equal(t, spec.stakingMock.validatorUpdate, gotValidatorSet)
			for _, c := range spec.src.Codes {
				assert.Equal(t, c.Pinned, keeper.IsPinnedCode(ctx, c.CodeID))
			}
		})
	}
}

func TestImportContractWithCodeHistoryReset(t *testing.T) {
	genesisTemplate := `
{
	"params":{
		"code_upload_access": {
			"permission": "Everybody"
		},
		"instantiate_default_permission": "Everybody"
	},
  "codes": [
    {
      "code_id": "1",
      "code_info": {
        "code_hash": %q,
        "creator": "cosmos1qtu5n0cnhfkjj6l2rq97hmky9fd89gwca9yarx",
        "instantiate_config": {
          "permission": "OnlyAddress",
          "address": "cosmos1qtu5n0cnhfkjj6l2rq97hmky9fd89gwca9yarx"
        }
      },
      "code_bytes": %q
    }
  ],
  "contracts": [
    {
      "contract_address": "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr",
      "contract_info": {
        "code_id": "1",
        "creator": "cosmos13x849jzd03vne42ynpj25hn8npjecxqrjghd8x",
        "admin": "cosmos1h5t8zxmjr30e9dqghtlpl40f2zz5cgey6esxtn",
        "label": "ȀĴnZV芢毤"
      }
    }
  ],
  "sequences": [
  {"id_key": "BGxhc3RDb2RlSWQ=", "value": "2"},
  {"id_key": "BGxhc3RDb250cmFjdElk", "value": "3"}
  ]
}`
	keeper, ctx, _ := setupKeeper(t)
	contractKeeper := NewGovPermissionKeeper(keeper)

	wasmCode, err := ioutil.ReadFile("./testdata/hackatom.wasm")
	require.NoError(t, err)

	wasmCodeHash := sha256.Sum256(wasmCode)
	enc64 := base64.StdEncoding.EncodeToString
	genesisStr := fmt.Sprintf(genesisTemplate, enc64(wasmCodeHash[:]), enc64(wasmCode))

	var importState wasmTypes.GenesisState
	err = keeper.cdc.UnmarshalJSON([]byte(genesisStr), &importState)
	require.NoError(t, err)
	require.NoError(t, importState.ValidateBasic(), genesisStr)

	ctx = ctx.WithBlockHeight(0).WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))

	// when
	_, err = InitGenesis(ctx, keeper, importState, &StakingKeeperMock{}, TestHandler(contractKeeper))
	require.NoError(t, err)

	// verify wasm code
	gotWasmCode, err := keeper.GetByteCode(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, wasmCode, gotWasmCode, "byte code does not match")

	// verify code info
	gotCodeInfo := keeper.GetCodeInfo(ctx, 1)
	require.NotNil(t, gotCodeInfo)
	codeCreatorAddr := "cosmos1qtu5n0cnhfkjj6l2rq97hmky9fd89gwca9yarx"
	expCodeInfo := types.CodeInfo{
		CodeHash: wasmCodeHash[:],
		Creator:  codeCreatorAddr,
		InstantiateConfig: wasmTypes.AccessConfig{
			Permission: types.AccessTypeOnlyAddress,
			Address:    codeCreatorAddr,
		},
	}
	assert.Equal(t, expCodeInfo, *gotCodeInfo)

	// verify contract
	contractAddr, _ := sdk.AccAddressFromBech32("cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr")
	gotContractInfo := keeper.GetContractInfo(ctx, contractAddr)
	require.NotNil(t, gotContractInfo)
	contractCreatorAddr := "cosmos13x849jzd03vne42ynpj25hn8npjecxqrjghd8x"
	adminAddr := "cosmos1h5t8zxmjr30e9dqghtlpl40f2zz5cgey6esxtn"

	expContractInfo := types.ContractInfo{
		CodeID:  firstCodeID,
		Creator: contractCreatorAddr,
		Admin:   adminAddr,
		Label:   "ȀĴnZV芢毤",
		Created: &types.AbsoluteTxPosition{BlockHeight: 0, TxIndex: 0},
	}
	assert.Equal(t, expContractInfo, *gotContractInfo)

	expHistory := []types.ContractCodeHistoryEntry{
		{
			Operation: types.ContractCodeHistoryOperationTypeGenesis,
			CodeID:    firstCodeID,
			Updated:   types.NewAbsoluteTxPosition(ctx),
		},
	}
	assert.Equal(t, expHistory, keeper.GetContractHistory(ctx, contractAddr))
	assert.Equal(t, uint64(2), keeper.PeekAutoIncrementID(ctx, types.KeyLastCodeID))
	assert.Equal(t, uint64(3), keeper.PeekAutoIncrementID(ctx, types.KeyLastInstanceID))
}

func TestSupportedGenMsgTypes(t *testing.T) {
	wasmCode, err := ioutil.ReadFile("./testdata/hackatom.wasm")
	require.NoError(t, err)
	var (
		myAddress          sdk.AccAddress = bytes.Repeat([]byte{1}, types.ContractAddrLen)
		verifierAddress    sdk.AccAddress = bytes.Repeat([]byte{2}, types.ContractAddrLen)
		beneficiaryAddress sdk.AccAddress = bytes.Repeat([]byte{3}, types.ContractAddrLen)
	)
	const denom = "stake"
	importState := types.GenesisState{
		Params: types.DefaultParams(),
		GenMsgs: []types.GenesisState_GenMsgs{
			{
				Sum: &types.GenesisState_GenMsgs_StoreCode{
					StoreCode: &types.MsgStoreCode{
						Sender:       myAddress.String(),
						WASMByteCode: wasmCode,
					},
				},
			},
			{
				Sum: &types.GenesisState_GenMsgs_InstantiateContract{
					InstantiateContract: &types.MsgInstantiateContract{
						Sender: myAddress.String(),
						CodeID: 1,
						Label:  "testing",
						Msg: HackatomExampleInitMsg{
							Verifier:    verifierAddress,
							Beneficiary: beneficiaryAddress,
						}.GetBytes(t),
						Funds: sdk.NewCoins(sdk.NewCoin(denom, sdk.NewInt(10))),
					},
				},
			},
			{
				Sum: &types.GenesisState_GenMsgs_ExecuteContract{
					ExecuteContract: &types.MsgExecuteContract{
						Sender:   verifierAddress.String(),
						Contract: BuildContractAddress(1, 1).String(),
						Msg:      []byte(`{"release":{}}`),
					},
				},
			},
		},
	}
	require.NoError(t, importState.ValidateBasic())
	ctx, keepers := CreateDefaultTestInput(t)
	keeper := keepers.WasmKeeper
	ctx = ctx.WithBlockHeight(0).WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	keepers.Faucet.Fund(ctx, myAddress, sdk.NewCoin(denom, sdk.NewInt(100)))

	// when
	_, err = InitGenesis(ctx, keeper, importState, &StakingKeeperMock{}, TestHandler(keepers.ContractKeeper))
	require.NoError(t, err)

	// verify code stored
	gotWasmCode, err := keeper.GetByteCode(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, wasmCode, gotWasmCode)
	codeInfo := keeper.GetCodeInfo(ctx, 1)
	require.NotNil(t, codeInfo)

	// verify contract instantiated
	cInfo := keeper.GetContractInfo(ctx, BuildContractAddress(1, 1))
	require.NotNil(t, cInfo)

	// verify contract executed
	gotBalance := keepers.BankKeeper.GetBalance(ctx, beneficiaryAddress, denom)
	assert.Equal(t, sdk.NewCoin(denom, sdk.NewInt(10)), gotBalance)
}

func setupKeeper(t *testing.T) (*Keeper, sdk.Context, []sdk.StoreKey) {
	t.Helper()
	tempDir, err := ioutil.TempDir("", "wasm")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })
	var (
		keyParams  = sdk.NewKVStoreKey(paramtypes.StoreKey)
		tkeyParams = sdk.NewTransientStoreKey(paramtypes.TStoreKey)
		keyWasm    = sdk.NewKVStoreKey(wasmTypes.StoreKey)
	)

	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db)
	ms.MountStoreWithDB(keyWasm, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyParams, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(tkeyParams, sdk.StoreTypeTransient, db)
	require.NoError(t, ms.LoadLatestVersion())

	ctx := sdk.NewContext(ms, tmproto.Header{
		Height: 1234567,
		Time:   time.Date(2020, time.April, 22, 12, 0, 0, 0, time.UTC),
	}, false, log.NewNopLogger())

	encodingConfig := MakeEncodingConfig(t)
	// register an example extension. must be protobuf
	encodingConfig.InterfaceRegistry.RegisterImplementations(
		(*types.ContractInfoExtension)(nil),
		&govtypes.Proposal{},
	)
	// also registering gov interfaces for nested Any type
	govtypes.RegisterInterfaces(encodingConfig.InterfaceRegistry)

	wasmConfig := wasmTypes.DefaultWasmConfig()
	pk := paramskeeper.NewKeeper(encodingConfig.Marshaler, encodingConfig.Amino, keyParams, tkeyParams)

	srcKeeper := NewKeeper(encodingConfig.Marshaler, keyWasm, paramskeeper.Keeper{}, pk.Subspace(wasmTypes.ModuleName), authkeeper.AccountKeeper{}, nil, stakingkeeper.Keeper{}, distributionkeeper.Keeper{}, nil, nil, nil, upgradekeeper.Keeper{}, nil, nil, nil, tempDir, wasmConfig, SupportedFeatures)
	return &srcKeeper, ctx, []sdk.StoreKey{keyWasm, keyParams}
}

type StakingKeeperMock struct {
	err             error
	validatorUpdate []abci.ValidatorUpdate
	expCalls        int
	gotCalls        int
}

func (s *StakingKeeperMock) ApplyAndReturnValidatorSetUpdates(_ sdk.Context) ([]abci.ValidatorUpdate, error) {
	s.gotCalls++
	return s.validatorUpdate, s.err
}

func (s *StakingKeeperMock) verifyCalls(t *testing.T) {
	assert.Equal(t, s.expCalls, s.gotCalls, "number calls")
}

type MockMsgHandler struct {
	result   *sdk.Result
	err      error
	expCalls int
	gotCalls int
	expMsg   sdk.Msg
	gotMsg   sdk.Msg
}

func (m *MockMsgHandler) Handle(ctx sdk.Context, msg sdk.Msg) (*sdk.Result, error) {
	m.gotCalls++
	m.gotMsg = msg
	return m.result, m.err
}

func (m *MockMsgHandler) verifyCalls(t *testing.T) {
	assert.Equal(t, m.expMsg, m.gotMsg, "message param")
	assert.Equal(t, m.expCalls, m.gotCalls, "number calls")
}
