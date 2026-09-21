package flatkv

import (
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

func BenchmarkClassifyAndPrefix(b *testing.B) {
	evmPairs := make([]*proto.KVPair, 2000)
	for i := range evmPairs {
		key := make([]byte, keys.InternalKeyLen(keys.EVMKeyStorage))
		binary.BigEndian.PutUint32(key[len(key)-4:], uint32(i))
		evmPairs[i] = &proto.KVPair{
			Key:   keys.BuildEVMKey(keys.EVMKeyStorage, key),
			Value: []byte{1},
		}
	}

	miscPairs := make([]*proto.KVPair, 500)
	for i := range miscPairs {
		key := make([]byte, 8)
		binary.BigEndian.PutUint32(key, uint32(i))
		miscPairs[i] = &proto.KVPair{
			Key:   key,
			Value: []byte{1},
		}
	}

	changeSets := []*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: evmPairs}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: miscPairs}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := classifyAndPrefix(changeSets); err != nil {
			b.Fatal(err)
		}
	}
}
