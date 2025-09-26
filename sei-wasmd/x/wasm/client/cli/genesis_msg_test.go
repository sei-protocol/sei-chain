package cli

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/testutil"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltest "github.com/cosmos/cosmos-sdk/x/genutil/client/testutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/CosmWasm/wasmd/x/wasm/types"
)

var wasmIdent = []byte("\x00\x61\x73\x6D")

var myWellFundedAccount = keeper.RandomBech32AccountAddress(nil)

const defaultTestKeyName = "my-key-name"

func TestGenesisStoreCodeCmd(t *testing.T) {
	minimalWasmGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	anyValidWasmFile, err := ioutil.TempFile(t.TempDir(), "wasm")
	require.NoError(t, err)
	anyValidWasmFile.Write(wasmIdent)
	require.NoError(t, anyValidWasmFile.Close())

	specs := map[string]struct {
		srcGenesis types.GenesisState
		mutator    func(cmd *cobra.Command)
		expError   bool
	}{
		"all good with actor address": {
			srcGenesis: minimalWasmGenesis,
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{anyValidWasmFile.Name()})
				flagSet := cmd.Flags()
				flagSet.Set("run-as", keeper.RandomBech32AccountAddress(t))
			},
		},
		"all good with key name": {
			srcGenesis: minimalWasmGenesis,
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{anyValidWasmFile.Name()})
				flagSet := cmd.Flags()
				flagSet.Set("run-as", defaultTestKeyName)
			},
		},
		"with unknown actor key name should fail": {
			srcGenesis: minimalWasmGenesis,
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{anyValidWasmFile.Name()})
				flagSet := cmd.Flags()
				flagSet.Set("run-as", "unknown key")
			},
			expError: true,
		},
		"without actor should fail": {
			srcGenesis: minimalWasmGenesis,
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{anyValidWasmFile.Name()})
			},
			expError: true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			homeDir := setupGenesis(t, spec.srcGenesis)

			// when
			cmd := GenesisStoreCodeCmd(homeDir, NewDefaultGenesisIO())
			spec.mutator(cmd)
			err := executeCmdWithContext(t, homeDir, cmd)
			if spec.expError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			// then
			moduleState := loadModuleState(t, homeDir)
			assert.Len(t, moduleState.GenMsgs, 1)
		})
	}
}

func TestInstantiateContractCmd(t *testing.T) {
	minimalWasmGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	anyValidWasmFile, err := ioutil.TempFile(t.TempDir(), "wasm")
	require.NoError(t, err)
	anyValidWasmFile.Write(wasmIdent)
	require.NoError(t, anyValidWasmFile.Close())

	specs := map[string]struct {
		srcGenesis  types.GenesisState
		mutator     func(cmd *cobra.Command)
		expMsgCount int
		expError    bool
	}{
		"all good with code id in genesis codes": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID: 1,
						CodeInfo: types.CodeInfo{
							CodeHash: []byte("a-valid-code-hash"),
							Creator:  keeper.RandomBech32AccountAddress(t),
							InstantiateConfig: types.AccessConfig{
								Permission: types.AccessTypeEverybody,
							},
						},
						CodeBytes: wasmIdent,
					},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{"1", `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("label", "testing")
				flagSet.Set("run-as", myWellFundedAccount)
				flagSet.Set("no-admin", "true")
			},
			expMsgCount: 1,
		},
		"all good with code id from genesis store messages without initial sequence": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				GenMsgs: []types.GenesisState_GenMsgs{
					{Sum: &types.GenesisState_GenMsgs_StoreCode{StoreCode: types.MsgStoreCodeFixture()}},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{"1", `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("label", "testing")
				flagSet.Set("run-as", myWellFundedAccount)
				flagSet.Set("admin", myWellFundedAccount)
			},
			expMsgCount: 2,
		},
		"all good with code id from genesis store messages and sequence set": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				GenMsgs: []types.GenesisState_GenMsgs{
					{Sum: &types.GenesisState_GenMsgs_StoreCode{StoreCode: types.MsgStoreCodeFixture()}},
				},
				Sequences: []types.Sequence{
					{IDKey: types.KeyLastCodeID, Value: 100},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{"100", `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("label", "testing")
				flagSet.Set("run-as", myWellFundedAccount)
				flagSet.Set("no-admin", "true")
			},
			expMsgCount: 2,
		},
		"fails with codeID not existing in codes": {
			srcGenesis: minimalWasmGenesis,
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{"2", `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("label", "testing")
				flagSet.Set("run-as", myWellFundedAccount)
				flagSet.Set("no-admin", "true")
			},
			expError: true,
		},
		"fails when instantiation permissions not granted": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				GenMsgs: []types.GenesisState_GenMsgs{
					{Sum: &types.GenesisState_GenMsgs_StoreCode{StoreCode: types.MsgStoreCodeFixture(func(code *types.MsgStoreCode) {
						code.InstantiatePermission = &types.AllowNobody
					})}},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{"1", `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("label", "testing")
				flagSet.Set("run-as", myWellFundedAccount)
				flagSet.Set("no-admin", "true")
			},
			expError: true,
		},
		"fails if no explicit --no-admin passed": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID: 1,
						CodeInfo: types.CodeInfo{
							CodeHash: []byte("a-valid-code-hash"),
							Creator:  keeper.RandomBech32AccountAddress(t),
							InstantiateConfig: types.AccessConfig{
								Permission: types.AccessTypeEverybody,
							},
						},
						CodeBytes: wasmIdent,
					},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{"1", `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("label", "testing")
				flagSet.Set("run-as", myWellFundedAccount)
			},
			expError: true,
		},
		"fails if both --admin and --no-admin passed": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID: 1,
						CodeInfo: types.CodeInfo{
							CodeHash: []byte("a-valid-code-hash"),
							Creator:  keeper.RandomBech32AccountAddress(t),
							InstantiateConfig: types.AccessConfig{
								Permission: types.AccessTypeEverybody,
							},
						},
						CodeBytes: wasmIdent,
					},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{"1", `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("label", "testing")
				flagSet.Set("run-as", myWellFundedAccount)
				flagSet.Set("no-admin", "true")
				flagSet.Set("admin", myWellFundedAccount)
			},
			expError: true,
		},
		"succeeds with unknown account when no funds": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID: 1,
						CodeInfo: types.CodeInfo{
							CodeHash: []byte("a-valid-code-hash"),
							Creator:  keeper.RandomBech32AccountAddress(t),
							InstantiateConfig: types.AccessConfig{
								Permission: types.AccessTypeEverybody,
							},
						},
						CodeBytes: wasmIdent,
					},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{"1", `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("label", "testing")
				flagSet.Set("run-as", keeper.RandomBech32AccountAddress(t))
				flagSet.Set("no-admin", "true")
			},
			expMsgCount: 1,
		},
		"succeeds with funds from well funded account": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID: 1,
						CodeInfo: types.CodeInfo{
							CodeHash: []byte("a-valid-code-hash"),
							Creator:  keeper.RandomBech32AccountAddress(t),
							InstantiateConfig: types.AccessConfig{
								Permission: types.AccessTypeEverybody,
							},
						},
						CodeBytes: wasmIdent,
					},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{"1", `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("label", "testing")
				flagSet.Set("run-as", myWellFundedAccount)
				flagSet.Set("amount", "100stake")
				flagSet.Set("no-admin", "true")
			},
			expMsgCount: 1,
		},
		"fails without enough sender balance": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID: 1,
						CodeInfo: types.CodeInfo{
							CodeHash: []byte("a-valid-code-hash"),
							Creator:  keeper.RandomBech32AccountAddress(t),
							InstantiateConfig: types.AccessConfig{
								Permission: types.AccessTypeEverybody,
							},
						},
						CodeBytes: wasmIdent,
					},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{"1", `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("label", "testing")
				flagSet.Set("run-as", keeper.RandomBech32AccountAddress(t))
				flagSet.Set("amount", "10stake")
				flagSet.Set("no-admin", "true")
			},
			expError: true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			homeDir := setupGenesis(t, spec.srcGenesis)

			// when
			cmd := GenesisInstantiateContractCmd(homeDir, NewDefaultGenesisIO())
			spec.mutator(cmd)
			err := executeCmdWithContext(t, homeDir, cmd)
			if spec.expError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			// then
			moduleState := loadModuleState(t, homeDir)
			assert.Len(t, moduleState.GenMsgs, spec.expMsgCount)
		})
	}
}

func TestExecuteContractCmd(t *testing.T) {
	const firstContractAddress = "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr"
	minimalWasmGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	anyValidWasmFile, err := ioutil.TempFile(t.TempDir(), "wasm")
	require.NoError(t, err)
	anyValidWasmFile.Write(wasmIdent)
	require.NoError(t, anyValidWasmFile.Close())

	specs := map[string]struct {
		srcGenesis  types.GenesisState
		mutator     func(cmd *cobra.Command)
		expMsgCount int
		expError    bool
	}{
		"all good with contract in genesis contracts": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID:    1,
						CodeInfo:  types.CodeInfoFixture(),
						CodeBytes: wasmIdent,
					},
				},
				Contracts: []types.Contract{
					{
						ContractAddress: firstContractAddress,
						ContractInfo: types.ContractInfoFixture(func(info *types.ContractInfo) {
							info.Created = nil
						}),
						ContractState: []types.Model{},
					},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{firstContractAddress, `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("run-as", myWellFundedAccount)
			},
			expMsgCount: 1,
		},
		"all good with contract from genesis store messages without initial sequence": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID:    1,
						CodeInfo:  types.CodeInfoFixture(),
						CodeBytes: wasmIdent,
					},
				},
				GenMsgs: []types.GenesisState_GenMsgs{
					{Sum: &types.GenesisState_GenMsgs_InstantiateContract{InstantiateContract: types.MsgInstantiateContractFixture()}},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{firstContractAddress, `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("run-as", myWellFundedAccount)
			},
			expMsgCount: 2,
		},
		"all good with contract from genesis store messages and contract sequence set": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID:    1,
						CodeInfo:  types.CodeInfoFixture(),
						CodeBytes: wasmIdent,
					},
				},
				GenMsgs: []types.GenesisState_GenMsgs{
					{Sum: &types.GenesisState_GenMsgs_InstantiateContract{InstantiateContract: types.MsgInstantiateContractFixture()}},
				},
				Sequences: []types.Sequence{
					{IDKey: types.KeyLastInstanceID, Value: 100},
				},
			},
			mutator: func(cmd *cobra.Command) {
				// See TestBuildContractAddress in keeper_test.go
				cmd.SetArgs([]string{"cosmos1mujpjkwhut9yjw4xueyugc02evfv46y0dtmnz4lh8xxkkdapym9stu5qm8", `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("run-as", myWellFundedAccount)
			},
			expMsgCount: 2,
		},
		"fails with unknown contract address": {
			srcGenesis: minimalWasmGenesis,
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{keeper.RandomBech32AccountAddress(t), `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("run-as", myWellFundedAccount)
			},
			expError: true,
		},
		"succeeds with unknown account when no funds": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID:    1,
						CodeInfo:  types.CodeInfoFixture(),
						CodeBytes: wasmIdent,
					},
				},
				Contracts: []types.Contract{
					{
						ContractAddress: firstContractAddress,
						ContractInfo: types.ContractInfoFixture(func(info *types.ContractInfo) {
							info.Created = nil
						}),
						ContractState: []types.Model{},
					},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{firstContractAddress, `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("run-as", keeper.RandomBech32AccountAddress(t))
			},
			expMsgCount: 1,
		},
		"succeeds with funds from well funded account": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID:    1,
						CodeInfo:  types.CodeInfoFixture(),
						CodeBytes: wasmIdent,
					},
				},
				Contracts: []types.Contract{
					{
						ContractAddress: firstContractAddress,
						ContractInfo: types.ContractInfoFixture(func(info *types.ContractInfo) {
							info.Created = nil
						}),
						ContractState: []types.Model{},
					},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{firstContractAddress, `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("run-as", myWellFundedAccount)
				flagSet.Set("amount", "100stake")
			},
			expMsgCount: 1,
		},
		"fails without enough sender balance": {
			srcGenesis: types.GenesisState{
				Params: types.DefaultParams(),
				Codes: []types.Code{
					{
						CodeID:    1,
						CodeInfo:  types.CodeInfoFixture(),
						CodeBytes: wasmIdent,
					},
				},
				Contracts: []types.Contract{
					{
						ContractAddress: firstContractAddress,
						ContractInfo: types.ContractInfoFixture(func(info *types.ContractInfo) {
							info.Created = nil
						}),
						ContractState: []types.Model{},
					},
				},
			},
			mutator: func(cmd *cobra.Command) {
				cmd.SetArgs([]string{firstContractAddress, `{}`})
				flagSet := cmd.Flags()
				flagSet.Set("run-as", keeper.RandomBech32AccountAddress(t))
				flagSet.Set("amount", "10stake")
			},
			expError: true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			homeDir := setupGenesis(t, spec.srcGenesis)
			cmd := GenesisExecuteContractCmd(homeDir, NewDefaultGenesisIO())
			spec.mutator(cmd)

			// when
			err := executeCmdWithContext(t, homeDir, cmd)
			if spec.expError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			// then
			moduleState := loadModuleState(t, homeDir)
			assert.Len(t, moduleState.GenMsgs, spec.expMsgCount)
		})
	}
}

func TestGetAllContracts(t *testing.T) {
	specs := map[string]struct {
		src types.GenesisState
		exp []ContractMeta
	}{
		"read from contracts state": {
			src: types.GenesisState{
				Contracts: []types.Contract{
					{
						ContractAddress: "first-contract",
						ContractInfo:    types.ContractInfo{Label: "first"},
					},
					{
						ContractAddress: "second-contract",
						ContractInfo:    types.ContractInfo{Label: "second"},
					},
				},
			},
			exp: []ContractMeta{
				{
					ContractAddress: "first-contract",
					Info:            types.ContractInfo{Label: "first"},
				},
				{
					ContractAddress: "second-contract",
					Info:            types.ContractInfo{Label: "second"},
				},
			},
		},
		"read from message state": {
			src: types.GenesisState{
				GenMsgs: []types.GenesisState_GenMsgs{
					{Sum: &types.GenesisState_GenMsgs_InstantiateContract{InstantiateContract: &types.MsgInstantiateContract{Label: "first"}}},
					{Sum: &types.GenesisState_GenMsgs_InstantiateContract{InstantiateContract: &types.MsgInstantiateContract{Label: "second"}}},
				},
			},
			exp: []ContractMeta{
				{
					ContractAddress: keeper.BuildContractAddress(0, 1).String(),
					Info:            types.ContractInfo{Label: "first"},
				},
				{
					ContractAddress: keeper.BuildContractAddress(0, 2).String(),
					Info:            types.ContractInfo{Label: "second"},
				},
			},
		},
		"read from message state with contract sequence": {
			src: types.GenesisState{
				Sequences: []types.Sequence{
					{IDKey: types.KeyLastInstanceID, Value: 100},
				},
				GenMsgs: []types.GenesisState_GenMsgs{
					{Sum: &types.GenesisState_GenMsgs_InstantiateContract{InstantiateContract: &types.MsgInstantiateContract{Label: "hundred"}}},
				},
			},
			exp: []ContractMeta{
				{
					ContractAddress: keeper.BuildContractAddress(0, 100).String(),
					Info:            types.ContractInfo{Label: "hundred"},
				},
			},
		},
		"read from contract and message state with contract sequence": {
			src: types.GenesisState{
				Contracts: []types.Contract{
					{
						ContractAddress: "first-contract",
						ContractInfo:    types.ContractInfo{Label: "first"},
					},
				},
				Sequences: []types.Sequence{
					{IDKey: types.KeyLastInstanceID, Value: 100},
				},
				GenMsgs: []types.GenesisState_GenMsgs{
					{Sum: &types.GenesisState_GenMsgs_InstantiateContract{InstantiateContract: &types.MsgInstantiateContract{Label: "hundred"}}},
				},
			},
			exp: []ContractMeta{
				{
					ContractAddress: "first-contract",
					Info:            types.ContractInfo{Label: "first"},
				},
				{
					ContractAddress: keeper.BuildContractAddress(0, 100).String(),
					Info:            types.ContractInfo{Label: "hundred"},
				},
			},
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			got := GetAllContracts(&spec.src)
			assert.Equal(t, spec.exp, got)
		})
	}
}

func setupGenesis(t *testing.T, wasmGenesis types.GenesisState) string {
	appCodec := keeper.MakeEncodingConfig(t).Marshaler
	homeDir := t.TempDir()

	require.NoError(t, os.Mkdir(path.Join(homeDir, "config"), 0o700))
	genFilename := path.Join(homeDir, "config", "genesis.json")
	appState := make(map[string]json.RawMessage)
	appState[types.ModuleName] = appCodec.MustMarshalJSON(&wasmGenesis)

	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
		// add a balance for the default sender account
		Address: myWellFundedAccount,
		Coins:   sdk.NewCoins(sdk.NewCoin("stake", sdk.NewInt(10000000000))),
	})
	appState[banktypes.ModuleName] = appCodec.MustMarshalJSON(bankGenesis)
	appState[stakingtypes.ModuleName] = appCodec.MustMarshalJSON(stakingtypes.DefaultGenesisState())

	appStateBz, err := json.Marshal(appState)
	require.NoError(t, err)
	genDoc := tmtypes.GenesisDoc{
		ChainID:  "testing",
		AppState: appStateBz,
	}
	err = genutil.ExportGenesisFile(&genDoc, genFilename)
	require.NoError(t, err)

	return homeDir
}

func executeCmdWithContext(t *testing.T, homeDir string, cmd *cobra.Command) error {
	logger := log.NewNopLogger()
	cfg, err := genutiltest.CreateDefaultTendermintConfig(homeDir)
	require.NoError(t, err)
	appCodec := keeper.MakeEncodingConfig(t).Marshaler
	serverCtx := server.NewContext(viper.New(), cfg, logger)
	clientCtx := client.Context{}.WithCodec(appCodec).WithHomeDir(homeDir)

	ctx := context.Background()
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)
	ctx = context.WithValue(ctx, server.ServerContextKey, serverCtx)
	flagSet := cmd.Flags()
	flagSet.Set("home", homeDir)
	flagSet.Set(flags.FlagKeyringBackend, keyring.BackendTest)

	mockIn := testutil.ApplyMockIODiscardOutErr(cmd)
	kb, err := keyring.New(sdk.KeyringServiceName(), keyring.BackendTest, homeDir, mockIn)
	require.NoError(t, err)
	_, err = kb.NewAccount(defaultTestKeyName, testdata.TestMnemonic, "", sdk.FullFundraiserPath, hd.Secp256k1)
	require.NoError(t, err)
	return cmd.ExecuteContext(ctx)
}

func loadModuleState(t *testing.T, homeDir string) types.GenesisState {
	genFilename := path.Join(homeDir, "config", "genesis.json")
	appState, _, err := genutiltypes.GenesisStateFromGenFile(genFilename)
	require.NoError(t, err)
	require.Contains(t, appState, types.ModuleName)

	appCodec := keeper.MakeEncodingConfig(t).Marshaler
	var moduleState types.GenesisState
	require.NoError(t, appCodec.UnmarshalJSON(appState[types.ModuleName], &moduleState))
	return moduleState
}
