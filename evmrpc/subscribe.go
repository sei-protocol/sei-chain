package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const SleepInterval = 5 * time.Second
const NewHeadsListenerBuffer = 10

// newHeadReceiptsTimeout bounds how long a newHeads header waits for its block's receipts to be written.
const newHeadReceiptsTimeout = time.Second

// newHeadReceiptsPollInterval is how often a newHeads header checks whether its block's receipts are written.
const newHeadReceiptsPollInterval = 5 * time.Millisecond

type SubscriptionAPI struct {
	tmClient            client.LocalClient
	subscriptionManager *SubscriptionManager
	subscriptonConfig   *SubscriptionConfig

	logFetcher          *LogFetcher
	newHeadListenersMtx *sync.RWMutex
	newHeadListeners    map[rpc.ID]chan map[string]interface{}
	connectionType      ConnectionType

	// logSubsCount bounds the number of concurrent logs subscriptions
	logSubsCount atomic.Uint64
}

type SubscriptionConfig struct {
	subscriptionCapacity int
	newHeadLimit         uint64
	logLimit             uint64
}

func NewSubscriptionAPI(tmClient client.LocalClient, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, logFetcher *LogFetcher, subscriptionConfig *SubscriptionConfig, filterConfig *FilterConfig, connectionType ConnectionType, blockHeaderNotifier *BlockHeaderNotifier) *SubscriptionAPI {
	logFetcher.filterConfig = filterConfig
	api := &SubscriptionAPI{
		tmClient:            tmClient,
		subscriptonConfig:   subscriptionConfig,
		logFetcher:          logFetcher,
		newHeadListenersMtx: &sync.RWMutex{},
		newHeadListeners:    make(map[rpc.ID]chan map[string]interface{}),
		connectionType:      connectionType,
		// subscriptionManager is only constructed for the legacy
		// event-bus path below; under Autobahn the notifier feeds the
		// fan-out directly and the manager is unused.
	}
	if blockHeaderNotifier != nil {
		// Autobahn (and any future direct-channel) path. The producer
		// pushes one event per committed block; there is no Tendermint
		// event-bus subscription.
		go api.runNewHeadsFromNotifier(blockHeaderNotifier, k, ctxProvider)
	} else {
		// Legacy CometBFT path: subscribe to the Tendermint event bus.
		api.subscriptionManager = NewSubscriptionManager(tmClient)
		id, subCh, err := api.subscriptionManager.Subscribe(context.Background(), NewHeadQueryBuilder(), api.subscriptonConfig.subscriptionCapacity)
		if err != nil {
			panic(err)
		}
		go func() {
			defer recoverAndLog()
			defer func() {
				_ = api.subscriptionManager.Unsubscribe(context.Background(), id)
			}()
			for {
				res := <-subCh
				eventHeader := res.Data.(tmtypes.EventDataNewBlockHeader)
				height := eventHeader.Header.Height
				baseFeePerGas, err := pickHeadBaseFee(k.GetNextBaseFeePerGas, ctxProvider, height)
				if err != nil {
					logger.Error("dropping newHeads event: base fee unavailable", "height", height, "err", err)
					continue
				}
				totals := api.newHeadReceiptTotals(height, eventHeader.ResultFinalizeBlock.TxResults)
				ethHeader, err := encodeTmHeader(eventHeader, baseFeePerGas, totals)
				if err != nil {
					fmt.Printf("error encoding new head event %#v due to %s\n", res.Data, err)
					continue
				}
				api.broadcastNewHead(ethHeader)
			}
		}()
	}
	return api
}

func (a *SubscriptionAPI) runNewHeadsFromNotifier(notifier *BlockHeaderNotifier, k *keeper.Keeper, ctxProvider func(int64) sdk.Context) {
	defer recoverAndLog()
	for evt := range notifier.recv() {
		// Defend against a misbehaving producer. OnBlockCommitted's
		// contract requires non-nil header/response, but a single bad
		// event must not kill the fan-out goroutine for all subscribers.
		if evt.header == nil || evt.response == nil {
			fmt.Printf("dropping malformed newHeads event: header=%v response=%v\n", evt.header, evt.response)
			continue
		}
		ctx := ctxProvider(evt.header.Height)
		baseFeePerGas, err := pickHeadBaseFee(k.GetNextBaseFeePerGas, ctxProvider, evt.header.Height)
		if err != nil {
			logger.Error("dropping newHeads event: base fee unavailable", "height", evt.header.Height, "err", err)
			continue
		}
		// Source gasLimit from the active SDK ConsensusParams rather than
		// evt.response.ConsensusParamUpdates: the latter is only populated
		// on actual updates (nil for nearly every block). See block.go's
		// GetBlockByNumber for the same pattern + rationale.
		var gasLimit int64
		if cp := ctx.ConsensusParams(); cp != nil && cp.Block != nil {
			gasLimit = cp.Block.MaxGas
		}
		totals := a.newHeadReceiptTotals(evt.header.Height, evt.response.TxResults)
		ethHeader := encodeCommittedBlock(evt, baseFeePerGas, gasLimit, totals)
		a.broadcastNewHead(ethHeader)
	}
}

// pickHeadBaseFee returns the baseFeePerGas eth_getBlockByNumber reports for the block at height: the base
// fee that block executed with, or the default minimum fee at height 1. It returns an error when the
// parent's state is unavailable.
func pickHeadBaseFee(getNextBaseFee func(sdk.Context) sdk.Dec, ctxProvider func(int64) sdk.Context, height int64) (*big.Int, error) {
	if height <= 1 {
		return evmtypes.DefaultMinFeePerGas.TruncateInt().BigInt(), nil
	}
	// The state committed at N holds the base fee for N+1, so a block's own fee is in its parent's state.
	parentCtx, err := ctxAtHeight(ctxProvider, height-1)
	if err != nil {
		return nil, err
	}
	return getNextBaseFee(parentCtx).TruncateInt().BigInt(), nil
}

// newHeadReceiptTotals returns the logsBloom and gasUsed eth_getBlockByNumber reports for the block at
// height. When that block's receipts cannot be read, it returns unreadReceiptTotals(txResults) instead.
func (a *SubscriptionAPI) newHeadReceiptTotals(height int64, txResults []*abci.ExecTxResult) receiptTotals {
	totals, err := a.readReceiptTotals(height)
	if err != nil {
		logger.Warn("newHeads header reports a full logs bloom: receipts unreadable", "height", height, "err", err)
		return unreadReceiptTotals(txResults)
	}
	return totals
}

// readReceiptTotals returns the receiptTotals of the block at height once its receipts are written. It
// returns an error when they are not written within newHeadReceiptsTimeout or cannot be read, and, like
// ctxAtHeight, re-raises a panic that is not an error or is a runtime.Error.
func (a *SubscriptionAPI) readReceiptTotals(height int64) (totals receiptTotals, err error) {
	// This runs on the goroutine that serves every newHeads subscriber, so a read that panics because its
	// data is unavailable takes the fallback rather than ending that goroutine.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("receipts of height %d are unreadable: %w", height, errorFromPanic(r))
		}
	}()
	f := a.logFetcher
	if err = awaitReceipts(f.k.ReceiptStore(), height, newHeadReceiptsTimeout); err != nil {
		return receiptTotals{}, err
	}
	block, err := blockByNumberWithRetry(context.Background(), f.tmClient, &height, 0)
	if err != nil {
		return receiptTotals{}, err
	}
	if err = requireReceiptsReadable(f, block); err != nil {
		return receiptTotals{}, err
	}
	msgs, err := filterTransactions(f.k, f.ctxProvider, f.txConfigProvider, block, false, f.cacheCreationMutex, f.globalBlockCache)
	if err != nil {
		return receiptTotals{}, err
	}
	return listedReceiptTotals(f.k, f.ctxProvider(LatestCtxHeight), block, msgs, f.cacheCreationMutex, f.globalBlockCache), nil
}

// requireReceiptsReadable returns the error that reading the receipt of an EVM transaction in block fails
// with, other than receipt.ErrNotFound.
func requireReceiptsReadable(f *LogFetcher, block *coretypes.ResultBlock) error {
	decode := f.txConfigProvider(block.Block.Height).TxDecoder()
	latestCtx := f.ctxProvider(LatestCtxHeight)
	for _, tx := range block.Block.Txs {
		sdkTx, err := decode(tx)
		if err != nil {
			continue
		}
		for _, msg := range sdkTx.GetMsgs() {
			evmMsg, ok := msg.(*evmtypes.MsgEVMTransaction)
			if !ok || evmMsg.IsAssociateTx() {
				continue
			}
			ethtx, _ := evmMsg.AsTransaction()
			if ethtx == nil {
				continue
			}
			// filterTransactions leaves out a transaction whose receipt read fails for any reason, which
			// would drop its logs from a header that, unlike eth_getBlockByNumber, is never read again.
			_, err := getOrSetCachedReceiptErr(f.cacheCreationMutex, f.globalBlockCache, latestCtx, f.k, block, ethtx.Hash())
			if err != nil && !errors.Is(err, receipt.ErrNotFound) {
				return fmt.Errorf("reading the receipt of %s: %w", ethtx.Hash().Hex(), err)
			}
		}
	}
	return nil
}

// awaitReceipts waits until store has written the receipts of height, and returns an error if it has not
// within timeout.
func awaitReceipts(store receipt.ReceiptStore, height int64, timeout time.Duration) error {
	if store == nil {
		return receipt.ErrNotConfigured
	}
	deadline := time.Now().Add(timeout)
	// Commit queues a block's receipts for an asynchronous write and blocks while that queue is full, so the
	// store trails the committed head by at most the queue's depth and this wait is normally milliseconds.
	for store.LatestVersion() < height {
		if time.Now().After(deadline) {
			return fmt.Errorf("receipts of height %d not written within %s (receipt store is at %d)", height, timeout, store.LatestVersion())
		}
		time.Sleep(newHeadReceiptsPollInterval)
	}
	return nil
}

// unreadReceiptTotals returns the receiptTotals a newHeads header reports for a block whose receipts cannot
// be read: a logs bloom that matches every filter, and the gas used summed over txResults.
func unreadReceiptTotals(txResults []*abci.ExecTxResult) receiptTotals {
	var gasUsed uint64
	for _, txRes := range txResults {
		gasUsed += uint64(txRes.GasUsed) //nolint:gosec
	}
	return receiptTotals{logsBloom: FullBloom(), gasUsed: gasUsed}
}

func (a *SubscriptionAPI) broadcastNewHead(ethHeader map[string]interface{}) {
	a.newHeadListenersMtx.Lock()
	defer a.newHeadListenersMtx.Unlock()
	toDelete := []rpc.ID{}
	for id, c := range a.newHeadListeners {
		if !handleListener(c, ethHeader) {
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		delete(a.newHeadListeners, id)
	}
}

func handleListener(c chan map[string]interface{}, ethHeader map[string]interface{}) bool {
	// if the channel is already closed, sending to it/closing it will panic
	defer func() { _ = recover() }()
	select {
	case c <- ethHeader:
		return true
	default:
		// this path is hit when the buffer is full, meaning that the subscriber is not consuming
		// fast enough
		close(c)
		return false
	}
}

func (a *SubscriptionAPI) NewHeads(ctx context.Context) (s *rpc.Subscription, err error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "eth_newHeads", a.connectionType, startTime, err, recover())
	}()
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()
	listener := make(chan map[string]interface{}, NewHeadsListenerBuffer)
	a.newHeadListenersMtx.Lock()
	defer a.newHeadListenersMtx.Unlock()
	if uint64(len(a.newHeadListeners)) >= a.subscriptonConfig.newHeadLimit {
		return nil, errors.New("no new subscription can be created")
	}
	a.newHeadListeners[rpcSub.ID] = listener

	go func() {
		defer recoverAndLog()
	OUTER:
		for {
			select {
			case res, ok := <-listener:
				if err := notifier.Notify(rpcSub.ID, res); err != nil {
					break OUTER
				}
				if !ok {
					break OUTER
				}
			case <-rpcSub.Err():
				break OUTER
			}
		}
		a.newHeadListenersMtx.Lock()
		defer a.newHeadListenersMtx.Unlock()
		delete(a.newHeadListeners, rpcSub.ID)
		defer func() { _ = recover() }() // might have already been closed
		close(listener)
	}()

	return rpcSub, nil
}

func (a *SubscriptionAPI) Logs(ctx context.Context, filter *filters.FilterCriteria) (s *rpc.Subscription, _err error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "eth_logs", a.connectionType, startTime, _err, recover())
	}()
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}
	// create empty filter if filter does not exist
	if filter == nil {
		filter = &filters.FilterCriteria{}
	}
	// fromBlock 0 with a tag toBlock is how ethclient sends an open-ended query, so it subscribes
	// to new logs as an omitted fromBlock does.
	if filter.FromBlock != nil && filter.FromBlock.Int64() == 0 &&
		filter.ToBlock != nil && filter.ToBlock.Int64() < 0 {
		filter = &filters.FilterCriteria{
			Addresses: filter.Addresses,
			Topics:    filter.Topics,
		}
	}

	start, err := a.logFetcher.pollStart(ctx, *filter)
	if err != nil {
		return nil, err
	}

	// Bound the number of concurrent logs subscriptions
	if !a.acquireLogSub() {
		return nil, errors.New("no new subscription can be created")
	}

	rpcSub := notifier.CreateSubscription()
	// The RPC framework cancels the per-call ctx as soon as eth_subscribe
	// returns. Bind a subscription-scoped context that keeps any request
	// values but outlives that cancel.
	subCtx, cancel := bindSubscriptionContext(ctx, rpcSub.Err())

	// Track subscription metrics
	wpMetrics := GetGlobalMetrics()
	wpMetrics.RecordSubscriptionStart()

	if filter.BlockHash != nil {
		go func() {
			var err error
			defer recoverAndLog()
			defer a.releaseLogSub()
			defer wpMetrics.RecordSubscriptionEnd()
			defer cancel()
			logs, _, err := a.logFetcher.GetLogsByFilters(subCtx, *filter)
			if err != nil {
				if subCtx.Err() != nil {
					return
				}
				wpMetrics.RecordSubscriptionError()
				_ = notifier.Notify(rpcSub.ID, newLogsErrorNotification(err))
				return
			}
			for _, log := range logs {
				if err = notifier.Notify(rpcSub.ID, log); err != nil {
					return
				}
			}
		}()
		return rpcSub, nil
	}

	go func() {
		var err error
		defer recoverAndLog()
		defer a.releaseLogSub()
		defer wpMetrics.RecordSubscriptionEnd()
		defer cancel()
		cursor := start
		for {
			var logs []*ethtypes.Log
			// Not admitted: a refused poll would end the subscription (any error but an oversized
			// block does, below), and the loop is paced by SleepInterval for at most
			// max_subscriptions_logs subscriptions. A historical fromBlock still makes each poll
			// scan up to max_blocks_for_log blocks until it catches up.
			logs, cursor, err = a.logFetcher.pollLogs(subCtx, *filter, cursor, nil)
			if err != nil {
				if subCtx.Err() != nil {
					return
				}
				wpMetrics.RecordSubscriptionError()
				_ = notifier.Notify(rpcSub.ID, newLogsErrorNotification(err))
				// A subscription moves on past a block it cannot serve, as a filter poll does.
				var oversized *oversizedBlockError
				if !errors.As(err, &oversized) {
					return
				}
			}
			for _, log := range logs {
				if err = notifier.Notify(rpcSub.ID, log); err != nil {
					return
				}
			}
			if logsExhausted(*filter, cursor) {
				return
			}
			// Wait before the next poll, but stop promptly when the
			// subscription ends (subCtx is cancelled on rpcSub.Err()).
			select {
			case <-subCtx.Done():
				return
			case <-time.After(SleepInterval):
			}
		}
	}()

	return rpcSub, nil
}

// logsErrorNotification is the notification a logs subscription sends for a poll that failed.
// BlockNumber is the block whose logs were skipped when the subscription continues past it.
type logsErrorNotification struct {
	Error       string          `json:"error"`
	BlockNumber *hexutil.Uint64 `json:"blockNumber,omitempty"`
}

// newLogsErrorNotification returns the notification for a poll that failed with err.
func newLogsErrorNotification(err error) logsErrorNotification {
	notification := logsErrorNotification{Error: err.Error()}
	var oversized *oversizedBlockError
	if errors.As(err, &oversized) {
		height := hexutil.Uint64(toUint64(oversized.height))
		notification.BlockNumber = &height
	}
	return notification
}

// bindSubscriptionContext returns a context for long-lived eth_subscribe work.
func bindSubscriptionContext(ctx context.Context, subErr <-chan error) (context.Context, context.CancelFunc) {
	subCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		select {
		case <-subErr:
			cancel()
		case <-subCtx.Done():
		}
	}()
	return subCtx, cancel
}

// acquireLogSub reserves a logs-subscription slot
func (a *SubscriptionAPI) acquireLogSub() bool {
	for {
		cur := a.logSubsCount.Load()
		if cur >= a.subscriptonConfig.logLimit {
			return false
		}
		if a.logSubsCount.CompareAndSwap(cur, cur+1) {
			return true
		}
		// A concurrent acquire/release changed the count; re-read and retry.
	}
}

// releaseLogSub frees a slot reserved by acquireLogSub.
func (a *SubscriptionAPI) releaseLogSub() {
	for {
		cur := a.logSubsCount.Load()
		if cur == 0 {
			return
		}
		if a.logSubsCount.CompareAndSwap(cur, cur-1) {
			return
		}
	}
}

const SubscriberPrefix = "evm.rpc."

type SubscriberID uint64

type SubInfo struct {
	Query          string
	SubscriptionCh <-chan coretypes.ResultEvent
}

type SubscriptionManager struct {
	subMu            sync.Mutex
	NextID           SubscriberID
	SubscriptionInfo map[SubscriberID]SubInfo
	tmClient         client.LocalClient
}

func NewSubscriptionManager(tmClient client.LocalClient) *SubscriptionManager {
	return &SubscriptionManager{
		subMu:            sync.Mutex{},
		NextID:           1,
		SubscriptionInfo: make(map[SubscriberID]SubInfo),
		tmClient:         tmClient,
	}
}

func (s *SubscriptionManager) Subscribe(ctx context.Context, q *QueryBuilder, limit int) (SubscriberID, <-chan coretypes.ResultEvent, error) {
	query := q.Build()
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.NextID
	// ignore deprecation here since the new endpoint does not support polling
	//nolint:staticcheck
	res, err := s.tmClient.Subscribe(ctx, fmt.Sprintf("%s%d", SubscriberPrefix, id), query, limit)
	if err != nil {
		return 0, nil, err
	}
	s.SubscriptionInfo[id] = SubInfo{Query: query, SubscriptionCh: res}
	s.NextID++
	return id, res, nil
}

func (s *SubscriptionManager) Unsubscribe(ctx context.Context, id SubscriberID) error {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	// ignore deprecation here since the new endpoint does not support polling
	//nolint:staticcheck
	err := s.tmClient.Unsubscribe(ctx, SubscriberPrefix, s.SubscriptionInfo[id].Query)
	if err != nil {
		return err
	}
	delete(s.SubscriptionInfo, id)
	return nil
}

// encodeCommittedBlock builds the eth_newHeads payload for an Autobahn-
// committed block. It differs from encodeTmHeader in two notable ways:
//
//  1. "hash" is the explicit Autobahn block-header hash from evt.hash
//     (the same value the EVM receipt store records as blockHash). See
//     blockHeaderEvent's doc for the rationale.
//  2. parentHash, receiptsRoot, and transactionsRoot are zero. The
//     Autobahn block-execution path does not compute a Tendermint-style
//     hash chain (LastBlockID / LastResultsHash / DataHash), so there is
//     nothing meaningful to surface for those fields. Subscribers that
//     chain-validate the head stream will need a different mechanism
//     under Autobahn.
//
// stateRoot is taken from evt.response.AppHash (the AppHash produced by
// finalizing *this* block). evt.header.AppHash would be wrong: by
// Tendermint convention Header.AppHash holds the result of the previous
// block, not the current one, so the producer leaves it unset.
//
// gasLimit is read by the caller from the active SDK ConsensusParams
// (see runNewHeadsFromNotifier); ConsensusParamUpdates on the response
// would be nil for the vast majority of blocks.
func encodeCommittedBlock(evt blockHeaderEvent, baseFee *big.Int, gasLimit int64, totals receiptTotals) map[string]interface{} {
	blockHash := common.BytesToHash(evt.hash)
	number := big.NewInt(evt.header.Height)
	miner := common.BytesToAddress(evt.header.ProposerAddress)
	appHash := common.BytesToHash(evt.response.AppHash)
	return map[string]interface{}{
		"difficulty":            (*hexutil.Big)(utils.Big0), // inapplicable to Sei
		"extraData":             hexutil.Bytes{},            // inapplicable to Sei
		"gasLimit":              hexutil.Uint64(gasLimit),   //nolint:gosec
		"gasUsed":               hexutil.Uint64(totals.gasUsed),
		"logsBloom":             totals.logsBloom,
		"miner":                 miner,
		"nonce":                 ethtypes.BlockNonce{}, // inapplicable to Sei
		"number":                (*hexutil.Big)(number),
		"parentHash":            common.Hash{}, // see function doc
		"receiptsRoot":          common.Hash{}, // see function doc
		"sha3Uncles":            common.Hash{}, // inapplicable to Sei
		"stateRoot":             appHash,
		"timestamp":             hexutil.Uint64(evt.header.Time.Unix()),      //nolint:gosec
		"milliTimestamp":        hexutil.Uint64(evt.header.Time.UnixMilli()), //nolint:gosec
		"transactionsRoot":      common.Hash{},                               // see function doc
		"mixHash":               common.Hash{},                               // inapplicable to Sei
		"excessBlobGas":         hexutil.Uint64(0),                           // inapplicable to Sei
		"parentBeaconBlockRoot": common.Hash{},                               // inapplicable to Sei
		"hash":                  blockHash,
		"baseFeePerGas":         (*hexutil.Big)(baseFee),
		"withdrawalsRoot":       common.Hash{},     // inapplicable to Sei
		"blobGasUsed":           hexutil.Uint64(0), // inapplicable to Sei
	}
}

func encodeTmHeader(
	header tmtypes.EventDataNewBlockHeader,
	baseFee *big.Int,
	totals receiptTotals,
) (map[string]interface{}, error) {
	blockHash := common.HexToHash(header.Header.Hash().String())
	number := big.NewInt(header.Header.Height)
	miner := common.HexToAddress(header.Header.ProposerAddress.String())
	lastHash := common.HexToHash(header.Header.LastBlockID.Hash.String())
	resultHash := common.HexToHash(header.Header.LastResultsHash.String())
	appHash := common.HexToHash(header.Header.AppHash.String())
	txHash := common.HexToHash(header.Header.DataHash.String())
	gasLimit := uint64(header.ResultFinalizeBlock.ConsensusParamUpdates.Block.MaxGas) //nolint:gosec
	result := map[string]interface{}{
		"difficulty":            (*hexutil.Big)(utils.Big0), // inapplicable to Sei
		"extraData":             hexutil.Bytes{},            // inapplicable to Sei
		"gasLimit":              hexutil.Uint64(gasLimit),
		"gasUsed":               hexutil.Uint64(totals.gasUsed),
		"logsBloom":             totals.logsBloom,
		"miner":                 miner,
		"nonce":                 ethtypes.BlockNonce{}, // inapplicable to Sei
		"number":                (*hexutil.Big)(number),
		"parentHash":            lastHash,
		"receiptsRoot":          resultHash,
		"sha3Uncles":            common.Hash{}, // inapplicable to Sei
		"stateRoot":             appHash,
		"timestamp":             hexutil.Uint64(header.Header.Time.Unix()),      //nolint:gosec
		"milliTimestamp":        hexutil.Uint64(header.Header.Time.UnixMilli()), //nolint:gosec
		"transactionsRoot":      txHash,
		"mixHash":               common.Hash{},     // inapplicable to Sei
		"excessBlobGas":         hexutil.Uint64(0), // inapplicable to Sei
		"parentBeaconBlockRoot": common.Hash{},     // inapplicable to Sei
		"hash":                  blockHash,
		"withdrawlsRoot":        common.Hash{}, // inapplicable to Sei
		"baseFeePerGas":         (*hexutil.Big)(baseFee),
		"withdrawalsRoot":       common.Hash{},     // inapplicable to Sei
		"blobGasUsed":           hexutil.Uint64(0), // inapplicable to Sei
	}
	return result, nil
}
