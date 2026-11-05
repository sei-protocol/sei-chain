package giga

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// legacyEncoderEveryBranch returns a change set that reaches every EVM branch of encodeLegacyChangeSets().
func legacyEncoderEveryBranch() *gigatypes.StateChangeSet {
	addr := func(b byte) common.Address { return common.Address{b, 0x5e, 0x11} }
	return &gigatypes.StateChangeSet{
		Accounts: []gigatypes.AccountChange{
			{Address: addr(0x01), Account: gigatypes.AccountState{Balance: big.NewInt(99)}},
			{
				Address: addr(0x02),
				Account: gigatypes.AccountState{Balance: big.NewInt(0)},
				Prior:   gigatypes.AccountState{Balance: big.NewInt(1)},
			},
			{
				Address: addr(0x03),
				Account: gigatypes.AccountState{Balance: nil},
				Prior:   gigatypes.AccountState{Balance: big.NewInt(1)},
			},
			{
				Address: addr(0x04),
				Account: gigatypes.AccountState{Balance: new(big.Int).Lsh(big.NewInt(1), 255)},
			},
			{
				Address: addr(0x05),
				Account: gigatypes.AccountState{Nonce: 0},
				Prior:   gigatypes.AccountState{Nonce: 1},
			},
			{Address: addr(0x06), Account: gigatypes.AccountState{Nonce: ^uint64(0)}},
			{
				Address: addr(0x07),
				Account: gigatypes.AccountState{Code: []byte{0x60, 0x01, 0x60, 0x02, 0xf3}},
			},
			{
				Address: addr(0x08),
				Account: gigatypes.AccountState{Code: nil},
				Prior:   gigatypes.AccountState{Code: []byte{0x60}},
			},
			{
				Address: addr(0x09),
				Account: gigatypes.AccountState{Code: []byte{}},
				Prior:   gigatypes.AccountState{Code: []byte{0x60}},
			},
		},
		Storage: []gigatypes.StorageChange{
			{Address: addr(0x0a), Key: common.Hash{0x21}, Value: common.Hash{0xaa}},
			{Address: addr(0x0b), Key: common.Hash{0x22}, Delete: true},
			{Address: addr(0x0c), Key: common.Hash{0x23}, Value: common.Hash{}},
		},
	}
}

// legacyEVMPairDigest renders every pair of the single EVM changeset encodeLegacyChangeSets() returns for changes,
// in order, as the key on one line and the delete flag and value on the next.
func legacyEVMPairDigest(t testing.TB, changes *gigatypes.StateChangeSet) string {
	encoded := encodeLegacyChangeSets(changes)
	require.Len(t, encoded, 1)
	require.Equal(t, keys.EVMStoreKey, encoded[0].Name)

	var sb strings.Builder
	for _, pair := range encoded[0].Changeset.Pairs {
		key, value := hex.EncodeToString(pair.Key), hex.EncodeToString(pair.Value)
		fmt.Fprintf(&sb, "%s\n  %t|%s\n", key, pair.Delete, value)
	}
	return sb.String()
}

// TestEncodeLegacyChangeSetsGolden pins the EVM pairs handed to the EVM state store.
func TestEncodeLegacyChangeSetsGolden(t *testing.T) {
	got := legacyEVMPairDigest(t, legacyEncoderEveryBranch())
	const want = `21015e110000000000000000000000000000000000
  false|0000000000000000000000000000000000000000000000000000000000000063
21025e110000000000000000000000000000000000
  true|
21035e110000000000000000000000000000000000
  true|
21045e110000000000000000000000000000000000
  false|8000000000000000000000000000000000000000000000000000000000000000
0a055e110000000000000000000000000000000000
  false|0000000000000000
0a065e110000000000000000000000000000000000
  false|ffffffffffffffff
08075e110000000000000000000000000000000000
  false|fa75857afed3839ad65a0a0c36243417b5745254a8de666dc416e2f26db904c0
07075e110000000000000000000000000000000000
  false|60016002f3
08085e110000000000000000000000000000000000
  true|
07085e110000000000000000000000000000000000
  true|
08095e110000000000000000000000000000000000
  true|
07095e110000000000000000000000000000000000
  true|
030a5e1100000000000000000000000000000000002100000000000000000000000000000000000000000000000000000000000000
  false|aa00000000000000000000000000000000000000000000000000000000000000
030b5e1100000000000000000000000000000000002200000000000000000000000000000000000000000000000000000000000000
  true|
030c5e1100000000000000000000000000000000002300000000000000000000000000000000000000000000000000000000000000
  true|
`
	require.Equal(t, want, got)
}

func TestEncodeLegacyChangeSetsGroupsMiscChangesByModuleAfterTheEVMChangeSet(t *testing.T) {
	changes := &gigatypes.StateChangeSet{
		Storage: []gigatypes.StorageChange{
			{Address: common.Address{0x11}, Key: common.Hash{0x21}, Value: common.Hash{0xaa}},
		},
		Misc: []gigatypes.MiscChange{
			{Module: "bank", Key: []byte("k1"), Value: []byte("v1")},
			{Module: "evmonly", Key: []byte("k2"), Value: []byte("v2")},
			{Module: "bank", Key: []byte("k3"), Value: []byte("ignored"), Delete: true},
		},
	}

	encoded := encodeLegacyChangeSets(changes)

	require.Len(t, encoded, 3)
	require.Equal(t, keys.EVMStoreKey, encoded[0].Name)
	require.Equal(t, []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k1"), Value: []byte("v1")},
			{Key: []byte("k3"), Delete: true},
		}}},
		{Name: "evmonly", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k2"), Value: []byte("v2")},
		}}},
	}, encoded[1:])
}

func TestEncodeLegacyChangeSetsOmitsTheEVMChangeSetWhenOnlyMiscChanges(t *testing.T) {
	changes := &gigatypes.StateChangeSet{
		Misc: []gigatypes.MiscChange{{Module: "evmonly", Key: []byte("k"), Value: []byte("v")}},
	}

	require.Equal(t, []*proto.NamedChangeSet{
		{Name: "evmonly", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}}},
	}, encodeLegacyChangeSets(changes))
}

func TestEncodeLegacyChangeSetsReturnsNothingForAnEmptySet(t *testing.T) {
	require.Empty(t, encodeLegacyChangeSets(&gigatypes.StateChangeSet{}))
}
