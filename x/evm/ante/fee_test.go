package ante_test

import (
	"encoding/hex"
	"math"
	"math/big"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestEVMFeeCheckDecorator(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	upgradeKeeper := &testkeeper.EVMTestApp.UpgradeKeeper
	handler := ante.NewEVMFeeCheckDecorator(k, upgradeKeeper)
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	copy(to[:], []byte("0x1234567890abcdef1234567890abcdef12345678"))
	chainID := k.ChainID(ctx)
	txData := ethtypes.DynamicFeeTx{
		Nonce:     0,
		GasFeeCap: big.NewInt(10000000000000),
		Gas:       1000,
		To:        to,
		Value:     big.NewInt(1000000000000000),
		Data:      []byte("abc"),
		ChainID:   chainID,
	}
	chainCfg := types.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	preprocessor := ante.NewEVMPreprocessDecorator(k, k.AccountKeeper())
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)

	// should return error because gas fee cap is too low
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)

	txData.GasFeeCap = k.GetMinimumFeePerGas(ctx).TruncateInt().BigInt()
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err = ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	msg, err = types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	// should return error because the sender does not have enough funds
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)

	amt := typedTx.Cost()
	coinsAmt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewIntFromBigInt(amt).Quo(sdk.NewIntFromBigInt(state.UseiToSweiMultiplier)).Add(sdk.OneInt())))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, coinsAmt)
	seiAddr := sdk.AccAddress(msg.Derived.SenderSeiAddr)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, seiAddr, coinsAmt)

	// should succeed now that the sender has enough funds
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)

	// should fail because of minimum fee
	txData.GasFeeCap = big.NewInt(0)
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err = ethtx.NewDynamicFeeTx(tx)
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
	require.NotNil(t, err)

	// should fail because of negative gas tip cap
	txData.GasTipCap = big.NewInt(-1)
	txData.GasFeeCap = big.NewInt(10000000000000)
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx = newDynamicFeeTxWithoutValidation(tx)
	msg, err = types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "gas fee cap cannot be negative")
}

func TestCalculatePriorityScenarios(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	upgradeKeeper := &testkeeper.EVMTestApp.UpgradeKeeper
	decorator := ante.NewEVMFeeCheckDecorator(k, upgradeKeeper)

	_1gwei := big.NewInt(100000000000)
	_1_1gwei := big.NewInt(1100000000000)
	_2gwei := big.NewInt(200000000000)
	maxInt := big.NewInt(math.MaxInt64)
	maxPriority := big.NewInt(antedecorators.MaxPriority)

	scenarios := []struct {
		name             string
		txData           ethtypes.TxData
		expectedPriority *big.Int
	}{
		{
			name: "DynamicFeeTx with tip",
			txData: &ethtypes.DynamicFeeTx{
				GasFeeCap: _1gwei,
				GasTipCap: _1gwei,
				Value:     _1gwei,
			},
			expectedPriority: _1gwei,
		},
		{
			name: "DynamicFeeTx with higher gas fee cap and gas tip cap",
			txData: &ethtypes.DynamicFeeTx{
				GasFeeCap: _1_1gwei,
				GasTipCap: _1_1gwei,
				Value:     _1gwei,
			},
			expectedPriority: _1_1gwei,
		},
		{
			name: "DynamicFeeTx value does not change priority",
			txData: &ethtypes.DynamicFeeTx{
				GasFeeCap: _1gwei,
				GasTipCap: _1gwei,
				Value:     _2gwei,
			},
			expectedPriority: _1gwei,
		},
		{
			name: "DynamicFeeTx with no tip",
			txData: &ethtypes.DynamicFeeTx{
				GasFeeCap: _1gwei,
				GasTipCap: big.NewInt(0),
				Value:     _1gwei,
			},
			expectedPriority: big.NewInt(0), // if you don't tip, you get lowest priority
		},
		{
			name: "DynamicFeeTx with a non-multiple of 10 tip",
			txData: &ethtypes.DynamicFeeTx{
				GasFeeCap: big.NewInt(1000000000000000),
				GasTipCap: big.NewInt(9999999999999),
				Value:     big.NewInt(1000000000),
			},
			expectedPriority: big.NewInt(9999999999999),
		},
		{
			name: "DynamicFeeTx test overflow",
			txData: &ethtypes.DynamicFeeTx{
				GasFeeCap: new(big.Int).Add(maxInt, big.NewInt(1)),
				GasTipCap: new(big.Int).Add(maxInt, big.NewInt(1)),
				Value:     big.NewInt(1000000000),
			},
			expectedPriority: maxPriority,
		},
		{
			name: "LegacyTx has priority with gas price",
			txData: &ethtypes.LegacyTx{
				GasPrice: _1gwei,
				Value:    _1gwei,
			},
			expectedPriority: _1gwei,
		},
		{
			name: "LegacyTx has zero priority with zero gas price",
			txData: &ethtypes.LegacyTx{
				GasPrice: big.NewInt(0),
				Value:    _1gwei,
			},
			expectedPriority: big.NewInt(0),
		},
		{
			name: "LegacyTx with a non-multiple of 10 gas price",
			txData: &ethtypes.LegacyTx{
				GasPrice: big.NewInt(9999999999999),
				Value:    big.NewInt(1000000000000000),
			},
			expectedPriority: big.NewInt(9999999999999),
		},
	}

	// Run each scenario
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			tx := ethtypes.NewTx(s.txData)
			txData, err := ethtx.NewTxDataFromTx(tx)
			require.NoError(t, err)
			priority := decorator.CalculatePriority(ctx, txData)

			if s.expectedPriority != nil {
				// Check the returned value
				if priority.Cmp(s.expectedPriority) != 0 {
					t.Errorf("Expected priority %v, but got %v", s.expectedPriority, priority)
				}
			}
		})
	}
}

func newDynamicFeeTxWithoutValidation(tx *ethtypes.Transaction) *ethtx.DynamicFeeTx {
	txData := &ethtx.DynamicFeeTx{
		Nonce:    tx.Nonce(),
		Data:     tx.Data(),
		GasLimit: tx.Gas(),
	}

	v, r, s := tx.RawSignatureValues()
	ethtx.SetConvertIfPresent(tx.To(), func(to *common.Address) string { return to.Hex() }, txData.SetTo)
	ethtx.SetConvertIfPresent(tx.Value(), sdk.NewIntFromBigInt, txData.SetAmount)
	ethtx.SetConvertIfPresent(tx.GasFeeCap(), sdk.NewIntFromBigInt, txData.SetGasFeeCap)
	ethtx.SetConvertIfPresent(tx.GasTipCap(), sdk.NewIntFromBigInt, txData.SetGasTipCap)
	al := tx.AccessList()
	ethtx.SetConvertIfPresent(&al, ethtx.NewAccessList, txData.SetAccesses)

	txData.SetSignatureValues(tx.ChainId(), v, r, s)
	return txData
}

func TestAntehandlersGetBaseFeeBeforeV620(t *testing.T) {
	// Set up test app and context
	testApp := testkeeper.EVMTestApp
	testHeight := int64(115000000) // A height >= 114945913 but before v6.2.0 upgrade
	testCtx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(testHeight)

	// Set the chain ID to "pacific-1" to trigger the upgrade check
	testCtx = testCtx.WithChainID("pacific-1")

	// Set the v6.2.0 upgrade height to a height higher than our test height
	// This simulates that the upgrade hasn't happened yet
	v620UpgradeHeight := int64(120000000)
	testApp.UpgradeKeeper.SetDone(testCtx.WithBlockHeight(v620UpgradeHeight), "6.2.0")

	// Create the fee check decorator
	k := &testApp.EvmKeeper
	upgradeKeeper := &testApp.UpgradeKeeper
	decorator := ante.NewEVMFeeCheckDecorator(k, upgradeKeeper)

	// Create a test transaction with a very low gas fee cap
	// This will cause the AnteHandle to fail with insufficient fee error
	// but we can verify that the getBaseFee logic is working correctly
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	copy(to[:], []byte("0x1234567890abcdef1234567890abcdef12345678"))
	chainID := k.ChainID(testCtx)

	// Create a transaction with very low gas fee cap to trigger the base fee check
	txData := ethtypes.DynamicFeeTx{
		Nonce:     0,
		GasFeeCap: big.NewInt(1), // Very low fee cap
		Gas:       1000,
		To:        to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainID,
	}

	chainCfg := types.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(testCtx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(testCtx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	// Create preprocessor decorator
	preprocessor := ante.NewEVMPreprocessDecorator(k, k.AccountKeeper())

	// Test with a height that should hit the "IN PACIFIC-1 CASE"
	// Height >= 114945913 but < v6.2.0 upgrade height
	ctx, err := preprocessor.AnteHandle(testCtx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)

	// Then run the fee check decorator - this should hit the "IN PACIFIC-1 CASE"
	_, err = decorator.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err, "Should fail with insufficient fee in pacific-1 case")
	require.Contains(t, err.Error(), "insufficient fee", "Error should be about insufficient fee")

	// Test with a height after v6.2.0 upgrade
	ctxAfterUpgrade := testCtx.WithBlockHeight(125000000) // After v6.2.0 upgrade
	ctx, err = preprocessor.AnteHandle(ctxAfterUpgrade, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = decorator.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err, "Should fail with insufficient fee after v6.2.0 upgrade")
	require.Contains(t, err.Error(), "insufficient fee", "Error should be about insufficient fee")

	// Test with a different chain ID (not pacific-1)
	ctxOtherChain := testCtx.WithChainID("test-chain")
	ctx, err = preprocessor.AnteHandle(ctxOtherChain, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = decorator.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err, "Should fail with insufficient fee for non-pacific-1 chain")
	require.Contains(t, err.Error(), "insufficient fee", "Error should be about insufficient fee")

	// Test with a height before the hardcoded height (114945913)
	// This should use GetBaseFeePerGas instead of GetCurrBaseFeePerGas
	ctxBeforeHardcoded := testCtx.WithBlockHeight(100000000) // Before 114945913
	ctx, err = preprocessor.AnteHandle(ctxBeforeHardcoded, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = decorator.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err, "Should fail with insufficient fee before hardcoded height")
	require.Contains(t, err.Error(), "insufficient fee", "Error should be about insufficient fee")
}
