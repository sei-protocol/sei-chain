package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

type discardStateWriter struct{}

var _ evmonly.StateWriter = (*discardStateWriter)(nil)

func (*discardStateWriter) ApplyChangeSet(evmonly.StateChangeSet) {}

type resultSinks struct {
	sink    evmonly.ResultSink
	close   func() error
	cleanup func() error
}

var _ evmonly.ResultSink = (*resultSinks)(nil)

func newResultSinks(cfg config, metrics *loadMetrics) (*resultSinks, error) {
	switch cfg.resultSink {
	case resultSinkDiscard:
		return &resultSinks{
			sink: discardResultSink{writer: &discardStateWriter{}},
		}, nil
	case resultSinkFile:
		return newFileResultSinks(cfg, metrics)
	default:
		return nil, fmt.Errorf("unsupported result-sink %q", cfg.resultSink)
	}
}

func (s *resultSinks) StoreBlockResult(ctx context.Context, height uint64, result *evmonly.BlockResult, release func()) error {
	return s.sink.StoreBlockResult(ctx, height, result, release)
}

func (s *resultSinks) Close() error {
	var closeErr error
	if s.close == nil {
		closeErr = nil
	} else {
		closeErr = s.close()
	}
	return errors.Join(closeErr, s.Cleanup())
}

func (s *resultSinks) Cleanup() error {
	if s.cleanup == nil {
		return nil
	}
	return s.cleanup()
}

type discardResultSink struct {
	writer evmonly.StateWriter
}

func (s discardResultSink) StoreBlockResult(_ context.Context, _ uint64, result *evmonly.BlockResult, release func()) error {
	defer release()
	s.writer.ApplyChangeSet(result.ChangeSet)
	return nil
}

type fileResultSinks struct {
	changeSetFile *appendRLPFile
	receiptFile   *appendRLPFile
	metrics       *loadMetrics
	cleanupMu     sync.Mutex
	paths         []string
	cleaned       map[string]struct{}
}

func newFileResultSinks(cfg config, metrics *loadMetrics) (*resultSinks, error) {
	if err := os.MkdirAll(cfg.persistDir, 0o750); err != nil {
		return nil, fmt.Errorf("create persist dir %s: %w", cfg.persistDir, err)
	}
	changeSetPath := filepath.Join(cfg.persistDir, "changesets.rlp")
	receiptPath := filepath.Join(cfg.persistDir, "receipts.rlp")
	files := &fileResultSinks{
		metrics: metrics,
		paths:   []string{changeSetPath, receiptPath},
		cleaned: map[string]struct{}{},
	}
	var err error
	files.changeSetFile, err = newAppendRLPFile(changeSetPath, cfg.persistBufferSize, cfg.persistSync)
	if err != nil {
		return nil, err
	}
	files.receiptFile, err = newAppendRLPFile(receiptPath, cfg.persistBufferSize, cfg.persistSync)
	if err != nil {
		return nil, errors.Join(err, files.Close())
	}
	async := newAsyncFileResultSinks(files, cfg.persistQueueSize, metrics)
	return &resultSinks{
		sink:    async,
		close:   async.Close,
		cleanup: files.Cleanup,
	}, nil
}

func (s *fileResultSinks) Close() error {
	var errs []error
	if s.changeSetFile != nil {
		errs = append(errs, s.changeSetFile.Close())
	}
	if s.receiptFile != nil {
		errs = append(errs, s.receiptFile.Close())
	}
	errs = append(errs, s.Cleanup())
	return errors.Join(errs...)
}

func (s *fileResultSinks) Cleanup() error {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()

	var errs []error
	for _, path := range s.paths {
		if _, ok := s.cleaned[path]; ok {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove persist file %s: %w", path, err))
			continue
		}
		s.cleaned[path] = struct{}{}
	}
	return errors.Join(errs...)
}

func (s *fileResultSinks) WriteRecord(kind string, height uint64, value any) error {
	var file *appendRLPFile
	switch kind {
	case resultSinkChangeSet:
		file = s.changeSetFile
	case resultSinkReceipts:
		file = s.receiptFile
	default:
		return fmt.Errorf("unsupported result sink record kind %q", kind)
	}
	startedAt := time.Now()
	bytes, err := file.WriteRecord(height, value)
	elapsed := time.Since(startedAt)
	if s.metrics != nil {
		s.metrics.recordSinkWrite(kind, bytes, elapsed, err == nil)
	}
	return err
}

type resultSinkRecord struct {
	height  uint64
	result  *evmonly.BlockResult
	release func()
}

type asyncFileResultSinks struct {
	files    *fileResultSinks
	metrics  *loadMetrics
	records  chan resultSinkRecord
	done     chan struct{}
	closeErr error
	close    sync.Once
	errMu    sync.Mutex
	err      error
}

func newAsyncFileResultSinks(files *fileResultSinks, queueSize int, metrics *loadMetrics) *asyncFileResultSinks {
	s := &asyncFileResultSinks{
		files:   files,
		metrics: metrics,
		records: make(chan resultSinkRecord, queueSize),
		done:    make(chan struct{}),
	}
	if metrics != nil {
		metrics.setSinkQueueCapacity(queueSize)
	}
	go s.run()
	return s
}

func (s *asyncFileResultSinks) StoreBlockResult(ctx context.Context, height uint64, result *evmonly.BlockResult, release func()) error {
	return s.enqueue(ctx, resultSinkRecord{
		height:  height,
		result:  result,
		release: release,
	})
}

func (s *asyncFileResultSinks) enqueue(ctx context.Context, record resultSinkRecord) error {
	if err := s.getErr(); err != nil {
		return err
	}
	select {
	case s.records <- record:
		s.recordEnqueued()
		return nil
	default:
	}

	startedAt := time.Now()
	select {
	case s.records <- record:
		if s.metrics != nil {
			s.metrics.recordSinkEnqueueWait(time.Since(startedAt))
		}
		s.recordEnqueued()
		return nil
	case <-s.done:
		if err := s.getErr(); err != nil {
			return err
		}
		return fmt.Errorf("result sink is closed")
	case <-ctx.Done():
		if s.metrics != nil {
			s.metrics.recordSinkEnqueueWait(time.Since(startedAt))
		}
		return ctx.Err()
	}
}

func (s *asyncFileResultSinks) recordEnqueued() {
	if s.metrics == nil {
		return
	}
	s.metrics.recordSinkEnqueued(resultSinkChangeSet)
	s.metrics.recordSinkEnqueued(resultSinkReceipts)
	s.metrics.setSinkQueued(len(s.records))
}

func (s *asyncFileResultSinks) run() {
	defer close(s.done)
	var writeErr error
	for record := range s.records {
		if s.metrics != nil {
			s.metrics.setSinkQueued(len(s.records))
		}
		if writeErr == nil {
			if err := s.writeRecord(record); err != nil {
				writeErr = err
				s.setErr(err)
			}
		}
		record.releaseResult()
		if s.metrics != nil {
			s.metrics.setSinkQueued(len(s.records))
		}
	}
	if s.metrics != nil {
		s.metrics.setSinkQueued(0)
	}
}

func (s *asyncFileResultSinks) writeRecord(record resultSinkRecord) error {
	if err := s.files.WriteRecord(resultSinkChangeSet, record.height, record.result.ChangeSet); err != nil {
		return err
	}
	return s.files.WriteRecord(resultSinkReceipts, record.height, record.result.Receipts)
}

func (r resultSinkRecord) releaseResult() {
	if r.release != nil {
		r.release()
	}
}

func (s *asyncFileResultSinks) Close() error {
	s.close.Do(func() {
		close(s.records)
		<-s.done
		if s.metrics != nil {
			s.metrics.setSinkQueued(0)
		}
		s.closeErr = errors.Join(s.getErr(), s.files.Close())
	})
	return s.closeErr
}

func (s *asyncFileResultSinks) setErr(err error) {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *asyncFileResultSinks) getErr() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

type appendRLPFile struct {
	mu          sync.Mutex
	file        *os.File
	writer      *bufio.Writer
	syncOnWrite bool
	closed      bool
}

func newAppendRLPFile(path string, bufferSize int, syncOnWrite bool) (*appendRLPFile, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // persist output path is an explicit CLI argument.
	if err != nil {
		return nil, fmt.Errorf("open persist file %s: %w", path, err)
	}
	return &appendRLPFile{
		file:        file,
		writer:      bufio.NewWriterSize(file, bufferSize),
		syncOnWrite: syncOnWrite,
	}, nil
}

func (f *appendRLPFile) WriteRecord(height uint64, value any) (int, error) {
	payload, err := rlp.EncodeToBytes(value)
	if err != nil {
		return 0, fmt.Errorf("encode rlp record for height %d: %w", height, err)
	}
	var header [16]byte
	binary.BigEndian.PutUint64(header[:8], height)
	binary.BigEndian.PutUint64(header[8:], uint64(len(payload)))

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, fmt.Errorf("write record for height %d: persist file is closed", height)
	}
	if _, err := f.writer.Write(header[:]); err != nil {
		return 0, fmt.Errorf("write record header for height %d: %w", height, err)
	}
	if _, err := f.writer.Write(payload); err != nil {
		return 0, fmt.Errorf("write record payload for height %d: %w", height, err)
	}
	if f.syncOnWrite {
		if err := f.writer.Flush(); err != nil {
			return 0, fmt.Errorf("flush record for height %d: %w", height, err)
		}
		if err := f.file.Sync(); err != nil {
			return 0, fmt.Errorf("sync record for height %d: %w", height, err)
		}
	}
	return len(header) + len(payload), nil
}

func (f *appendRLPFile) sync() error {
	if err := f.writer.Flush(); err != nil {
		return err
	}
	if err := f.file.Sync(); err != nil {
		return fmt.Errorf("sync persist file: %w", err)
	}
	return nil
}

func (f *appendRLPFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return errors.Join(f.sync(), f.file.Close())
}
