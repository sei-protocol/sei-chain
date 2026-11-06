package builder

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

var logger = seilog.NewLogger("giga", "apphash", "builder")

// Hash log columns holding the fields of each app hash the builder builds.
const (
	appHashColumn         = "appHash"
	previousAppHashColumn = "previousAppHash"
	blockHashColumn       = "blockHash"
	stateHashColumn       = "stateHash"
	budHashColumn         = "budHash"
	receiptHashColumn     = "receiptHash"
)

var _ AppHashBuilder = (*StandardAppHashBuilder)(nil)

// StandardAppHashBuilder is an AppHashBuilder that stores app hashes in a hash vault it is given. The caller owns
// the vault, and closes it after the builder.
type StandardAppHashBuilder struct {

	// The EVM chain ID committed to by every app hash.
	chainID uint64

	// The first block with an app hash. Its previous app hash is all zeros.
	gigaActivationHeight uint64

	// Holds the app hash data of every block built.
	vault vault.HashVault

	// Receives each app hash built, as a debugging aid. Nil if hash logging is disabled or has failed. Owned by the
	// builder goroutine, which closes it as it exits.
	hashLog *hashlog.HashLogWriter

	// Requests for the builder goroutine, run in order. Each is one of the *Request types.
	requests chan any

	// The most requests run before the blocks they complete are stored and published.
	maxBatchSize int

	// Cancelled by Close(), or by the first request that fails. Its cause is the error every later call returns.
	ctx context.Context

	// Cancels ctx with a cause.
	cancel context.CancelCauseFunc

	// Tracks the builder goroutine, which Close() waits for.
	wg sync.WaitGroup

	// The block height of the newest published app hash, or 0 if none has been published. Sealed as the builder
	// goroutine exits.
	publishedHeight *threading.Counter

	// The error from closing the vault iterators the builder goroutine holds. Set by the builder goroutine as it
	// exits, and read by Close() once it has exited.
	closeErr error

	// Every field below is owned by the builder goroutine.

	// The progress of each input through the block heights, indexed by hashInput. Each report (i.e. call to a
	// Report*() method) is checked against its input's tracker before its value is used.
	reportTrackers [hashInputCount]reportTracker

	// The lowest block height the vault knows.
	vaultLowest uint64

	// The highest block height the vault knows.
	vaultHighest uint64

	// Whether SetupComplete() has succeeded.
	setupDone bool

	// The newest block whose app hash is known. The next block built chains from it.
	headHeight uint64

	// The app hash of block headHeight.
	headAppHash [32]byte

	// The newest published app hash, or nil if none has been published.
	newestPublished *apphash.AppHashData

	// The newest published app hashes, in block order, at most recentAppHashCacheSize of them. Empty only if none
	// has been published.
	cache *structures.Deque[*apphash.AppHashData]

	// The most app hashes cache holds.
	recentAppHashCacheSize int

	// The open read of stored app hashes older than cache, or nil if none is open.
	readCursor *vaultReadCursor

	// How long readCursor may go without serving a read before it is closed.
	readCursorIdleTimeout time.Duration

	// After setup, the stored records above headHeight. Each block built is checked against its stored record. Nil
	// once no stored record is above headHeight.
	storedAboveHeadIterator apphash.AppHashIterator

	// The blocks above headHeight waiting for their inputs to be reported (i.e. passed to the Report*() methods),
	// by block height.
	pendingBlocks map[uint64]*pendingBlock
}

// What the vault says about the builder's state at startup: its bounds, and the block the next block built chains
// from.
type startupState struct {

	// The lowest block height the vault knows.
	vaultLowest uint64

	// The highest block height the vault knows.
	vaultHighest uint64

	// The newest block whose app hash is known. The next block built chains from it.
	headHeight uint64

	// The app hash of block headHeight.
	headAppHash [32]byte
}

// Starts the builder over hashVault, which must stay open until the builder is closed. Errors if the vault holds app
// hashes of another chain.
func NewStandardAppHashBuilder(
	ctx context.Context,
	config AppHashBuilderConfig,
	hashVault vault.HashVault,
) (*StandardAppHashBuilder, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid app hash builder config: %w", err)
	}
	startup, err := loadStartupState(hashVault, config.ChainID)
	if err != nil {
		return nil, fmt.Errorf("failed to load the app hash builder's startup state: %w", err)
	}
	ctx, cancel := context.WithCancelCause(ctx)
	b := &StandardAppHashBuilder{
		chainID:              config.ChainID,
		gigaActivationHeight: config.GigaActivationHeight,
		vault:                hashVault,
		hashLog:              openHashLog(config),
		requests:             make(chan any, config.InputBufferSize),
		maxBatchSize:         config.MaxBatchSize,
		ctx:                  ctx,
		cancel:               cancel,
		publishedHeight:      threading.NewCounter(0),
		vaultLowest:          startup.vaultLowest,
		vaultHighest:         startup.vaultHighest,
		headHeight:           startup.headHeight,
		headAppHash:          startup.headAppHash,
		cache: structures.NewDequeWithCapacity[*apphash.AppHashData](
			config.RecentAppHashCacheSize),
		recentAppHashCacheSize: config.RecentAppHashCacheSize,
		readCursorIdleTimeout:  config.ReadCursorIdleTimeout,
		pendingBlocks:          make(map[uint64]*pendingBlock),
	}
	b.wg.Go(b.run)
	return b, nil
}

func (b *StandardAppHashBuilder) ReportBlockHash(ctx context.Context, blockHeight uint64, blockHash [32]byte) error {
	request := reportRequest{input: blockHashInput, blockHeight: blockHeight, value: blockHash}
	if err := b.submit(ctx, request); err != nil {
		return fmt.Errorf("failed to report the block hash of block %d: %w", blockHeight, err)
	}
	return nil
}

func (b *StandardAppHashBuilder) ReportStateHash(
	ctx context.Context,
	blockHeight uint64,
	stateHash [32]byte,
	componentHashes map[string][]byte,
) error {
	request := reportRequest{
		input:           stateHashInput,
		blockHeight:     blockHeight,
		value:           stateHash,
		componentHashes: componentHashes,
	}
	if err := b.submit(ctx, request); err != nil {
		return fmt.Errorf("failed to report the state hash of block %d: %w", blockHeight, err)
	}
	return nil
}

func (b *StandardAppHashBuilder) ReportBUDHash(ctx context.Context, blockHeight uint64, budHash [32]byte) error {
	request := reportRequest{input: budHashInput, blockHeight: blockHeight, value: budHash}
	if err := b.submit(ctx, request); err != nil {
		return fmt.Errorf("failed to report the BUD hash of block %d: %w", blockHeight, err)
	}
	return nil
}

func (b *StandardAppHashBuilder) ReportReceiptHash(
	ctx context.Context,
	blockHeight uint64,
	receiptHash apphash.ReceiptHash,
) error {
	request := reportRequest{input: receiptHashInput, blockHeight: blockHeight, value: receiptHash}
	if err := b.submit(ctx, request); err != nil {
		return fmt.Errorf("failed to report the receipt hash of block %d: %w", blockHeight, err)
	}
	return nil
}

func (b *StandardAppHashBuilder) SetupComplete(ctx context.Context, blockHeight uint64) error {
	reply := make(chan callResult[struct{}], 1)
	if err := b.submit(ctx, setupCompleteRequest{blockHeight: blockHeight, reply: reply}); err != nil {
		return fmt.Errorf("failed to complete app hash builder setup at block %d: %w", blockHeight, err)
	}
	if _, err := awaitReply(ctx, b, reply); err != nil {
		return fmt.Errorf("failed to complete app hash builder setup at block %d: %w", blockHeight, err)
	}
	return nil
}

func (b *StandardAppHashBuilder) GetAppHash(ctx context.Context, blockHeight uint64) (*apphash.AppHashData, error) {
	if blockHeight < b.gigaActivationHeight {
		return nil, fmt.Errorf("block %d is below the giga activation height %d, so it has no app hash",
			blockHeight, b.gigaActivationHeight)
	}
	reached, _, err := b.publishedHeight.Await(ctx, blockHeight)
	if err != nil {
		return nil, fmt.Errorf("abandoned waiting for the app hash of block %d: %w", blockHeight, err)
	}
	if !reached {
		return nil, fmt.Errorf("the app hash of block %d was never published: %w",
			blockHeight, b.stoppedError())
	}
	appHash, err := b.requestAppHash(ctx, blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to get the app hash of block %d: %w", blockHeight, err)
	}
	return appHash, nil
}

func (b *StandardAppHashBuilder) Bounds() (uint64, uint64, bool, error) {
	reply := make(chan callResult[appHashBounds], 1)
	if err := b.submit(context.Background(), boundsRequest{reply: reply}); err != nil {
		return 0, 0, false, fmt.Errorf("failed to request the app hash bounds: %w", err)
	}
	bounds, err := awaitReply(context.Background(), b, reply)
	if err != nil {
		return 0, 0, false, fmt.Errorf("failed to read the app hash bounds: %w", err)
	}
	return bounds.lowest, bounds.highest, bounds.ok, nil
}

func (b *StandardAppHashBuilder) Close() error {
	var sweepErr error
	reply := make(chan callResult[struct{}], 1)
	// A builder already stopped refuses the request, and has nothing left to store.
	if b.submit(context.Background(), closeRequest{reply: reply}) == nil {
		if _, err := awaitReply(context.Background(), b, reply); err != nil {
			sweepErr = fmt.Errorf("failed to store the queued app hashes: %w", err)
		}
	}
	b.cancel(fmt.Errorf("app hash builder is closed"))
	b.wg.Wait()
	if err := errors.Join(sweepErr, b.closeErr); err != nil {
		return fmt.Errorf("failed to close app hash builder: %w", err)
	}
	return nil
}

// GetLatestBlock returns the head of the builder configured by config without opening it: the highest block its
// hash vault knows. It fails while the builder is open.
func GetLatestBlock(config AppHashBuilderConfig) (uint64, error) {
	if err := config.Validate(); err != nil {
		return 0, fmt.Errorf("invalid app hash builder config: %w", err)
	}
	_, highest, err := vault.GetBounds(config.HashVaultConfig, config.GigaActivationHeight)
	if err != nil {
		return 0, fmt.Errorf("failed to read the app hash builder's head: %w", err)
	}
	return highest, nil
}

// Opens the hash log if config enables one. Returns nil if it does not, or if the hash log fails to open, which
// does not stop the builder since the hash log is a debugging aid.
func openHashLog(config AppHashBuilderConfig) *hashlog.HashLogWriter {
	if !config.HashLogEnabled {
		return nil
	}
	hashLog, err := hashlog.NewHashLogWriter(&config.HashLogConfig)
	if err != nil {
		logger.Error("failed to open the app hash log; disabling hash logging", "err", err)
		return nil
	}
	return hashLog
}

// Reads the builder's startup state from hashVault. Its head is the highest block the vault knows. Errors if the
// vault's records belong to a chain other than chainID.
func loadStartupState(hashVault vault.HashVault, chainID uint64) (startupState, error) {
	lowest, highest, err := hashVault.Bounds()
	if err != nil {
		return startupState{}, fmt.Errorf("failed to read hash vault bounds: %w", err)
	}
	newest, err := readStoredRecord(hashVault, highest)
	if err != nil {
		return startupState{}, fmt.Errorf("failed to read the newest app hash: %w", err)
	}
	if newest.ChainID() != chainID {
		return startupState{}, fmt.Errorf("hash vault holds app hashes for chain ID %d, not %d",
			newest.ChainID(), chainID)
	}
	return startupState{
		vaultLowest:  lowest,
		vaultHighest: highest,
		headHeight:   highest,
		headAppHash:  newest.AppHash(),
	}, nil
}

// Returns hashVault's record of blockHeight.
func readStoredRecord(hashVault vault.HashVault, blockHeight uint64) (*apphash.AppHashData, error) {
	it, err := hashVault.Iterator(blockHeight, blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to read the app hash of block %d: %w", blockHeight, err)
	}
	defer func() { _ = it.Close() }()
	record, err := nextStoredRecord(it, blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to read hash vault record %d: %w", blockHeight, err)
	}
	return record, nil
}

// Hands request to the builder goroutine.
func (b *StandardAppHashBuilder) submit(ctx context.Context, request any) error {
	// A stopped builder must refuse the call rather than race it into the buffer, where nothing would run it.
	if b.ctx.Err() != nil {
		return b.stoppedError()
	}
	select {
	case b.requests <- request:
		return nil
	case <-b.ctx.Done():
		return b.stoppedError()
	case <-ctx.Done():
		return fmt.Errorf("app hash builder call abandoned: %w", ctx.Err())
	}
}

// Returns the error every call returns once the builder is stopped.
func (b *StandardAppHashBuilder) stoppedError() error {
	return fmt.Errorf("app hash builder stopped: %w", context.Cause(b.ctx))
}

// Waits for the reply to a request already handed to the builder goroutine. Errors if ctx is cancelled first.
func awaitReply[T any](ctx context.Context, b *StandardAppHashBuilder, reply <-chan callResult[T]) (T, error) {
	select {
	case result := <-reply:
		return result.value, result.err
	case <-ctx.Done():
		// The reply is buffered, so the builder goroutine never blocks on the abandoned reply.
		var zero T
		return zero, fmt.Errorf("app hash builder call abandoned: %w", ctx.Err())
	case <-b.ctx.Done():
		// Once the builder goroutine exits, the reply is either buffered or never sent. A buffered reply is the
		// request's result, so it reaches the caller.
		b.wg.Wait()
		select {
		case result := <-reply:
			return result.value, result.err
		default:
			var zero T
			return zero, b.stoppedError()
		}
	}
}

// Asks the builder goroutine for the published app hash of blockHeight, or nil if it is not published yet.
func (b *StandardAppHashBuilder) requestAppHash(ctx context.Context, blockHeight uint64) (*apphash.AppHashData, error) {
	reply := make(chan callResult[*apphash.AppHashData], 1)
	if err := b.submit(ctx, getAppHashRequest{blockHeight: blockHeight, reply: reply}); err != nil {
		return nil, fmt.Errorf("failed to request the app hash: %w", err)
	}
	appHash, err := awaitReply(ctx, b, reply)
	if err != nil {
		return nil, fmt.Errorf("failed to read the app hash: %w", err)
	}
	return appHash, nil
}

// The builder goroutine. Runs requests until a closeRequest, Close() or a failed request, then closes its vault
// iterators and the hash log, and seals publishedHeight.
func (b *StandardAppHashBuilder) run() {
	defer b.closeStores()
	// Runs before closeStores(), so a GetAppHash() call woken by the seal finds the builder stopped with a
	// cause.
	defer b.cancel(fmt.Errorf("app hash builder is closed"))
	for {
		select {
		case <-b.ctx.Done():
			return
		case request := <-b.requests:
			closing, err := b.runBatch(request)
			if err != nil {
				logger.Error("app hash builder stopped", "err", err)
				b.cancel(err)
				return
			}
			if closing {
				return
			}
		}
	}
}

// Runs first and the requests queued behind it, at most maxBatchSize in all, then publishes the blocks they
// completed. A closeRequest ends the batch, and closing reports that one was run.
func (b *StandardAppHashBuilder) runBatch(first any) (closing bool, err error) {
	if closeReq, ok := first.(closeRequest); ok {
		return true, b.publishBeforeClosing(closeReq)
	}
	if err := b.handleRequest(first); err != nil {
		return false, fmt.Errorf("failed to handle %T: %w", first, err)
	}
	for count := 1; count < b.maxBatchSize; count++ {
		var request any
		select {
		case <-b.ctx.Done():
			return false, nil
		case request = <-b.requests:
		default:
		}
		if request == nil {
			// No request is queued.
			break
		}
		if closeReq, ok := request.(closeRequest); ok {
			return true, b.publishBeforeClosing(closeReq)
		}
		if err := b.handleRequest(request); err != nil {
			return false, fmt.Errorf("failed to handle %T: %w", request, err)
		}
	}
	if err := b.publishCompleteBlocks(); err != nil {
		return false, fmt.Errorf("failed to publish app hashes: %w", err)
	}
	return false, nil
}

// Stores and publishes the blocks completed so far, and replies to closeReq with the result.
func (b *StandardAppHashBuilder) publishBeforeClosing(closeReq closeRequest) error {
	err := b.publishCompleteBlocks()
	closeReq.reply <- callResult[struct{}]{err: err}
	if err != nil {
		return fmt.Errorf("failed to publish app hashes before closing: %w", err)
	}
	return nil
}

// Runs one request. An error stops the builder.
func (b *StandardAppHashBuilder) handleRequest(request any) error {
	switch r := request.(type) {
	case reportRequest:
		if err := b.handleReport(r.input, r.blockHeight, r.value, r.componentHashes); err != nil {
			return fmt.Errorf("failed to take in the %s of block %d: %w", r.input, r.blockHeight, err)
		}
		return nil
	case setupCompleteRequest:
		err := b.completeSetup(r.blockHeight)
		r.reply <- callResult[struct{}]{err: err}
		if err != nil {
			return fmt.Errorf("failed to complete setup at block %d: %w", r.blockHeight, err)
		}
		return nil
	case getAppHashRequest:
		appHash, err := b.readAppHash(r.blockHeight)
		r.reply <- callResult[*apphash.AppHashData]{value: appHash, err: err}
		return nil
	case boundsRequest:
		bounds, err := b.readBounds()
		r.reply <- callResult[appHashBounds]{value: bounds, err: err}
		return nil
	default:
		return fmt.Errorf("unknown app hash builder request %T", request)
	}
}

// Takes in one report (i.e. one call to a Report*() method): an input's value for one block, and the debugging
// hashes reported with it. A report below the giga activation height is ignored. Before setup, a value for a
// stored block is checked against the stored record.
func (b *StandardAppHashBuilder) handleReport(
	input hashInput,
	blockHeight uint64,
	value [32]byte,
	componentHashes map[string][]byte,
) error {
	if blockHeight < b.gigaActivationHeight {
		return nil
	}
	tracker := &b.reportTrackers[input]
	if tracker.startingPointKnown && blockHeight != tracker.nextHeight {
		return fmt.Errorf("%s reported for block %d, expected block %d", input, blockHeight, tracker.nextHeight)
	}
	tracker.startingPointKnown = true
	tracker.nextHeight = blockHeight + 1

	if !b.setupDone && blockHeight <= b.vaultHighest {
		if err := b.compareStoredInput(input, tracker, blockHeight, value); err != nil {
			return fmt.Errorf("failed to check the %s of block %d against the hash vault: %w",
				input, blockHeight, err)
		}
		return nil
	}

	block := b.pendingBlocks[blockHeight]
	if block == nil {
		block = &pendingBlock{}
		b.pendingBlocks[blockHeight] = block
	}
	block.values[input] = value
	block.reported[input] = true
	if componentHashes != nil {
		block.componentHashes = componentHashes
	}
	return nil
}

// Checks a report made before setup (i.e. a call to a Report*() method) against the stored record of its block.
// Panics if they differ.
func (b *StandardAppHashBuilder) compareStoredInput(
	input hashInput,
	tracker *reportTracker,
	blockHeight uint64,
	value [32]byte,
) error {
	if blockHeight < b.vaultLowest {
		// The record has been pruned, so there is nothing to compare against.
		return nil
	}
	if tracker.storedRecordIterator == nil {
		iterator, err := b.vault.Iterator(blockHeight, b.vaultHighest)
		if err != nil {
			return fmt.Errorf("failed to read stored app hashes from block %d: %w", blockHeight, err)
		}
		tracker.storedRecordIterator = iterator
	}
	stored, err := nextStoredRecord(tracker.storedRecordIterator, blockHeight)
	if err != nil {
		return fmt.Errorf("failed to read the stored record of block %d: %w", blockHeight, err)
	}
	if storedValue := input.of(stored); storedValue != value {
		b.panicOnMismatch(blockHeight, input.String(), storedValue, value)
	}
	if blockHeight == b.vaultHighest {
		err := tracker.storedRecordIterator.Close()
		tracker.storedRecordIterator = nil
		if err != nil {
			return fmt.Errorf("failed to close the stored record iterator of the %s: %w", input, err)
		}
	}
	return nil
}

// Returns the published app hash of blockHeight, or nil if it is not published yet. Errors before setup, and if
// blockHeight is pruned.
func (b *StandardAppHashBuilder) readAppHash(blockHeight uint64) (*apphash.AppHashData, error) {
	if !b.setupDone {
		return nil, fmt.Errorf("cannot read app hashes before setup is complete")
	}
	if err := b.maybeRetireReadCursor(blockHeight); err != nil {
		return nil, fmt.Errorf("failed to retire the read cursor before reading block %d: %w", blockHeight, err)
	}
	if b.newestPublished == nil || blockHeight > b.newestPublished.BlockHeight() {
		return nil, nil
	}
	oldestCached := b.cache.PeekFront().BlockHeight()
	if blockHeight >= oldestCached {
		//nolint:gosec // the offset is below recentAppHashCacheSize, an int
		return b.cache.Get(int(blockHeight - oldestCached)), nil
	}
	appHash, err := b.readStoredAppHash(blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to read the stored app hash of block %d: %w", blockHeight, err)
	}
	return appHash, nil
}

// Returns the range of available app hashes. Errors before setup.
func (b *StandardAppHashBuilder) readBounds() (appHashBounds, error) {
	if !b.setupDone {
		return appHashBounds{}, fmt.Errorf("cannot read app hash bounds before setup is complete")
	}
	if b.newestPublished == nil {
		return appHashBounds{}, nil
	}
	vaultLowest, _, err := b.vault.Bounds()
	if err != nil {
		return appHashBounds{}, fmt.Errorf("failed to read the hash vault bounds: %w", err)
	}
	return appHashBounds{
		lowest:  min(max(vaultLowest, b.gigaActivationHeight), b.cache.PeekFront().BlockHeight()),
		highest: b.newestPublished.BlockHeight(),
		ok:      true,
	}, nil
}

// Close the cursor if it can't be used to serve this read request.
func (b *StandardAppHashBuilder) maybeRetireReadCursor(blockHeight uint64) error {
	if b.readCursor == nil || b.readCursor.CanRead(blockHeight) {
		return nil
	}
	return b.closeReadCursor()
}

// Returns the stored app hash of blockHeight, a published block, from the open read cursor, opening one there if none
// is open. A cursor is closed once it is exhausted.
func (b *StandardAppHashBuilder) readStoredAppHash(blockHeight uint64) (*apphash.AppHashData, error) {
	if b.readCursor == nil {
		cursor, err := openVaultReadCursor(
			b.vault, blockHeight, b.newestPublished.BlockHeight(), b.readCursorIdleTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to open a read cursor at block %d: %w", blockHeight, err)
		}
		b.readCursor = cursor
	}
	appHash, err := b.readCursor.Read(blockHeight)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to read block %d: %w", blockHeight, err),
			b.closeReadCursor())
	}
	if b.readCursor.Exhausted() {
		if err := b.closeReadCursor(); err != nil {
			return nil, fmt.Errorf("failed to close the exhausted read cursor: %w", err)
		}
	}
	return appHash, nil
}

// Closes the open read cursor, if any.
func (b *StandardAppHashBuilder) closeReadCursor() error {
	if b.readCursor == nil {
		return nil
	}
	err := b.readCursor.Close()
	b.readCursor = nil
	if err != nil {
		return fmt.Errorf("failed to close the read cursor: %w", err)
	}
	return nil
}

// Publishes the app hashes through blockHeight, building and storing any that are not stored yet.
func (b *StandardAppHashBuilder) completeSetup(blockHeight uint64) error {
	if b.setupDone {
		return fmt.Errorf("setup is already complete")
	}
	if err := b.closeIterators(); err != nil {
		return fmt.Errorf("failed to end pre-setup checks: %w", err)
	}

	if blockHeight > b.headHeight {
		if err := b.storeSetupBlocks(blockHeight); err != nil {
			return fmt.Errorf("failed to store app hashes through block %d: %w", blockHeight, err)
		}
	} else if err := b.rewindHead(blockHeight); err != nil {
		return fmt.Errorf("failed to rewind to block %d: %w", blockHeight, err)
	}

	b.pendingBlocks = make(map[uint64]*pendingBlock)
	for i := range b.reportTrackers {
		b.reportTrackers[i].startingPointKnown = true
		b.reportTrackers[i].nextHeight = b.headHeight + 1
	}
	b.setupDone = true
	if err := b.advancePublishedHeight(); err != nil {
		return fmt.Errorf("failed to publish the app hashes through block %d: %w", blockHeight, err)
	}
	return nil
}

// Used by setup when the storage layer is ahead of the hash vault: builds and stores the app hash of every block
// after the newest one already known (or from the giga activation height, if none is stored), using the inputs
// reported (i.e. passed to the Report*() methods) before setup. Errors if any of those blocks is missing an input.
func (b *StandardAppHashBuilder) storeSetupBlocks(
	// The storage layer's height: the last block every store has applied, and the last block stored here.
	blockHeight uint64,
) error {
	records := make([]*apphash.AppHashData, 0, blockHeight-b.headHeight)
	for height := b.headHeight + 1; height <= blockHeight; height++ {
		block := b.pendingBlocks[height]
		if block == nil || !block.complete() {
			return fmt.Errorf("cannot compute the app hash of block %d: not every input was reported",
				height)
		}
		record := b.buildRecord(height, block)
		records = append(records, record)
		b.headHeight = height
		b.headAppHash = record.AppHash()
	}
	if err := b.appendToVault(records); err != nil {
		return fmt.Errorf("failed to store app hashes through block %d: %w", blockHeight, err)
	}
	for _, record := range records {
		b.publish(record)
	}
	return nil
}

// Used by setup when the hash vault is at or ahead of the storage layer: the next block built is the one after the
// storage layer's height (or the giga activation height, if that is higher), and each block built after setup is
// checked against the app hash already stored for it.
func (b *StandardAppHashBuilder) rewindHead(
	// The storage layer's height: the last block every store has applied.
	blockHeight uint64,
) error {
	b.headHeight = max(blockHeight, b.gigaActivationHeight-1)
	b.headAppHash = [32]byte{}
	b.newestPublished = nil
	if b.vaultHighest < b.gigaActivationHeight {
		return nil
	}

	firstRead := max(b.headHeight, b.gigaActivationHeight)
	iterator, err := b.vault.Iterator(firstRead, b.vaultHighest)
	if err != nil {
		return fmt.Errorf("failed to read stored app hashes from block %d: %w", firstRead, err)
	}
	if b.headHeight >= b.gigaActivationHeight {
		anchor, err := nextStoredRecord(iterator, b.headHeight)
		if err != nil {
			return fmt.Errorf("failed to read the app hash of block %d: %w", b.headHeight,
				errors.Join(err, iterator.Close()))
		}
		b.headAppHash = anchor.AppHash()
		b.publish(anchor)
	}
	if b.headHeight == b.vaultHighest {
		if err := iterator.Close(); err != nil {
			return fmt.Errorf("failed to close stored app hash iterator: %w", err)
		}
		return nil
	}
	b.storedAboveHeadIterator = iterator
	return nil
}

// Builds every block whose inputs are all reported (i.e. passed to the Report*() methods), stores the ones not
// already stored, and publishes them. Runs after each batch of requests, but does nothing during setup, when
// storeSetupBlocks() builds the reported blocks instead.
func (b *StandardAppHashBuilder) publishCompleteBlocks() error {
	if !b.setupDone {
		return nil
	}
	var toPublish []*apphash.AppHashData
	var toStore []*apphash.AppHashData
	for {
		height := b.headHeight + 1
		block := b.pendingBlocks[height]
		if block == nil || !block.complete() {
			break
		}
		delete(b.pendingBlocks, height)
		record := b.buildRecord(height, block)
		if b.storedAboveHeadIterator != nil {
			if err := b.compareStoredRecord(record); err != nil {
				return fmt.Errorf("failed to check block %d against the hash vault: %w", height, err)
			}
		} else {
			toStore = append(toStore, record)
		}
		b.headHeight = height
		b.headAppHash = record.AppHash()
		toPublish = append(toPublish, record)
	}
	if len(toPublish) == 0 {
		return nil
	}

	if len(toStore) > 0 {
		if err := b.appendToVault(toStore); err != nil {
			return fmt.Errorf("failed to store app hashes through block %d: %w", b.headHeight, err)
		}
	}
	for _, record := range toPublish {
		b.publish(record)
	}
	if err := b.advancePublishedHeight(); err != nil {
		return fmt.Errorf("failed to publish app hashes through block %d: %w", b.headHeight, err)
	}
	return nil
}

// Makes record, the app hash of the block after the newest published one, the newest published app hash, and adds
// it to the recent app hash cache. GetAppHash() callers learn of it once advancePublishedHeight() runs.
func (b *StandardAppHashBuilder) publish(record *apphash.AppHashData) {
	if b.cache.Len() == b.recentAppHashCacheSize {
		b.cache.PopFront()
	}
	b.cache.PushBack(record)
	b.newestPublished = record
}

// Raises publishedHeight to the block height of the newest published app hash, waking the GetAppHash() calls
// waiting for it.
func (b *StandardAppHashBuilder) advancePublishedHeight() error {
	if b.newestPublished == nil {
		return nil
	}
	height := b.newestPublished.BlockHeight()
	if _, err := b.publishedHeight.IncrementN(height - b.publishedHeight.Get()); err != nil {
		return fmt.Errorf("failed to advance the published height to block %d: %w", height, err)
	}
	return nil
}

// Compares a block built after setup with its stored record. Panics if they differ.
func (b *StandardAppHashBuilder) compareStoredRecord(record *apphash.AppHashData) error {
	stored, err := nextStoredRecord(b.storedAboveHeadIterator, record.BlockHeight())
	if err != nil {
		return fmt.Errorf("failed to read the stored record of block %d: %w", record.BlockHeight(), err)
	}
	if stored.AppHash() != record.AppHash() {
		b.panicOnMismatch(record.BlockHeight(), "app hash", stored.AppHash(), record.AppHash())
	}
	if record.BlockHeight() == b.vaultHighest {
		err := b.storedAboveHeadIterator.Close()
		b.storedAboveHeadIterator = nil
		if err != nil {
			return fmt.Errorf("failed to close stored app hash iterator: %w", err)
		}
	}
	return nil
}

// Returns the app hash data of a block whose inputs are all reported (i.e. passed to the Report*() methods),
// chained from the app hash of the block before it, and writes it to the hash log.
func (b *StandardAppHashBuilder) buildRecord(blockHeight uint64, block *pendingBlock) *apphash.AppHashData {
	record := apphash.NewAppHashData(
		b.chainID,
		blockHeight,
		block.values[blockHashInput],
		block.values[stateHashInput],
		block.values[budHashInput],
		block.values[receiptHashInput],
		b.headAppHash,
	)
	b.writeToHashLog(record, block.componentHashes)
	return record
}

// Writes record's fields and componentHashes to the hash log as one row. The hash log is a debugging aid, so a
// failure disables it rather than stopping the builder.
func (b *StandardAppHashBuilder) writeToHashLog(record *apphash.AppHashData, componentHashes map[string][]byte) {
	if b.hashLog == nil {
		return
	}
	hashes := make(map[string][]byte, len(componentHashes)+6)
	for name, hash := range componentHashes {
		hashes[name] = hash
	}
	hashes[appHashColumn] = hashBytes(record.AppHash())
	hashes[previousAppHashColumn] = hashBytes(record.PreviousAppHash())
	hashes[blockHashColumn] = hashBytes(record.BlockHash())
	hashes[stateHashColumn] = hashBytes(record.StateHash())
	hashes[budHashColumn] = hashBytes(record.BUDHash())
	hashes[receiptHashColumn] = hashBytes(record.ReceiptHash())
	if err := b.hashLog.Write(record.BlockHeight(), hashes); err != nil {
		logger.Error("failed to write to the app hash log; disabling hash logging", "err", err)
		_ = b.hashLog.Close()
		b.hashLog = nil
	}
}

// Returns a slice holding a copy of hash.
func hashBytes(hash [32]byte) []byte {
	return hash[:]
}

// Appends records to the hash vault, durably if HashVaultConfig.FsyncOnFlush is set, and updates the builder's
// record of the vault's lowest and highest block heights to match. Closes the read cursor if it is idle.
func (b *StandardAppHashBuilder) appendToVault(records []*apphash.AppHashData) error {
	last := records[len(records)-1].BlockHeight()
	if err := b.vault.Append(records); err != nil {
		return fmt.Errorf("failed to append app hashes through block %d to the hash vault: %w", last, err)
	}
	b.vaultHighest = last
	if b.readCursor != nil && b.readCursor.ShouldBeRetired() {
		if err := b.closeReadCursor(); err != nil {
			return fmt.Errorf("failed to close the idle read cursor: %w", err)
		}
	}
	return nil
}

// Closes the vault iterators the builder goroutine holds and the hash log, seals publishedHeight, and records the
// error for Close(). A hash log failure is logged rather than recorded, since the hash log is a debugging aid.
func (b *StandardAppHashBuilder) closeStores() {
	if b.hashLog != nil {
		if err := b.hashLog.Close(); err != nil {
			logger.Error("failed to close the app hash log", "err", err)
		}
		b.hashLog = nil
	}
	var sealErr error
	if err := b.publishedHeight.Seal(); err != nil {
		sealErr = fmt.Errorf("failed to seal the published height: %w", err)
	}
	b.closeErr = errors.Join(b.closeIterators(), sealErr)
}

// Closes every vault iterator the builder goroutine holds.
func (b *StandardAppHashBuilder) closeIterators() error {
	errs := make([]error, 0, hashInputCount+2)
	for i := range b.reportTrackers {
		tracker := &b.reportTrackers[i]
		if tracker.storedRecordIterator != nil {
			errs = append(errs, tracker.storedRecordIterator.Close())
			tracker.storedRecordIterator = nil
		}
	}
	if b.storedAboveHeadIterator != nil {
		errs = append(errs, b.storedAboveHeadIterator.Close())
		b.storedAboveHeadIterator = nil
	}
	errs = append(errs, b.closeReadCursor())
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("failed to close hash vault iterators: %w", err)
	}
	return nil
}

// Advances it to its next record, which must be for blockHeight, and returns that record.
func nextStoredRecord(it apphash.AppHashIterator, blockHeight uint64) (*apphash.AppHashData, error) {
	ok, err := it.Next()
	if err != nil {
		return nil, fmt.Errorf("failed to read the stored app hash of block %d: %w", blockHeight, err)
	}
	if !ok {
		return nil, fmt.Errorf("no app hash is stored for block %d", blockHeight)
	}
	record := it.Entry()
	if record.BlockHeight() != blockHeight {
		return nil, fmt.Errorf("expected the stored app hash of block %d, read block %d",
			blockHeight, record.BlockHeight())
	}
	return record, nil
}

// Crashes the node because a value computed for blockHeight differs from the one it stored earlier, after flushing
// the hash log so that the rows written so far, the computed app hash among them when it is the one that changed,
// survive the crash as evidence.
func (b *StandardAppHashBuilder) panicOnMismatch(blockHeight uint64, what string, stored [32]byte, computed [32]byte) {
	logger.Error("App hash builder detected a changed hash; the node attempted to change its mind about a block "+
		"it already committed to. DO NOT RESTART WITHOUT HUMAN INVESTIGATION.",
		"blockHeight", blockHeight,
		"value", what,
		"stored", fmt.Sprintf("%x", stored),
		"computed", fmt.Sprintf("%x", computed),
	)
	if b.hashLog != nil {
		if err := b.hashLog.Flush(); err != nil {
			logger.Error("failed to flush the app hash log", "err", err)
		}
	}
	panic(fmt.Sprintf("%s of block %d changed: stored %x, computed %x", what, blockHeight, stored, computed))
}
