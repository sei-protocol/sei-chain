package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/triedb/hashdb"
	"github.com/ethereum/go-ethereum/trie/triedb/pathdb"

	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var ethReplayInitialied = false

func (k *Keeper) InitGenesis(ctx sdk.Context, genState types.GenesisState) {
	moduleAcc := authtypes.NewEmptyModuleAccount(types.ModuleName, authtypes.Minter, authtypes.Burner)
	k.accountKeeper.SetModuleAccount(ctx, moduleAcc)

	k.SetParams(ctx, genState.Params)

	seiAddrFc := k.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName) // feeCollector == coinbase
	k.SetAddressMapping(ctx, seiAddrFc, GetCoinbaseAddress())

	for _, addr := range genState.AddressAssociations {
		k.SetAddressMapping(ctx, sdk.MustAccAddressFromBech32(addr.SeiAddress), common.HexToAddress(addr.EthAddress))
	}

	if k.EthReplayConfig.Enabled && !ethReplayInitialied {
		header := k.OpenEthDatabase()
		params := k.GetParams(ctx)
		params.ChainId = sdk.OneInt()
		k.SetParams(ctx, params)
		k.SetReplayInitialHeight(ctx, header.Number.Int64())
		ethReplayInitialied = true
	}
}

func (k *Keeper) OpenEthDatabase() *ethtypes.Header {
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
	sdb := state.NewDatabaseWithNodeDB(db, triedb)
	tr, err := sdb.OpenTrie(header.Root)
	if err != nil {
		panic(err)
	}
	k.Root = header.Root
	k.DB = sdb
	k.Trie = tr
	return header
}
