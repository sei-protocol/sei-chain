package evmonly

import (
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// seiLoadERC20Runtime deploys sei-load's ERC20 contract through the real executor and returns
// the runtime code it leaves.
func seiLoadERC20Runtime(t *testing.T, chainID *big.Int, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	bin, err := os.ReadFile("testdata/seiload_erc20.bin")
	require.NoError(t, err)
	deploy, err := hex.DecodeString(strings.TrimSpace(string(bin)))
	require.NoError(t, err)
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}}.Pack("LoadToken", "LT")
	require.NoError(t, err)

	state := NewMemoryState()
	state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(100*testFundedBalanceWei))
	executor := NewExecutor(Config{MockExecution: true}, withTestState(state))
	defer executor.Close()
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: [][]byte{
		signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), append(deploy, args...), 1_500_000),
	}})
	require.NoError(t, err)
	require.Equal(t, uint64(1), result.Txs[0].Status)
	require.Len(t, result.ChangeSet.Code, 1)
	return result.ChangeSet.Code[0].Code
}

func mockERC20TransferData(t *testing.T, to common.Address, amount int64) []byte {
	t.Helper()
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: addressType}, {Type: uintType}}.Pack(to, big.NewInt(amount))
	require.NoError(t, err)
	return append(append([]byte(nil), mockERC20TransferSelector...), args...)
}

func TestMockedERC20TransfersWriteTheStorageTheContractWrites(t *testing.T) {
	chainID := big.NewInt(testChainID)
	alice, err := crypto.HexToECDSA("45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8")
	require.NoError(t, err)
	bob, err := crypto.GenerateKey()
	require.NoError(t, err)
	runtime := seiLoadERC20Runtime(t, chainID, alice)
	token := testAddress(0xe2)
	carol, dave := testAddress(0xc1), testAddress(0xd1)
	bobAddr := crypto.PubkeyToAddress(bob.PublicKey)

	txs := [][]byte{
		signLegacyTx(t, alice, chainID, 0, &token, big.NewInt(0), mockERC20TransferData(t, carol, 1)),
		signLegacyTx(t, alice, chainID, 1, &token, big.NewInt(0), mockERC20TransferData(t, carol, 1)),
		signLegacyTx(t, bob, chainID, 0, &token, big.NewInt(0), mockERC20TransferData(t, dave, 7)),
		signLegacyTx(t, bob, chainID, 1, &token, big.NewInt(0), mockERC20TransferData(t, bobAddr, 3)),
		signLegacyTx(t, alice, chainID, 2, &token, big.NewInt(0), mockERC20TransferData(t, bobAddr, 2)),
	}
	run := func(mock bool) *BlockResult {
		state := NewMemoryState()
		state.SetBalance(crypto.PubkeyToAddress(alice.PublicKey), big.NewInt(100*testFundedBalanceWei))
		state.SetBalance(bobAddr, big.NewInt(100*testFundedBalanceWei))
		state.SetCode(token, runtime)
		state.SetState(token, mockERC20BalanceSlot(dave), common.BigToHash(big.NewInt(100)))
		executor := NewExecutor(Config{MockExecution: mock}, withTestState(state))
		t.Cleanup(executor.Close)
		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: txs})
		require.NoError(t, err)
		return result
	}
	real, mocked := run(false), run(true)

	require.NotEmpty(t, real.ChangeSet.Storage)
	require.Equal(t, real.ChangeSet.Storage, mocked.ChangeSet.Storage)
	require.Equal(t, real.ChangeSet.Nonces, mocked.ChangeSet.Nonces)
	require.Len(t, mocked.Txs, len(real.Txs))
	for i := range real.Txs {
		require.Equal(t, real.Txs[i].Status, mocked.Txs[i].Status, "tx %d", i)
		require.Len(t, mocked.Receipts[i].Logs, len(real.Receipts[i].Logs), "tx %d", i)
		for j, want := range real.Receipts[i].Logs {
			got := mocked.Receipts[i].Logs[j]
			require.Equal(t, want.Address, got.Address)
			require.Equal(t, want.Topics, got.Topics)
			require.Equal(t, want.Data, got.Data)
			require.Equal(t, want.Index, got.Index)
		}
	}
}

func TestMockedTransfersChargeTheGasLimitAndMoveValue(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA("45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8")
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa1)
	coinbase := blockContext(chainID).Coinbase
	funded := big.NewInt(testFundedBalanceWei)

	state := NewMemoryState()
	state.SetBalance(sender, funded)
	executor := NewExecutor(Config{MockExecution: true}, withTestState(state))
	defer executor.Close()
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: [][]byte{
		signLegacyTxWithGas(t, key, chainID, 0, &recipient, big.NewInt(5), nil, 21_000),
		signLegacyTxWithGas(t, key, chainID, 1, &recipient, big.NewInt(6), nil, 21_000),
	}})
	require.NoError(t, err)

	require.Equal(t, uint64(42_000), result.GasUsed)
	require.Equal(t, uint64(42_000), result.Receipts[1].CumulativeGasUsed)
	fees := big.NewInt(42_000 * testGasPriceWei)
	want := StateChangeSet{
		Balances: []BalanceChange{
			{Address: recipient, Balance: big.NewInt(11), PriorBalance: big.NewInt(0)},
			{Address: coinbase, Balance: fees, PriorBalance: big.NewInt(0)},
			{Address: sender, Balance: new(big.Int).Sub(funded, new(big.Int).Add(fees, big.NewInt(11))), PriorBalance: funded},
		},
		Nonces: []NonceChange{{Address: sender, Nonce: 2, PriorNonce: 0}},
	}
	require.Equal(t, withCanonicalBalances(want), withCanonicalBalances(result.ChangeSet))
}

func TestMockExecutionRejectsAStaleNonceWithoutChangingState(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA("45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8")
	require.NoError(t, err)
	recipient := testAddress(0xa1)

	state := NewMemoryState()
	state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(testFundedBalanceWei))
	executor := NewExecutor(Config{MockExecution: true, RejectUnappliableTxs: true}, withTestState(state))
	defer executor.Close()
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: [][]byte{
		signLegacyTxWithGas(t, key, chainID, 3, &recipient, big.NewInt(5), nil, 21_000),
	}})
	require.NoError(t, err)

	require.True(t, result.Txs[0].Rejected)
	require.Equal(t, ethtypes.ReceiptStatusFailed, result.Receipts[0].Status)
	require.Zero(t, result.GasUsed)
	require.Empty(t, result.ChangeSet.Balances)
	require.Empty(t, result.ChangeSet.Nonces)
}

func TestParallelMockMatchesTheContractAndTheSerialMock(t *testing.T) {
	chainID := big.NewInt(testChainID)
	deployer, err := crypto.HexToECDSA("45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8")
	require.NoError(t, err)
	runtime := seiLoadERC20Runtime(t, chainID, deployer)
	token := testAddress(0xe2)
	senders := make([]*ecdsa.PrivateKey, 10)
	for i := range senders {
		senders[i], err = crypto.GenerateKey()
		require.NoError(t, err)
	}
	var txs [][]byte
	for n := range uint64(20) {
		for i, key := range senders {
			to := crypto.PubkeyToAddress(senders[(i+int(n)+1)%len(senders)].PublicKey)
			txs = append(txs, signLegacyTx(t, key, chainID, n, &token, big.NewInt(0), mockERC20TransferData(t, to, int64(n%3)+1)))
		}
	}
	run := func(cfg Config) *BlockResult {
		state := NewMemoryState()
		for _, key := range senders {
			state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(100*testFundedBalanceWei))
		}
		state.SetCode(token, runtime)
		executor := NewExecutor(cfg, withTestState(state))
		t.Cleanup(executor.Close)
		ctx := blockContext(chainID)
		ctx.GasLimit = 300_000_000
		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: ctx, Txs: txs})
		require.NoError(t, err)
		require.Len(t, result.Txs, len(txs))
		return result
	}
	real := run(Config{})
	serial := run(Config{MockExecution: true})
	parallel := run(Config{MockExecution: true, OCCWorkers: 8})

	require.NotEmpty(t, real.ChangeSet.Storage)
	require.Equal(t, real.ChangeSet.Storage, parallel.ChangeSet.Storage)
	require.Equal(t, real.ChangeSet.Nonces, parallel.ChangeSet.Nonces)
	require.Equal(t, serial.ChangeSet, parallel.ChangeSet)
	for i := range real.Receipts {
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, parallel.Receipts[i].Status, "tx %d", i)
		require.Equal(t, serial.Receipts[i].Bloom, parallel.Receipts[i].Bloom, "tx %d", i)
		require.Equal(t, real.Receipts[i].Bloom, parallel.Receipts[i].Bloom, "tx %d", i)
		require.Len(t, parallel.Receipts[i].Logs, len(real.Receipts[i].Logs), "tx %d", i)
		for j, want := range real.Receipts[i].Logs {
			got := parallel.Receipts[i].Logs[j]
			require.Equal(t, want.Topics, got.Topics)
			require.Equal(t, want.Data, got.Data)
			require.Equal(t, want.Index, got.Index)
		}
	}
}
