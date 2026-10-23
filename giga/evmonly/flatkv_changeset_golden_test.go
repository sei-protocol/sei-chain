package evmonly

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// flatKVEveryBranch covers every encodeFlatKVChangeSet path.
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
	encoded, err := NewFlatKVChangeSetEncoder()(changes)
	require.NoError(t, err)
	require.Len(t, encoded, 1)

	var sb strings.Builder
	for _, pair := range encoded[0].Changeset.Pairs {
		fmt.Fprintf(&sb, "%s|%t|%s\n", hex.EncodeToString(pair.Key), pair.Delete, hex.EncodeToString(pair.Value))
	}
	return sb.String()
}

// TestEncodeFlatKVChangeSetGolden pins the emitted pairs, which enter FlatKV's LtHash.
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

func TestEncodeFlatKVChangeSetRejectsNegativeBalance(t *testing.T) {
	_, err := NewFlatKVChangeSetEncoder()(StateChangeSet{
		Balances: []BalanceChange{{Address: common.Address{0x11}, Balance: big.NewInt(-1)}},
	})
	require.ErrorContains(t, err, "balance must fit in an unsigned 256-bit integer")
}
