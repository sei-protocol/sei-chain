package ante

import (
	"encoding/binary"
	"errors"
	"sync"

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

	checkTxMtx      *sync.Mutex
	checkTxHeight   int64
	checkTxNonceMap map[string]uint64
}

func NewEVMSigVerifyDecorator(evmKeeper *evmkeeper.Keeper) *EVMSigVerifyDecorator {
	return &EVMSigVerifyDecorator{
		evmKeeper:  evmKeeper,
		checkTxMtx: &sync.Mutex{},
	}
}

func (svd *EVMSigVerifyDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if ctx.IsCheckTx() {
		// Sig verify for EVM CheckTx needs to run sequentially to handle applications that rapid-fires transactions under the same
		// account with incrementing nonce correctly
		svd.checkTxMtx.Lock()
		defer svd.checkTxMtx.Unlock()
		if ctx.BlockHeight() > svd.checkTxHeight {
			svd.checkTxHeight = ctx.BlockHeight()
			svd.checkTxNonceMap = map[string]uint64{}
		}
	}

	ethTx, found := types.GetContextEthTx(ctx)
	if !found {
		return ctx, errors.New("EVM transaction is not found in EVM ante route")
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

	// overwrite nextNonce with temporary value if the check is under CheckTx
	if ctx.IsCheckTx() {
		if nonce, ok := svd.checkTxNonceMap[evmAddr.Hex()]; ok {
			nextNonce = nonce
		}
	}
	if ethTx.Nonce() != nextNonce {
		return ctx, sdkerrors.ErrWrongSequence
	}

	if ctx.IsCheckTx() {
		svd.checkTxNonceMap[evmAddr.Hex()] = nextNonce + 1
	}

	return next(ctx, tx, simulate)
}
