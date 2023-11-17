package ante_test

import (
	"encoding/hex"
	"math/big"
	"sync"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestEVMSigVerifyDecorator(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	handler := ante.NewEVMSigVerifyDecorator(k, 5*time.Second)
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	copy(to[:], []byte("0x1234567890abcdef1234567890abcdef12345678"))
	txData := ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      1000,
		To:       to,
		Value:    big.NewInt(1000),
		Data:     []byte("abc"),
	}
	chainID := k.ChainID(ctx)
	evmParams := k.GetParams(ctx)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	preprocessor := ante.NewEVMPreprocessDecorator(k, k.AccountKeeper())
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)

	// should return error because nonce is incorrect
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)

	// should return error if acc is not found (i.e. preprocess not called)
	txData.Nonce = 0
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	msg, err = types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)

	// should succeed
	txData.Nonce = 0
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	msg, err = types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
}

func TestSigVerifyCheckTxNonceGap(t *testing.T) {
	_, addr := testkeeper.MockAddressPair()
	k, ctx := testkeeper.MockEVMKeeper()
	ctx = ctx.WithIsCheckTx(true)
	ctx = types.SetContextEVMAddress(ctx, addr)
	k.SetNonce(ctx, addr, 1)

	var leadingPriority int64 = 1000
	for _, test := range []struct {
		name               string
		nonces             []uint64
		shouldSuccess      []bool
		expectedPriorities []int64
		delay              []time.Duration
	}{{
		"single tx with correct nonce",
		[]uint64{1},
		[]bool{true},
		[]int64{1000},
		[]time.Duration{0 * time.Second},
	}, {
		"single tx with incorrect nonce",
		[]uint64{2},
		[]bool{false},
		[]int64{},
		[]time.Duration{0 * time.Second},
	}, {
		"multiple txs with duplicated nonce",
		[]uint64{1, 1},
		[]bool{true, false},
		[]int64{1000},
		[]time.Duration{0 * time.Second, 2 * time.Second},
	}, {
		"multiple txs with incrementing nonce",
		[]uint64{3, 1, 2},
		[]bool{true, true, true},
		[]int64{998, 1000, 999},
		[]time.Duration{0 * time.Second, 0 * time.Second, 0 * time.Second},
	}, {
		"multiple txs with gapped nonce",
		[]uint64{3, 1},
		[]bool{false, true},
		[]int64{0, 1000},
		[]time.Duration{0 * time.Second, 0 * time.Second},
	}} {
		var wg sync.WaitGroup
		handler := ante.NewEVMSigVerifyDecorator(k, 5*time.Second)
		for i, nonce := range test.nonces {
			wg.Add(1)
			i, nonce := i, nonce
			go func() {
				defer wg.Done()
				runCtx := types.SetContextEthTx(ctx, ethtypes.NewTx(&ethtypes.DynamicFeeTx{Nonce: nonce})).WithPriority(leadingPriority)
				if test.delay[i] > 0 {
					time.Sleep(test.delay[i])
				}
				resCtx, err := handler.AnteHandle(runCtx, nil, false, noop)
				if test.shouldSuccess[i] {
					require.Nil(t, err, test.name)
					require.Equal(t, test.expectedPriorities[i], resCtx.Priority(), test.name)
				} else {
					require.NotNil(t, err, test.name)
				}
			}()
		}
		wg.Wait()
	}
}

func TestSigVerifyCheckTxCleanup(t *testing.T) {
	_, addr := testkeeper.MockAddressPair()
	k, ctx := testkeeper.MockEVMKeeper()
	ctx = ctx.WithIsCheckTx(true)
	ctx = types.SetContextEVMAddress(ctx, addr)
	k.SetNonce(ctx, addr, 1)
	handler := ante.NewEVMSigVerifyDecorator(k, 5*time.Second)
	ctx = types.SetContextEthTx(ctx, ethtypes.NewTx(&ethtypes.DynamicFeeTx{Nonce: 1}))
	handler.AnteHandle(ctx, nil, false, noop) // checktx at height 8
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	handler.AnteHandle(ctx, nil, false, noop)        // checktx at height 9, which should clean height 8's state
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() - 1) // revert back to height 8. Note that this would not happen in practice. We are doing it here merely to verify cleanup's effect.
	_, err := handler.AnteHandle(ctx, nil, false, noop)
	require.Nil(t, err) // there should be no error since the dedupe map should've been cleared already when the height 9 tx was checked.
}

func noop(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}
