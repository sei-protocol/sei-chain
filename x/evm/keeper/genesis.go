package keeper

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/triedb/hashdb"
	"github.com/ethereum/go-ethereum/trie/triedb/pathdb"

	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc1155"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc721"
	artifactsutils "github.com/sei-protocol/sei-chain/x/evm/artifacts/utils"
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

	erc20CodeID, err := k.wasmKeeper.Create(ctx, k.accountKeeper.GetModuleAddress(types.ModuleName), erc20.GetBin(), nil)
	if err != nil {
		ctx.Logger().Error(fmt.Sprintf("error creating CWERC20 pointer code due to %s", err))
	} else {
		prefix.NewStore(k.PrefixStore(ctx, types.PointerCWCodePrefix), types.PointerCW20ERC20Prefix).Set(
			artifactsutils.GetVersionBz(erc20.CurrentVersion),
			artifactsutils.GetCodeIDBz(erc20CodeID),
		)
	}

	erc721CodeID, err := k.wasmKeeper.Create(ctx, k.accountKeeper.GetModuleAddress(types.ModuleName), erc721.GetBin(), nil)
	if err != nil {
		ctx.Logger().Error(fmt.Sprintf("error creating CWERC721 pointer code due to %s", err))
	} else {
		prefix.NewStore(k.PrefixStore(ctx, types.PointerCWCodePrefix), types.PointerCW721ERC721Prefix).Set(
			artifactsutils.GetVersionBz(erc721.CurrentVersion),
			artifactsutils.GetCodeIDBz(erc721CodeID),
		)
	}

	erc1155CodeID, err := k.wasmKeeper.Create(ctx, k.accountKeeper.GetModuleAddress(types.ModuleName), erc1155.GetBin(), nil)
	if err != nil {
		ctx.Logger().Error(fmt.Sprintf("error creating CWERC1155 pointer code due to %s", err))
	} else {
		prefix.NewStore(k.PrefixStore(ctx, types.PointerCWCodePrefix), types.PointerCW1155ERC1155Prefix).Set(
			artifactsutils.GetVersionBz(erc1155.CurrentVersion),
			artifactsutils.GetCodeIDBz(erc1155CodeID),
		)
	}

	if k.EthReplayConfig.Enabled && !ethReplayInitialied {
		header := k.OpenEthDatabase()
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
