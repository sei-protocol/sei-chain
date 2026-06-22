package hashlog

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func cs(name string, pairs ...*proto.KVPair) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name:      name,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}
}

func kv(key string, value string) *proto.KVPair {
	return &proto.KVPair{Key: []byte(key), Value: []byte(value)}
}

func del(key string) *proto.KVPair {
	return &proto.KVPair{Delete: true, Key: []byte(key)}
}

func TestHashDiffDeterministic(t *testing.T) {
	a := []*proto.NamedChangeSet{cs("bank", kv("a", "1"), kv("b", "2"))}
	b := []*proto.NamedChangeSet{cs("bank", kv("a", "1"), kv("b", "2"))}
	require.Equal(t, hashDiff(a), hashDiff(b))
	require.Len(t, hashDiff(a), 8)
}

func TestHashDiffOrderSensitivePairs(t *testing.T) {
	a := []*proto.NamedChangeSet{cs("bank", kv("a", "1"), kv("b", "2"))}
	b := []*proto.NamedChangeSet{cs("bank", kv("b", "2"), kv("a", "1"))}
	require.NotEqual(t, hashDiff(a), hashDiff(b))
}

func TestHashDiffOrderSensitiveChangeSets(t *testing.T) {
	a := []*proto.NamedChangeSet{cs("bank", kv("a", "1")), cs("evm", kv("b", "2"))}
	b := []*proto.NamedChangeSet{cs("evm", kv("b", "2")), cs("bank", kv("a", "1"))}
	require.NotEqual(t, hashDiff(a), hashDiff(b))
}

func TestHashDiffDeleteFlagSensitive(t *testing.T) {
	a := []*proto.NamedChangeSet{cs("bank", kv("a", ""))}
	b := []*proto.NamedChangeSet{cs("bank", del("a"))}
	require.NotEqual(t, hashDiff(a), hashDiff(b))
}

func TestHashDiffBoundarySensitive(t *testing.T) {
	// Length-prefixing must prevent ambiguity between {"ab",""} and {"a","b"}.
	a := []*proto.NamedChangeSet{cs("bank", kv("ab", ""))}
	b := []*proto.NamedChangeSet{cs("bank", kv("a", "b"))}
	require.NotEqual(t, hashDiff(a), hashDiff(b))
}

func TestHashDiffNameSensitive(t *testing.T) {
	a := []*proto.NamedChangeSet{cs("bank", kv("a", "1"))}
	b := []*proto.NamedChangeSet{cs("evm", kv("a", "1"))}
	require.NotEqual(t, hashDiff(a), hashDiff(b))
}

func TestHashDiffEmptyStable(t *testing.T) {
	require.Equal(t, hashDiff(nil), hashDiff([]*proto.NamedChangeSet{}))
	require.Len(t, hashDiff(nil), 8)
}
