package benchmarks

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"

	seiapp "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

func setup(t *testing.T, db dbm.DB, withGenesis bool, invCheckPeriod uint, opts ...wasm.Option) (*seiapp.App, seiapp.GenesisState) {
	wasmApp := seiapp.Setup(t, false, false, false)
	if withGenesis {
		return wasmApp, seiapp.NewDefaultGenesisState(seiapp.MakeEncodingConfig().Marshaler)
	}
	return wasmApp, seiapp.GenesisState{}
}

// SetupWithGenesisAccounts initializes a new WasmApp with the provided genesis
// accounts and possible balances.
func SetupWithGenesisAccounts(b testing.TB, db dbm.DB, genAccs []authtypes.GenesisAccount, balances ...banktypes.Balance) *seiapp.App {
	wasmApp, genesisState := setup(b.(*testing.T), db, true, 0)
	authGenesis := authtypes.NewGenesisState(authtypes.DefaultParams(), genAccs)
	appCodec := seiapp.MakeEncodingConfig().Marshaler

	genesisState[authtypes.ModuleName] = appCodec.MustMarshalJSON(authGenesis)

	totalSupply := sdk.NewCoins()
	for _, b := range balances {
		totalSupply = totalSupply.Add(b.Coins...)
	}

	bankGenesis := banktypes.NewGenesisState(banktypes.DefaultGenesisState().Params, balances, totalSupply, []banktypes.Metadata{}, banktypes.DefaultGenesisState().WeiBalances)
	genesisState[banktypes.ModuleName] = appCodec.MustMarshalJSON(bankGenesis)

	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	if err != nil {
		panic(err)
	}

	wasmApp.InitChain(
		context.Background(),
		&abci.RequestInitChain{
			Validators:      []abci.ValidatorUpdate{},
			ConsensusParams: seiapp.DefaultConsensusParams,
			AppStateBytes:   stateBytes,
		},
	)

	wasmApp.SetDeliverStateToCommit()
	wasmApp.Commit(context.Background())
	legacyabci.BeginBlock(wasmApp.GetContextForDeliverTx([]byte{}), wasmApp.LastBlockHeight()+1, []abci.VoteInfo{}, []abci.Misbehavior{}, wasmApp.BeginBlockKeepers)

	return wasmApp
}

type AppInfo struct {
	App          *seiapp.App
	MinterKey    *secp256k1.PrivKey
	MinterAddr   sdk.AccAddress
	ContractAddr string
	Denom        string
	AccNum       uint64
	SeqNum       uint64
	TxConfig     client.TxConfig
}

func InitializeWasmApp(b testing.TB, db dbm.DB, numAccounts int) AppInfo {
	// constants
	minter := secp256k1.GenPrivKey()
	addr := sdk.AccAddress(minter.PubKey().Address())
	denom := "uatom"

	// genesis setup (with a bunch of random accounts)
	genAccs := make([]authtypes.GenesisAccount, numAccounts+1)
	bals := make([]banktypes.Balance, numAccounts+1)
	genAccs[0] = &authtypes.BaseAccount{
		Address: addr.String(),
	}
	bals[0] = banktypes.Balance{
		Address: addr.String(),
		Coins:   sdk.NewCoins(sdk.NewInt64Coin(denom, 100000000000)),
	}
	for i := 0; i <= numAccounts; i++ {
		acct := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
		if i == 0 {
			acct = addr.String()
		}
		genAccs[i] = &authtypes.BaseAccount{
			Address: acct,
		}
		bals[i] = banktypes.Balance{
			Address: acct,
			Coins:   sdk.NewCoins(sdk.NewInt64Coin(denom, 100000000000)),
		}
	}
	wasmApp := SetupWithGenesisAccounts(b, db, genAccs, bals...)

	// add wasm contract
	height := int64(2)
	txGen := seiapp.MakeEncodingConfig().TxConfig
	legacyabci.BeginBlock(wasmApp.GetContextForDeliverTx([]byte{}), height, []abci.VoteInfo{}, []abci.Misbehavior{}, wasmApp.BeginBlockKeepers)

	// upload the code
	cw20Code, err := os.ReadFile("./testdata/cw20_base.wasm")
	require.NoError(b, err)
	storeMsg := wasmtypes.MsgStoreCode{
		Sender:       addr.String(),
		WASMByteCode: cw20Code,
	}
	storeTx, err := seiapp.GenTx(txGen, []sdk.Msg{&storeMsg}, nil, 55123123, "", []uint64{0}, []uint64{0}, minter)
	require.NoError(b, err)
	_, res, err := wasmApp.Deliver(txGen.TxEncoder(), storeTx)
	require.NoError(b, err)
	codeID := uint64(1)

	// instantiate the contract
	initialBalances := make([]balance, numAccounts+1)
	for i := 0; i <= numAccounts; i++ {
		acct := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
		if i == 0 {
			acct = addr.String()
		}
		initialBalances[i] = balance{
			Address: acct,
			Amount:  1000000000,
		}
	}
	init := cw20InitMsg{
		Name:            "Cash Money",
		Symbol:          "CASH",
		Decimals:        2,
		InitialBalances: initialBalances,
	}
	initBz, err := json.Marshal(init)
	require.NoError(b, err)
	initMsg := wasmtypes.MsgInstantiateContract{
		Sender: addr.String(),
		Admin:  addr.String(),
		CodeID: codeID,
		Label:  "Demo contract",
		Msg:    initBz,
	}
	gasWanted := 500000 + 10000*uint64(numAccounts)
	initTx, err := seiapp.GenTx(txGen, []sdk.Msg{&initMsg}, nil, gasWanted, "", []uint64{0}, []uint64{1}, minter)
	require.NoError(b, err)
	_, res, err = wasmApp.Deliver(txGen.TxEncoder(), initTx)
	require.NoError(b, err)

	// TODO: parse contract address better
	evt := res.Events[len(res.Events)-1]
	attr := evt.Attributes[0]
	contractAddr := string(attr.Value)

	wasmApp.EndBlock(wasmApp.GetContextForDeliverTx([]byte{}), height, 0)
	wasmApp.SetDeliverStateToCommit()
	wasmApp.Commit(context.Background())

	return AppInfo{
		App:          wasmApp,
		MinterKey:    minter,
		MinterAddr:   addr,
		ContractAddr: contractAddr,
		Denom:        denom,
		AccNum:       0,
		SeqNum:       2,
		TxConfig:     seiapp.MakeEncodingConfig().TxConfig,
	}
}

func GenSequenceOfTxs(b testing.TB, info *AppInfo, msgGen func(*AppInfo) ([]sdk.Msg, error), numToGenerate int) []sdk.Tx {
	fees := sdk.Coins{sdk.NewInt64Coin(info.Denom, 0)}
	txs := make([]sdk.Tx, numToGenerate)

	for i := 0; i < numToGenerate; i++ {
		msgs, err := msgGen(info)
		require.NoError(b, err)
		txs[i], err = seiapp.GenTx(
			info.TxConfig,
			msgs,
			fees,
			1234567,
			"",
			[]uint64{info.AccNum},
			[]uint64{info.SeqNum},
			info.MinterKey,
		)
		require.NoError(b, err)
		info.SeqNum += 1
	}

	return txs
}
