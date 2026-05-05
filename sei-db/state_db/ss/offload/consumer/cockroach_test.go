package consumer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
)

func makeRecord(version int64, changesets ...*dbproto.NamedChangeSet) Record {
	return Record{
		Topic:  "historical-offload",
		Offset: version,
		Entry: &dbproto.ChangelogEntry{
			Version:    version,
			Changesets: changesets,
		},
	}
}

func TestBuildMutationBatchesEmpty(t *testing.T) {
	require.Empty(t, buildMutationBatches(makeRecord(1), 500))
}

func TestBuildMutationBatchesSingleBatch(t *testing.T) {
	rec := makeRecord(7, &dbproto.NamedChangeSet{
		Name: "evm",
		Changeset: dbproto.ChangeSet{Pairs: []*dbproto.KVPair{
			{Key: []byte("k1"), Value: []byte("v1")},
			{Key: []byte("k2"), Delete: true},
		}},
	})
	batches := buildMutationBatches(rec, 500)
	require.Len(t, batches, 1)

	b := batches[0]
	require.Contains(t, b.Stmt, "INSERT INTO state_mutations")
	require.Contains(t, b.Stmt, "ON CONFLICT (store_name, key, version) DO UPDATE")
	require.Contains(t, b.Stmt, "($1,$2,$3,$4,$5)")
	require.Contains(t, b.Stmt, "($6,$7,$8,$9,$10)")
	require.Equal(t, 2, strings.Count(b.Stmt, "($"))
	require.Len(t, b.Args, 10)

	// First row: name, key, version, value, deleted.
	require.Equal(t, "evm", b.Args[0])
	require.Equal(t, []byte("k1"), b.Args[1])
	require.Equal(t, int64(7), b.Args[2])
	require.Equal(t, []byte("v1"), b.Args[3])
	require.Equal(t, false, b.Args[4])
	// Second row: delete=true.
	require.Equal(t, true, b.Args[9])
}

func TestBuildMutationBatchesSplits(t *testing.T) {
	pairs := make([]*dbproto.KVPair, 250)
	for i := range pairs {
		pairs[i] = &dbproto.KVPair{Key: []byte{byte(i)}, Value: []byte{0x1}}
	}
	rec := makeRecord(9, &dbproto.NamedChangeSet{
		Name:      "bank",
		Changeset: dbproto.ChangeSet{Pairs: pairs},
	})

	batches := buildMutationBatches(rec, 100)
	require.Len(t, batches, 3) // 100 + 100 + 50
	require.Len(t, batches[0].Args, 500)
	require.Len(t, batches[1].Args, 500)
	require.Len(t, batches[2].Args, 250)
}

func TestBuildMutationBatchesAcrossStores(t *testing.T) {
	rec := makeRecord(3,
		&dbproto.NamedChangeSet{
			Name:      "evm",
			Changeset: dbproto.ChangeSet{Pairs: []*dbproto.KVPair{{Key: []byte("a"), Value: []byte("1")}}},
		},
		&dbproto.NamedChangeSet{
			Name:      "bank",
			Changeset: dbproto.ChangeSet{Pairs: []*dbproto.KVPair{{Key: []byte("b"), Value: []byte("2")}}},
		},
	)
	batches := buildMutationBatches(rec, 500)
	require.Len(t, batches, 1)
	require.Equal(t, "evm", batches[0].Args[0])
	require.Equal(t, "bank", batches[0].Args[5])
}

func TestBuildMutationBatchesDefaultCap(t *testing.T) {
	pairs := make([]*dbproto.KVPair, mutationBatchRows+1)
	for i := range pairs {
		pairs[i] = &dbproto.KVPair{Key: []byte{byte(i)}}
	}
	rec := makeRecord(1, &dbproto.NamedChangeSet{
		Name:      "x",
		Changeset: dbproto.ChangeSet{Pairs: pairs},
	})
	batches := buildMutationBatches(rec, 0)
	require.Len(t, batches, 2)
}
