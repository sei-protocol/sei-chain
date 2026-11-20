package statesync

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

const testChainID = "chain-a"

// openTestStore opens a FlatKV store on root that writes a checkpoint every block.
func openTestStore(t *testing.T, root string) *flatkv.CommitStore {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = root
	cfg.SnapshotInterval = 1
	cfg.SnapshotKeepRecent = 2
	wal, err := flatkv.OpenStateWAL(cfg)
	require.NoError(t, err)
	s, err := flatkv.NewCommitStore(context.Background(), cfg, wal)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// commitBlock commits one block that writes to all four FlatKV databases and to the bank module,
// and waits for its checkpoint. It returns the height of the block.
func commitBlock(t *testing.T, s *flatkv.CommitStore, n byte) int64 {
	t.Helper()
	var addr ktype.Address
	addr[19] = n
	var slot ktype.Slot
	slot[31] = n
	nonce := make([]byte, vtype.NonceLen)
	binary.BigEndian.PutUint64(nonce, uint64(n))
	var codeHash vtype.CodeHash
	codeHash[0] = n
	value := make([]byte, 32)
	value[31] = n
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: nonce},
			{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]), Value: codeHash[:]},
			{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr[:]), Value: []byte{0x60, n}},
			{Key: keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)), Value: value},
		}}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte{'b', n}, Value: []byte{n}}}}},
	}))
	v, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, s.FlushSnapshots())
	return v
}

// stagedCheckpoint commits two blocks to a new store and returns a hard-linked copy of the
// checkpoint of the second block, and its height.
func stagedCheckpoint(t *testing.T) (string, int64) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "flatkv")
	s := openTestStore(t, root)
	commitBlock(t, s, 1)
	height := commitBlock(t, s, 2)
	staged := filepath.Join(filepath.Dir(root), "staged")
	require.NoError(t, flatkv.HardLinkSnapshot(root, height, staged))
	return staged, height
}

// memObject is one object of a memStore.
type memObject struct {
	data     []byte
	version  string
	modified time.Time
}

// memStore is an ObjectStore in memory. beforePut, when set, runs at the start of each Put. now is
// the clock of the modified time of each Put.
type memStore struct {
	mu        sync.Mutex
	objects   map[string]memObject
	versions  int
	beforePut func(key string)
	now       func() time.Time
}

func newMemStore() *memStore {
	return &memStore{objects: map[string]memObject{}, now: time.Now}
}

func (s *memStore) Put(_ context.Context, key string, body io.Reader, size int64, pre Precondition) error {
	if s.beforePut != nil {
		s.beforePut(key)
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
	if err := s.check(key, pre); err != nil {
		return err
	}
	s.versions++
	s.objects[key] = memObject{data: data, version: fmt.Sprint(s.versions), modified: s.now()}
	return nil
}

// check returns an error wrapping ErrPreconditionFailed when the object at key does not meet pre.
func (s *memStore) check(key string, pre Precondition) error {
	obj, ok := s.objects[key]
	if (pre.Absent && ok) || (pre.Version != "" && (!ok || obj.version != pre.Version)) {
		return fmt.Errorf("put %s: %w", key, ErrPreconditionFailed)
	}
	return nil
}

func (s *memStore) Get(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[key]
	if !ok {
		return nil, ObjectInfo{}, fmt.Errorf("%s: %w", key, ErrObjectNotFound)
	}
	return io.NopCloser(bytes.NewReader(obj.data)), ObjectInfo{Version: obj.version, Modified: obj.modified}, nil
}

// putLatest stores content as the latest.txt object of the test chain.
func (s *memStore) putLatest(t *testing.T, content string) {
	t.Helper()
	require.NoError(t, s.Put(t.Context(), LatestKey(testChainID), strings.NewReader(content), int64(len(content)), Precondition{}))
}
