package keeper_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/testutil/keeper"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/helpers"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/rand"
	tmtypes "github.com/tendermint/tendermint/types"
)

func TestPurgePrefixNotHang(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	_, evmAddr := keeper.MockAddressPair()
	for i := 0; i < 50; i++ {
		ctx = ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore())
		store := k.PrefixStore(ctx, types.StateKey(evmAddr))
		store.Set([]byte{0x03}, []byte("test"))
	}
	require.NotPanics(t, func() { k.PurgePrefix(ctx, types.StateKey(evmAddr)) })
}

func TestGetChainID(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	require.Equal(t, config.DefaultChainID, k.ChainID(ctx).Int64())

	ctx = ctx.WithChainID("pacific-1")
	require.Equal(t, int64(1329), k.ChainID(ctx).Int64())

	ctx = ctx.WithChainID("atlantic-2")
	require.Equal(t, int64(1328), k.ChainID(ctx).Int64())

	ctx = ctx.WithChainID("arctic-1")
	require.Equal(t, int64(713715), k.ChainID(ctx).Int64())
}

func TestGetVMBlockContext(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	moduleAddr := k.AccountKeeper().GetModuleAddress(authtypes.FeeCollectorName)
	evmAddr, _ := k.GetEVMAddress(ctx, moduleAddr)
	k.DeleteAddressMapping(ctx, moduleAddr, evmAddr)
	_, err := k.GetVMBlockContext(ctx, 0)
	require.NotNil(t, err)
}

func TestGetHashFn(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	f := k.GetHashFn(ctx)
	require.Equal(t, common.Hash{}, f(math.MaxInt64+1))
	require.Equal(t, common.BytesToHash(ctx.HeaderHash()), f(uint64(ctx.BlockHeight())))
	require.Equal(t, common.Hash{}, f(uint64(ctx.BlockHeight())+1))
	require.Equal(t, common.Hash{}, f(uint64(ctx.BlockHeight())-1))
}

func TestKeeper_CalculateNextNonce(t *testing.T) {
	address1 := common.BytesToAddress([]byte("addr1"))
	key1 := tmtypes.TxKey(rand.NewRand().Bytes(32))
	key2 := tmtypes.TxKey(rand.NewRand().Bytes(32))
	tests := []struct {
		name          string
		address       common.Address
		pending       bool
		setup         func(ctx sdk.Context, k *evmkeeper.Keeper)
		expectedNonce uint64
	}{
		{
			name:          "latest block, no latest stored",
			address:       address1,
			pending:       false,
			expectedNonce: 0,
		},
		{
			name:    "latest block, latest stored",
			address: address1,
			pending: false,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
			},
			expectedNonce: 50,
		},
		{
			name:    "latest block, latest stored with pending nonces",
			address: address1,
			pending: false,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
				// because pending:false, these won't matter
				k.AddPendingNonce(key1, address1, 50, 0)
				k.AddPendingNonce(key2, address1, 51, 0)
			},
			expectedNonce: 50,
		},
		{
			name:    "pending block, nonce should follow the last pending",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
				k.AddPendingNonce(key1, address1, 50, 0)
				k.AddPendingNonce(key2, address1, 51, 0)
			},
			expectedNonce: 52,
		},
		{
			name:    "pending block, nonce should be the value of hole",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
				k.AddPendingNonce(key1, address1, 50, 0)
				// missing 51, so nonce = 51
				k.AddPendingNonce(key2, address1, 52, 0)
			},
			expectedNonce: 51,
		},
		{
			name:    "pending block, completed nonces should also be skipped",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
				k.AddPendingNonce(key1, address1, 50, 0)
				k.AddPendingNonce(key2, address1, 51, 0)
				k.SetNonce(ctx, address1, 52)
				k.RemovePendingNonce(key1)
				k.RemovePendingNonce(key2)
			},
			expectedNonce: 52,
		},
		{
			name:    "pending block, hole created by expiration",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
				k.AddPendingNonce(key1, address1, 50, 0)
				k.AddPendingNonce(key2, address1, 51, 0)
				k.RemovePendingNonce(key1)
			},
			expectedNonce: 50,
		},
		{
			name:    "pending block, skipped nonces all in pending",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				// next expected for latest is 50, but 51,52 were sent
				k.SetNonce(ctx, address1, 50)
				k.AddPendingNonce(key1, address1, 51, 0)
				k.AddPendingNonce(key2, address1, 52, 0)
			},
			expectedNonce: 50,
		},
		{
			name:    "try 1000 nonces concurrently",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				// next expected for latest is 50, but 51,52 were sent
				k.SetNonce(ctx, address1, 50)
				wg := sync.WaitGroup{}
				for i := 50; i < 1000; i++ {
					wg.Add(1)
					go func(nonce int) {
						defer wg.Done()
						key := tmtypes.TxKey(rand.NewRand().Bytes(32))
						// call this just to exercise locks
						k.CalculateNextNonce(ctx, address1, true)
						k.AddPendingNonce(key, address1, uint64(nonce), 0)
					}(i)
				}
				wg.Wait()
			},
			expectedNonce: 1000,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			k, ctx := keeper.MockEVMKeeper()
			if test.setup != nil {
				test.setup(ctx, k)
			}
			next := k.CalculateNextNonce(ctx, test.address, test.pending)
			require.Equal(t, test.expectedNonce, next)
		})
	}
}

func TestDeferredInfo(t *testing.T) {
	a := app.Setup(false, false, false)
	k := a.EvmKeeper
	ctx := a.GetContextForDeliverTx([]byte{})
	ctx = ctx.WithTxIndex(1)
	k.AppendToEvmTxDeferredInfo(ctx, ethtypes.Bloom{1, 2, 3}, common.Hash{4, 5, 6}, sdk.NewInt(1))
	ctx = ctx.WithTxIndex(2)
	k.AppendToEvmTxDeferredInfo(ctx, ethtypes.Bloom{7, 8}, common.Hash{9, 0}, sdk.NewInt(1))
	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}, {Code: 1, Log: "test error"}})
	msg := mockEVMTransactionMessage(t)
	k.SetMsgs([]*types.MsgEVMTransaction{nil, {}, {}, msg})
	infoList := k.GetAllEVMTxDeferredInfo(ctx)
	require.Equal(t, 3, len(infoList))
	require.Equal(t, uint32(1), infoList[0].TxIndex)
	require.Equal(t, ethtypes.Bloom{1, 2, 3}, ethtypes.BytesToBloom(infoList[0].TxBloom))
	require.Equal(t, common.Hash{4, 5, 6}, common.BytesToHash(infoList[0].TxHash))
	require.Equal(t, sdk.NewInt(1), infoList[0].Surplus)
	require.Equal(t, uint32(2), infoList[1].TxIndex)
	require.Equal(t, ethtypes.Bloom{7, 8}, ethtypes.BytesToBloom(infoList[1].TxBloom))
	require.Equal(t, common.Hash{9, 0}, common.BytesToHash(infoList[1].TxHash))
	require.Equal(t, sdk.NewInt(1), infoList[1].Surplus)
	require.Equal(t, uint32(3), infoList[2].TxIndex)
	require.Equal(t, ethtypes.Bloom{}, ethtypes.BytesToBloom(infoList[2].TxBloom))
	etx, _ := msg.AsTransaction()
	require.Equal(t, etx.Hash(), common.BytesToHash(infoList[2].TxHash))
	require.Equal(t, "test error", infoList[2].Error)
	// test clear tx deferred info
	a.SetDeliverStateToCommit()
	a.Commit(context.Background()) // commit would clear transient stores
	k.SetTxResults([]*abci.ExecTxResult{})
	k.SetMsgs([]*types.MsgEVMTransaction{})
	infoList = k.GetAllEVMTxDeferredInfo(ctx)
	require.Empty(t, len(infoList))
}

func TestAddPendingNonce(t *testing.T) {
	k, _ := keeper.MockEVMKeeper()
	k.AddPendingNonce(tmtypes.TxKey{1}, common.HexToAddress("123"), 1, 1)
	k.AddPendingNonce(tmtypes.TxKey{2}, common.HexToAddress("123"), 2, 1)
	k.AddPendingNonce(tmtypes.TxKey{3}, common.HexToAddress("123"), 2, 2) // should replace the one above
	pendingTxs := k.GetPendingTxs()[common.HexToAddress("123").Hex()]
	require.Equal(t, 2, len(pendingTxs))
	require.Equal(t, tmtypes.TxKey{1}, pendingTxs[0].Key)
	require.Equal(t, uint64(1), pendingTxs[0].Nonce)
	require.Equal(t, int64(1), pendingTxs[0].Priority)
	require.Equal(t, tmtypes.TxKey{3}, pendingTxs[1].Key)
	require.Equal(t, uint64(2), pendingTxs[1].Nonce)
	require.Equal(t, int64(2), pendingTxs[1].Priority)
	keyToNonce := k.GetKeysToNonces()
	require.Equal(t, common.HexToAddress("123"), keyToNonce[tmtypes.TxKey{1}].Address)
	require.Equal(t, uint64(1), keyToNonce[tmtypes.TxKey{1}].Nonce)
	require.Equal(t, common.HexToAddress("123"), keyToNonce[tmtypes.TxKey{3}].Address)
	require.Equal(t, uint64(2), keyToNonce[tmtypes.TxKey{3}].Nonce)
	require.NotContains(t, keyToNonce, tmtypes.TxKey{2})
}

func TestGetCustomPrecompiles(t *testing.T) {
	// Read all version files from precompiles subfolders
	tagSet := make(map[string]bool)

	// Define the precompiles directories to scan
	precompileDirs := []string{
		"addr", "bank", "distribution", "gov", "ibc", "json",
		"oracle", "p256", "pointer", "pointerview", "solo", "staking", "wasmd",
	}
	precompileTags := map[string][]string{}

	// Read versions from each precompile directory
	for _, dir := range precompileDirs {
		versionFile := fmt.Sprintf("../../../precompiles/%s/versions", dir)
		content, err := os.ReadFile(versionFile)
		if err != nil {
			t.Logf("Warning: Could not read %s: %v", versionFile, err)
			continue
		}

		// Parse each line as a tag
		precompileTags[dir] = utils.Map(strings.Split(string(content), "\n"), strings.TrimSpace)
		for _, line := range precompileTags[dir] {
			if line != "" {
				tagSet[line] = true
			}
		}
	}

	// Convert to slice and sort
	var tags []string
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	// Verify we have tags
	require.Greater(t, len(tags), 0, "Should have found at least one tag")

	// Setup keeper and context
	k, ctx := keeper.MockEVMKeeperPrecompiles()

	// Set up upgrade heights with increment of 10
	baseHeight := 1000000
	tagToHeight := map[string]int64{}
	for i, tag := range tags {
		height := baseHeight + (i * 10)
		k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(int64(height)), tag)
		t.Logf("Set upgrade height %d for tag %s", height, tag)
		tagToHeight[tag] = int64(height)
	}

	for precompile, tags := range precompileTags {
		for _, tag := range tags {
			ps := k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(tagToHeight[tag] + 1))
			for p, rtag := range ps {
				if p.Hex() != precompile {
					continue
				}
				require.Equal(t, tag, rtag)
			}
		}
	}
}

func mockEVMTransactionMessage(t *testing.T) *types.MsgEVMTransaction {
	k, ctx := testkeeper.MockEVMKeeper()
	chainID := k.ChainID(ctx)
	chainCfg := types.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	txData := ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10000000000000),
		Gas:       1000,
		To:        to,
		Value:     big.NewInt(1000000000000000),
		Data:      []byte("abc"),
		ChainID:   chainID,
	}

	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	typedTx, err := ethtx.NewDynamicFeeTx(tx)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	return msg
}

func TestGetBaseFeeBeforeV620(t *testing.T) {
	// Set up a test app and context
	testApp := app.Setup(false, false, false)
	testHeight := int64(1000)
	testCtx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(testHeight)

	// Set chain ID to pacific-1
	testCtx = testCtx.WithChainID("pacific-1")

	// Set the v6.2.0 upgrade height to a value higher than our test height
	v620UpgradeHeight := int64(2000)
	testApp.UpgradeKeeper.SetDone(testCtx.WithBlockHeight(v620UpgradeHeight), "6.2.0")

	keeper := &testApp.EvmKeeper

	// Before upgrade: should be nil
	baseFee := keeper.GetBaseFee(testCtx)
	require.Nil(t, baseFee, "Base fee should be nil for pacific-1 before v6.2.0 upgrade")

	// After upgrade: should not be nil
	ctxAfterUpgrade := testCtx.WithBlockHeight(2500)
	baseFeeAfter := keeper.GetBaseFee(ctxAfterUpgrade)
	require.NotNil(t, baseFeeAfter, "Base fee should not be nil for pacific-1 after v6.2.0 upgrade")

	// Non-pacific-1 chain: should not be nil
	ctxOtherChain := testCtx.WithChainID("test-chain")
	baseFeeOther := keeper.GetBaseFee(ctxOtherChain)
	require.NotNil(t, baseFeeOther, "Base fee should not be nil for non-pacific-1 chains")
}

var SignerMap = map[derived.SignerVersion]func(*big.Int) ethtypes.Signer{
	derived.London: ethtypes.NewLondonSigner,
	derived.Cancun: ethtypes.NewCancunSigner,
}

var SignerMapAllVersions = map[string]func(*big.Int) ethtypes.Signer{
	"London": ethtypes.NewLondonSigner,
	"Cancun": ethtypes.NewCancunSigner,
}

func TestRecovery(t *testing.T) {
	// Transaction data from block 170818561 on Pacific-1 (chain ID 1329)
	// This is a legacy transaction with V=35, which encodes chain ID 0
	// Expected sender: 0x07fF2517E630c1CEa9cC1eC594957cC293aa80B2
	// Expected tx hash: 0x882c9df49bb1e77800f0f1d91e07cecde91c4178a9894a0f679e8daf4bc0c4df

	to := common.HexToAddress("0xa26b9bfe606d29f16b5aecf30f9233934452c4e2")
	gasPrice := big.NewInt(3987777747)
	value := big.NewInt(0)
	data := common.Hex2Bytes("a0712d68000000000000000000000000000000000000000000000000000000003b9aca00")
	v := big.NewInt(35)
	r, _ := new(big.Int).SetString("8774931ee5b3fed1eafb67cc3e38202265381f5aaebb2ca7fa8ff679068f0337", 16)
	s, _ := new(big.Int).SetString("2a07e49c9f0bbaea59534553003e33fb476169072f6f18872983872b3e447370", 16)

	ethTx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    7,
		GasPrice: gasPrice,
		Gas:      500000,
		To:       &to,
		Value:    value,
		Data:     data,
		V:        v,
		R:        r,
		S:        s,
	})

	expectedSender := "0x07fF2517E630c1CEa9cC1eC594957cC293aa80B2"
	expectedTxHash := "0x882c9df49bb1e77800f0f1d91e07cecde91c4178a9894a0f679e8daf4bc0c4df"

	fmt.Printf("\n=== Transaction Info ===\n")
	fmt.Printf("Transaction chain ID (from V): %v\n", ethTx.ChainId())
	fmt.Printf("Transaction Protected: %v\n", ethTx.Protected())
	fmt.Printf("Transaction Hash: %v\n", ethTx.Hash().Hex())
	fmt.Printf("Expected Hash: %v\n", expectedTxHash)
	fmt.Printf("Hash Match: %v\n", ethTx.Hash().Hex() == expectedTxHash)

	V, R, S := ethTx.RawSignatureValues()
	fmt.Printf("\n=== Raw Signature ===\n")
	fmt.Printf("V: %v (0x%x)\n", V, V)
	fmt.Printf("R: %v\n", R)
	fmt.Printf("S: %v\n", S)

	// Test different recovery scenarios
	type scenario struct {
		name          string
		chainIDForCfg *big.Int // Chain ID to use for creating config/signer
		chainIDForV   *big.Int // Chain ID to use for V adjustment
		useEthTxHash  bool     // Use ethTx.Hash() vs signer.Hash()
		vAdjustments  []int64  // V adjustments to try (e.g., -8, 0, etc.)
		signerVersion string   // "London", "Cancun", or "" for default
	}

	scenarios := []scenario{
		{
			name:          "Scenario 1: Network chain ID (1329) for everything",
			chainIDForCfg: big.NewInt(1329),
			chainIDForV:   big.NewInt(1329),
			useEthTxHash:  false,
			vAdjustments:  []int64{0}, // Use AdjustV result directly
		},
		{
			name:          "Scenario 2: TX chain ID (0) for everything",
			chainIDForCfg: big.NewInt(0),
			chainIDForV:   big.NewInt(0),
			useEthTxHash:  false,
			vAdjustments:  []int64{0},
		},
		{
			name:          "Scenario 3: Network chain ID (1329) for config, TX chain ID (0) for V",
			chainIDForCfg: big.NewInt(1329),
			chainIDForV:   big.NewInt(0),
			useEthTxHash:  false,
			vAdjustments:  []int64{0},
		},
		{
			name:          "Scenario 3: Network chain ID (0) for config, TX chain ID (1329) for V",
			chainIDForCfg: big.NewInt(0),
			chainIDForV:   big.NewInt(1329),
			useEthTxHash:  false,
			vAdjustments:  []int64{0},
		},
		{
			name:          "Scenario 4: Use ethTx.Hash() with TX chain ID (0) for V",
			chainIDForCfg: big.NewInt(0), // Doesn't matter since we use ethTx.Hash()
			chainIDForV:   big.NewInt(0),
			useEthTxHash:  true,
			vAdjustments:  []int64{0},
		},
		{
			name:          "Scenario 5: Use ethTx.Hash() with raw V values (27, 28)",
			chainIDForCfg: big.NewInt(0),
			chainIDForV:   nil, // Skip AdjustV
			useEthTxHash:  true,
			vAdjustments:  []int64{-8, -7}, // V-8=27, V-7=28
		},
		{
			name:          "Scenario 6: v6.1.4 logic - London signer with TX chain ID (0)",
			chainIDForCfg: big.NewInt(0),
			chainIDForV:   big.NewInt(0),
			useEthTxHash:  false, // Use signer.Hash() with London signer
			vAdjustments:  []int64{0},
			signerVersion: "London",
		},
		{
			name:          "Scenario 7: FrontierSigner - raw V values (27, 28)",
			chainIDForCfg: nil, // Not used for Frontier
			chainIDForV:   nil, // No adjustment
			useEthTxHash:  false,
			vAdjustments:  []int64{-8, -7}, // Try V-8=27, V-7=28
			signerVersion: "Frontier",
		},
		{
			name:          "Scenario 8: FrontierSigner - with AdjustV(chain ID 0)",
			chainIDForCfg: nil,           // Not used for Frontier
			chainIDForV:   big.NewInt(0), // Adjust with chain ID 0
			useEthTxHash:  false,
			vAdjustments:  []int64{0, 1}, // Try adjusted V and adjusted V+1
			signerVersion: "Frontier",
		},
		{
			name:          "Scenario 9: FrontierSigner - with AdjustV(chain ID 1329)",
			chainIDForCfg: nil,              // Not used for Frontier
			chainIDForV:   big.NewInt(1329), // Adjust with chain ID 1329
			useEthTxHash:  false,
			vAdjustments:  []int64{0}, // Try adjusted V
			signerVersion: "Frontier",
		},
		{
			name:          "Scenario 10: Unadjust V=35 (assume it's pre-adjusted with chain ID 0)",
			chainIDForCfg: nil,
			chainIDForV:   nil,
			useEthTxHash:  true,
			vAdjustments:  []int64{8}, // V + 8 = 35 + 8 = 43 (reverse of V - 8)
			signerVersion: "",
		},
		{
			name:          "Scenario 11: Unadjust V=35 (assume it's pre-adjusted with chain ID 1329)",
			chainIDForCfg: nil,
			chainIDForV:   nil,
			useEthTxHash:  true,
			vAdjustments:  []int64{2666}, // V + (1329*2) + 8 = 35 + 2658 + 8 = 2701
			signerVersion: "",
		},
		{
			name:          "Scenario 12: PRODUCTION - London signer with chain ID 0, V=27",
			chainIDForCfg: big.NewInt(0),
			chainIDForV:   big.NewInt(0),
			useEthTxHash:  false, // Use signer.Hash() with chain ID 0
			vAdjustments:  []int64{0},
			signerVersion: "London",
		},
	}

	fmt.Printf("\n=== Testing Recovery Scenarios ===\n")

	successFound := false
	var successScenario string

	for _, sc := range scenarios {
		fmt.Printf("\n--- %s ---\n", sc.name)

		// Wrap in a function to recover from panics
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("  ❌ Panic: %v\n", r)
				}
			}()

			var txHash common.Hash
			if sc.useEthTxHash {
				txHash = ethTx.Hash()
				fmt.Printf("Using ethTx.Hash(): %v\n", txHash.Hex())
			} else {
				if sc.signerVersion == "Frontier" {
					// FrontierSigner doesn't take a chain ID
					frontierSigner := ethtypes.FrontierSigner{}
					txHash = frontierSigner.Hash(ethTx)
					fmt.Printf("Using FrontierSigner (unprotected)\n")
					fmt.Printf("Hash: %v\n", txHash.Hex())
				} else {
					var signer ethtypes.Signer
					if sc.signerVersion != "" {
						signer = SignerMapAllVersions[sc.signerVersion](sc.chainIDForCfg)
						fmt.Printf("Using %s signer with chain ID %v\n", sc.signerVersion, sc.chainIDForCfg)
					} else {
						version := derived.Cancun
						signer = SignerMap[version](sc.chainIDForCfg)
						fmt.Printf("Using Cancun signer with chain ID %v\n", sc.chainIDForCfg)
					}
					txHash = signer.Hash(ethTx)
					fmt.Printf("Hash: %v\n", txHash.Hex())
				}
			}

			// First try the configured V adjustments
			for _, vAdj := range sc.vAdjustments {
				var vToUse *big.Int
				if sc.chainIDForV == nil {
					// Use raw V + adjustment
					vToUse = new(big.Int).Add(V, big.NewInt(vAdj))
					fmt.Printf("  Trying V=%v (raw V + %d)\n", vToUse, vAdj)
				} else {
					// Use AdjustV
					vToUse = AdjustV(V, ethTx.Type(), sc.chainIDForV)
					if vAdj != 0 {
						vToUse = new(big.Int).Add(vToUse, big.NewInt(vAdj))
					}
					fmt.Printf("  Trying V=%v (AdjustV with chain ID %v, then +%d)\n", vToUse, sc.chainIDForV, vAdj)
				}

				evmAddr, _, _, err := helpers.GetAddresses(vToUse, R, S, txHash)
				if err != nil {
					fmt.Printf("    ❌ Error: %v\n", err)
				} else {
					match := evmAddr.Hex() == expectedSender
					if match {
						fmt.Printf("    ✅ SUCCESS: %v\n", evmAddr.Hex())
						successFound = true
						successScenario = fmt.Sprintf("%s with V=%v", sc.name, vToUse)
						return // Exit early on success
					} else {
						fmt.Printf("    ❌ Wrong address: %v\n", evmAddr.Hex())
					}
				}
			}

			// If configured V adjustments didn't work, brute force V values
			fmt.Printf("  Brute forcing V values from -3000 to 3000...\n")
			foundV35 := false
			for v := int64(-3000); v <= 3000; v++ {
				vToUse := big.NewInt(v)
				evmAddr, _, _, err := helpers.GetAddresses(vToUse, R, S, txHash)

				// Track when we test V=35 specifically
				if v == 35 {
					foundV35 = true
					if err != nil {
						fmt.Printf("    V=35 (original): Error: %v\n", err)
					} else {
						fmt.Printf("    V=35 (original): Recovered address: %v\n", evmAddr.Hex())
					}
				}

				if err == nil && evmAddr.Hex() == expectedSender {
					fmt.Printf("    ✅ BRUTE FORCE SUCCESS! V=%v recovers the correct sender\n", v)
					successFound = true
					successScenario = fmt.Sprintf("%s with brute-forced V=%v", sc.name, v)
					return // Exit early on success
				}
			}
			if !foundV35 {
				fmt.Printf("    ⚠️  WARNING: V=35 was not tested in the range!\n")
			}
			fmt.Printf("  ❌ Brute force failed for this scenario\n")
		}()
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Expected sender: %s\n", expectedSender)

	if successFound {
		fmt.Printf("✅ SUCCESS! Winning scenario: %s\n", successScenario)
	} else {
		fmt.Printf("❌ FAILURE: No scenario successfully recovered the expected sender\n")
		fmt.Printf("Brute forced V values from -3000 to 3000 for each scenario's hash - none worked\n")
		t.Fatal("Failed to recover the correct sender with any tested scenario or brute force")
	}
}

func AdjustV(V *big.Int, txType uint8, chainID *big.Int) *big.Int {
	// Non-legacy TX always needs to be bumped by 27
	if txType != ethtypes.LegacyTxType {
		return new(big.Int).Add(V, utils.Big27)
	}

	// legacy TX needs to be adjusted based on chainID
	V = new(big.Int).Sub(V, new(big.Int).Mul(chainID, utils.Big2))
	return V.Sub(V, utils.Big8)
}
