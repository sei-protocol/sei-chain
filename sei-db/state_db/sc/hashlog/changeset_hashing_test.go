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

func TestHashChangesetDeterministic(t *testing.T) {
	a := []*proto.NamedChangeSet{cs("bank", kv("a", "1"), kv("b", "2"))}
	b := []*proto.NamedChangeSet{cs("bank", kv("a", "1"), kv("b", "2"))}
	require.Equal(t, hashChangeset(a), hashChangeset(b))
	require.Len(t, hashChangeset(a), 8)
}

func TestHashChangesetOrderSensitivePairs(t *testing.T) {
	a := []*proto.NamedChangeSet{cs("bank", kv("a", "1"), kv("b", "2"))}
	b := []*proto.NamedChangeSet{cs("bank", kv("b", "2"), kv("a", "1"))}
	require.NotEqual(t, hashChangeset(a), hashChangeset(b))
}

func TestHashChangesetOrderSensitiveChangeSets(t *testing.T) {
	a := []*proto.NamedChangeSet{cs("bank", kv("a", "1")), cs("evm", kv("b", "2"))}
	b := []*proto.NamedChangeSet{cs("evm", kv("b", "2")), cs("bank", kv("a", "1"))}
	require.NotEqual(t, hashChangeset(a), hashChangeset(b))
}

func TestHashChangesetDeleteFlagSensitive(t *testing.T) {
	a := []*proto.NamedChangeSet{cs("bank", kv("a", ""))}
	b := []*proto.NamedChangeSet{cs("bank", del("a"))}
	require.NotEqual(t, hashChangeset(a), hashChangeset(b))
}

func TestHashChangesetBoundarySensitive(t *testing.T) {
	// Length-prefixing must prevent ambiguity between {"ab",""} and {"a","b"}.
	a := []*proto.NamedChangeSet{cs("bank", kv("ab", ""))}
	b := []*proto.NamedChangeSet{cs("bank", kv("a", "b"))}
	require.NotEqual(t, hashChangeset(a), hashChangeset(b))
}

func TestHashChangesetChangeSetFramingSensitive(t *testing.T) {
	// Count-prefix framing makes the grouping of pairs into change sets significant: two pairs under one
	// (empty-named) change set must not collide with the same two pairs split across two (empty-named) change
	// sets, even though both flatten to the same name/key/value stream.
	a := []*proto.NamedChangeSet{cs("", kv("a", "1"), kv("b", "2"))}
	b := []*proto.NamedChangeSet{cs("", kv("a", "1")), cs("", kv("b", "2"))}
	require.NotEqual(t, hashChangeset(a), hashChangeset(b))
}

func TestHashChangesetPairCountSensitive(t *testing.T) {
	// A trailing empty change set must change the digest: the change-set count is folded in, so an extra
	// (empty) group is observable even though it contributes no name, key, or value bytes.
	a := []*proto.NamedChangeSet{cs("bank", kv("a", "1"))}
	b := []*proto.NamedChangeSet{cs("bank", kv("a", "1")), cs("")}
	require.NotEqual(t, hashChangeset(a), hashChangeset(b))
}

func TestHashChangesetNameSensitive(t *testing.T) {
	a := []*proto.NamedChangeSet{cs("bank", kv("a", "1"))}
	b := []*proto.NamedChangeSet{cs("evm", kv("a", "1"))}
	require.NotEqual(t, hashChangeset(a), hashChangeset(b))
}

func TestHashChangesetEmptyStable(t *testing.T) {
	require.Equal(t, hashChangeset(nil), hashChangeset([]*proto.NamedChangeSet{}))
	require.Len(t, hashChangeset(nil), 8)
}

func TestHashChangesetToleratesNilEntries(t *testing.T) {
	// hashChangeset runs on the background hasher goroutine, so a nil change set or nil pair must be skipped
	// rather than panic and take down the node.
	require.NotPanics(t, func() {
		hashChangeset([]*proto.NamedChangeSet{nil, cs("bank", kv("a", "1"), nil, kv("b", "2"))})
	})
}
