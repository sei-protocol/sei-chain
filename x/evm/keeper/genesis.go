package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/triedb/hashdb"
	"github.com/ethereum/go-ethereum/trie/triedb/pathdb"

	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var initialied = false

func (k *Keeper) InitGenesis(ctx sdk.Context, genState types.GenesisState) {
	moduleAcc := authtypes.NewEmptyModuleAccount(types.ModuleName, authtypes.Minter, authtypes.Burner)
	k.accountKeeper.SetModuleAccount(ctx, moduleAcc)

	k.SetParams(ctx, types.DefaultParams())

	seiAddrFc := k.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName) // feeCollector == coinbase
	k.SetAddressMapping(ctx, seiAddrFc, GetCoinbaseAddress())

	for _, addr := range genState.AddressAssociations {
		k.SetAddressMapping(ctx, sdk.MustAccAddressFromBech32(addr.SeiAddress), common.HexToAddress(addr.EthAddress))
	}

	if k.EthReplayConfig.Enabled && !initialied {
		root, db, trie := k.openEthDatabase()
		k.Root = root
		k.DB = db
		k.Trie = trie
		params := k.GetParams(ctx)
		params.ChainId = sdk.OneInt()
		k.SetParams(ctx, params)
		initialied = true
	}
}

func (k *Keeper) openEthDatabase() (common.Hash, state.Database, state.Trie) {
	db, err := rawdb.Open(rawdb.OpenOptions{
		Type:              "pebble",
		Directory:         k.EthReplayConfig.EthDataDir,
		AncientsDirectory: fmt.Sprintf("%s/ancient", k.EthReplayConfig.EthDataDir),
		Namespace:         "",
		Cache:             256,
		Handles:           256,
		ReadOnly:          true,
	})
	if err != nil {
		panic(err)
	}
	config := &trie.Config{
		Preimages: true,
		IsVerkle:  false,
	}
	scheme, err := rawdb.ParseStateScheme(rawdb.ReadStateScheme(db), db)
	if err != nil {
		panic(err)
	}
	var triedb *trie.Database
	if scheme == rawdb.HashScheme {
		config.HashDB = hashdb.Defaults
		triedb = trie.NewDatabase(db, config)
	} else {
		config.PathDB = pathdb.ReadOnly
		triedb = trie.NewDatabase(db, config)
	}
	header := rawdb.ReadHeadHeader(db)
	fmt.Printf("Root height is %s\n", header.Number)
	sdb := state.NewDatabaseWithNodeDB(db, triedb)
	tr, err := sdb.OpenTrie(header.Root)
	if err != nil {
		panic(err)
	}
	return header.Root, sdb, tr
}
