package dbsync

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/tendermint/tendermint/config"
	sm "github.com/tendermint/tendermint/internal/state"
	"github.com/tendermint/tendermint/libs/log"
	dstypes "github.com/tendermint/tendermint/proto/tendermint/dbsync"
	"github.com/tendermint/tendermint/types"
)

const ApplicationDBSubdirectory = "application.db"

// TODO: this is bad as TM shouldn't be aware of wasm. DB sync/restore logic should ideally happen
// on application-level (i.e. Cosmos layer) and communicate to TM via new ABCI methods
const WasmDirectory = "wasm/wasm/state/wasm"
const WasmSuffix = "_wasm"
const LockFile = "LOCK"

type Syncer struct {
	mtx    *sync.RWMutex
	logger log.Logger

	active                 bool
	heightToSync           uint64
	peersToSync            []types.NodeID
	expectedChecksums      map[string][]byte
	pendingFiles           map[string]struct{}
	syncedFiles            map[string]struct{}
	completionSignals      map[string]chan struct{}
	metadataSetAt          time.Time
	timeoutInSeconds       time.Duration
	fileQueue              []*dstypes.FileResponse
	applicationDBDirectory string
	wasmStateDirectory     string
	sleepInSeconds         time.Duration
	fileWorkerCount        int
	fileWorkerTimeout      time.Duration
	fileWorkerCancelFn     context.CancelFunc

	metadataRequestFn func(context.Context) error
	fileRequestFn     func(context.Context, types.NodeID, uint64, string) error
	commitStateFn     func(context.Context, uint64) (sm.State, *types.Commit, error)
	postSyncFn        func(context.Context, sm.State, *types.Commit) error
	resetDirFn        func(*Syncer)

	state  sm.State
	commit *types.Commit
}

func defaultResetDirFn(s *Syncer) {
	os.RemoveAll(s.applicationDBDirectory)
	os.MkdirAll(s.applicationDBDirectory, fs.ModePerm)
	os.RemoveAll(s.wasmStateDirectory)
	os.MkdirAll(s.wasmStateDirectory, fs.ModePerm)
}

func NewSyncer(
	logger log.Logger,
	dbsyncConfig config.DBSyncConfig,
	baseConfig config.BaseConfig,
	enable bool,
	metadataRequestFn func(context.Context) error,
	fileRequestFn func(context.Context, types.NodeID, uint64, string) error,
	commitStateFn func(context.Context, uint64) (sm.State, *types.Commit, error),
	postSyncFn func(context.Context, sm.State, *types.Commit) error,
	resetDirFn func(*Syncer),
) *Syncer {
	return &Syncer{
		logger:                 logger,
		active:                 enable,
		timeoutInSeconds:       time.Duration(dbsyncConfig.TimeoutInSeconds) * time.Second,
		fileQueue:              []*dstypes.FileResponse{},
		applicationDBDirectory: path.Join(baseConfig.DBDir(), ApplicationDBSubdirectory),
		wasmStateDirectory:     path.Join(baseConfig.RootDir, WasmDirectory),
		sleepInSeconds:         time.Duration(dbsyncConfig.NoFileSleepInSeconds) * time.Second,
		fileWorkerCount:        dbsyncConfig.FileWorkerCount,
		fileWorkerTimeout:      time.Duration(dbsyncConfig.FileWorkerTimeout) * time.Second,
		metadataRequestFn:      metadataRequestFn,
		fileRequestFn:          fileRequestFn,
		commitStateFn:          commitStateFn,
		postSyncFn:             postSyncFn,
		resetDirFn:             resetDirFn,
		mtx:                    &sync.RWMutex{},
	}
}

func (s *Syncer) SetMetadata(ctx context.Context, sender types.NodeID, metadata *dstypes.MetadataResponse) {
	s.mtx.RLock()

	if !s.active {
		s.mtx.RUnlock()
		return
	}
	s.mtx.RUnlock()

	if len(metadata.Filenames) != len(metadata.Md5Checksum) {
		s.logger.Error("received bad metadata with inconsistent files and checksums count")
		return
	}

	timedOut, now := s.isCurrentMetadataTimedOut()
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if timedOut {
		if s.fileWorkerCancelFn != nil {
			s.fileWorkerCancelFn()
		}

		state, commit, err := s.commitStateFn(ctx, metadata.Height)
		if err != nil {
			return
		}
		s.state = state
		s.commit = commit
		s.metadataSetAt = now
		s.heightToSync = metadata.Height
		s.expectedChecksums = map[string][]byte{}
		s.syncedFiles = map[string]struct{}{}
		s.pendingFiles = map[string]struct{}{}
		s.completionSignals = map[string]chan struct{}{}
		for i, filename := range metadata.Filenames {
			if filename == LockFile {
				// ignore lockfile
				continue
			}
			s.expectedChecksums[filename] = metadata.Md5Checksum[i]
		}
		s.fileQueue = []*dstypes.FileResponse{}
		s.peersToSync = []types.NodeID{sender}
		s.resetDirFn(s)

		cancellableCtx, cancel := context.WithCancel(ctx)
		s.fileWorkerCancelFn = cancel
		s.requestFiles(cancellableCtx, s.metadataSetAt)
	} else if metadata.Height == s.heightToSync {
		s.peersToSync = append(s.peersToSync, sender)
	}
}

func (s *Syncer) Process(ctx context.Context) {
	for {
		s.mtx.RLock()
		if !s.active {
			s.mtx.RUnlock()
			break
		}
		s.mtx.RUnlock()
		timedOut, _ := s.isCurrentMetadataTimedOut()
		if timedOut {
			s.logger.Info(fmt.Sprintf("last metadata has timed out; sleeping for %f seconds", s.sleepInSeconds.Seconds()))
			s.metadataRequestFn(ctx)
			time.Sleep(s.sleepInSeconds)
			continue
		}
		file := s.popFile()
		if file == nil {
			s.mtx.RLock()
			numSynced := len(s.syncedFiles)
			numTotal := len(s.expectedChecksums)
			s.mtx.RUnlock()
			s.logger.Info(fmt.Sprintf("no file to sync; sync'ed %d out of %d so far; sleeping for %f seconds", numSynced, numTotal, s.sleepInSeconds.Seconds()))
			time.Sleep(s.sleepInSeconds)
			continue
		}
		if err := s.processFile(ctx, file); err != nil {
			s.logger.Error(err.Error())
		}
	}
}

func (s *Syncer) Stop() {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.active {
		s.resetDirFn(s)
		s.active = false
	}
}

func (s *Syncer) processFile(ctx context.Context, file *dstypes.FileResponse) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	defer func() {
		delete(s.pendingFiles, file.Filename)
	}()

	if file.Height != s.heightToSync {
		return fmt.Errorf("current height is %d but received file for height %d", s.heightToSync, file.Height)
	}

	if expectedChecksum, ok := s.expectedChecksums[file.Filename]; !ok {
		return fmt.Errorf("received unexpected file %s", file.Filename)
	} else if _, ok := s.syncedFiles[file.Filename]; ok {
		return fmt.Errorf("received duplicate file %s", file.Filename)
	} else if _, ok := s.pendingFiles[file.Filename]; !ok {
		return fmt.Errorf("received unrequested file %s", file.Filename)
	} else {
		checkSum := md5.Sum(file.Data)
		if !bytes.Equal(checkSum[:], expectedChecksum) {
			return errors.New("received unexpected checksum")
		}
	}

	var dbFile *os.File
	var err error
	if strings.HasSuffix(file.Filename, WasmSuffix) {
		dbFile, err = os.Create(path.Join(s.wasmStateDirectory, strings.TrimSuffix(file.Filename, WasmSuffix)))
	} else {
		dbFile, err = os.Create(path.Join(s.applicationDBDirectory, file.Filename))
	}
	if err != nil {
		return err
	}
	defer dbFile.Close()
	_, err = dbFile.Write(file.Data)
	if err != nil {
		return err
	}

	s.syncedFiles[file.Filename] = struct{}{}
	if len(s.syncedFiles) == len(s.expectedChecksums) {
		// we have finished syncing
		if err := s.postSyncFn(ctx, s.state, s.commit); err != nil {
			// no graceful way to handle postsync error since we might be in a partially updated state
			panic(err)
		}
		s.active = false
	}
	s.completionSignals[file.Filename] <- struct{}{}
	return nil
}

func (s *Syncer) isCurrentMetadataTimedOut() (bool, time.Time) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	now := time.Now()
	if s.metadataSetAt.IsZero() {
		return true, now
	}
	return now.After(s.metadataSetAt.Add(s.timeoutInSeconds)), now
}

func (s *Syncer) requestFiles(ctx context.Context, metadataSetAt time.Time) {
	worker := func() {
		for {
			s.mtx.Lock()
			if metadataSetAt != s.metadataSetAt {
				s.mtx.Unlock()
				break
			}
			if len(s.expectedChecksums) == len(s.pendingFiles)+len(s.syncedFiles) {
				// even if there are still pending items, there should be enough
				// workers to handle them given one worker can have at most one
				// pending item at a time
				s.mtx.Unlock()
				break
			}
			var picked string
			for filename := range s.expectedChecksums {
				_, pending := s.pendingFiles[filename]
				_, synced := s.syncedFiles[filename]
				if pending || synced {
					continue
				}
				picked = filename
				break
			}
			s.pendingFiles[picked] = struct{}{}
			completionSignal := make(chan struct{}, 1)
			s.completionSignals[picked] = completionSignal
			s.fileRequestFn(ctx, s.peersToSync[0], s.heightToSync, picked)
			s.mtx.Unlock()

			ticker := time.NewTicker(s.fileWorkerTimeout)
			defer ticker.Stop()

			select {
			case <-completionSignal:

			case <-ticker.C:
				s.mtx.Lock()
				delete(s.pendingFiles, picked)
				s.mtx.Unlock()

			case <-ctx.Done():
				return
			}

			ticker.Stop()
		}
	}
	for i := 0; i < s.fileWorkerCount; i++ {
		go worker()
	}
}

func (s *Syncer) popFile() *dstypes.FileResponse {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if len(s.fileQueue) == 0 {
		return nil
	}

	file := s.fileQueue[0]
	s.fileQueue = s.fileQueue[1:]
	return file
}

func (s *Syncer) PushFile(file *dstypes.FileResponse) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.fileQueue = append(s.fileQueue, file)
}
