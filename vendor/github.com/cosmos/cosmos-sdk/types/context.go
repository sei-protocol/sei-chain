package types

import (
	"context"
	"time"

	"github.com/gogo/protobuf/proto"
	abci "github.com/tendermint/tendermint/abci/types"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/store/gaskv"
	stypes "github.com/cosmos/cosmos-sdk/store/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
)

/*
Context is an immutable object contains all information needed to
process a request.

It contains a context.Context object inside if you want to use that,
but please do not over-use it. We try to keep all data structured
and standard additions here would be better just to add to the Context struct
*/
type Context struct {
	ctx               context.Context
	ms                MultiStore
	nextMs            MultiStore          // ms of the next height; only used in tracing
	nextStoreKeys     map[string]struct{} // store key names that should use nextMs
	header            tmproto.Header
	headerHash        tmbytes.HexBytes
	chainID           string
	txBytes           []byte
	txSum             [32]byte
	logger            log.Logger
	voteInfo          []abci.VoteInfo
	gasMeter          GasMeter
	gasEstimate       uint64
	occEnabled        bool
	blockGasMeter     GasMeter
	checkTx           bool
	recheckTx         bool // if recheckTx == true, then checkTx must also be true
	minGasPrice       DecCoins
	consParams        *tmproto.ConsensusParams
	eventManager      *EventManager
	evmEventManager   *EVMEventManager
	priority          int64                 // The tx priority, only relevant in CheckTx
	pendingTxChecker  abci.PendingTxChecker // Checker for pending transaction, only relevant in CheckTx
	checkTxCallback   func(Context, error)  // callback to make at the end of CheckTx. Input param is the error (nil-able) of `runMsgs`
	deliverTxCallback func(Context)         // callback to make at the end of DeliverTx.
	expireTxHandler   func()                // callback that the mempool invokes when a tx is expired

	txBlockingChannels   acltypes.MessageAccessOpsChannelMapping
	txCompletionChannels acltypes.MessageAccessOpsChannelMapping
	txMsgAccessOps       map[int][]acltypes.AccessOperation

	// EVM properties
	evm                                 bool   // EVM transaction flag
	evmNonce                            uint64 // EVM Transaction nonce
	evmSenderAddress                    string // EVM Sender address
	evmTxHash                           string // EVM TX hash
	evmVmError                          string // EVM VM error during execution
	evmEntryViaWasmdPrecompile          bool   // EVM is entered via wasmd precompile directly
	evmPrecompileCalledFromDelegateCall bool   // EVM precompile is called from a delegate call

	msgValidator *acltypes.MsgValidator
	messageIndex int // Used to track current message being processed
	txIndex      int

	traceSpanContext context.Context

	isTracing   bool
	storeTracer gaskv.IStoreTracer
}

// Proposed rename, not done to avoid API breakage
type Request = Context

// Read-only accessors
func (c Context) Context() context.Context {
	return c.ctx
}

func (c Context) MultiStore() MultiStore {
	return c.ms
}

func (c Context) BlockHeight() int64 {
	return c.header.Height
}

func (c Context) BlockTime() time.Time {
	return c.header.Time
}

func (c Context) ChainID() string {
	return c.chainID
}

func (c Context) TxBytes() []byte {
	return c.txBytes
}

func (c Context) TxSum() [32]byte {
	return c.txSum
}

func (c Context) Logger() log.Logger {
	return c.logger
}

func (c Context) VoteInfos() []abci.VoteInfo {
	return c.voteInfo
}

func (c Context) GasMeter() GasMeter {
	return c.gasMeter
}

func (c Context) GasEstimate() uint64 {
	return c.gasEstimate
}

func (c Context) IsCheckTx() bool {
	return c.checkTx
}

func (c Context) IsReCheckTx() bool {
	return c.recheckTx
}

func (c Context) IsOCCEnabled() bool {
	return c.occEnabled
}

func (c Context) MinGasPrices() DecCoins {
	return c.minGasPrice
}

func (c Context) EventManager() *EventManager {
	return c.eventManager
}

func (c Context) EVMEventManager() *EVMEventManager {
	return c.evmEventManager
}

func (c Context) Priority() int64 {
	return c.priority
}

func (c Context) ExpireTxHandler() abci.ExpireTxHandler {
	return c.expireTxHandler
}

func (c Context) EVMSenderAddress() string {
	return c.evmSenderAddress
}

func (c Context) EVMNonce() uint64 {
	return c.evmNonce
}

func (c Context) EVMTxHash() string {
	return c.evmTxHash
}

func (c Context) IsEVM() bool {
	return c.evm
}

func (c Context) EVMVMError() string {
	return c.evmVmError
}

func (c Context) EVMEntryViaWasmdPrecompile() bool {
	return c.evmEntryViaWasmdPrecompile
}

func (c Context) EVMPrecompileCalledFromDelegateCall() bool {
	return c.evmPrecompileCalledFromDelegateCall
}

func (c Context) PendingTxChecker() abci.PendingTxChecker {
	return c.pendingTxChecker
}

func (c Context) CheckTxCallback() func(Context, error) {
	return c.checkTxCallback
}

func (c Context) DeliverTxCallback() func(Context) {
	return c.deliverTxCallback
}

func (c Context) TxCompletionChannels() acltypes.MessageAccessOpsChannelMapping {
	return c.txCompletionChannels
}

func (c Context) TxBlockingChannels() acltypes.MessageAccessOpsChannelMapping {
	return c.txBlockingChannels
}

func (c Context) TxMsgAccessOps() map[int][]acltypes.AccessOperation {
	return c.txMsgAccessOps
}

func (c Context) MessageIndex() int {
	return c.messageIndex
}

func (c Context) TxIndex() int {
	return c.txIndex
}

func (c Context) MsgValidator() *acltypes.MsgValidator {
	return c.msgValidator
}

// clone the header before returning
func (c Context) BlockHeader() tmproto.Header {
	msg := proto.Clone(&c.header).(*tmproto.Header)
	return *msg
}

func (c Context) TraceSpanContext() context.Context {
	return c.traceSpanContext
}

func (c Context) IsTracing() bool {
	return c.isTracing
}

func (c Context) StoreTracer() gaskv.IStoreTracer {
	if c.storeTracer == nil {
		return nil
	}
	return c.storeTracer
}

// WithEventManager returns a Context with an updated tx priority
func (c Context) WithPriority(p int64) Context {
	c.priority = p
	return c
}

// HeaderHash returns a copy of the header hash obtained during abci.RequestBeginBlock
func (c Context) HeaderHash() tmbytes.HexBytes {
	hash := make([]byte, len(c.headerHash))
	copy(hash, c.headerHash)
	return hash
}

func (c Context) ConsensusParams() *tmproto.ConsensusParams {
	return proto.Clone(c.consParams).(*tmproto.ConsensusParams)
}

// create a new context
func NewContext(ms MultiStore, header tmproto.Header, isCheckTx bool, logger log.Logger) Context {
	// https://github.com/gogo/protobuf/issues/519
	header.Time = header.Time.UTC()
	return Context{
		ctx:             context.Background(),
		ms:              ms,
		header:          header,
		chainID:         header.ChainID,
		checkTx:         isCheckTx,
		logger:          logger,
		gasMeter:        NewInfiniteGasMeter(1, 1),
		minGasPrice:     DecCoins{},
		eventManager:    NewEventManager(),
		evmEventManager: NewEVMEventManager(),

		txBlockingChannels:   make(acltypes.MessageAccessOpsChannelMapping),
		txCompletionChannels: make(acltypes.MessageAccessOpsChannelMapping),
		txMsgAccessOps:       make(map[int][]acltypes.AccessOperation),
	}
}

// WithContext returns a Context with an updated context.Context.
func (c Context) WithContext(ctx context.Context) Context {
	c.ctx = ctx
	return c
}

// WithMultiStore returns a Context with an updated MultiStore.
func (c Context) WithMultiStore(ms MultiStore) Context {
	c.ms = ms
	return c
}

// WithBlockHeader returns a Context with an updated tendermint block header in UTC time.
func (c Context) WithBlockHeader(header tmproto.Header) Context {
	// https://github.com/gogo/protobuf/issues/519
	header.Time = header.Time.UTC()
	c.header = header
	return c
}

// WithHeaderHash returns a Context with an updated tendermint block header hash.
func (c Context) WithHeaderHash(hash []byte) Context {
	temp := make([]byte, len(hash))
	copy(temp, hash)

	c.headerHash = temp
	return c
}

// WithBlockTime returns a Context with an updated tendermint block header time in UTC time
func (c Context) WithBlockTime(newTime time.Time) Context {
	newHeader := c.BlockHeader()
	// https://github.com/gogo/protobuf/issues/519
	newHeader.Time = newTime.UTC()
	return c.WithBlockHeader(newHeader)
}

// WithProposer returns a Context with an updated proposer consensus address.
func (c Context) WithProposer(addr ConsAddress) Context {
	newHeader := c.BlockHeader()
	newHeader.ProposerAddress = addr.Bytes()
	return c.WithBlockHeader(newHeader)
}

// WithBlockHeight returns a Context with an updated block height.
func (c Context) WithBlockHeight(height int64) Context {
	newHeader := c.BlockHeader()
	newHeader.Height = height
	return c.WithBlockHeader(newHeader)
}

// WithChainID returns a Context with an updated chain identifier.
func (c Context) WithChainID(chainID string) Context {
	c.chainID = chainID
	return c
}

// WithTxBytes returns a Context with an updated txBytes.
func (c Context) WithTxBytes(txBytes []byte) Context {
	c.txBytes = txBytes
	return c
}

func (c Context) WithTxSum(txSum [32]byte) Context {
	c.txSum = txSum
	return c
}

// WithLogger returns a Context with an updated logger.
func (c Context) WithLogger(logger log.Logger) Context {
	c.logger = logger
	return c
}

// WithVoteInfos returns a Context with an updated consensus VoteInfo.
func (c Context) WithVoteInfos(voteInfo []abci.VoteInfo) Context {
	c.voteInfo = voteInfo
	return c
}

// WithGasMeter returns a Context with an updated transaction GasMeter.
func (c Context) WithGasMeter(meter GasMeter) Context {
	c.gasMeter = meter
	return c
}

// WithGasEstimate returns a Context with an updated gas estimate.
func (c Context) WithGasEstimate(gasEstimate uint64) Context {
	c.gasEstimate = gasEstimate
	return c
}

// WithIsCheckTx enables or disables CheckTx value for verifying transactions and returns an updated Context
func (c Context) WithIsCheckTx(isCheckTx bool) Context {
	c.checkTx = isCheckTx
	return c
}

// WithIsOCCEnabled enables or disables whether OCC is used as the concurrency algorithm
func (c Context) WithIsOCCEnabled(isOCCEnabled bool) Context {
	c.occEnabled = isOCCEnabled
	return c
}

// WithIsRecheckTx called with true will also set true on checkTx in order to
// enforce the invariant that if recheckTx = true then checkTx = true as well.
func (c Context) WithIsReCheckTx(isRecheckTx bool) Context {
	if isRecheckTx {
		c.checkTx = true
	}
	c.recheckTx = isRecheckTx
	return c
}

// WithMinGasPrices returns a Context with an updated minimum gas price value
func (c Context) WithMinGasPrices(gasPrices DecCoins) Context {
	c.minGasPrice = gasPrices
	return c
}

// WithConsensusParams returns a Context with an updated consensus params
func (c Context) WithConsensusParams(params *tmproto.ConsensusParams) Context {
	c.consParams = params
	return c
}

// WithEventManager returns a Context with an updated event manager
func (c Context) WithEventManager(em *EventManager) Context {
	c.eventManager = em
	return c
}

func (c Context) WithEvmEventManager(em *EVMEventManager) Context {
	c.evmEventManager = em
	return c
}

// TxMsgAccessOps returns a Context with an updated list of completion channel
func (c Context) WithTxMsgAccessOps(accessOps map[int][]acltypes.AccessOperation) Context {
	c.txMsgAccessOps = accessOps
	return c
}

// WithTxCompletionChannels returns a Context with an updated list of completion channel
func (c Context) WithTxCompletionChannels(completionChannels acltypes.MessageAccessOpsChannelMapping) Context {
	c.txCompletionChannels = completionChannels
	return c
}

// WithTxBlockingChannels returns a Context with an updated list of blocking channels for completion signals
func (c Context) WithTxBlockingChannels(blockingChannels acltypes.MessageAccessOpsChannelMapping) Context {
	c.txBlockingChannels = blockingChannels
	return c
}

// WithMessageIndex returns a Context with the current message index that's being processed
func (c Context) WithMessageIndex(messageIndex int) Context {
	c.messageIndex = messageIndex
	return c
}

// WithTxIndex returns a Context with the current transaction index that's being processed
func (c Context) WithTxIndex(txIndex int) Context {
	c.txIndex = txIndex
	return c
}

func (c Context) WithMsgValidator(msgValidator *acltypes.MsgValidator) Context {
	c.msgValidator = msgValidator
	return c
}

func (c Context) WithTraceSpanContext(ctx context.Context) Context {
	c.traceSpanContext = ctx
	return c
}

func (c Context) WithEVMSenderAddress(address string) Context {
	c.evmSenderAddress = address
	return c
}

func (c Context) WithEVMNonce(nonce uint64) Context {
	c.evmNonce = nonce
	return c
}

func (c Context) WithIsEVM(isEVM bool) Context {
	c.evm = isEVM
	return c
}

func (c Context) WithEVMTxHash(txHash string) Context {
	c.evmTxHash = txHash
	return c
}

func (c Context) WithEVMVMError(vmError string) Context {
	c.evmVmError = vmError
	return c
}

func (c Context) WithEVMEntryViaWasmdPrecompile(e bool) Context {
	c.evmEntryViaWasmdPrecompile = e
	return c
}

func (c Context) WithEVMPrecompileCalledFromDelegateCall(e bool) Context {
	c.evmPrecompileCalledFromDelegateCall = e
	return c
}

func (c Context) WithPendingTxChecker(checker abci.PendingTxChecker) Context {
	c.pendingTxChecker = checker
	return c
}

func (c Context) WithCheckTxCallback(checkTxCallback func(Context, error)) Context {
	c.checkTxCallback = checkTxCallback
	return c
}

func (c Context) WithDeliverTxCallback(deliverTxCallback func(Context)) Context {
	c.deliverTxCallback = deliverTxCallback
	return c
}

func (c Context) WithExpireTxHandler(expireTxHandler func()) Context {
	c.expireTxHandler = expireTxHandler
	return c
}

func (c Context) WithIsTracing(it bool) Context {
	c.isTracing = it
	if it {
		c.storeTracer = NewStoreTracer()
	}
	return c
}

func (c Context) WithNextMs(ms MultiStore, nextStoreKeys []string) Context {
	c.nextMs = ms
	c.nextStoreKeys = make(map[string]struct{}, len(nextStoreKeys))
	for _, k := range nextStoreKeys {
		c.nextStoreKeys[k] = struct{}{}
	}
	return c
}

// TODO: remove???
func (c Context) IsZero() bool {
	return c.ms == nil
}

// WithValue is deprecated, provided for backwards compatibility
// Please use
//
//	ctx = ctx.WithContext(context.WithValue(ctx.Context(), key, false))
//
// instead of
//
//	ctx = ctx.WithValue(key, false)
func (c Context) WithValue(key, value interface{}) Context {
	c.ctx = context.WithValue(c.ctx, key, value)
	return c
}

// Value is deprecated, provided for backwards compatibility
// Please use
//
//	ctx.Context().Value(key)
//
// instead of
//
//	ctx.Value(key)
func (c Context) Value(key interface{}) interface{} {
	return c.ctx.Value(key)
}

// ----------------------------------------------------------------------------
// Store / Caching
// ----------------------------------------------------------------------------

// KVStore fetches a KVStore from the MultiStore.
func (c Context) KVStore(key StoreKey) KVStore {
	if c.isTracing {
		if _, ok := c.nextStoreKeys[key.Name()]; ok {
			return gaskv.NewStore(c.nextMs.GetKVStore(key), c.GasMeter(), stypes.KVGasConfig(), key.Name(), c.StoreTracer())
		}
	}
	return gaskv.NewStore(c.MultiStore().GetKVStore(key), c.GasMeter(), stypes.KVGasConfig(), key.Name(), c.StoreTracer())
}

// TransientStore fetches a TransientStore from the MultiStore.
func (c Context) TransientStore(key StoreKey) KVStore {
	if c.isTracing {
		if _, ok := c.nextStoreKeys[key.Name()]; ok {
			return gaskv.NewStore(c.nextMs.GetKVStore(key), c.GasMeter(), stypes.TransientGasConfig(), key.Name(), c.StoreTracer())
		}
	}
	return gaskv.NewStore(c.MultiStore().GetKVStore(key), c.GasMeter(), stypes.TransientGasConfig(), key.Name(), c.StoreTracer())
}

// CacheContext returns a new Context with the multi-store cached and a new
// EventManager. The cached context is written to the context when writeCache
// is called.
func (c Context) CacheContext() (cc Context, writeCache func()) {
	cms := c.MultiStore().CacheMultiStore()
	cc = c.WithMultiStore(cms).WithEventManager(NewEventManager())
	return cc, cms.Write
}

// ContextKey defines a type alias for a stdlib Context key.
type ContextKey string

// SdkContextKey is the key in the context.Context which holds the sdk.Context.
const SdkContextKey ContextKey = "sdk-context"

// WrapSDKContext returns a stdlib context.Context with the provided sdk.Context's internal
// context as a value. It is useful for passing an sdk.Context  through methods that take a
// stdlib context.Context parameter such as generated gRPC methods. To get the original
// sdk.Context back, call UnwrapSDKContext.
func WrapSDKContext(ctx Context) context.Context {
	return context.WithValue(ctx.ctx, SdkContextKey, ctx)
}

// UnwrapSDKContext retrieves a Context from a context.Context instance
// attached with WrapSDKContext. It panics if a Context was not properly
// attached
func UnwrapSDKContext(ctx context.Context) Context {
	return ctx.Value(SdkContextKey).(Context)
}
