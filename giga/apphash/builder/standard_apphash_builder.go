package builder

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
)

var logger = seilog.NewLogger("giga", "apphash", "builder")

var _ AppHashBuilder = (*StandardAppHashBuilder)(nil)

// StandardAppHashBuilder is an AppHashBuilder that stores app hashes in a StandardHashVault.
type StandardAppHashBuilder struct {

	// The EVM chain ID committed to by every app hash.
	chainID uint64

	// The block whose app hash is defined as all zeros.
	initialBlock uint64

	// Holds the app hash data of every block built.
	vault vault.HashVault

	// Requests for the builder goroutine, run in order. Each is one of the *Request types.
	requests chan any

	// Cancelled by Close(), or by the first request that fails. Its cause is the error every later call returns.
	ctx context.Context

	// Cancels ctx with a cause.
	cancel context.CancelCauseFunc

	// Tracks the builder goroutine, which Close() waits for before closing the vault.
	wg sync.WaitGroup

	// Every field below is owned by the builder goroutine.

	// The progress of each input through the block heights, indexed by hashInput. Each report (i.e. call to a
	// Report*() method) is checked against its input's tracker before its value is used.
	reportTrackers [hashInputCount]reportTracker

	// Whether the vault holds any record.
	vaultHasRecords bool

	// The lowest block height in the vault. Valid only if vaultHasRecords.
	vaultLowest uint64

	// The highest block height in the vault. Valid only if vaultHasRecords.
	vaultHighest uint64

	// Whether SetupComplete() has succeeded.
	setupDone bool

	// The newest block whose app hash is known. The next block built chains from it.
	headHeight uint64

	// The app hash of block headHeight.
	headAppHash [32]byte

	// The newest published app hash, or nil if none has been published.
	newestPublished *apphash.AppHashData

	// After setup, the stored records above headHeight. Each block built is checked against its stored record. Nil
	// once no stored record is above headHeight.
	storedAboveHeadIterator apphash.AppHashIterator

	// The blocks above headHeight waiting for their inputs to be reported (i.e. passed to the Report*() methods),
	// by block height.
	pendingBlocks map[uint64]*pendingBlock

	// Called with each published app hash, in registration order.
	listeners []func(appHash *apphash.AppHashData)

	// The highest height passed to Prune(). Applied as blocks are published.
	requestedPruneHeight uint64

	// The height most recently passed to the vault's Prune().
	appliedPruneHeight uint64
}

// What the vault says about the builder's state at startup: its bounds, and the block the next block built chains
// from.
type startupState struct {

	// Whether the vault holds any record.
	vaultHasRecords bool

	// The lowest block height in the vault. Valid only if vaultHasRecords.
	vaultLowest uint64

	// The highest block height in the vault. Valid only if vaultHasRecords.
	vaultHighest uint64

	// The newest block whose app hash is known. The next block built chains from it.
	headHeight uint64

	// The app hash of block headHeight.
	headAppHash [32]byte
}

// Opens the builder's hash vault and starts the builder. Errors if the vault holds app hashes of another chain.
func NewStandardAppHashBuilder(
	config AppHashBuilderConfig,
	// The EVM chain ID committed to by every app hash.
	chainID uint64,
	// The block whose app hash is defined as all zeros. App hashes are computed for the blocks after it, and
	// inputs at or below it are discarded.
	initialBlock uint64,
) (*StandardAppHashBuilder, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid app hash builder config: %w", err)
	}
	hashVault, err := vault.NewStandardHashVault(config.HashVault)
	if err != nil {
		return nil, fmt.Errorf("failed to open hash vault: %w", err)
	}
	startup, err := loadStartupState(hashVault, chainID, initialBlock)
	if err != nil {
		_ = hashVault.Close()
		return nil, fmt.Errorf("failed to load the app hash builder's startup state: %w", err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	b := &StandardAppHashBuilder{
		chainID:         chainID,
		initialBlock:    initialBlock,
		vault:           hashVault,
		requests:        make(chan any, config.InputBufferSize),
		ctx:             ctx,
		cancel:          cancel,
		vaultHasRecords: startup.vaultHasRecords,
		vaultLowest:     startup.vaultLowest,
		vaultHighest:    startup.vaultHighest,
		headHeight:      startup.headHeight,
		headAppHash:     startup.headAppHash,
		pendingBlocks:   make(map[uint64]*pendingBlock),
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

func (b *StandardAppHashBuilder) ReportStateHash(ctx context.Context, blockHeight uint64, stateHash [32]byte) error {
	request := reportRequest{input: stateHashInput, blockHeight: blockHeight, value: stateHash}
	if err := b.submit(ctx, request); err != nil {
		return fmt.Errorf("failed to report the state hash of block %d: %w", blockHeight, err)
	}
	return nil
}

func (b *StandardAppHashBuilder) ReportBUD(ctx context.Context, blockHeight uint64, bud [32]byte) error {
	request := reportRequest{input: budInput, blockHeight: blockHeight, value: bud}
	if err := b.submit(ctx, request); err != nil {
		return fmt.Errorf("failed to report the BUD of block %d: %w", blockHeight, err)
	}
	return nil
}

func (b *StandardAppHashBuilder) ReportReceiptHash(
	ctx context.Context,
	blockHeight uint64,
	receiptHash [32]byte,
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
	if _, err := awaitReply(b, reply); err != nil {
		return fmt.Errorf("failed to complete app hash builder setup at block %d: %w", blockHeight, err)
	}
	return nil
}

func (b *StandardAppHashBuilder) RegisterListener(
	ctx context.Context,
	listener func(appHash *apphash.AppHashData),
) (*apphash.AppHashData, error) {
	if listener == nil {
		return nil, fmt.Errorf("app hash listener must not be nil")
	}
	reply := make(chan callResult[*apphash.AppHashData], 1)
	if err := b.submit(ctx, registerListenerRequest{listener: listener, reply: reply}); err != nil {
		return nil, fmt.Errorf("failed to register app hash listener: %w", err)
	}
	published, err := awaitReply(b, reply)
	if err != nil {
		return nil, fmt.Errorf("failed to register app hash listener: %w", err)
	}
	return published, nil
}

func (b *StandardAppHashBuilder) Iterator(
	ctx context.Context,
	startingBlockHeight uint64,
) (apphash.AppHashIterator, error) {
	reply := make(chan callResult[apphash.AppHashIterator], 1)
	if err := b.submit(ctx, iteratorRequest{startingBlockHeight: startingBlockHeight, reply: reply}); err != nil {
		return nil, fmt.Errorf("failed to create app hash iterator: %w", err)
	}
	it, err := awaitReply(b, reply)
	if err != nil {
		return nil, fmt.Errorf("failed to create app hash iterator: %w", err)
	}
	return it, nil
}

func (b *StandardAppHashBuilder) Prune(ctx context.Context, blockHeight uint64) error {
	if err := b.submit(ctx, pruneRequest{blockHeight: blockHeight}); err != nil {
		return fmt.Errorf("failed to request pruning below block %d: %w", blockHeight, err)
	}
	return nil
}

func (b *StandardAppHashBuilder) Close() error {
	b.cancel(fmt.Errorf("app hash builder is closed"))
	b.wg.Wait()
	// The builder goroutine has exited, so the iterators it held can be closed here.
	if err := errors.Join(b.closeIterators(), b.vault.Close()); err != nil {
		return fmt.Errorf("failed to close app hash builder: %w", err)
	}
	return nil
}

// Reads the builder's startup state from hashVault. Its head is the newest stored record if that is above
// initialBlock, and initialBlock otherwise. Errors if the stored records belong to a chain other than chainID.
func loadStartupState(hashVault vault.HashVault, chainID uint64, initialBlock uint64) (startupState, error) {
	startup := startupState{headHeight: initialBlock}
	ok, lowest, highest, err := hashVault.Bounds()
	if err != nil {
		return startupState{}, fmt.Errorf("failed to read hash vault bounds: %w", err)
	}
	if !ok {
		return startup, nil
	}
	startup.vaultHasRecords = true
	startup.vaultLowest = lowest
	startup.vaultHighest = highest

	newest, err := readStoredRecord(hashVault, highest)
	if err != nil {
		return startupState{}, fmt.Errorf("failed to read the newest stored app hash: %w", err)
	}
	if newest.ChainID() != chainID {
		return startupState{}, fmt.Errorf("hash vault holds app hashes for chain ID %d, not %d",
			newest.ChainID(), chainID)
	}
	if highest > initialBlock {
		startup.headHeight = highest
		startup.headAppHash = newest.AppHash()
	}
	return startup, nil
}

// Returns the record stored in hashVault at blockHeight.
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

// Waits for the reply to a request already handed to the builder goroutine.
func awaitReply[T any](b *StandardAppHashBuilder, reply <-chan callResult[T]) (T, error) {
	select {
	case result := <-reply:
		return result.value, result.err
	case <-b.ctx.Done():
		// The request may have run before the builder stopped; its result, such as an open iterator, must
		// reach the caller.
		select {
		case result := <-reply:
			return result.value, result.err
		default:
			var zero T
			return zero, b.stoppedError()
		}
	}
}

// The builder goroutine. Runs requests until Close() or a request fails.
func (b *StandardAppHashBuilder) run() {
	for {
		select {
		case <-b.ctx.Done():
			return
		case request := <-b.requests:
			if err := b.runBatch(request); err != nil {
				logger.Error("app hash builder stopped", "err", err)
				b.cancel(err)
				return
			}
		}
	}
}

// Runs first and every request already queued behind it, then publishes the blocks they completed.
func (b *StandardAppHashBuilder) runBatch(first any) error {
	if err := b.handleRequest(first); err != nil {
		return fmt.Errorf("failed to handle %T: %w", first, err)
	}
	for {
		select {
		case <-b.ctx.Done():
			return nil
		case request := <-b.requests:
			if err := b.handleRequest(request); err != nil {
				return fmt.Errorf("failed to handle %T: %w", request, err)
			}
		default:
			if err := b.publishCompleteBlocks(); err != nil {
				return fmt.Errorf("failed to publish app hashes: %w", err)
			}
			return nil
		}
	}
}

// Runs one request. An error stops the builder.
func (b *StandardAppHashBuilder) handleRequest(request any) error {
	switch r := request.(type) {
	case reportRequest:
		if err := b.handleReport(r.input, r.blockHeight, r.value); err != nil {
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
	case registerListenerRequest:
		published, err := b.registerListener(r.listener)
		r.reply <- callResult[*apphash.AppHashData]{value: published, err: err}
		return nil
	case iteratorRequest:
		it, err := b.newIterator(r.startingBlockHeight)
		r.reply <- callResult[apphash.AppHashIterator]{value: it, err: err}
		return nil
	case pruneRequest:
		b.requestedPruneHeight = max(b.requestedPruneHeight, r.blockHeight)
		return nil
	default:
		return fmt.Errorf("unknown app hash builder request %T", request)
	}
}

// Takes in one report (i.e. one call to a Report*() method): an input's value for one block. Before setup, a value
// for a stored block is checked against the stored record.
func (b *StandardAppHashBuilder) handleReport(input hashInput, blockHeight uint64, value [32]byte) error {
	tracker := &b.reportTrackers[input]
	firstReport := !tracker.startingPointKnown
	if !firstReport && blockHeight != tracker.nextHeight {
		return fmt.Errorf("%s reported for block %d, expected block %d", input, blockHeight, tracker.nextHeight)
	}
	tracker.startingPointKnown = true
	tracker.nextHeight = blockHeight + 1

	if blockHeight <= b.initialBlock {
		if firstReport {
			logger.Info("discarding app hash inputs at or below the initial block",
				"input", input.String(), "blockHeight", blockHeight, "initialBlock", b.initialBlock)
		}
		return nil
	}
	if !b.setupDone && b.vaultHasRecords && blockHeight <= b.vaultHighest {
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
		panicOnMismatch(blockHeight, input.String(), storedValue, value)
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

// Adds listener, returning the newest published app hash. Errors before setup.
func (b *StandardAppHashBuilder) registerListener(
	listener func(appHash *apphash.AppHashData),
) (*apphash.AppHashData, error) {
	if !b.setupDone {
		return nil, fmt.Errorf("cannot register an app hash listener before setup is complete")
	}
	b.listeners = append(b.listeners, listener)
	return b.newestPublished, nil
}

// Returns an iterator over the published app hashes from startingBlockHeight. Errors before setup.
func (b *StandardAppHashBuilder) newIterator(startingBlockHeight uint64) (apphash.AppHashIterator, error) {
	if !b.setupDone {
		return nil, fmt.Errorf("cannot iterate app hashes before setup is complete")
	}
	if b.newestPublished == nil || startingBlockHeight > b.newestPublished.BlockHeight() {
		return emptyIterator{}, nil
	}
	it, err := b.vault.Iterator(startingBlockHeight, b.newestPublished.BlockHeight())
	if err != nil {
		return nil, fmt.Errorf("failed to iterate app hashes from block %d: %w", startingBlockHeight, err)
	}
	return it, nil
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
		b.reportTrackers[i].nextHeight = blockHeight + 1
	}
	b.setupDone = true
	return nil
}

// Used by setup when the storage layer is ahead of the hash vault: builds and stores the app hash of every block
// after the newest one already known (the newest stored, or the initial block if none is stored), using the inputs
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
		record := b.newRecord(height, block)
		records = append(records, record)
		b.headHeight = height
		b.headAppHash = record.AppHash()
	}
	if err := b.appendToVault(records); err != nil {
		return fmt.Errorf("failed to store app hashes through block %d: %w", blockHeight, err)
	}
	b.newestPublished = records[len(records)-1]
	return nil
}

// Used by setup when the hash vault is at or ahead of the storage layer: the next block built chains from the
// storage layer's height (or from the initial block, if that is higher), and each block built after setup is checked
// against the app hash already stored for it.
func (b *StandardAppHashBuilder) rewindHead(
	// The storage layer's height: the last block every store has applied.
	blockHeight uint64,
) error {
	b.headHeight = max(blockHeight, b.initialBlock)
	b.headAppHash = [32]byte{}
	b.newestPublished = nil
	if !b.vaultHasRecords || b.vaultHighest <= b.initialBlock {
		return nil
	}

	firstRead := b.headHeight
	if b.headHeight == b.initialBlock {
		firstRead = b.initialBlock + 1
	}
	iterator, err := b.vault.Iterator(firstRead, b.vaultHighest)
	if err != nil {
		return fmt.Errorf("failed to read stored app hashes from block %d: %w", firstRead, err)
	}
	if b.headHeight > b.initialBlock {
		anchor, err := nextStoredRecord(iterator, b.headHeight)
		if err != nil {
			return fmt.Errorf("failed to read the app hash of block %d: %w", b.headHeight,
				errors.Join(err, iterator.Close()))
		}
		b.headAppHash = anchor.AppHash()
		b.newestPublished = anchor
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
// already stored, and passes them to the listeners in block order. Runs after each batch of requests, but does
// nothing during setup, when storeSetupBlocks() builds the reported blocks instead.
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
		record := b.newRecord(height, block)
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
		b.newestPublished = record
		for _, listener := range b.listeners {
			listener(record)
		}
	}
	if err := b.applyPrune(); err != nil {
		return fmt.Errorf("failed to apply the requested prune: %w", err)
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
		panicOnMismatch(record.BlockHeight(), "app hash", stored.AppHash(), record.AppHash())
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
// chained from the app hash of the block before it.
func (b *StandardAppHashBuilder) newRecord(blockHeight uint64, block *pendingBlock) *apphash.AppHashData {
	return apphash.NewAppHashData(
		b.chainID,
		blockHeight,
		block.values[blockHashInput],
		block.values[stateHashInput],
		block.values[budInput],
		block.values[receiptHashInput],
		b.headAppHash,
	)
}

// Durably appends records to the hash vault, and updates the builder's record of the vault's lowest and highest
// block heights to match.
func (b *StandardAppHashBuilder) appendToVault(records []*apphash.AppHashData) error {
	last := records[len(records)-1].BlockHeight()
	if err := b.vault.Append(records); err != nil {
		return fmt.Errorf("failed to append app hashes through block %d to the hash vault: %w", last, err)
	}
	if !b.vaultHasRecords {
		b.vaultHasRecords = true
		b.vaultLowest = records[0].BlockHeight()
	}
	b.vaultHighest = last
	return nil
}

// Passes the requested prune height on to the vault, capped so the newest published app hash is kept.
func (b *StandardAppHashBuilder) applyPrune() error {
	target := min(b.requestedPruneHeight, b.newestPublished.BlockHeight())
	if target <= b.appliedPruneHeight {
		return nil
	}
	if err := b.vault.Prune(target); err != nil {
		return fmt.Errorf("failed to prune app hashes below block %d: %w", target, err)
	}
	b.appliedPruneHeight = target
	return nil
}

// Closes every vault iterator the builder goroutine holds.
func (b *StandardAppHashBuilder) closeIterators() error {
	errs := make([]error, 0, hashInputCount+1)
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

// Crashes the node because a value computed for blockHeight differs from the one it stored earlier.
func panicOnMismatch(blockHeight uint64, what string, stored [32]byte, computed [32]byte) {
	logger.Error("App hash builder detected a changed hash; the node attempted to change its mind about a block "+
		"it already committed to. DO NOT RESTART WITHOUT HUMAN INVESTIGATION.",
		"blockHeight", blockHeight,
		"value", what,
		"stored", fmt.Sprintf("%x", stored),
		"computed", fmt.Sprintf("%x", computed),
	)
	panic(fmt.Sprintf("%s of block %d changed: stored %x, computed %x", what, blockHeight, stored, computed))
}
