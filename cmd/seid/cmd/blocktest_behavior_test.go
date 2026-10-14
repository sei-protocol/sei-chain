package cmd

// Behavior tests pinning go-ethereum block-test fixture decoding used by `seid blocktest`.
// Fixture: one London block with one legacy transfer from Anvil account 0 (chain id 1).

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

const blocktestFixture = "testdata/blocktest_behavior.json"

var (
	btAnvil0    = common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	btRecipient = common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	btCoinbase  = common.HexToAddress("0x000000000000000000000000000000000000c0de")
	btContract  = common.HexToAddress("0x000000000000000000000000000000000000bbbb")
	btBlockHash = common.HexToHash("0x2155b96284ea1d5b6c51ffc051007ff9c3097a0c381d36a4310341625ba4374e")
	btGenesis   = common.HexToHash("0xa15e942106bbc26f2ae26f5de7626c8f9fa220546fa66f707aa55f484bea21e8")
	btTxHash    = common.HexToHash("0x6cc971c9a2106a003ac4433f7c62f640d1bb358018508d23a717a93aefdc0e3b")
)

func requireBigEq(t *testing.T, expected, actual *big.Int) {
	t.Helper()
	require.NotNil(t, actual)
	require.Zero(t, expected.Cmp(actual), "expected %s, got %s", expected, actual)
}

func bigFromString(t *testing.T, s string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s, 10)
	require.True(t, ok)
	return v
}

func TestBehaviorBlocktestIngesterDecodesFixture(t *testing.T) {
	bz, err := os.ReadFile(blocktestFixture)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "fixture.json")
	require.NoError(t, os.WriteFile(path, rewriteKey(bz, path+"::minimal"), 0o600))
	bt := testIngester(path, "minimal")
	require.NotNil(t, bt)

	js := bt.JSON()
	require.Equal(t, "London", js.Network)
	require.Equal(t, "NoProof", js.SealEngine)
	require.Equal(t, btBlockHash, common.Hash(js.BestBlock))
	require.Equal(t, btGenesis, js.Genesis.Hash)
	requireBigEq(t, big.NewInt(0), js.Genesis.Number)

	// pre-state
	require.Len(t, js.Pre, 2)
	requireBigEq(t, bigFromString(t, "10000000000000000000"), js.Pre[btAnvil0].Balance)
	require.Equal(t, uint64(0), js.Pre[btAnvil0].Nonce)
	require.Empty(t, js.Pre[btAnvil0].Code)
	requireBigEq(t, big.NewInt(0), js.Pre[btContract].Balance)
	require.Equal(t, uint64(1), js.Pre[btContract].Nonce)
	require.Equal(t, []byte{0x60, 0x00}, js.Pre[btContract].Code)
	require.Equal(t, map[common.Hash]common.Hash{common.HexToHash("0x01"): common.HexToHash("0x02")}, js.Pre[btContract].Storage)

	// post-state
	require.Len(t, js.Post, 4)
	requireBigEq(t, bigFromString(t, "9999957999999999000"), js.Post[btAnvil0].Balance)
	require.Equal(t, uint64(1), js.Post[btAnvil0].Nonce)
	requireBigEq(t, big.NewInt(1000), js.Post[btRecipient].Balance)
	requireBigEq(t, bigFromString(t, "23625000000000"), js.Post[btCoinbase].Balance)

	// block header
	require.Len(t, js.Blocks, 1)
	bb := js.Blocks[0]
	require.Empty(t, bb.ExpectException)
	require.Empty(t, bb.UncleHeaders)
	requireBigEq(t, big.NewInt(1), bb.BlockHeader.Number)
	require.Equal(t, btBlockHash, bb.BlockHeader.Hash)
	require.Equal(t, btGenesis, bb.BlockHeader.ParentHash)
	require.Equal(t, btCoinbase, bb.BlockHeader.Coinbase)
	requireBigEq(t, big.NewInt(875000000), bb.BlockHeader.BaseFeePerGas)
	require.Equal(t, uint64(30_000_000), bb.BlockHeader.GasLimit)
	require.Equal(t, uint64(21000), bb.BlockHeader.GasUsed)
	require.Equal(t, uint64(1010), bb.BlockHeader.Timestamp)

	// RLP-decoded block
	b, err := bb.Decode()
	require.NoError(t, err)
	require.Equal(t, btBlockHash, b.Hash())
	require.Equal(t, btGenesis, b.ParentHash())
	require.Equal(t, uint64(1), b.NumberU64())
	require.Equal(t, btCoinbase, b.Coinbase())
	requireBigEq(t, big.NewInt(875000000), b.BaseFee())
	require.Empty(t, b.Withdrawals())
	require.Len(t, b.Transactions(), 1)
	tx := b.Transactions()[0]
	require.Equal(t, btTxHash, tx.Hash())
	require.Equal(t, uint8(ethtypes.LegacyTxType), tx.Type())
	requireBigEq(t, big.NewInt(1), tx.ChainId())
	require.Equal(t, btRecipient, *tx.To())
	requireBigEq(t, big.NewInt(1000), tx.Value())
	require.Equal(t, uint64(21000), tx.Gas())
	requireBigEq(t, big.NewInt(2_000_000_000), tx.GasPrice())
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx)
	require.NoError(t, err)
	require.Equal(t, btAnvil0, sender)
}

func TestBehaviorBlocktestIngesterTestNameKey(t *testing.T) {
	bz, err := os.ReadFile(blocktestFixture)
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "ethtests", "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ethtests", "sub", "x.json"), rewriteKey(bz, "sub/x.json::minimal"), 0o600))
	t.Chdir(dir)

	// "./ethtests/" prefix is stripped from the lookup key
	bt := testIngester("./ethtests/sub/x.json", "minimal")
	require.Equal(t, btBlockHash, bt.JSON().Blocks[0].BlockHeader.Hash)

	require.PanicsWithValue(t,
		"Unable to find test name sub/x.json::other at test file path ./ethtests/sub/x.json",
		func() { testIngester("./ethtests/sub/x.json", "other") })
}

// rewriteKey replaces the fixture's top-level "minimal" key.
func rewriteKey(bz []byte, key string) []byte {
	const orig = `"minimal":`
	for i := 0; i+len(orig) <= len(bz); i++ {
		if string(bz[i:i+len(orig)]) == orig {
			out := append([]byte{}, bz[:i]...)
			out = append(out, []byte(`"`+key+`":`)...)
			return append(out, bz[i+len(orig):]...)
		}
	}
	panic("key not found")
}
