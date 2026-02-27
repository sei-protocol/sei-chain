package staking_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	ethtx "github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

// Using f from staking_test.go

func TestStakingPrecompileEventsEmission(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup validators - make them Bonded so they can accept delegations
	valPub := ed25519.GenPrivKey().PubKey()
	valAddr := setupValidator(t, ctx, testApp, stakingtypes.Bonded, valPub)
	valStr := valAddr.String()

	valPub2 := ed25519.GenPrivKey().PubKey()
	valAddr2 := setupValidator(t, ctx, testApp, stakingtypes.Bonded, valPub2)
	valStr2 := valAddr2.String()

	// Setup test account
	privKey := testkeeper.MockPrivateKey()
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	// Fund the account with more funds
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(2000000000000)))
	require.NoError(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))

	// Test delegate event
	t.Run("TestDelegateEvent", func(t *testing.T) {
		abi := pcommon.MustGetABI(f, "abi.json")
		args, err := abi.Pack("delegate", valStr)
		require.NoError(t, err)

		addr := common.HexToAddress(staking.StakingAddress)
		delegateAmount := big.NewInt(100_000_000_000_000) // 100 usei in wei

		tx := createEVMTx(t, k, ctx, privKey, &addr, args, delegateAmount)
		res := executeEVMTx(t, testApp, ctx, tx, privKey)

		require.Empty(t, res.VmError)
		require.NotEmpty(t, res.Logs)

		// Verify the events (Delegate event + DelegationRewardsWithdrawn event)
		require.Len(t, res.Logs, 2)
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

	// Test rewards withdrawn event emitted during delegate
	t.Run("TestDelegateRewardsWithdrawnEvent", func(t *testing.T) {
		// Delegate again to the same validator to trigger reward withdrawal
		abi := pcommon.MustGetABI(f, "abi.json")
		args, err := abi.Pack("delegate", valStr)
		require.NoError(t, err)

		addr := common.HexToAddress(staking.StakingAddress)
		delegateAmount := big.NewInt(100_000_000_000_000) // 100 usei in wei

		tx := createEVMTx(t, k, ctx, privKey, &addr, args, delegateAmount)
		res := executeEVMTx(t, testApp, ctx, tx, privKey)

		require.Empty(t, res.VmError)
		require.NotEmpty(t, res.Logs)

		// Verify we have 2 logs: Delegate event and DelegationRewardsWithdrawn event
		require.Len(t, res.Logs, 2)

		// The second log should be the DelegationRewardsWithdrawn event
		rewardsLog := res.Logs[1]

		// Check event signature for DelegationRewardsWithdrawn
		expectedSig := pcommon.DelegationRewardsWithdrawnEventSig
		require.Equal(t, expectedSig.Hex(), rewardsLog.Topics[0])

		// Check indexed delegator address
		require.Equal(t, common.BytesToHash(evmAddr.Bytes()).Hex(), rewardsLog.Topics[1])

		// Decode the event data
		// Data layout: offset for string (32 bytes), amount (32 bytes), string length (32 bytes), string data
		require.GreaterOrEqual(t, len(rewardsLog.Data), 96) // At least 3 * 32 bytes

		// Verify the amount is encoded in the data (at position 32-64)
		// Note: In a test without block progression, rewards should be 0
		amountBytes := rewardsLog.Data[32:64]
		rewardsAmount := new(big.Int).SetBytes(amountBytes)
		require.True(t, rewardsAmount.Sign() >= 0, "rewards amount should be non-negative")

		// Verify the validator string is encoded in the data
		strLen := new(big.Int).SetBytes(rewardsLog.Data[64:96]).Int64()
		validatorStr := string(rewardsLog.Data[96 : 96+strLen])
		require.Equal(t, valStr, validatorStr)
	})

	// Test redelegate event
	t.Run("TestRedelegateEvent", func(t *testing.T) {
		// First, delegate some funds to the first validator
		addr := common.HexToAddress(staking.StakingAddress)
		delegateArgs, err := pcommon.MustGetABI(f, "abi.json").Pack("delegate", valStr)
		require.NoError(t, err)

		delegateTx := createEVMTx(t, k, ctx, privKey, &addr, delegateArgs, big.NewInt(100_000_000_000_000)) // 100 usei in wei
		delegateRes := executeEVMTx(t, testApp, ctx, delegateTx, privKey)
		require.Empty(t, delegateRes.VmError)

		// Now redelegate some funds to the second validator
		abi := pcommon.MustGetABI(f, "abi.json")
		redelegateAmount := big.NewInt(50) // 50 usei (same as original test)
		args, err := abi.Pack("redelegate", valStr, valStr2, redelegateAmount)
		require.NoError(t, err)

		tx := createEVMTx(t, k, ctx, privKey, &addr, args, big.NewInt(0))
		res := executeEVMTx(t, testApp, ctx, tx, privKey)

		require.Empty(t, res.VmError)
		require.NotEmpty(t, res.Logs)

		// Verify the events (Redelegate event + src DelegationRewardsWithdrawn + dst DelegationRewardsWithdrawn)
		require.Len(t, res.Logs, 3)
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

	// Test rewards withdrawn events emitted during redelegate
	t.Run("TestRedelegateRewardsWithdrawnEvents", func(t *testing.T) {
		// At this point from prior tests, there is an existing delegation to valStr2
		// (from the redelegate test above). We delegate more to valStr and then
		// redelegate from valStr to valStr2, which should trigger reward withdrawal
		// from both src (valStr) and dst (valStr2).
		addr := common.HexToAddress(staking.StakingAddress)

		// Delegate to valStr to have funds to redelegate
		delegateArgs, err := pcommon.MustGetABI(f, "abi.json").Pack("delegate", valStr)
		require.NoError(t, err)
		delegateTx := createEVMTx(t, k, ctx, privKey, &addr, delegateArgs, big.NewInt(100_000_000_000_000))
		delegateRes := executeEVMTx(t, testApp, ctx, delegateTx, privKey)
		require.Empty(t, delegateRes.VmError)

		// Redelegate from valStr to valStr2 (valStr2 already has a delegation)
		abi := pcommon.MustGetABI(f, "abi.json")
		redelegateAmount := big.NewInt(50) // 50 usei
		args, err := abi.Pack("redelegate", valStr, valStr2, redelegateAmount)
		require.NoError(t, err)

		tx := createEVMTx(t, k, ctx, privKey, &addr, args, big.NewInt(0))
		res := executeEVMTx(t, testApp, ctx, tx, privKey)

		require.Empty(t, res.VmError)
		require.NotEmpty(t, res.Logs)

		// Verify we have 3 logs: Redelegate event + src RewardsWithdrawn + dst RewardsWithdrawn
		require.Len(t, res.Logs, 3)

		// Log[0]: Redelegate event
		require.Equal(t, pcommon.RedelegateEventSig.Hex(), res.Logs[0].Topics[0])

		// Log[1]: DelegationRewardsWithdrawn for src validator
		srcRewardsLog := res.Logs[1]
		require.Equal(t, pcommon.DelegationRewardsWithdrawnEventSig.Hex(), srcRewardsLog.Topics[0])
		require.Equal(t, common.BytesToHash(evmAddr.Bytes()).Hex(), srcRewardsLog.Topics[1])
		require.GreaterOrEqual(t, len(srcRewardsLog.Data), 96)

		// Verify src validator string
		srcStrLen := new(big.Int).SetBytes(srcRewardsLog.Data[64:96]).Int64()
		srcValidatorStr := string(srcRewardsLog.Data[96 : 96+srcStrLen])
		require.Equal(t, valStr, srcValidatorStr)

		// Verify src rewards amount is non-negative
		srcAmount := new(big.Int).SetBytes(srcRewardsLog.Data[32:64])
		require.True(t, srcAmount.Sign() >= 0, "src rewards amount should be non-negative")

		// Log[2]: DelegationRewardsWithdrawn for dst validator
		dstRewardsLog := res.Logs[2]
		require.Equal(t, pcommon.DelegationRewardsWithdrawnEventSig.Hex(), dstRewardsLog.Topics[0])
		require.Equal(t, common.BytesToHash(evmAddr.Bytes()).Hex(), dstRewardsLog.Topics[1])
		require.GreaterOrEqual(t, len(dstRewardsLog.Data), 96)

		// Verify dst validator string
		dstStrLen := new(big.Int).SetBytes(dstRewardsLog.Data[64:96]).Int64()
		dstValidatorStr := string(dstRewardsLog.Data[96 : 96+dstStrLen])
		require.Equal(t, valStr2, dstValidatorStr)

		// Verify dst rewards amount is non-negative
		dstAmount := new(big.Int).SetBytes(dstRewardsLog.Data[32:64])
		require.True(t, dstAmount.Sign() >= 0, "dst rewards amount should be non-negative")
	})

	// Test undelegate event
	t.Run("TestUndelegateEvent", func(t *testing.T) {
		// First, delegate some funds
		addr := common.HexToAddress(staking.StakingAddress)
		delegateArgs, err := pcommon.MustGetABI(f, "abi.json").Pack("delegate", valStr)
		require.NoError(t, err)

		delegateTx := createEVMTx(t, k, ctx, privKey, &addr, delegateArgs, big.NewInt(100_000_000_000_000)) // 100 usei in wei
		delegateRes := executeEVMTx(t, testApp, ctx, delegateTx, privKey)
		require.Empty(t, delegateRes.VmError)

		// Now undelegate some funds
		abi := pcommon.MustGetABI(f, "abi.json")
		undelegateAmount := big.NewInt(30) // 30 usei (same as original test)
		args, err := abi.Pack("undelegate", valStr, undelegateAmount)
		require.NoError(t, err)

		tx := createEVMTx(t, k, ctx, privKey, &addr, args, big.NewInt(0))
		res := executeEVMTx(t, testApp, ctx, tx, privKey)

		require.Empty(t, res.VmError)
		require.NotEmpty(t, res.Logs)

		// Verify we have 2 logs: Undelegate event and DelegationRewardsWithdrawn event
		require.Len(t, res.Logs, 2)
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

	// Test rewards withdrawn event emitted during undelegate
	t.Run("TestUndelegateRewardsWithdrawnEvent", func(t *testing.T) {
		// First, delegate some funds
		addr := common.HexToAddress(staking.StakingAddress)
		delegateArgs, err := pcommon.MustGetABI(f, "abi.json").Pack("delegate", valStr)
		require.NoError(t, err)

		delegateTx := createEVMTx(t, k, ctx, privKey, &addr, delegateArgs, big.NewInt(100_000_000_000_000)) // 100 usei in wei
		delegateRes := executeEVMTx(t, testApp, ctx, delegateTx, privKey)
		require.Empty(t, delegateRes.VmError)

		// Now undelegate some funds
		abi := pcommon.MustGetABI(f, "abi.json")
		undelegateAmount := big.NewInt(30) // 30 usei
		args, err := abi.Pack("undelegate", valStr, undelegateAmount)
		require.NoError(t, err)

		tx := createEVMTx(t, k, ctx, privKey, &addr, args, big.NewInt(0))
		res := executeEVMTx(t, testApp, ctx, tx, privKey)

		require.Empty(t, res.VmError)
		require.NotEmpty(t, res.Logs)

		// Verify we have 2 logs: Undelegate event and DelegationRewardsWithdrawn event
		require.Len(t, res.Logs, 2)

		// The second log should be the DelegationRewardsWithdrawn event
		rewardsLog := res.Logs[1]

		// Check event signature for DelegationRewardsWithdrawn
		expectedSig := pcommon.DelegationRewardsWithdrawnEventSig
		require.Equal(t, expectedSig.Hex(), rewardsLog.Topics[0])

		// Check indexed delegator address
		require.Equal(t, common.BytesToHash(evmAddr.Bytes()).Hex(), rewardsLog.Topics[1])

		// Decode the event data
		// For the DelegationRewardsWithdrawn event, the validator string and amount are not indexed
		// Data layout: offset for string (32 bytes), amount (32 bytes), string length (32 bytes), string data
		require.GreaterOrEqual(t, len(rewardsLog.Data), 96) // At least 3 * 32 bytes

		// Verify the amount is encoded in the data (at position 32-64)
		// Note: In a simple test without block progression, rewards might be 0
		amountBytes := rewardsLog.Data[32:64]
		rewardsAmount := new(big.Int).SetBytes(amountBytes)
		// Rewards amount should be >= 0 (could be 0 if no rewards accumulated)
		require.True(t, rewardsAmount.Sign() >= 0, "rewards amount should be non-negative")
	})

	// Test createValidator event
	t.Run("TestCreateValidatorEvent", func(t *testing.T) {
		// Setup a new account for validator creation
		validatorPrivKey := testkeeper.MockPrivateKey()
		validatorSeiAddr, validatorEvmAddr := testkeeper.PrivateKeyToAddresses(validatorPrivKey)
		k.SetAddressMapping(ctx, validatorSeiAddr, validatorEvmAddr)

		// Fund the validator account
		amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000000000)))
		require.NoError(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
		require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, validatorSeiAddr, amt))

		// Create validator arguments - use ed25519 key
		ed25519PrivKey := ed25519.GenPrivKey()
		pubKeyHex := hex.EncodeToString(ed25519PrivKey.PubKey().Bytes())
		moniker := "Test Validator"
		commissionRate := "0.1"
		commissionMaxRate := "0.2"
		commissionMaxChangeRate := "0.05"
		minSelfDelegation := big.NewInt(1000)

		abi := pcommon.MustGetABI(f, "abi.json")
		args, err := abi.Pack("createValidator", pubKeyHex, moniker, commissionRate, commissionMaxRate, commissionMaxChangeRate, minSelfDelegation)
		require.NoError(t, err)

		addr := common.HexToAddress(staking.StakingAddress)
		delegateAmount := big.NewInt(1_000_000_000_000_000_000) // 1 SEI in wei (1,000,000 usei)

		tx := createEVMTx(t, k, ctx, validatorPrivKey, &addr, args, delegateAmount)
		res := executeEVMTx(t, testApp, ctx, tx, validatorPrivKey)

		require.Empty(t, res.VmError)
		require.NotEmpty(t, res.Logs)

		// Verify the event
		require.Len(t, res.Logs, 1)
		log := res.Logs[0]

		// Check event signature
		expectedSig := pcommon.ValidatorCreatedEventSig
		require.Equal(t, expectedSig.Hex(), log.Topics[0])

		// Check indexed creator address
		require.Equal(t, common.BytesToHash(validatorEvmAddr.Bytes()).Hex(), log.Topics[1])

		// Verify data is not empty (contains validator address and moniker)
		require.NotEmpty(t, log.Data)
		require.GreaterOrEqual(t, len(log.Data), 128) // At least 4 * 32 bytes for offsets and lengths
	})

	// Test editValidator event
	t.Run("TestEditValidatorEvent", func(t *testing.T) {
		// First create a validator using existing test setup
		validatorPrivKey := testkeeper.MockPrivateKey()
		validatorSeiAddr, validatorEvmAddr := testkeeper.PrivateKeyToAddresses(validatorPrivKey)
		k.SetAddressMapping(ctx, validatorSeiAddr, validatorEvmAddr)

		// Fund the validator account
		amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000000000)))
		require.NoError(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
		require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, validatorSeiAddr, amt))

		// Create validator first - use ed25519 key
		ed25519PrivKey := ed25519.GenPrivKey()
		pubKeyHex := hex.EncodeToString(ed25519PrivKey.PubKey().Bytes())
		moniker := "Initial Validator"
		commissionRate := "0.1"
		commissionMaxRate := "0.2"
		commissionMaxChangeRate := "0.05"
		minSelfDelegation := big.NewInt(1000)

		abi := pcommon.MustGetABI(f, "abi.json")
		createArgs, err := abi.Pack("createValidator", pubKeyHex, moniker, commissionRate, commissionMaxRate, commissionMaxChangeRate, minSelfDelegation)
		require.NoError(t, err)

		addr := common.HexToAddress(staking.StakingAddress)
		createTx := createEVMTx(t, k, ctx, validatorPrivKey, &addr, createArgs, big.NewInt(1_000_000_000_000_000_000))
		createRes := executeEVMTx(t, testApp, ctx, createTx, validatorPrivKey)
		require.Empty(t, createRes.VmError)

		// Now edit the validator
		newMoniker := "Edited Validator"
		newCommissionRate := ""               // Empty string to not change commission rate
		newMinSelfDelegation := big.NewInt(0) // 0 to not change minimum self-delegation

		editArgs, err := abi.Pack("editValidator", newMoniker, newCommissionRate, newMinSelfDelegation)
		require.NoError(t, err)

		editTx := createEVMTx(t, k, ctx, validatorPrivKey, &addr, editArgs, big.NewInt(0))
		editRes := executeEVMTx(t, testApp, ctx, editTx, validatorPrivKey)

		require.Empty(t, editRes.VmError)
		require.NotEmpty(t, editRes.Logs)

		// Verify the event (should be the last log)
		log := editRes.Logs[len(editRes.Logs)-1]

		// Check event signature
		expectedSig := pcommon.ValidatorEditedEventSig
		require.Equal(t, expectedSig.Hex(), log.Topics[0])

		// Check indexed editor address
		require.Equal(t, common.BytesToHash(validatorEvmAddr.Bytes()).Hex(), log.Topics[1])

		// Verify data is not empty (contains validator address and new moniker)
		require.NotEmpty(t, log.Data)
		require.GreaterOrEqual(t, len(log.Data), 128) // At least 4 * 32 bytes for offsets and lengths
	})
}

func createEVMTx(t *testing.T, k *evmkeeper.Keeper, ctx sdk.Context, privKey cryptotypes.PrivKey, to *common.Address, data []byte, value *big.Int) *ethtypes.Transaction {
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)

	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	nonce := k.GetNonce(ctx, evmAddr)

	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000, // Increased gas limit for staking operations
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
	ante.Preprocess(ctx, req, testApp.EvmKeeper.ChainID(ctx), false)

	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.NoError(t, err)

	return res
}

// setupValidator is already defined in staking_test.go
