package dbsync

import (
	"context"
	"crypto/md5"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/internal/state"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proto/tendermint/dbsync"
	"github.com/tendermint/tendermint/types"
)

func getTestSyncer(t *testing.T) *Syncer {
	baseConfig := config.DefaultBaseConfig()
	dbsyncConfig := config.DefaultDBSyncConfig()
	dbsyncConfig.TimeoutInSeconds = 99999
	dbsyncConfig.NoFileSleepInSeconds = 5
	dbsyncConfig.FileWorkerTimeout = 10
	syncer := NewSyncer(
		log.NewNopLogger(),
		*dbsyncConfig,
		baseConfig,
		true,
		func(ctx context.Context) error { return nil },
		func(ctx context.Context, ni types.NodeID, u uint64, s string) error { return nil },
		func(ctx context.Context, u uint64) (state.State, *types.Commit, error) {
			return state.State{}, nil, nil
		},
		func(ctx context.Context, s state.State, c *types.Commit) error { return nil },
		func(s *Syncer) {
			s.applicationDBDirectory = t.TempDir()
			s.wasmStateDirectory = t.TempDir()
		},
	)
	syncer.active = true
	syncer.applicationDBDirectory = t.TempDir()
	syncer.wasmStateDirectory = t.TempDir()
	return syncer
}

func TestSetMetadata(t *testing.T) {
	syncer := getTestSyncer(t)
	// initial
	syncer.SetMetadata(context.Background(), types.NodeID("someone"), &dbsync.MetadataResponse{
		Height:      1,
		Hash:        []byte("hash"),
		Filenames:   []string{"f1"},
		Md5Checksum: [][]byte{[]byte("sum")},
	})
	syncer.fileWorkerCancelFn()
	require.Equal(t, uint64(1), syncer.heightToSync)
	require.NotNil(t, syncer.metadataSetAt)
	require.Equal(t, 1, len(syncer.expectedChecksums))
	require.Equal(t, 1, len(syncer.peersToSync))

	// second time
	syncer.SetMetadata(context.Background(), types.NodeID("someone else"), &dbsync.MetadataResponse{
		Height:      1,
		Hash:        []byte("hash"),
		Filenames:   []string{"f1"},
		Md5Checksum: [][]byte{[]byte("sum")},
	})
	require.Equal(t, uint64(1), syncer.heightToSync)
	require.NotNil(t, syncer.metadataSetAt)
	require.Equal(t, 1, len(syncer.expectedChecksums))
	require.Equal(t, 2, len(syncer.peersToSync))
}

func TestFileProcessHappyPath(t *testing.T) {
	// successful process
	syncer := getTestSyncer(t)
	data := []byte("data")
	sum := md5.Sum(data)
	syncer.SetMetadata(context.Background(), types.NodeID("someone"), &dbsync.MetadataResponse{
		Height:      1,
		Hash:        []byte("hash"),
		Filenames:   []string{"f1"},
		Md5Checksum: [][]byte{sum[:]},
	})
	for {
		syncer.mtx.RLock()
		_, ok := syncer.pendingFiles["f1"]
		syncer.mtx.RUnlock()
		if ok {
			break
		}
	}
	syncer.PushFile(&dbsync.FileResponse{
		Height:   1,
		Filename: "f1",
		Data:     data,
	})
	syncer.Process(context.Background())
}

func TestFileProcessTimeoutReprocess(t *testing.T) {
	// successful process
	syncer := getTestSyncer(t)
	data := []byte("data")
	sum := md5.Sum(data)
	syncer.SetMetadata(context.Background(), types.NodeID("someone"), &dbsync.MetadataResponse{
		Height:      1,
		Hash:        []byte("hash"),
		Filenames:   []string{"f1"},
		Md5Checksum: [][]byte{sum[:]},
	})
	for {
		syncer.mtx.RLock()
		_, ok := syncer.pendingFiles["f1"]
		syncer.mtx.RUnlock()
		if ok {
			break
		}
	}
	time.Sleep(syncer.fileWorkerTimeout + time.Second) // add some padding
	syncer.PushFile(&dbsync.FileResponse{
		Height:   1,
		Filename: "f1",
		Data:     data,
	})
	syncer.Process(context.Background())
}
