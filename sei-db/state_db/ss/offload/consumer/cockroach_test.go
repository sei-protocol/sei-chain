package consumer

import (
	"strings"
	"testing"

	"github.com/lib/pq"
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

// pq.CopyIn is what copyMutations uses; pin its shape so an upstream rename
// or column reorder breaks loudly instead of silently.
func TestCopyInStatementShape(t *testing.T) {
	stmt := pq.CopyIn("state_mutations", "store_name", "key", "version", "value", "deleted")
	require.True(t, strings.HasPrefix(stmt, "COPY"),
		"pq.CopyIn must produce a COPY statement; got %q", stmt)
	for _, frag := range []string{"state_mutations", "store_name", "key", "version", "value", "deleted"} {
		require.Contains(t, stmt, frag)
	}
}

func TestRecordPairCount(t *testing.T) {
	rec := makeRecord(7,
		&dbproto.NamedChangeSet{
			Name: "evm",
			Changeset: dbproto.ChangeSet{Pairs: []*dbproto.KVPair{
				{Key: []byte("k1"), Value: []byte("v1")},
				{Key: []byte("k2"), Delete: true},
			}},
		},
		&dbproto.NamedChangeSet{
			Name:      "bank",
			Changeset: dbproto.ChangeSet{Pairs: []*dbproto.KVPair{{Key: []byte("a"), Value: []byte("1")}}},
		},
	)

	total := 0
	for _, ncs := range rec.Entry.Changesets {
		total += len(ncs.Changeset.Pairs)
	}
	require.Equal(t, 3, total)
}
