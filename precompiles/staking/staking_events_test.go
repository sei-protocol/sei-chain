package staking_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/app"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	ethtx "github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestStakingPrecompileEventsEmission(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup validator
	valPub := secp256k1.GenPrivKey().PubKey()
	valAddr := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub)
	valStr := valAddr.String()

	// Setup test account
	privKey := testkeeper.MockPrivateKey()
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	
	// Fund the account
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
	require.NoError(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))

	// Test delegate event
	t.Run("TestDelegateEvent", func(t *testing.T) {
		abi := pcommon.MustGetABI(f, "abi.json")
		args, err := abi.Pack("delegate", valStr)
		require.NoError(t, err)

		addr := common.HexToAddress(staking.StakingAddress)
		delegateAmount := big.NewInt(100_000_000_000_000)
		
		tx := createEVMTx(t, k, ctx, privKey, &addr, args, delegateAmount)
		res := executeEVMTx(t, testApp, ctx, tx, privKey)
		
		require.Empty(t, res.VmError)
		require.NotEmpty(t, res.Logs)
		
		// Verify the event
		require.Len(t, res.Logs, 1)
		log := res.Logs[0]
		
		// Check event signature
		expectedSig := pcommon.DelegateEventSig
		require.Equal(t, expectedSig.Hex(), log.Topics[0])
		
		// Check indexed delegator address
		require.Equal(t, common.BytesToHash(evmAddr.Bytes()).Hex(), log.Topics[1])
		
		// Decode the event data
		// For the Delegate event, the validator string and amount are not indexed
		// So we need to decode them from the data field
		// The data contains: offset for string, amount, string length, string data
		require.GreaterOrEqual(t, len(log.Data), 96) // At least 3 * 32 bytes
		
		// Verify the amount is encoded in the data (at position 32-64)
		amountBytes := log.Data[32:64]
		amount := new(big.Int).SetBytes(amountBytes)
		require.Equal(t, delegateAmount, amount)
	})

	// Test redelegate event
	t.Run("TestRedelegateEvent", func(t *testing.T) {
		// First, delegate some funds to the first validator
		addr := common.HexToAddress(staking.StakingAddress)
		delegateArgs, err := pcommon.MustGetABI(f, "abi.json").Pack("delegate", valStr)
		require.NoError(t, err)
		
		delegateTx := createEVMTx(t, k, ctx, privKey, &addr, delegateArgs, big.NewInt(100_000_000_000_000))
		delegateRes := executeEVMTx(t, testApp, ctx, delegateTx, privKey)
		require.Empty(t, delegateRes.VmError)
		
		// Setup second validator
		valPub2 := secp256k1.GenPrivKey().PubKey()
		valAddr2 := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub2)
		valStr2 := valAddr2.String()
		
		abi := pcommon.MustGetABI(f, "abi.json")
		redelegateAmount := big.NewInt(50_000_000_000_000)
		args, err := abi.Pack("redelegate", valStr, valStr2, redelegateAmount)
		require.NoError(t, err)

		tx := createEVMTx(t, k, ctx, privKey, &addr, args, big.NewInt(0))
		res := executeEVMTx(t, testApp, ctx, tx, privKey)
		
		require.Empty(t, res.VmError)
		require.NotEmpty(t, res.Logs)
		
		// Verify the event
		require.Len(t, res.Logs, 1)
		log := res.Logs[0]
		
		// Check event signature
		expectedSig := pcommon.RedelegateEventSig
		require.Equal(t, expectedSig.Hex(), log.Topics[0])
		
		// Check indexed delegator address
		require.Equal(t, common.BytesToHash(evmAddr.Bytes()).Hex(), log.Topics[1])
		
		// Decode the event data
		// For the Redelegate event, srcValidator, dstValidator, and amount are not indexed
		// The amount is at position 64-96 in the data
		require.GreaterOrEqual(t, len(log.Data), 96) // At least 3 * 32 bytes
		
		// Verify the amount is encoded in the data (at position 64-96)
		amountBytes := log.Data[64:96]
		amount := new(big.Int).SetBytes(amountBytes)
		require.Equal(t, redelegateAmount, amount)
	})

	// Test undelegate event
	t.Run("TestUndelegateEvent", func(t *testing.T) {
		abi := pcommon.MustGetABI(f, "abi.json")
		undelegateAmount := big.NewInt(25_000_000_000_000)
		args, err := abi.Pack("undelegate", valStr, undelegateAmount)
		require.NoError(t, err)

		addr := common.HexToAddress(staking.StakingAddress)
		tx := createEVMTx(t, k, ctx, privKey, &addr, args, big.NewInt(0))
		res := executeEVMTx(t, testApp, ctx, tx, privKey)
		
		require.Empty(t, res.VmError)
		require.NotEmpty(t, res.Logs)
		
		// Verify the event
		require.Len(t, res.Logs, 1)
		log := res.Logs[0]
		
		// Check event signature
		expectedSig := pcommon.UndelegateEventSig
		require.Equal(t, expectedSig.Hex(), log.Topics[0])
		
		// Check indexed delegator address
		require.Equal(t, common.BytesToHash(evmAddr.Bytes()).Hex(), log.Topics[1])
		
		// Decode the event data
		// For the Undelegate event, the validator string and amount are not indexed
		require.GreaterOrEqual(t, len(log.Data), 96) // At least 3 * 32 bytes
		
		// Verify the amount is encoded in the data (at position 32-64)
		amountBytes := log.Data[32:64]
		amount := new(big.Int).SetBytes(amountBytes)
		require.Equal(t, undelegateAmount, amount)
	})
}

func createEVMTx(t *testing.T, k *evmkeeper.Keeper, ctx sdk.Context, privKey cryptotypes.PrivKey, to *common.Address, data []byte, value *big.Int) *ethtypes.Transaction {
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	
	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	nonce := k.GetNonce(ctx, evmAddr)
	
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       to,
		Value:    value,
		Data:     data,
		Nonce:    nonce,
	}
	
	chainID := k.ChainID(ctx)
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.NoError(t, err)
	
	return tx
}

func executeEVMTx(t *testing.T, testApp *app.App, ctx sdk.Context, tx *ethtypes.Transaction, privKey cryptotypes.PrivKey) *evmtypes.MsgEVMTransactionResponse {
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.NoError(t, err)
	
	req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	require.NoError(t, err)
	
	msgServer := evmkeeper.NewMsgServerImpl(&testApp.EvmKeeper)
	ante.Preprocess(ctx, req, testApp.EvmKeeper.ChainID(ctx))
	
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.NoError(t, err)
	
	return res
} 