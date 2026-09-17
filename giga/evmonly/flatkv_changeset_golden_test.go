package evmonly

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// flatKVEveryBranch covers every encodeFlatKVChangeSet path except storage clears,
// which TestEncodeFlatKVChangeSetGoldenStorageClear covers.
func flatKVEveryBranch() StateChangeSet {
	addr := func(b byte) common.Address { return common.Address{b, 0x5e, 0x11} }
	return StateChangeSet{
		Balances: []BalanceChange{
			{Address: addr(0x01), Balance: big.NewInt(99)},
			{Address: addr(0x02), Balance: big.NewInt(0)},
			{Address: addr(0x03), Balance: nil},
			{Address: addr(0x04), Balance: new(big.Int).Lsh(big.NewInt(1), 255)},
		},
		Nonces: []NonceChange{
			{Address: addr(0x05), Nonce: 0},
			{Address: addr(0x06), Nonce: ^uint64(0)},
		},
		Code: []CodeChange{
			{Address: addr(0x07), Code: []byte{0x60, 0x01, 0x60, 0x02, 0xf3}},
			{Address: addr(0x08), Delete: true},
			{Address: addr(0x09), Code: nil},
		},
		Storage: []StorageChange{
			{Address: addr(0x0a), Key: common.Hash{0x21}, Value: common.Hash{0xaa}},
			{Address: addr(0x0b), Key: common.Hash{0x22}, Delete: true},
			{Address: addr(0x0c), Key: common.Hash{0x23}, Value: common.Hash{}},
		},
	}
}

// flatKVPairDigest renders every pair in order.
func flatKVPairDigest(t testing.TB, changes StateChangeSet) string {
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = t.TempDir()
	store, err := openFlatKVTestStore(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	return flatKVPairListing(t, store, changes)
}

func flatKVPairListing(t testing.TB, store *flatkv.CommitStore, changes StateChangeSet) string {
	encoded, err := NewFlatKVChangeSetEncoder(store)(changes)
	require.NoError(t, err)
	require.Len(t, encoded, 1)

	var sb strings.Builder
	for _, pair := range encoded[0].Changeset.Pairs {
		fmt.Fprintf(&sb, "%s|%t|%s\n", hex.EncodeToString(pair.Key), pair.Delete, hex.EncodeToString(pair.Value))
	}
	return sb.String()
}

// TestEncodeFlatKVChangeSetGolden pins the encoder's output. The pairs are committed to
// FlatKV and enter its LtHash, so a change here changes the state root.
func TestEncodeFlatKVChangeSetGolden(t *testing.T) {
	got := flatKVPairDigest(t, flatKVEveryBranch())
	const want = `21015e110000000000000000000000000000000000|false|0000000000000000000000000000000000000000000000000000000000000063
21025e110000000000000000000000000000000000|true|
21035e110000000000000000000000000000000000|true|
21045e110000000000000000000000000000000000|false|8000000000000000000000000000000000000000000000000000000000000000
0a055e110000000000000000000000000000000000|false|0000000000000000
0a065e110000000000000000000000000000000000|false|ffffffffffffffff
08075e110000000000000000000000000000000000|false|fa75857afed3839ad65a0a0c36243417b5745254a8de666dc416e2f26db904c0
07075e110000000000000000000000000000000000|false|60016002f3
08085e110000000000000000000000000000000000|true|
07085e110000000000000000000000000000000000|true|
08095e110000000000000000000000000000000000|true|
07095e110000000000000000000000000000000000|true|
030a5e1100000000000000000000000000000000002100000000000000000000000000000000000000000000000000000000000000|false|aa00000000000000000000000000000000000000000000000000000000000000
030b5e1100000000000000000000000000000000002200000000000000000000000000000000000000000000000000000000000000|true|
030c5e1100000000000000000000000000000000002300000000000000000000000000000000000000000000000000000000000000|true|
`
	require.Equal(t, want, got)
}

func TestEncodeFlatKVChangeSetGoldenStorageClear(t *testing.T) {
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = t.TempDir()
	store, err := openFlatKVTestStore(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	addr := func(b byte) common.Address { return common.Address{b} }
	storageAddress := addr(0x0d)
	encode := NewFlatKVChangeSetEncoder(store)
	initial, err := encode(StateChangeSet{
		Storage: []StorageChange{
			{Address: storageAddress, Key: common.Hash{0x21}, Value: common.Hash{0xaa}},
			{Address: storageAddress, Key: common.Hash{0x22}, Value: common.Hash{0xbb}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.CommitStateChanges(1, initial))

	got := flatKVPairListing(t, store, StateChangeSet{
		Balances:      []BalanceChange{{Address: addr(0x01), Balance: big.NewInt(99)}},
		StorageClears: []common.Address{storageAddress},
		Storage: []StorageChange{{
			Address: storageAddress,
			Key:     common.Hash{0x23},
			Value:   common.Hash{0xcc},
		}},
	})
	const want = `210100000000000000000000000000000000000000|false|0000000000000000000000000000000000000000000000000000000000000063
030d000000000000000000000000000000000000002100000000000000000000000000000000000000000000000000000000000000|true|
030d000000000000000000000000000000000000002200000000000000000000000000000000000000000000000000000000000000|true|
030d000000000000000000000000000000000000002300000000000000000000000000000000000000000000000000000000000000|false|cc00000000000000000000000000000000000000000000000000000000000000
`
	require.Equal(t, want, got)
}

func TestEncodeFlatKVChangeSetRejectsNegativeBalance(t *testing.T) {
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = t.TempDir()
	store, err := openFlatKVTestStore(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	_, err = NewFlatKVChangeSetEncoder(store)(StateChangeSet{
		Balances: []BalanceChange{{Address: common.Address{0x11}, Balance: big.NewInt(-1)}},
	})
	require.ErrorContains(t, err, "balance must fit in an unsigned 256-bit integer")
}

func BenchmarkEncodeFlatKVChangeSet(b *testing.B) {
	// 1,167 accounts touched produce about the 3,500 pairs a full block emits.
	const accounts = 1167
	changes := StateChangeSet{}
	for i := range accounts {
		addr := common.Address{byte(i), byte(i >> 8), 0x5e}
		changes.Balances = append(changes.Balances, BalanceChange{Address: addr, Balance: big.NewInt(int64(i) * 1e9)})
		changes.Nonces = append(changes.Nonces, NonceChange{Address: addr, Nonce: uint64(i)})
		changes.Storage = append(changes.Storage, StorageChange{
			Address: addr, Key: common.Hash{byte(i)}, Value: common.Hash{byte(i ^ 0xff)},
		})
	}
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = b.TempDir()
	store, err := openFlatKVTestStore(b.Context(), cfg)
	require.NoError(b, err)
	b.Cleanup(func() { require.NoError(b, store.Close()) })
	encode := NewFlatKVChangeSetEncoder(store)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := encode(changes); err != nil {
			b.Fatal(err)
		}
	}
}
