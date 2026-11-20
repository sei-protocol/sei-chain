package bootstrap

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/statesync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

const (
	uploadChainID = "chain-a"
	uploadNodeID  = "node-a"
)

// uploadHome is a FlatKV root that writes a checkpoint every block, and a state sync proof dir.
type uploadHome struct {
	flatKVDir string
	proofDir  string
	store     *flatkv.CommitStore
}

func newUploadHome(t *testing.T) *uploadHome {
	t.Helper()
	dir := t.TempDir()
	h := &uploadHome{
		flatKVDir: filepath.Join(dir, "flatkv"),
		proofDir:  filepath.Join(dir, "state_sync_proofs"),
	}
	cfg := flatkvconfig.DefaultTestConfig(t)
	cfg.DataDir = h.flatKVDir
	cfg.SnapshotInterval = 1
	cfg.SnapshotKeepRecent = 3
	wal, err := flatkv.OpenStateWAL(cfg)
	require.NoError(t, err)
	h.store, err = flatkv.NewCommitStore(context.Background(), cfg, wal)
	require.NoError(t, err)
	require.NoError(t, h.store.LoadLatest())
	t.Cleanup(func() { _ = h.store.Close() })
	require.NoError(t, os.MkdirAll(h.proofDir, 0o750))
	return h
}

// commit commits one block and waits for its checkpoint. It returns the height of the block.
func (h *uploadHome) commit(t *testing.T) int64 {
	t.Helper()
	n := byte(h.store.Version() + 1)
	require.NoError(t, h.store.ApplyChangeSets(h.store.Version()+1, []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte{'b', n}, Value: []byte{n}}}}},
	}))
	v, err := h.store.Commit(h.store.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, h.store.FlushSnapshots())
	return v
}

func (h *uploadHome) writeProof(t *testing.T, height int64) []byte {
	t.Helper()
	proof := []byte(fmt.Sprintf("proof of %d", height))
	path := filepath.Join(h.proofDir, statesyncproof.FileName(types.RoadHeight(height))) //nolint:gosec // test heights are positive.
	require.NoError(t, os.WriteFile(path, proof, 0o600))
	return proof
}

func (h *uploadHome) uploader(store statesync.ObjectStore) *stateSyncUploader {
	return newStateSyncUploader(uploadConfig(store), h.flatKVDir, h.proofDir)
}

// uploadConfig is the upload of node uploadNodeID of chain uploadChainID to store, with no interval.
func uploadConfig(store statesync.ObjectStore) StateSyncUploadConfig {
	return StateSyncUploadConfig{Store: store, ChainID: uploadChainID, NodeID: uploadNodeID}
}

// fakeObject is one object of a fakeStore.
type fakeObject struct {
	data     []byte
	version  string
	modified time.Time
}

// fakeStore is an ObjectStore in memory. put, when set, runs before each Put and can fail it. now
// is the clock of the modified time of each write.
type fakeStore struct {
	mu       sync.Mutex
	objects  map[string]fakeObject
	versions int
	gets     int
	put      func(ctx context.Context, key string, body io.Reader) error
	now      func() time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{objects: map[string]fakeObject{}, now: time.Now}
}

func (s *fakeStore) Put(ctx context.Context, key string, body io.Reader, size int64, pre statesync.Precondition) error {
	if s.put != nil {
		if err := s.put(ctx, key, body); err != nil {
			return err
		}
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return fmt.Errorf("put %s: body is %d bytes, size is %d", key, len(data), size)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[key]
	if (pre.Absent && ok) || (pre.Version != "" && (!ok || obj.version != pre.Version)) {
		return fmt.Errorf("put %s: %w", key, statesync.ErrPreconditionFailed)
	}
	s.versions++
	s.objects[key] = fakeObject{data: data, version: fmt.Sprint(s.versions), modified: s.now()}
	return nil
}

func (s *fakeStore) Get(_ context.Context, key string) (io.ReadCloser, statesync.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	obj, ok := s.objects[key]
	if !ok {
		return nil, statesync.ObjectInfo{}, fmt.Errorf("%s: %w", key, statesync.ErrObjectNotFound)
	}
	return io.NopCloser(bytes.NewReader(obj.data)), statesync.ObjectInfo{Version: obj.version, Modified: obj.modified}, nil
}

func (s *fakeStore) Stat(_ context.Context, key string) (statesync.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[key]
	if !ok {
		return statesync.ObjectInfo{}, fmt.Errorf("%s: %w", key, statesync.ErrObjectNotFound)
	}
	return statesync.ObjectInfo{Version: obj.version, Modified: obj.modified}, nil
}

func (s *fakeStore) object(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[key]
	return obj.data, ok
}

// set stores data at key with no precondition, as another node would.
func (s *fakeStore) set(key string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions++
	s.objects[key] = fakeObject{data: data, version: fmt.Sprint(s.versions), modified: s.now()}
}

// requireArchive asserts that store holds the archive of height with proof, and latest.txt at
// height.
func requireArchive(t *testing.T, store *fakeStore, height int64, proof []byte) {
	t.Helper()
	data, ok := store.object(statesync.ArchiveKey(uploadChainID, height))
	require.True(t, ok, "no archive at %d", height)
	tr := tar.NewReader(bytes.NewReader(data))
	hdr, err := tr.Next()
	require.NoError(t, err)
	manifestData, err := io.ReadAll(tr)
	require.NoError(t, err)
	m, err := statesync.DecodeManifest(manifestData)
	require.NoError(t, err, hdr.Name)
	require.Equal(t, height, m.Height)
	require.Equal(t, uploadChainID, m.ChainID)
	_, err = tr.Next()
	require.NoError(t, err)
	gotProof, err := io.ReadAll(tr)
	require.NoError(t, err)
	require.Equal(t, proof, gotProof)

	latest, err := statesync.ReadLatest(t.Context(), store, uploadChainID)
	require.NoError(t, err)
	require.Equal(t, height, latest)
}

func TestStateSyncUploadUploadsNewestCheckpointWithProof(t *testing.T) {
	h := newUploadHome(t)
	h.commit(t)
	withProof := h.commit(t)
	proof := h.writeProof(t, withProof)
	h.commit(t)
	store := newFakeStore()
	u := h.uploader(store)

	require.NoError(t, u.sweep(t.Context()))

	requireArchive(t, store, withProof, proof)
	_, ok := store.object(statesync.ArchiveKey(uploadChainID, withProof-1))
	require.False(t, ok)
	require.NoDirExists(t, u.stagingDir)
}

func TestStateSyncUploadUploadsEachNewCheckpoint(t *testing.T) {
	h := newUploadHome(t)
	store := newFakeStore()
	u := h.uploader(store)
	first := h.commit(t)
	h.writeProof(t, first)
	require.NoError(t, u.sweep(t.Context()))

	second := h.commit(t)
	proof := h.writeProof(t, second)
	require.NoError(t, u.sweep(t.Context()))

	requireArchive(t, store, second, proof)
}

func TestStateSyncUploadDoesNothingWithoutAProof(t *testing.T) {
	h := newUploadHome(t)
	h.commit(t)
	store := newFakeStore()

	require.NoError(t, h.uploader(store).sweep(t.Context()))

	require.Empty(t, store.objects)
	require.Zero(t, store.gets)
}

func TestStateSyncUploadSkipsAHeightTheStoreHas(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	h.writeProof(t, height)
	store := newFakeStore()
	store.set(statesync.LatestKey(uploadChainID), []byte(fmt.Sprintf("%d\n", height+5)))
	u := h.uploader(store)

	require.NoError(t, u.sweep(t.Context()))
	require.NoError(t, u.sweep(t.Context()))

	_, ok := store.object(statesync.ArchiveKey(uploadChainID, height))
	require.False(t, ok)
	require.Equal(t, 1, store.gets, "the second sweep must use the cached height")
}

func TestStateSyncUploadRetriesAFailedUpload(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	proof := h.writeProof(t, height)
	store := newFakeStore()
	store.put = func(_ context.Context, key string, _ io.Reader) error {
		if key == statesync.ArchiveKey(uploadChainID, height) {
			return errors.New("s3 is down")
		}
		return nil
	}
	u := h.uploader(store)

	require.ErrorContains(t, u.sweep(t.Context()), "s3 is down")
	_, err := statesync.ReadLatest(t.Context(), store, uploadChainID)
	require.ErrorIs(t, err, statesync.ErrObjectNotFound)
	require.NoDirExists(t, u.stagingDir)
	_, claimed := store.object(statesync.ClaimKey(uploadChainID))
	require.True(t, claimed, "the node keeps its claim of the round")

	store.put = nil
	require.NoError(t, u.sweep(t.Context()))
	requireArchive(t, store, height, proof)
}

func TestStateSyncUploadWritesLatestOnlyAfterTheArchive(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	h.writeProof(t, height)
	store := newFakeStore()
	archiveKey := statesync.ArchiveKey(uploadChainID, height)
	store.put = func(_ context.Context, key string, _ io.Reader) error {
		if key == statesync.LatestKey(uploadChainID) {
			_, ok := store.object(archiveKey)
			require.True(t, ok, "latest.txt written before the archive")
		}
		return nil
	}

	require.NoError(t, h.uploader(store).sweep(t.Context()))
}

func TestStateSyncUploadCompletesWhenTheCheckpointIsPruned(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	proof := h.writeProof(t, height)
	store := newFakeStore()
	store.put = func(_ context.Context, key string, _ io.Reader) error {
		if key == statesync.ArchiveKey(uploadChainID, height) {
			return os.RemoveAll(filepath.Join(h.flatKVDir, flatkv.SnapshotDirName(height)))
		}
		return nil
	}

	require.NoError(t, h.uploader(store).sweep(t.Context()))

	require.NoDirExists(t, filepath.Join(h.flatKVDir, flatkv.SnapshotDirName(height)))
	requireArchive(t, store, height, proof)
}

func TestStateSyncUploadReportsADamagedCheckpoint(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	h.writeProof(t, height)
	manifests, err := filepath.Glob(filepath.Join(h.flatKVDir, flatkv.SnapshotDirName(height), "account", "MANIFEST-*"))
	require.NoError(t, err)
	require.NotEmpty(t, manifests)
	for _, m := range manifests {
		require.NoError(t, os.Remove(m))
	}
	store := newFakeStore()
	u := h.uploader(store)

	require.Error(t, u.sweep(t.Context()))

	_, ok := store.object(statesync.ArchiveKey(uploadChainID, height))
	require.False(t, ok)
	require.NoDirExists(t, u.stagingDir)
}

func TestStateSyncUploadStageReportsAPrunedCheckpoint(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	h.writeProof(t, height)
	u := h.uploader(newFakeStore())
	require.NoError(t, os.Remove(u.proofPath(height)))

	_, err := u.stage(height)
	require.ErrorIs(t, err, errCheckpointPruned)

	require.NoError(t, os.RemoveAll(filepath.Join(h.flatKVDir, flatkv.SnapshotDirName(height))))
	_, err = u.stage(height)
	require.ErrorIs(t, err, errCheckpointPruned)
}

func TestStateSyncUploadKeepsAHigherLatestWrittenDuringTheUpload(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	h.writeProof(t, height)
	store := newFakeStore()
	archiveKey := statesync.ArchiveKey(uploadChainID, height)
	store.put = func(_ context.Context, key string, _ io.Reader) error {
		if key == archiveKey {
			store.set(statesync.LatestKey(uploadChainID), []byte(fmt.Sprintf("%d\n", height+5)))
		}
		return nil
	}
	u := h.uploader(store)

	require.NoError(t, u.sweep(t.Context()))

	_, ok := store.object(archiveKey)
	require.True(t, ok)
	latest, err := statesync.ReadLatest(t.Context(), store, uploadChainID)
	require.NoError(t, err)
	require.Equal(t, height+5, latest)
	require.Equal(t, height+5, u.uploaded)
}

func TestStateSyncUploadKeepsAnArchiveTheStoreHas(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	h.writeProof(t, height)
	store := newFakeStore()
	archiveKey := statesync.ArchiveKey(uploadChainID, height)
	store.set(archiveKey, []byte("archive of another node"))
	store.put = func(_ context.Context, key string, _ io.Reader) error {
		if key == archiveKey {
			return errors.New("the archive is sent again")
		}
		return nil
	}
	u := h.uploader(store)

	require.NoError(t, u.sweep(t.Context()))

	data, _ := store.object(archiveKey)
	require.Equal(t, "archive of another node", string(data))
	latest, err := statesync.ReadLatest(t.Context(), store, uploadChainID)
	require.NoError(t, err)
	require.Equal(t, height, latest)
	require.Equal(t, height, u.uploaded)
	require.NoDirExists(t, u.stagingDir)
}

func TestStateSyncUploadKeepsAnArchiveWrittenDuringTheUpload(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	h.writeProof(t, height)
	store := newFakeStore()
	archiveKey := statesync.ArchiveKey(uploadChainID, height)
	store.put = func(_ context.Context, key string, _ io.Reader) error {
		if key == archiveKey {
			store.set(archiveKey, []byte("archive of another node"))
		}
		return nil
	}

	require.NoError(t, h.uploader(store).sweep(t.Context()))

	data, _ := store.object(archiveKey)
	require.Equal(t, "archive of another node", string(data))
	latest, err := statesync.ReadLatest(t.Context(), store, uploadChainID)
	require.NoError(t, err)
	require.Equal(t, height, latest)
}

func TestStateSyncUploadDoesNotRecordAnArchiveInAWriteConflict(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	h.writeProof(t, height)
	store := newFakeStore()
	archiveKey := statesync.ArchiveKey(uploadChainID, height)
	store.put = func(_ context.Context, key string, _ io.Reader) error {
		if key == archiveKey {
			return fmt.Errorf("put %s: %w", key, statesync.ErrWriteConflict)
		}
		return nil
	}

	require.ErrorIs(t, h.uploader(store).sweep(t.Context()), statesync.ErrWriteConflict)

	_, err := statesync.ReadLatest(t.Context(), store, uploadChainID)
	require.ErrorIs(t, err, statesync.ErrObjectNotFound)
}

func TestOpenRemovesAStateSyncUploadStaging(t *testing.T) {
	cfg := newHome(t, nil)
	staging := stateSyncUploadStagingDir(cfg.FlatKVConfig.DataDir)
	require.NoError(t, os.MkdirAll(filepath.Join(staging, "account"), 0o750))

	startManager(t, cfg)

	require.NoDirExists(t, staging)
}

func TestStateSyncUploadRemovesAnEarlierStaging(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	proof := h.writeProof(t, height)
	store := newFakeStore()
	u := h.uploader(store)
	require.NoError(t, os.MkdirAll(filepath.Join(u.stagingDir, "account"), 0o750))

	require.NoError(t, u.sweep(t.Context()))

	requireArchive(t, store, height, proof)
}

func TestStateSyncUploadStagingIsNotACheckpoint(t *testing.T) {
	h := newUploadHome(t)
	h.commit(t)
	want, err := flatkv.SnapshotVersions(h.flatKVDir)
	require.NoError(t, err)
	u := h.uploader(newFakeStore())
	require.NoError(t, os.MkdirAll(filepath.Join(u.stagingDir, "account"), 0o750))

	got, err := flatkv.SnapshotVersions(h.flatKVDir)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestStateSyncUploadStopCancelsAnUpload(t *testing.T) {
	h := newUploadHome(t)
	height := h.commit(t)
	h.writeProof(t, height)
	store := newFakeStore()
	started := make(chan struct{})
	store.put = func(ctx context.Context, key string, _ io.Reader) error {
		if key != statesync.ArchiveKey(uploadChainID, height) {
			return nil
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	u := h.uploader(store)
	var upload stateSyncUpload
	require.NoError(t, upload.start(func(ctx context.Context) { u.run(ctx, time.Hour) }))
	<-started

	upload.stop()

	_, err := statesync.ReadLatest(t.Context(), store, uploadChainID)
	require.ErrorIs(t, err, statesync.ErrObjectNotFound)
	require.NoDirExists(t, u.stagingDir)
}

func TestStateSyncUploadReadsTheStoreAtStart(t *testing.T) {
	h := newUploadHome(t)
	store := newFakeStore()
	u := h.uploader(store)
	var upload stateSyncUpload
	require.NoError(t, upload.start(func(ctx context.Context) { u.run(ctx, time.Hour) }))
	defer upload.stop()

	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.gets == 1
	}, 10*time.Second, 10*time.Millisecond, "no checkpoint, but the loop must read latest.txt at start")
}

func TestStartStateSyncUploadTwiceFails(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.NoError(t, manager.StartStateSyncUpload(uploadConfig(newFakeStore())))
	require.Error(t, manager.StartStateSyncUpload(uploadConfig(newFakeStore())))
}

func TestStartStateSyncUploadAfterCloseFails(t *testing.T) {
	manager, err := NewGigaStorageManager(context.Background(), newHome(t, nil))
	require.NoError(t, err)
	require.NoError(t, manager.Close())

	require.Error(t, manager.StartStateSyncUpload(uploadConfig(newFakeStore())))
}

func TestStartStateSyncUploadRejectsAnEmptyChainID(t *testing.T) {
	manager, _ := openManager(t, nil)
	cfg := uploadConfig(newFakeStore())
	cfg.ChainID = ""

	require.Error(t, manager.StartStateSyncUpload(cfg))
}

func TestStartStateSyncUploadRejectsAnEmptyNodeID(t *testing.T) {
	manager, _ := openManager(t, nil)
	cfg := uploadConfig(newFakeStore())
	cfg.NodeID = ""

	require.Error(t, manager.StartStateSyncUpload(cfg))
}

func TestStateSyncUploadStopIsIdempotent(t *testing.T) {
	var upload stateSyncUpload
	require.NoError(t, upload.start(func(ctx context.Context) { <-ctx.Done() }))

	upload.stop()
	upload.stop()
}

// fixedClock returns a clock at start that advance moves forward.
func fixedClock(start time.Time) (now func() time.Time, advance func(time.Duration)) {
	var mu sync.Mutex
	t := start
	now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return t
	}
	advance = func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		t = t.Add(d)
	}
	return now, advance
}

func TestStateSyncUploadWaitsForTheInterval(t *testing.T) {
	h := newUploadHome(t)
	now, advance := fixedClock(time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	store := newFakeStore()
	store.now = now
	cfg := uploadConfig(store)
	cfg.Interval = time.Hour
	u := newStateSyncUploader(cfg, h.flatKVDir, h.proofDir)
	u.now = now
	first := h.commit(t)
	h.writeProof(t, first)
	require.NoError(t, u.sweep(t.Context()))
	second := h.commit(t)
	proof := h.writeProof(t, second)

	advance(time.Hour - time.Second)
	require.NoError(t, u.sweep(t.Context()))
	_, ok := store.object(statesync.ArchiveKey(uploadChainID, second))
	require.False(t, ok, "the last upload of the chain is less than the interval ago")

	advance(time.Second)
	require.NoError(t, u.sweep(t.Context()))
	requireArchive(t, store, second, proof)
}

func TestStateSyncUploadLeavesARoundClaimedByAnotherNode(t *testing.T) {
	h := newUploadHome(t)
	now, advance := fixedClock(time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	store := newFakeStore()
	store.now = now
	store.set(statesync.ClaimKey(uploadChainID), []byte("0 node-b\n"))
	u := h.uploader(store)
	u.now = now
	height := h.commit(t)
	proof := h.writeProof(t, height)

	require.NoError(t, u.sweep(t.Context()))
	_, ok := store.object(statesync.ArchiveKey(uploadChainID, height))
	require.False(t, ok, "node-b holds the claim of the round")

	advance(stateSyncUploadClaimTimeout)
	require.NoError(t, u.sweep(t.Context()))
	requireArchive(t, store, height, proof)
	claim, _ := store.object(statesync.ClaimKey(uploadChainID))
	require.Equal(t, "0 node-a\n", string(claim))
}

func TestStateSyncUploadClaimsEachRound(t *testing.T) {
	h := newUploadHome(t)
	store := newFakeStore()
	u := h.uploader(store)
	first := h.commit(t)
	h.writeProof(t, first)
	require.NoError(t, u.sweep(t.Context()))
	claim, _ := store.object(statesync.ClaimKey(uploadChainID))
	require.Equal(t, "0 node-a\n", string(claim))

	other := newStateSyncUploader(StateSyncUploadConfig{Store: store, ChainID: uploadChainID, NodeID: "node-b"}, h.flatKVDir, h.proofDir)
	second := h.commit(t)
	proof := h.writeProof(t, second)
	require.NoError(t, other.sweep(t.Context()))

	requireArchive(t, store, second, proof)
	claim, _ = store.object(statesync.ClaimKey(uploadChainID))
	require.Equal(t, fmt.Sprintf("%d node-b\n", first), string(claim))
}

func TestSweepDelay(t *testing.T) {
	require.Equal(t, 30*time.Second, sweepDelay(30*time.Second, 0))
	require.Equal(t, time.Minute, sweepDelay(30*time.Second, 1))
	require.Equal(t, 8*time.Minute, sweepDelay(30*time.Second, 4))
	require.Equal(t, stateSyncUploadMaxBackoff, sweepDelay(30*time.Second, 6))
	require.Equal(t, stateSyncUploadMaxBackoff, sweepDelay(30*time.Second, 1000))
}
