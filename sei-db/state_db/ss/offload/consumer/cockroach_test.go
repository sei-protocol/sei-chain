package consumer

import (
	"os"
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

// Pins pq.CopyIn's shape so an upstream column rename breaks loudly.
func TestCopyInStatementShape(t *testing.T) {
	stmt := pq.CopyIn("state_mutations", "store_name", "key", "version", "value", "deleted")
	require.True(t, strings.HasPrefix(stmt, "COPY"),
		"pq.CopyIn must produce a COPY statement; got %q", stmt)
	for _, frag := range []string{"state_mutations", "store_name", "key", "version", "value", "deleted"} {
		require.Contains(t, stmt, frag)
	}
}

func TestCompactMutationsKeepsLastMutationPerKey(t *testing.T) {
	rec := makeRecord(7,
		&dbproto.NamedChangeSet{
			Name: "evm",
			Changeset: dbproto.ChangeSet{Pairs: []*dbproto.KVPair{
				{Key: []byte("k"), Value: []byte("old")},
				{Key: []byte("other"), Value: []byte("v")},
				{Key: []byte("k"), Value: []byte("new")},
			}},
		},
		&dbproto.NamedChangeSet{
			Name: "bank",
			Changeset: dbproto.ChangeSet{Pairs: []*dbproto.KVPair{
				{Key: []byte("k"), Value: []byte("bank")},
			}},
		},
	)

	mutations := compactMutations(rec.Entry)
	require.Len(t, mutations, 3)
	require.Equal(t, "evm", mutations[0].storeName)
	require.Equal(t, []byte("k"), mutations[0].pair.Key)
	require.Equal(t, []byte("new"), mutations[0].pair.Value)
	require.Equal(t, []byte("other"), mutations[1].pair.Key)
	require.Equal(t, "bank", mutations[2].storeName)
}

func TestMutationRowsUsesNilValueForDeletes(t *testing.T) {
	rec := makeRecord(7, &dbproto.NamedChangeSet{
		Name: "evm",
		Changeset: dbproto.ChangeSet{Pairs: []*dbproto.KVPair{
			{Key: []byte("k"), Value: []byte("v"), Delete: true},
		}},
	})

	rows := mutationRows([]Record{rec})
	require.Len(t, rows, 1)
	require.Nil(t, rows[0].value)
	require.True(t, rows[0].deleted)
}

func TestSchemaKeepsWriteAmplificationLow(t *testing.T) {
	raw, err := os.ReadFile("schema/schema.sql")
	require.NoError(t, err)

	schema := string(raw)
	require.Contains(t, schema, "kafka_partition")
	require.NotContains(t, schema, "CREATE INDEX IF NOT EXISTS state_mutations_by_version_idx")
	require.NotContains(t, schema, "CREATE INDEX IF NOT EXISTS state_mutations_by_store_version_idx")
	require.Contains(t, schema, "DROP INDEX IF EXISTS state_mutations@state_mutations_by_version_idx")
	require.Contains(t, schema, "DROP INDEX IF EXISTS state_mutations@state_mutations_by_store_version_idx")
}
