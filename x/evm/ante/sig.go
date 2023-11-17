package ante

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type CtxKeyType string
type CtxValueTypeCheckTxNonces map[string]uint64

const CtxKeyCheckTxNonces CtxKeyType = CtxKeyType("CtxKeyCheckTxNonces")

type EVMSigVerifyDecorator struct {
	evmKeeper *evmkeeper.Keeper

	checkTxTimeout time.Duration
	checkTxMtx     *sync.Mutex
	// Block height -> current number of transactions being CheckTx'ed on that height
	heightCounts map[int64]int
	// Block height -> Address -> Nonce -> channel of transaction priority of the tx
	// from the (address, nonce, height) tuple.
	// Note that an entry may be set either by tx with nonce (sender) or nonce+1 (receiver),
	// whichever comes first. This is also why this map cannot be used for dedup and we need
	// a dedicated dedup map
	nonceGaps map[int64]map[string]map[uint64]chan int64
	// Block height -> Address -> Nonce -> non-nil if the (address, nonce, height) tuple has
	// been received.
	nonceSeen map[int64]map[string]map[uint64]struct{}
}

func NewEVMSigVerifyDecorator(evmKeeper *evmkeeper.Keeper, checkTxTimeout time.Duration) *EVMSigVerifyDecorator {
	return &EVMSigVerifyDecorator{
		evmKeeper:      evmKeeper,
		checkTxTimeout: checkTxTimeout,
		checkTxMtx:     &sync.Mutex{},
		heightCounts:   map[int64]int{},
		nonceGaps:      map[int64]map[string]map[uint64]chan int64{},
		nonceSeen:      map[int64]map[string]map[uint64]struct{}{},
	}
}

func (svd *EVMSigVerifyDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	ethTx, found := types.GetContextEthTx(ctx)
	if !found {
		return ctx, errors.New("EVM transaction is not found in EVM ante route")
	}
	if ctx.IsCheckTx() {
		svd.CheckTxInit(ctx.BlockHeight())

		defer svd.CheckTxCleanup(ctx.BlockHeight())
	}

	if !ethTx.Protected() {
		return ctx, sdkerrors.ErrNoSignatures
	}

	evmAddr, found := types.GetContextEVMAddress(ctx)
	if !found {
		return ctx, errors.New("failed to get sender from EVM tx")
	}

	nextNonce := uint64(0)
	noncebz := svd.evmKeeper.PrefixStore(ctx, types.NonceKeyPrefix).Get(evmAddr[:])
	if noncebz != nil {
		nextNonce = binary.BigEndian.Uint64(noncebz)
	}

	if ctx.IsCheckTx() {
		addrHex := evmAddr.Hex()
		if svd.IsNonceDup(ctx.BlockHeight(), addrHex, ethTx.Nonce()) || ethTx.Nonce() < nextNonce {
			return ctx, sdkerrors.ErrWrongSequence
		} else if ethTx.Nonce() > nextNonce {
			chanToWait := svd.GetOrSetNonceChannel(ctx.BlockHeight(), addrHex, ethTx.Nonce()-1)
			select {
			case prevPriority := <-chanToWait:
				// we want transactions from the same address on the same block with incrementing nonce
				// to have decrementing priority so that they will be ordered correctly when they are
				// added to block proposals.
				ctx = ctx.WithPriority(prevPriority - 1)
			case <-time.After(svd.checkTxTimeout):
				return ctx, sdkerrors.ErrWrongSequence
			}
		}

		chanToSend := svd.GetOrSetNonceChannel(ctx.BlockHeight(), addrHex, ethTx.Nonce())
		chanToSend <- ctx.Priority()
	} else if ethTx.Nonce() != nextNonce {
		return ctx, sdkerrors.ErrWrongSequence
	}

	return next(ctx, tx, simulate)
}

func (svd *EVMSigVerifyDecorator) CheckTxInit(height int64) {
	svd.checkTxMtx.Lock()
	defer svd.checkTxMtx.Unlock()

	if _, ok := svd.heightCounts[height]; !ok {
		svd.heightCounts[height] = 0
		svd.nonceGaps[height] = map[string]map[uint64]chan int64{}
		svd.nonceSeen[height] = map[string]map[uint64]struct{}{}
	}
	svd.heightCounts[height]++
}

func (svd *EVMSigVerifyDecorator) CheckTxCleanup(height int64) {
	svd.checkTxMtx.Lock()
	defer svd.checkTxMtx.Unlock()

	svd.heightCounts[height]--
	latestHeight := int64(0)
	for h := range svd.heightCounts {
		if h > latestHeight {
			latestHeight = h
		}
	}
	heightsToClear := []int64{}
	for h, c := range svd.heightCounts {
		if h < latestHeight && c == 0 {
			heightsToClear = append(heightsToClear, h)
		}
	}

	for _, h := range heightsToClear {
		delete(svd.heightCounts, h)
		delete(svd.nonceGaps, h)
		delete(svd.nonceSeen, h)
	}
}

func (svd *EVMSigVerifyDecorator) IsNonceDup(height int64, addr string, nonce uint64) bool {
	svd.checkTxMtx.Lock()
	defer svd.checkTxMtx.Unlock()
	if _, ok := svd.nonceSeen[height][addr]; !ok {
		svd.nonceSeen[height][addr] = map[uint64]struct{}{}
	}
	if _, seen := svd.nonceSeen[height][addr][nonce]; !seen {
		svd.nonceSeen[height][addr][nonce] = struct{}{}
		return false
	}
	return true
}

func (svd *EVMSigVerifyDecorator) GetOrSetNonceChannel(height int64, addr string, nonce uint64) chan int64 {
	svd.checkTxMtx.Lock()
	defer svd.checkTxMtx.Unlock()
	if _, ok := svd.nonceGaps[height][addr]; !ok {
		svd.nonceGaps[height][addr] = map[uint64]chan int64{}
	}
	if _, found := svd.nonceGaps[height][addr][nonce]; !found {
		svd.nonceGaps[height][addr][nonce] = make(chan int64, 1)
	}
	return svd.nonceGaps[height][addr][nonce]
}
