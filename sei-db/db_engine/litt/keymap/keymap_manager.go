package keymap

import (
	"fmt"
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/unflushed"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// KeymapManager manages modification of and access to the keymap. Write operations are offloaded
// to a background goroutine so that callers are not blocked by keymap I/O.
type KeymapManager struct {
	logger *slog.Logger

	// Responsible for handling fatal DB errors.
	errorMonitor *util.ErrorMonitor

	// The keymap implementation to use.
	keymap Keymap

	// Work for the keymap manager loop is sent to this channel.
	workChan chan any

	// The loop accumulates consecutive write requests into a single keymap.Put() call.
	// Once the accumulated key count reaches this threshold, the batch is flushed to the keymap
	// before draining further messages.
	targetWriteBatchSize int

	// Holds data that hasn't yet been flushed fully to disk. Getting flushed to the keymap is a requirement
	// for being evicted from this cache, so we need to inform it when keys are flushed to the keymap.
	unflushedDataCache *unflushed.UnflushedDataCache

	// Metrics for the database.
	m         *metrics.LittDBMetrics
	tableName string

	// Set to true when the loop should stop after the current iteration.
	stopped bool
}

// NewKeymapManager creates a new KeymapManager and starts its background goroutine.
func NewKeymapManager(
	logger *slog.Logger,
	errorMonitor *util.ErrorMonitor,
	keymap Keymap,
	unflushedDataCache *unflushed.UnflushedDataCache,
	workChanSize int,
	targetWriteBatchSize int,
	m *metrics.LittDBMetrics,
	tableName string,
) *KeymapManager {
	km := &KeymapManager{
		logger:               logger,
		errorMonitor:         errorMonitor,
		keymap:               keymap,
		workChan:             make(chan any, workChanSize),
		targetWriteBatchSize: targetWriteBatchSize,
		unflushedDataCache:   unflushedDataCache,
		m:                    m,
		tableName:            tableName,
	}
	m.RegisterChannel(tableName+"/keymap_manager", func() int { return len(km.workChan) })
	go km.run()
	return km
}

// WriteKeys enqueues a batch of keys to be written to the keymap. This method is non-blocking as
// long as there is space in the work channel. Keys may not be visible to LookupAddress() until
// after a Flush() completes.
func (k *KeymapManager) WriteKeys(keys []types.ScopedKey) error {
	err := util.Send(k.errorMonitor, k.workChan, &keymapManagerWriteRequest{keys: keys})
	if err != nil {
		return fmt.Errorf("failed to enqueue write request: %w", err)
	}
	return nil
}

// DeleteKeys enqueues a batch of keys to be deleted from the keymap. This method is non-blocking
// as long as there is space in the work channel.
func (k *KeymapManager) DeleteKeys(keys []types.ScopedKey) error {
	err := util.Send(k.errorMonitor, k.workChan, &keymapManagerDeleteRequest{keys: keys})
	if err != nil {
		return fmt.Errorf("failed to enqueue delete request: %w", err)
	}
	return nil
}

// LookupAddress looks up the address for a key. Returns true if the key exists, and false otherwise.
func (k *KeymapManager) LookupAddress(key []byte) (types.Address, bool, error) {
	address, exists, err := k.keymap.Get(key)
	if err != nil {
		return types.Address{}, false, fmt.Errorf("failed to look up address in keymap: %w", err)
	}
	return address, exists, nil
}

// Flush blocks until all keys enqueued via WriteKeys() prior to this call have been written to
// the keymap and reported to the durable key channel.
func (k *KeymapManager) Flush() error {
	responseChan := make(chan struct{}, 1)
	err := util.Send(k.errorMonitor, k.workChan, &keymapManagerFlushRequest{responseChan: responseChan})
	if err != nil {
		return fmt.Errorf("failed to enqueue flush request: %w", err)
	}
	_, err = util.Await(k.errorMonitor, responseChan)
	if err != nil {
		return fmt.Errorf("failed to wait for flush completion: %w", err)
	}
	return nil
}

// Stop cleanly shuts down the keymap manager loop. All previously enqueued writes are processed
// before the loop exits. Blocks until shutdown is complete.
func (k *KeymapManager) Stop() error {
	responseChan := make(chan struct{}, 1)
	err := util.Send(k.errorMonitor, k.workChan, &keymapManagerShutdownRequest{responseChan: responseChan})
	if err != nil {
		return fmt.Errorf("failed to enqueue shutdown request: %w", err)
	}
	_, err = util.Await(k.errorMonitor, responseChan)
	if err != nil {
		return fmt.Errorf("failed to wait for shutdown completion: %w", err)
	}
	return nil
}

// run is the keymap manager loop. It processes write, flush, and shutdown requests serially.
// Consecutive write requests are coalesced into a single keymap.Put() call up to
// targetWriteBatchSize keys. Because the work channel is FIFO, a flush or shutdown request
// is only handled after all previously enqueued write requests have been completed.
func (k *KeymapManager) run() {
	for !k.stopped {
		k.m.SetKeymapManagerPhase("idle")
		select {
		case <-k.errorMonitor.ImmediateShutdownRequired():
			k.m.SetKeymapManagerPhase("")
			k.logger.Info("shutting down keymap manager loop due to error monitor")
			return
		case message := <-k.workChan:
			if req, ok := message.(*keymapManagerWriteRequest); ok {
				k.drainAndWrite(req)
			} else {
				k.handleNonWriteMessage(message)
			}
		}
	}
	k.m.SetKeymapManagerPhase("")
}

// drainAndWrite accumulates the initial write request plus any consecutive write requests
// that are immediately available in the work channel, up to targetWriteBatchSize total keys.
// The accumulated batch is submitted as a single keymap.Put(). If a non-write message is
// encountered while draining, the accumulated writes are flushed first, then the non-write
// message is handled.
func (k *KeymapManager) drainAndWrite(first *keymapManagerWriteRequest) {
	k.m.SetKeymapManagerPhase("drain")
	batch := first.keys
	batchSize := len(batch)

	for batchSize < k.targetWriteBatchSize {
		select {
		case message := <-k.workChan:
			if req, ok := message.(*keymapManagerWriteRequest); ok {
				batch = append(batch, req.keys...)
				batchSize += len(req.keys)
			} else {
				k.flushWriteBatch(batch)
				k.handleNonWriteMessage(message)
				return
			}
		default:
			k.flushWriteBatch(batch)
			return
		}
	}

	k.flushWriteBatch(batch)
}

// flushWriteBatch writes a combined batch of keys to the keymap and sends them to the
// durable key channel.
func (k *KeymapManager) flushWriteBatch(batch []types.ScopedKey) {
	k.m.SetKeymapManagerPhase("put")
	err := k.keymap.Put(batch)
	if err != nil {
		k.errorMonitor.Panic(fmt.Errorf("failed to write keys to keymap: %w", err))
		return
	}
	k.m.ReportKeymapBatch(k.tableName, len(batch))

	k.m.SetKeymapManagerPhase("report_flushed")
	err = k.unflushedDataCache.ReportFlushedKeys(batch)
	if err != nil {
		k.errorMonitor.Panic(fmt.Errorf("failed to report flushed keys: %w", err))
	}
}

// handleNonWriteMessage dispatches a message that is not a write request.
func (k *KeymapManager) handleNonWriteMessage(message any) {
	if req, ok := message.(*keymapManagerDeleteRequest); ok {
		k.m.SetKeymapManagerPhase("delete")
		err := k.keymap.Delete(req.keys)
		if err != nil {
			k.errorMonitor.Panic(fmt.Errorf("failed to delete keys from keymap: %w", err))
		}
	} else if req, ok := message.(*keymapManagerFlushRequest); ok {
		k.m.SetKeymapManagerPhase("flush")
		err := k.keymap.Flush()
		if err != nil {
			k.errorMonitor.Panic(fmt.Errorf("failed to flush keymap: %w", err))
			return
		}
		req.responseChan <- struct{}{}
	} else if req, ok := message.(*keymapManagerShutdownRequest); ok {
		k.m.SetKeymapManagerPhase("shutdown")
		err := k.keymap.Flush()
		if err != nil {
			k.errorMonitor.Panic(fmt.Errorf("failed to flush keymap on shutdown: %w", err))
			return
		}
		req.responseChan <- struct{}{}
		k.stopped = true
	} else {
		k.errorMonitor.Panic(fmt.Errorf("unknown keymap manager message type %T", message))
		k.stopped = true
	}
}
