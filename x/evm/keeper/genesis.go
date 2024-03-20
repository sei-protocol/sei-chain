package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	ethtests "github.com/ethereum/go-ethereum/tests"
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

	if k.BlockTest != nil {
		fmt.Println("In k.BlockTest != nil")
		header, db, trie := k.openEthDatabase2() // TODO: replace with your own openEthDatabase function
		k.Root = header.Root
		k.DB = db
		k.Trie = trie
		params := k.GetParams(ctx)
		params.ChainId = sdk.OneInt()
		k.SetParams(ctx, params)
		if k.EthReplayConfig.EthDataEarliestBlock == 0 {
			k.EthReplayConfig.EthDataEarliestBlock = uint64(header.Number.Int64())
		}
		ethReplayInitialied = true
		if !k.EthReplayConfig.Enabled {
			fmt.Println("EthReplayConfig has been automatically enabled")
			k.EthReplayConfig.Enabled = true
		}
		return
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

func (k *Keeper) openEthDatabase2() (*ethtypes.Header) {
	network := "Shanghai" // pull this in from the test
	config, ok := ethtests.Forks[network]
	if !ok {
		panic("fork not found")
	}
	fmt.Println("In openEthDatabase2")
	var (
		db    = rawdb.NewMemoryDatabase()
		tconf = &trie.Config{
			Preimages: true,
			IsVerkle:  false,
		}
	)
	scheme := rawdb.HashScheme // TODO: not sure if this is right
	if scheme == rawdb.PathScheme {
		tconf.PathDB = pathdb.Defaults
	} else {
		tconf.HashDB = hashdb.Defaults
	}
	// Commit genesis state
	gspec := extractGenesis(k.BlockTest, config)
	triedb := trie.NewDatabase(db, tconf)
	gblock, err := gspec.Commit(db, triedb)
	if err != nil {
		panic(err)
	}
	sdb := state.NewDatabaseWithNodeDB(db, triedb)
	tr, err := sdb.OpenTrie(gblock.Header_.Root)
	if err != nil {
		panic(err)
	}
	k.Root = gblock.Header_.Root
	k.DB = sdb
	k.Trie = tr
	return gblock.Header_
}

func extractGenesis(t *ethtests.BlockTest, config *params.ChainConfig) *core.Genesis {
	return &core.Genesis{
		Config:        config,
		Nonce:         t.Json.Genesis.Nonce.Uint64(),
		Timestamp:     t.Json.Genesis.Timestamp,
		ParentHash:    t.Json.Genesis.ParentHash,
		ExtraData:     t.Json.Genesis.ExtraData,
		GasLimit:      t.Json.Genesis.GasLimit,
		GasUsed:       t.Json.Genesis.GasUsed,
		Difficulty:    t.Json.Genesis.Difficulty,
		Mixhash:       t.Json.Genesis.MixHash,
		Coinbase:      t.Json.Genesis.Coinbase,
		Alloc:         t.Json.Pre,
		BaseFee:       t.Json.Genesis.BaseFeePerGas,
		BlobGasUsed:   t.Json.Genesis.BlobGasUsed,
		ExcessBlobGas: t.Json.Genesis.ExcessBlobGas,
	}
}
