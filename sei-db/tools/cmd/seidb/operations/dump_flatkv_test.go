package operations

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// TestDumpFlatKVFromStoreAllBuckets seeds a mix of account, code, storage and
// legacy rows, runs dumpFlatKVFromStore across all four buckets, and checks
// that every file gets the right header, the right number of data lines, and
// the right format. Physical keys are emitted verbatim (no logical
// stripping), which is the contract dump-flatkv promises.
func TestDumpFlatKVFromStoreAllBuckets(t *testing.T) {
	store := newTestFlatKVStore(t)
	defer func() { require.NoError(t, store.Close()) }()

	addrA := addrN(0x11)
	addrB := addrN(0x22)

	evmCS := &proto.NamedChangeSet{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addrA, 1),
			noncePair(addrB, 2),
			codePair(addrA, []byte{0x60, 0x80}),
			storagePair(addrA, slotN(0x01), 0xAA),
			storagePair(addrA, slotN(0x02), 0xBB),
			storagePair(addrB, slotN(0x01), 0xCC),
		}},
	}
	bankCS := &proto.NamedChangeSet{
		Name: "bank",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("supply/usei"), Value: []byte("100")},
		}},
	}

	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{evmCS, bankCS}))
	_, err := store.Commit()
	require.NoError(t, err)

	outDir := t.TempDir()
	require.NoError(t, dumpFlatKVFromStore(store, outDir, store.Version(), ""))

	type expect struct {
		lines int
	}
	want := map[string]expect{
		"account": {lines: 2}, // 2 nonces -> 2 account rows
		"code":    {lines: 1}, // 1 code
		"storage": {lines: 3}, // 3 storage slots
		"legacy":  {lines: 1}, // 1 bank row
	}

	for name, w := range want {
		path := filepath.Join(outDir, name)
		f, err := os.Open(path)
		require.NoError(t, err, "bucket file %s must exist", name)
		defer f.Close() //nolint:errcheck // test cleanup

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

		require.True(t, scanner.Scan(), "missing header in %s", name)
		header := scanner.Text()
		require.True(t,
			strings.HasPrefix(header, "Bucket "+name+" at version "),
			"unexpected header in %s: %q", name, header,
		)

		data := 0
		for scanner.Scan() {
			line := scanner.Text()
			require.True(t, strings.HasPrefix(line, "Key: "),
				"bucket %s: expected line to start with 'Key: ', got %q", name, line)
			require.Contains(t, line, ", Value: ",
				"bucket %s: expected ', Value: ' separator, got %q", name, line)
			data++
		}
		require.NoError(t, scanner.Err(), "scanner error on %s", name)
		require.Equal(t, w.lines, data,
			"bucket %s: expected %d data lines, got %d", name, w.lines, data)
	}
}

// TestDumpFlatKVFromStoreSingleBucket verifies the --bucket filter keeps
// writes restricted to exactly one file even though the iterator still
// walks every DB under the hood.
func TestDumpFlatKVFromStoreSingleBucket(t *testing.T) {
	store := newTestFlatKVStore(t)
	defer func() { require.NoError(t, store.Close()) }()

	addrA := addrN(0x11)
	evmCS := &proto.NamedChangeSet{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addrA, 1),
			codePair(addrA, []byte{0x60}),
			storagePair(addrA, slotN(0x01), 0xAA),
		}},
	}
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{evmCS}))
	_, err := store.Commit()
	require.NoError(t, err)

	outDir := t.TempDir()
	require.NoError(t, dumpFlatKVFromStore(store, outDir, store.Version(), "storage"))

	// Only storage file should exist; the others must not be created.
	for _, name := range flatkvBucketOrder {
		path := filepath.Join(outDir, name)
		_, statErr := os.Stat(path)
		if name == "storage" {
			require.NoError(t, statErr, "storage bucket file must exist")
		} else {
			require.True(t, os.IsNotExist(statErr),
				"bucket %s: expected file to be absent when --bucket=storage, got err=%v",
				name, statErr)
		}
	}
}

func TestIsFlatKVBucket(t *testing.T) {
	for _, b := range flatkvBucketOrder {
		require.True(t, isFlatKVBucket(b), "%s should be accepted", b)
	}
	require.False(t, isFlatKVBucket(""), "empty should not validate")
	require.False(t, isFlatKVBucket("metadata"), "metadata is intentionally excluded from dump-flatkv")
	require.False(t, isFlatKVBucket("evm"), "evm is a module, not a bucket")
}
