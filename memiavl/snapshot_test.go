package memiavl

import (
	"io"
	"testing"

	"github.com/cosmos/iavl"
	protoio "github.com/gogo/protobuf/io"
	proto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
)

func TestSnapshotEncodingRoundTrip(t *testing.T) {
	// setup test tree
	tree := New(0)
	for _, changes := range ChangeSets[:len(ChangeSets)-1] {
		_, _, err := tree.ApplyChangeSet(changes, true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()
	require.NoError(t, tree.WriteSnapshot(snapshotDir))

	snapshot, err := OpenSnapshot(snapshotDir)
	require.NoError(t, err)

	tree2 := NewFromSnapshot(snapshot, true, 0)

	require.Equal(t, tree.Version(), tree2.Version())
	require.Equal(t, tree.RootHash(), tree2.RootHash())

	// verify all the node hashes in snapshot
	for i := 0; i < snapshot.nodesLen(); i++ {
		node := snapshot.Node(uint32(i))
		require.Equal(t, node.Hash(), HashNode(node))
	}

	require.NoError(t, snapshot.Close())

	// test modify tree loaded from snapshot
	snapshot, err = OpenSnapshot(snapshotDir)
	require.NoError(t, err)
	tree3 := NewFromSnapshot(snapshot, true, 0)
	hash, v, err := tree3.ApplyChangeSet(ChangeSets[len(ChangeSets)-1], true)
	require.NoError(t, err)
	require.Equal(t, RefHashes[len(ChangeSets)-1], hash)
	require.Equal(t, len(ChangeSets), int(v))
	require.NoError(t, snapshot.Close())
}

func TestSnapshotExport(t *testing.T) {
	expNodes := []*iavl.ExportNode{
		{Key: []byte("hello"), Value: []byte("world1"), Version: 2, Height: 0},
		{Key: []byte("hello1"), Value: []byte("world1"), Version: 2, Height: 0},
		{Key: []byte("hello1"), Value: nil, Version: 3, Height: 1},
		{Key: []byte("hello2"), Value: []byte("world1"), Version: 3, Height: 0},
		{Key: []byte("hello3"), Value: []byte("world1"), Version: 3, Height: 0},
		{Key: []byte("hello3"), Value: nil, Version: 3, Height: 1},
		{Key: []byte("hello2"), Value: nil, Version: 3, Height: 2},
	}

	// setup test tree
	tree := New(0)
	for _, changes := range ChangeSets[:3] {
		_, _, err := tree.ApplyChangeSet(changes, true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()
	require.NoError(t, tree.WriteSnapshot(snapshotDir))

	snapshot, err := OpenSnapshot(snapshotDir)
	require.NoError(t, err)

	var nodes []*iavl.ExportNode
	exporter := snapshot.Export()
	for {
		node, err := exporter.Next()
		if err == iavl.ExportDone {
			break
		}
		require.NoError(t, err)
		nodes = append(nodes, node)
	}

	require.Equal(t, expNodes, nodes)
}

func TestSnapshotImportExport(t *testing.T) {
	// setup test tree
	tree := New(0)
	for _, changes := range ChangeSets {
		_, _, err := tree.ApplyChangeSet(changes, true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()
	require.NoError(t, tree.WriteSnapshot(snapshotDir))
	snapshot, err := OpenSnapshot(snapshotDir)
	require.NoError(t, err)

	ch := make(chan *iavl.ExportNode)

	go func() {
		defer close(ch)

		exporter := snapshot.Export()
		for {
			node, err := exporter.Next()
			if err == iavl.ExportDone {
				break
			}
			require.NoError(t, err)
			ch <- node
		}
	}()

	snapshotDir2 := t.TempDir()
	err = doImport(snapshotDir2, tree.Version(), ch)
	require.NoError(t, err)

	snapshot2, err := OpenSnapshot(snapshotDir2)
	require.NoError(t, err)
	require.Equal(t, snapshot.RootNode().Hash(), snapshot2.RootNode().Hash())

	// verify all the node hashes in snapshot
	for i := 0; i < snapshot2.nodesLen(); i++ {
		node := snapshot2.Node(uint32(i))
		require.Equal(t, node.Hash(), HashNode(node))
	}
}

func TestDBSnapshotRestore(t *testing.T) {
	db, err := Load(t.TempDir(), Options{
		CreateIfMissing:                 true,
		InitialStores:                   []string{"test"},
		AsyncCommitBuffer:               -1,
		SupportExportNonSnapshotVersion: true,
	})
	require.NoError(t, err)

	for _, changes := range ChangeSets {
		cs := []*NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		_, _, err := db.Commit(cs)
		require.NoError(t, err)

		testSnapshotRoundTrip(t, db)
	}

	require.NoError(t, db.RewriteSnapshot())
	require.NoError(t, db.Reload())
	require.Equal(t, len(ChangeSets), int(db.metadata.CommitInfo.Version))
	testSnapshotRoundTrip(t, db)
}

func testSnapshotRoundTrip(t *testing.T, db *DB) {
	reader, writer := makeProtoIOPair()
	go func() {
		defer writer.Close()
		require.NoError(t, db.Snapshot(uint64(db.Version()), writer))
	}()

	restoreDir := t.TempDir()
	_, err := Import(restoreDir, uint64(db.Version()), 0, reader)
	require.NoError(t, err)

	db2, err := Load(restoreDir, Options{})
	require.NoError(t, err)
	require.Equal(t, db.LastCommitInfo(), db2.LastCommitInfo())
	require.Equal(t, db.Hash(), db2.Hash())

	// the imported db function normally
	_, _, err = db2.Commit(nil)
	require.NoError(t, err)
}

type protoReader struct {
	ch chan proto.Message
}

func (r *protoReader) ReadMsg(msg proto.Message) error {
	m, ok := <-r.ch
	if !ok {
		return io.EOF
	}
	proto.Merge(msg, m)
	return nil
}

type protoWriter struct {
	ch chan proto.Message
}

func (w *protoWriter) WriteMsg(msg proto.Message) error {
	w.ch <- msg
	return nil
}

func (w *protoWriter) Close() error {
	close(w.ch)
	return nil
}

func makeProtoIOPair() (protoio.Reader, protoio.WriteCloser) {
	ch := make(chan proto.Message)
	return &protoReader{ch}, &protoWriter{ch}
}
