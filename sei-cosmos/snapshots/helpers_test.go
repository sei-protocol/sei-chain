package snapshots_test

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"golang.org/x/sync/errgroup"

	protoio "github.com/gogo/protobuf/io"
	"github.com/sei-protocol/sei-chain/sei-cosmos/snapshots"
	"github.com/sei-protocol/sei-chain/sei-cosmos/snapshots/types"
	snapshottypes "github.com/sei-protocol/sei-chain/sei-cosmos/snapshots/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/stretchr/testify/require"
	db "github.com/tendermint/tm-db"
)

func checksums(slice [][]byte) [][]byte {
	hasher := sha256.New()
	checksums := make([][]byte, len(slice))
	for i, chunk := range slice {
		hasher.Write(chunk)
		checksums[i] = hasher.Sum(nil)
		hasher.Reset()
	}
	return checksums
}

func hash(chunks [][]byte) []byte {
	hasher := sha256.New()
	for _, chunk := range chunks {
		hasher.Write(chunk)
	}
	return hasher.Sum(nil)
}

func makeChunks(chunks [][]byte) <-chan io.ReadCloser {
	ch := make(chan io.ReadCloser, len(chunks))
	for _, chunk := range chunks {
		ch <- io.NopCloser(bytes.NewReader(chunk))
	}
	close(ch)
	return ch
}

func readChunks(chunks <-chan io.ReadCloser) [][]byte {
	bodies := [][]byte{}
	for chunk := range chunks {
		body, err := io.ReadAll(chunk)
		if err != nil {
			panic(err)
		}
		bodies = append(bodies, body)
	}
	return bodies
}

// snapshotItems serialize a array of bytes as SnapshotItem_ExtensionPayload, and return the chunks.
func snapshotItems(items [][]byte) [][]byte {
	// copy the same parameters from the code
	snapshotChunkSize := uint64(10e6)
	snapshotBufferSize := int(snapshotChunkSize)

	ch := make(chan io.ReadCloser)
	go func() {
		chunkWriter := snapshots.NewChunkWriter(ch, snapshotChunkSize)
		bufWriter := bufio.NewWriterSize(chunkWriter, snapshotBufferSize)
		zWriter, _ := zlib.NewWriterLevel(bufWriter, 7)
		protoWriter := protoio.NewDelimitedWriter(zWriter)
		for _, item := range items {
			types.WriteExtensionItem(protoWriter, item)
		}
		_ = protoWriter.Close()
		_ = zWriter.Close()
		_ = bufWriter.Flush()
		_ = chunkWriter.Close()
	}()

	var chunks [][]byte
	for chunkBody := range ch {
		chunk, err := io.ReadAll(chunkBody)
		if err != nil {
			panic(err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

type mockSnapshotter struct {
	items [][]byte
}

func (m *mockSnapshotter) Restore(
	height uint64, format uint32, protoReader protoio.Reader,
) (snapshottypes.SnapshotItem, error) {
	if format == 0 {
		return snapshottypes.SnapshotItem{}, types.ErrUnknownFormat
	}
	if m.items != nil {
		return snapshottypes.SnapshotItem{}, errors.New("already has contents")
	}

	m.items = [][]byte{}
	for {
		item := &snapshottypes.SnapshotItem{}
		err := protoReader.ReadMsg(item)
		if err == io.EOF {
			break
		} else if err != nil {
			return snapshottypes.SnapshotItem{}, sdkerrors.Wrap(err, "invalid protobuf message")
		}
		payload := item.GetExtensionPayload()
		if payload == nil {
			return snapshottypes.SnapshotItem{}, sdkerrors.Wrap(err, "invalid protobuf message")
		}
		m.items = append(m.items, payload.Payload)
	}

	return snapshottypes.SnapshotItem{}, nil
}

func (m *mockSnapshotter) Snapshot(height uint64, protoWriter protoio.Writer) error {
	for _, item := range m.items {
		if err := types.WriteExtensionItem(protoWriter, item); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockSnapshotter) SnapshotFormat() uint32 {
	return 1
}

func (m *mockSnapshotter) SupportedFormats() []uint32 {
	return []uint32{1}
}

// setupBusyManager creates a manager with an empty store that is busy creating a snapshot at height 1.
// The snapshot will complete when cleanup runs.
func setupBusyManager(t *testing.T) *snapshots.Manager {
	t.Helper()

	tempdir := t.TempDir()
	store, err := snapshots.NewStore(db.NewMemDB(), tempdir)
	require.NoError(t, err)

	started := make(chan struct{})
	hung := newHungSnapshotter(started)
	mgr := snapshots.NewManager(store, hung, log.NewNopLogger())

	var eg errgroup.Group
	eg.Go(func() error {
		_, err := mgr.Create(1)
		return err
	})

	<-started // ensure snapshot op is actually in progress before returning

	t.Cleanup(func() {
		hung.Close() // unblock Snapshot/Create
		require.NoError(t, eg.Wait())
		require.NoError(t, mgr.Close())
	})

	return mgr
}

// hungSnapshotter can be used to test operations in progress. Call close to end the snapshot.
type hungSnapshotter struct {
	ch      chan struct{}
	started chan struct{}

	startedOnce sync.Once
	closeOnce   sync.Once
}

func newHungSnapshotter(started chan struct{}) *hungSnapshotter {
	return &hungSnapshotter{
		ch:      make(chan struct{}),
		started: started,
	}
}

func (m *hungSnapshotter) Close() {
	m.closeOnce.Do(func() {
		close(m.ch)
	})
}

func (m *hungSnapshotter) Snapshot(height uint64, protoWriter protoio.Writer) error {
	m.startedOnce.Do(func() {
		close(m.started)
	})

	<-m.ch
	return nil
}

func (m *hungSnapshotter) Restore(uint64, uint32, protoio.Reader) (snapshottypes.SnapshotItem, error) {
	panic("not implemented")
}
