package types

import (
	"context"
	"sync"
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
	ctx           context.Context
	mtx           *sync.RWMutex
	ms            MultiStore
	header        tmproto.Header
	headerHash    tmbytes.HexBytes
	chainID       string
	txBytes       []byte
	logger        log.Logger
	voteInfo      []abci.VoteInfo
	gasMeter      GasMeter
	blockGasMeter GasMeter
	checkTx       bool
	recheckTx     bool // if recheckTx == true, then checkTx must also be true
	minGasPrice   DecCoins
	consParams    *tmproto.ConsensusParams
	eventManager  *EventManager
	priority      int64 // The tx priority, only relevant in CheckTx

	txBlockingChannels   acltypes.MessageAccessOpsChannelMapping
	txCompletionChannels acltypes.MessageAccessOpsChannelMapping
	txMsgAccessOps       map[int][]acltypes.AccessOperation

	msgValidator *acltypes.MsgValidator
	messageIndex int // Used to track current message being processed

	contextMemCache *ContextMemCache
}

// Proposed rename, not done to avoid API breakage
type Request = Context

// Read-only accessors
func (c Context) Context() context.Context {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.ctx
}

func (c Context) MultiStore() MultiStore {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.ms
}

func (c Context) BlockHeight() int64 {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.header.Height
}

func (c Context) BlockTime() time.Time {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.header.Time
}

func (c Context) ChainID() string {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.chainID
}

func (c Context) TxBytes() []byte {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.txBytes
}

func (c Context) Logger() log.Logger {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.logger
}

func (c Context) VoteInfos() []abci.VoteInfo {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.voteInfo
}

func (c Context) GasMeter() GasMeter {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.gasMeter
}

func (c Context) BlockGasMeter() GasMeter {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.blockGasMeter
}

func (c Context) IsCheckTx() bool {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.checkTx
}

func (c Context) IsReCheckTx() bool {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.recheckTx
}

func (c Context) MinGasPrices() DecCoins {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.minGasPrice
}

func (c Context) EventManager() *EventManager {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.eventManager
}

func (c Context) Priority() int64 {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.priority
}

func (c Context) TxCompletionChannels() acltypes.MessageAccessOpsChannelMapping {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.txCompletionChannels
}

func (c Context) TxBlockingChannels() acltypes.MessageAccessOpsChannelMapping {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.txBlockingChannels
}

func (c Context) TxMsgAccessOps() map[int][]acltypes.AccessOperation {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.txMsgAccessOps
}

func (c Context) MessageIndex() int {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.messageIndex
}

func (c Context) MsgValidator() *acltypes.MsgValidator {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.msgValidator
}

func (c Context) ContextMemCache() *ContextMemCache {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.contextMemCache
}

// clone the header before returning
func (c Context) BlockHeader() tmproto.Header {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	msg := proto.Clone(&c.header).(*tmproto.Header)
	return *msg
}

// WithEventManager returns a Context with an updated tx priority
func (c Context) WithPriority(p int64) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.priority = p
	return c
}

// HeaderHash returns a copy of the header hash obtained during abci.RequestBeginBlock
func (c Context) HeaderHash() tmbytes.HexBytes {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	hash := make([]byte, len(c.headerHash))
	copy(hash, c.headerHash)
	return hash
}

func (c Context) ConsensusParams() *tmproto.ConsensusParams {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return proto.Clone(c.consParams).(*tmproto.ConsensusParams)
}

// create a new context
func NewContext(ms MultiStore, header tmproto.Header, isCheckTx bool, logger log.Logger) Context {
	// https://github.com/gogo/protobuf/issues/519
	header.Time = header.Time.UTC()
	return Context{
		ctx:             context.Background(),
		mtx:             &sync.RWMutex{},
		ms:              ms,
		header:          header,
		chainID:         header.ChainID,
		checkTx:         isCheckTx,
		logger:          logger,
		gasMeter:        stypes.NewInfiniteGasMeter(),
		minGasPrice:     DecCoins{},
		eventManager:    NewEventManager(),
		contextMemCache: NewContextMemCache(),

		txBlockingChannels:   make(acltypes.MessageAccessOpsChannelMapping),
		txCompletionChannels: make(acltypes.MessageAccessOpsChannelMapping),
		txMsgAccessOps:       make(map[int][]acltypes.AccessOperation),
	}
}

// WithContext returns a Context with an updated context.Context.
func (c Context) WithContext(ctx context.Context) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.ctx = ctx
	return c
}

// WithMultiStore returns a Context with an updated MultiStore.
func (c Context) WithMultiStore(ms MultiStore) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.ms = ms
	return c
}

// WithBlockHeader returns a Context with an updated tendermint block header in UTC time.
func (c Context) WithBlockHeader(header tmproto.Header) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	// https://github.com/gogo/protobuf/issues/519
	header.Time = header.Time.UTC()
	c.header = header
	return c
}

// WithHeaderHash returns a Context with an updated tendermint block header hash.
func (c Context) WithHeaderHash(hash []byte) Context {
	temp := make([]byte, len(hash))
	copy(temp, hash)

	c.mtx.Lock()
	defer c.mtx.Unlock()
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
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.chainID = chainID
	return c
}

// WithTxBytes returns a Context with an updated txBytes.
func (c Context) WithTxBytes(txBytes []byte) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.txBytes = txBytes
	return c
}

// WithLogger returns a Context with an updated logger.
func (c Context) WithLogger(logger log.Logger) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.logger = logger
	return c
}

// WithVoteInfos returns a Context with an updated consensus VoteInfo.
func (c Context) WithVoteInfos(voteInfo []abci.VoteInfo) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.voteInfo = voteInfo
	return c
}

// WithGasMeter returns a Context with an updated transaction GasMeter.
func (c Context) WithGasMeter(meter GasMeter) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.gasMeter = meter
	return c
}

// WithBlockGasMeter returns a Context with an updated block GasMeter
func (c Context) WithBlockGasMeter(meter GasMeter) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.blockGasMeter = meter
	return c
}

// WithIsCheckTx enables or disables CheckTx value for verifying transactions and returns an updated Context
func (c Context) WithIsCheckTx(isCheckTx bool) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.checkTx = isCheckTx
	return c
}

// WithIsRecheckTx called with true will also set true on checkTx in order to
// enforce the invariant that if recheckTx = true then checkTx = true as well.
func (c Context) WithIsReCheckTx(isRecheckTx bool) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if isRecheckTx {
		c.checkTx = true
	}
	c.recheckTx = isRecheckTx
	return c
}

// WithMinGasPrices returns a Context with an updated minimum gas price value
func (c Context) WithMinGasPrices(gasPrices DecCoins) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.minGasPrice = gasPrices
	return c
}

// WithConsensusParams returns a Context with an updated consensus params
func (c Context) WithConsensusParams(params *tmproto.ConsensusParams) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.consParams = params
	return c
}

// WithEventManager returns a Context with an updated event manager
func (c Context) WithEventManager(em *EventManager) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.eventManager = em
	return c
}

// TxMsgAccessOps returns a Context with an updated list of completion channel
func (c Context) WithTxMsgAccessOps(accessOps map[int][]acltypes.AccessOperation) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.txMsgAccessOps = accessOps
	return c
}

// WithTxCompletionChannels returns a Context with an updated list of completion channel
func (c Context) WithTxCompletionChannels(completionChannels acltypes.MessageAccessOpsChannelMapping) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.txCompletionChannels = completionChannels
	return c
}

// WithTxBlockingChannels returns a Context with an updated list of blocking channels for completion signals
func (c Context) WithTxBlockingChannels(blockingChannels acltypes.MessageAccessOpsChannelMapping) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.txBlockingChannels = blockingChannels
	return c
}

// WithMessageIndex returns a Context with the current message index that's being processed
func (c Context) WithMessageIndex(messageIndex int) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.messageIndex = messageIndex
	return c
}

func (c Context) WithMsgValidator(msgValidator *acltypes.MsgValidator) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.msgValidator = msgValidator
	return c
}

// WithContextMemCache returns a Context with a new context mem cache
func (c Context) WithContextMemCache(contextMemCache *ContextMemCache) Context {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.contextMemCache = contextMemCache
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
	c.mtx.Lock()
	defer c.mtx.Unlock()
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
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.ctx.Value(key)
}

// ----------------------------------------------------------------------------
// Store / Caching
// ----------------------------------------------------------------------------

// KVStore fetches a KVStore from the MultiStore.
func (c Context) KVStore(key StoreKey) KVStore {
	return gaskv.NewStore(c.MultiStore().GetKVStore(key), c.GasMeter(), stypes.KVGasConfig())
}

// TransientStore fetches a TransientStore from the MultiStore.
func (c Context) TransientStore(key StoreKey) KVStore {
	return gaskv.NewStore(c.MultiStore().GetKVStore(key), c.GasMeter(), stypes.TransientGasConfig())
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
	ctx.mtx.Lock()
	defer ctx.mtx.Unlock()
	return context.WithValue(ctx.ctx, SdkContextKey, ctx)
}

// UnwrapSDKContext retrieves a Context from a context.Context instance
// attached with WrapSDKContext. It panics if a Context was not properly
// attached
func UnwrapSDKContext(ctx context.Context) Context {
	return ctx.Value(SdkContextKey).(Context)
}
