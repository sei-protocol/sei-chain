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
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const SleepInterval = 5 * time.Second
const NewHeadsListenerBuffer = 10

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
				ctx := ctxProvider(eventHeader.Header.Height)
				baseFeePerGas := k.GetNextBaseFeePerGas(ctx).TruncateInt().BigInt()
				ethHeader, err := encodeTmHeader(eventHeader, baseFeePerGas)
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
		baseFeePerGas := pickHeadBaseFee(k.GetNextBaseFeePerGas, ctxProvider, evt.header.Height)
		// Source gasLimit from the active SDK ConsensusParams rather than
		// evt.response.ConsensusParamUpdates: the latter is only populated
		// on actual updates (nil for nearly every block). See block.go's
		// GetBlockByNumber for the same pattern + rationale.
		var gasLimit int64
		if cp := ctx.ConsensusParams(); cp != nil && cp.Block != nil {
			gasLimit = cp.Block.MaxGas
		}
		ethHeader := encodeCommittedBlock(evt, baseFeePerGas, gasLimit)
		a.broadcastNewHead(ethHeader)
	}
}

// pickHeadBaseFee returns the baseFeePerGas to attach to the eth_newHeads
// notification for the block at `height`. Mirrors block.go's
// GetBlockByNumber: GetNextBaseFeePerGas(ctx_at_N) is the fee for N+1, so
// we call it on the *parent* ctx (height-1). Genesis (height 1) has no
// parent; return the configured default min fee instead.
//
// `getNextBaseFee` is a function pointer rather than a *keeper.Keeper
// method so tests can inject a fake without needing a full keeper.
func pickHeadBaseFee(getNextBaseFee func(sdk.Context) sdk.Dec, ctxProvider func(int64) sdk.Context, height int64) *big.Int {
	if height > 1 {
		return getNextBaseFee(ctxProvider(height - 1)).TruncateInt().BigInt()
	}
	return evmtypes.DefaultMinFeePerGas.TruncateInt().BigInt()
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
	// when fromBlock is 0 and toBlock is latest, adjust the filter
	// to unbounded filter
	if filter.FromBlock != nil && filter.FromBlock.Int64() == 0 &&
		filter.ToBlock != nil && filter.ToBlock.Int64() < 0 {
		latest := big.NewInt(a.logFetcher.ctxProvider(LatestCtxHeight).BlockHeight())
		unboundedFilter := &filters.FilterCriteria{
			FromBlock: latest, // set to latest block height
			ToBlock:   nil,    // set to nil to continue listening
			Addresses: filter.Addresses,
			Topics:    filter.Topics,
		}
		filter = unboundedFilter
	}

	// Bound the number of concurrent logs subscriptions
	if !a.acquireLogSub() {
		return nil, errors.New("no new subscription can be created")
	}

	rpcSub := notifier.CreateSubscription()

	// Track subscription metrics
	wpMetrics := GetGlobalMetrics()
	wpMetrics.RecordSubscriptionStart()

	if filter.BlockHash != nil {
		go func() {
			var err error
			defer recoverAndLog()
			defer a.releaseLogSub()
			defer wpMetrics.RecordSubscriptionEnd()
			logs, _, err := a.logFetcher.GetLogsByFilters(ctx, *filter, 0)
			if err != nil {
				wpMetrics.RecordSubscriptionError()
				_ = notifier.Notify(rpcSub.ID, err)
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
		begin := int64(0)
		for {
			var logs []*ethtypes.Log
			var lastToHeight int64
			logs, lastToHeight, err = a.logFetcher.GetLogsByFilters(ctx, *filter, begin)
			if err != nil {
				wpMetrics.RecordSubscriptionError()
				_ = notifier.Notify(rpcSub.ID, err)
				return
			}
			for _, log := range logs {
				if err = notifier.Notify(rpcSub.ID, log); err != nil {
					return
				}
			}
			if filter.ToBlock != nil && lastToHeight >= filter.ToBlock.Int64() {
				return
			}
			begin = lastToHeight
			filter.FromBlock = big.NewInt(lastToHeight + 1)
			// Wait before the next poll, but stop promptly if the client
			// disconnects or unsubscribes (rpcSub.Err()). Note: ctx here is the
			// per-call context, which the RPC framework cancels as soon as the
			// eth_subscribe call returns, so it must NOT be used to tear down the
			// long-lived subscription loop.
			select {
			case <-rpcSub.Err():
				return
			case <-time.After(SleepInterval):
			}
		}
	}()

	return rpcSub, nil
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
func encodeCommittedBlock(evt blockHeaderEvent, baseFee *big.Int, gasLimit int64) map[string]interface{} {
	blockHash := common.BytesToHash(evt.hash)
	number := big.NewInt(evt.header.Height)
	miner := common.BytesToAddress(evt.header.ProposerAddress)
	appHash := common.BytesToHash(evt.response.AppHash)
	// TODO(autobahn): TxResult.GasUsed can be wrong for ante-failing EVM
	// txs; block.go (GetBlockByNumber) sums receipt.GasUsed for that
	// reason. We approximate here to keep newHeads cheap; subscribers
	// needing exact gas should call eth_getBlockByNumber.
	var totalGasUsed int64
	for _, txRes := range evt.response.TxResults {
		totalGasUsed += txRes.GasUsed
	}
	return map[string]interface{}{
		"difficulty":            (*hexutil.Big)(utils.Big0),   // inapplicable to Sei
		"extraData":             hexutil.Bytes{},              // inapplicable to Sei
		"gasLimit":              hexutil.Uint64(gasLimit),     //nolint:gosec
		"gasUsed":               hexutil.Uint64(totalGasUsed), //nolint:gosec
		"logsBloom":             ethtypes.Bloom{},             // TODO(autobahn): derive from receipts so newHeads subscribers can pre-filter logs
		"miner":                 miner,
		"nonce":                 ethtypes.BlockNonce{}, // inapplicable to Sei
		"number":                (*hexutil.Big)(number),
		"parentHash":            common.Hash{}, // see function doc
		"receiptsRoot":          common.Hash{}, // see function doc
		"sha3Uncles":            common.Hash{}, // inapplicable to Sei
		"stateRoot":             appHash,
		"timestamp":             hexutil.Uint64(evt.header.Time.Unix()), //nolint:gosec
		"transactionsRoot":      common.Hash{},                          // see function doc
		"mixHash":               common.Hash{},                          // inapplicable to Sei
		"excessBlobGas":         hexutil.Uint64(0),                      // inapplicable to Sei
		"parentBeaconBlockRoot": common.Hash{},                          // inapplicable to Sei
		"hash":                  blockHash,
		"baseFeePerGas":         (*hexutil.Big)(baseFee),
		"withdrawalsRoot":       common.Hash{},     // inapplicable to Sei
		"blobGasUsed":           hexutil.Uint64(0), // inapplicable to Sei
	}
}

func encodeTmHeader(
	header tmtypes.EventDataNewBlockHeader,
	baseFee *big.Int,
) (map[string]interface{}, error) {
	blockHash := common.HexToHash(header.Header.Hash().String())
	number := big.NewInt(header.Header.Height)
	miner := common.HexToAddress(header.Header.ProposerAddress.String())
	gasWanted := int64(0)
	lastHash := common.HexToHash(header.Header.LastBlockID.Hash.String())
	resultHash := common.HexToHash(header.Header.LastResultsHash.String())
	appHash := common.HexToHash(header.Header.AppHash.String())
	txHash := common.HexToHash(header.Header.DataHash.String())
	for _, txRes := range header.ResultFinalizeBlock.TxResults {
		gasWanted += txRes.GasUsed
	}
	gasLimit := uint64(header.ResultFinalizeBlock.ConsensusParamUpdates.Block.MaxGas) //nolint:gosec
	result := map[string]interface{}{
		"difficulty":            (*hexutil.Big)(utils.Big0), // inapplicable to Sei
		"extraData":             hexutil.Bytes{},            // inapplicable to Sei
		"gasLimit":              hexutil.Uint64(gasLimit),
		"gasUsed":               hexutil.Uint64(gasWanted), //nolint:gosec
		"logsBloom":             ethtypes.Bloom{},          // inapplicable to Sei
		"miner":                 miner,
		"nonce":                 ethtypes.BlockNonce{}, // inapplicable to Sei
		"number":                (*hexutil.Big)(number),
		"parentHash":            lastHash,
		"receiptsRoot":          resultHash,
		"sha3Uncles":            common.Hash{}, // inapplicable to Sei
		"stateRoot":             appHash,
		"timestamp":             hexutil.Uint64(header.Header.Time.Unix()), //nolint:gosec
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
