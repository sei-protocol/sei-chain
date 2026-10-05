package app

// Behavior tests pinning go-ethereum APIs used by eth replay and blocktest tooling.

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	ethtests "github.com/ethereum/go-ethereum/tests"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/replay"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

var (
	replayFixtureBlockHash = common.HexToHash("0x2155b96284ea1d5b6c51ffc051007ff9c3097a0c381d36a4310341625ba4374e")
	replayFixtureTxHash    = common.HexToHash("0x6cc971c9a2106a003ac4433f7c62f640d1bb358018508d23a717a93aefdc0e3b")
	replayAnvil0           = common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
)

func loadReplayBlockTest(t *testing.T) *ethtests.BlockTest {
	t.Helper()
	bz, err := os.ReadFile("../cmd/seid/cmd/testdata/blocktest_behavior.json")
	require.NoError(t, err)
	var tests map[string]ethtests.BlockTest
	require.NoError(t, json.Unmarshal(bz, &tests))
	bt, ok := tests["minimal"]
	require.True(t, ok)
	return &bt
}

// encodeTx round-trips a decoded block tx through MsgEVMTransaction unchanged.
func TestBehaviorEthReplayEncodeDecodedBlockTx(t *testing.T) {
	bt := loadReplayBlockTest(t)
	require.Len(t, bt.Json.Blocks, 1)
	b, err := bt.Json.Blocks[0].Decode()
	require.NoError(t, err)
	require.Equal(t, replayFixtureBlockHash, b.Hash())
	require.False(t, IsWithdrawalAddress(replayAnvil0, []*ethtypes.Block{b}))

	txConfig := MakeEncodingConfig().TxConfig
	bz := encodeTx(b.Transactions()[0], txConfig)
	require.NotEmpty(t, bz)
	sdkTx, err := txConfig.TxDecoder()(bz)
	require.NoError(t, err)
	require.Len(t, sdkTx.GetMsgs(), 1)
	msg, ok := sdkTx.GetMsgs()[0].(*evmtypes.MsgEVMTransaction)
	require.True(t, ok)
	ethTx, _ := msg.AsTransaction()
	require.NotNil(t, ethTx)
	require.Equal(t, replayFixtureTxHash, ethTx.Hash())
	require.Equal(t, uint8(ethtypes.LegacyTxType), ethTx.Type())

	// pre-state as iterated by app.BlockTest
	require.Len(t, bt.Json.Pre, 2)
	require.Equal(t, "10000000000000000000", bt.Json.Pre[replayAnvil0].Balance.String())
}

// writeReplayEthDB commits a genesis to a pebble geth datadir using the given state scheme.
func writeReplayEthDB(t *testing.T, dir string, scheme string) *ethtypes.Block {
	t.Helper()
	db, err := node.OpenDatabase(node.OpenOptions{
		Type:              "pebble",
		Directory:         dir,
		AncientsDirectory: fmt.Sprintf("%s/ancient", dir),
		Cache:             16,
		Handles:           16,
	})
	require.NoError(t, err)
	cfg := triedb.HashDefaults
	if scheme == rawdb.PathScheme {
		cfg = &triedb.Config{PathDB: pathdb.Defaults}
	}
	tdb := triedb.NewDatabase(db, cfg)
	gspec := &core.Genesis{
		Config:   params.TestChainConfig,
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(params.InitialBaseFee),
		Alloc: ethtypes.GenesisAlloc{
			replayAnvil0: {Balance: big.NewInt(1_000_000)},
		},
	}
	block, err := gspec.Commit(db, tdb)
	require.NoError(t, err)
	require.NoError(t, tdb.Close())
	require.NoError(t, db.Close())
	return block
}

func TestBehaviorOpenEthDatabase(t *testing.T) {
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		t.Run(scheme, func(t *testing.T) {
			dir := t.TempDir()
			genesis := writeReplayEthDB(t, dir, scheme)

			k := &keeper.Keeper{EthReplayConfig: replay.Config{EthDataDir: dir}}
			header := k.OpenEthDatabase()
			t.Cleanup(func() {
				tdb := k.CachingDB.TrieDB()
				require.NoError(t, tdb.Close())
				require.NoError(t, tdb.Disk().Close())
			})
			require.NotNil(t, header)
			require.Equal(t, genesis.Hash(), header.Hash())
			require.Equal(t, uint64(0), header.Number.Uint64())
			require.Equal(t, genesis.Root(), header.Root)
			require.Equal(t, genesis.Root(), k.Root)
			require.NotNil(t, k.DB)
			require.NotNil(t, k.CachingDB)
			require.NotNil(t, k.Trie)

			acct, err := k.Trie.GetAccount(replayAnvil0)
			require.NoError(t, err)
			require.NotNil(t, acct)
			require.Equal(t, uint64(1_000_000), acct.Balance.Uint64())
			require.Equal(t, uint64(0), acct.Nonce)
		})
	}
}
