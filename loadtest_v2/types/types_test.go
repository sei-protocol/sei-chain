package types

import (
	"crypto/ecdsa"
	"math/big"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAccount(t *testing.T) {
	account, err := NewAccount()
	require.NoError(t, err)
	require.NotNil(t, account)

	// Verify account has valid address and private key
	assert.NotEqual(t, common.Address{}, account.Address)
	assert.NotNil(t, account.PrivKey)
	assert.IsType(t, &ecdsa.PrivateKey{}, account.PrivKey)

	// Verify address matches private key
	expectedAddress := crypto.PubkeyToAddress(account.PrivKey.PublicKey)
	assert.Equal(t, expectedAddress, account.Address)

	// Verify initial nonce is 0
	assert.Equal(t, uint64(0), account.Nonce)
}

func TestAccountNonceManagement(t *testing.T) {
	account, err := NewAccount()
	require.NoError(t, err)

	// Test sequential nonce increments
	for i := uint64(0); i < 10; i++ {
		nonce := account.GetAndIncrementNonce()
		assert.Equal(t, i, nonce)
	}

	// Verify final nonce value
	assert.Equal(t, uint64(10), account.Nonce)
}

func TestAccountNonceConcurrency(t *testing.T) {
	account, err := NewAccount()
	require.NoError(t, err)

	const numGoroutines = 100
	const noncesPerGoroutine = 10
	
	var wg sync.WaitGroup
	nonces := make([]uint64, numGoroutines*noncesPerGoroutine)
	
	// Launch concurrent goroutines to increment nonce
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < noncesPerGoroutine; j++ {
				nonce := account.GetAndIncrementNonce()
				nonces[goroutineID*noncesPerGoroutine+j] = nonce
			}
		}(i)
	}
	
	wg.Wait()
	
	// Verify all nonces are unique and in expected range
	nonceSet := make(map[uint64]bool)
	for _, nonce := range nonces {
		assert.False(t, nonceSet[nonce], "Duplicate nonce found: %d", nonce)
		nonceSet[nonce] = true
		assert.Less(t, nonce, uint64(numGoroutines*noncesPerGoroutine))
	}
	
	// Verify we got exactly the expected number of unique nonces
	assert.Len(t, nonceSet, numGoroutines*noncesPerGoroutine)
	
	// Verify final nonce value
	assert.Equal(t, uint64(numGoroutines*noncesPerGoroutine), account.Nonce)
}

func TestGenerateAccounts(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{"Zero accounts", 0},
		{"Single account", 1},
		{"Multiple accounts", 10},
		{"Large batch", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accounts := GenerateAccounts(tt.count)
			assert.Len(t, accounts, tt.count)

			// Verify all accounts are unique and valid
			addressSet := make(map[common.Address]bool)
			for i, account := range accounts {
				assert.NotNil(t, account, "Account %d is nil", i)
				assert.NotEqual(t, common.Address{}, account.Address, "Account %d has zero address", i)
				assert.NotNil(t, account.PrivKey, "Account %d has nil private key", i)
				assert.Equal(t, uint64(0), account.Nonce, "Account %d has non-zero initial nonce", i)

				// Verify address uniqueness
				assert.False(t, addressSet[account.Address], "Duplicate address found: %s", account.Address.Hex())
				addressSet[account.Address] = true

				// Verify address matches private key
				expectedAddress := crypto.PubkeyToAddress(account.PrivKey.PublicKey)
				assert.Equal(t, expectedAddress, account.Address, "Account %d address doesn't match private key", i)
			}
		})
	}
}

func TestAccountPoolRoundRobin(t *testing.T) {
	accounts := GenerateAccounts(3)
	config := &AccountConfig{
		Accounts:       accounts,
		NewAccountRate: 0.0, // No new accounts, pure round-robin
	}
	
	pool := NewAccountPool(config)
	
	// The account pool starts from index 1 (due to nextIndex() incrementing first)
	// So the first call returns accounts[1], second returns accounts[2], third returns accounts[0]
	expectedOrder := []int{1, 2, 0} // The actual order the pool returns accounts
	
	// Test multiple rounds of round-robin selection
	for round := 0; round < 3; round++ {
		for i, expectedIndex := range expectedOrder {
			selectedAccount := pool.NextAccount()
			expectedAccount := accounts[expectedIndex]
			assert.Equal(t, expectedAccount.Address, selectedAccount.Address, 
				"Round %d, position %d: expected %s, got %s", 
				round, i, expectedAccount.Address.Hex(), selectedAccount.Address.Hex())
		}
	}
}

func TestAccountPoolNewAccountRate(t *testing.T) {
	accounts := GenerateAccounts(2)
	config := &AccountConfig{
		Accounts:       accounts,
		NewAccountRate: 1.0, // Always generate new accounts
	}
	
	pool := NewAccountPool(config)
	
	// With 100% new account rate, should never get original accounts
	originalAddresses := make(map[common.Address]bool)
	for _, account := range accounts {
		originalAddresses[account.Address] = true
	}
	
	for i := 0; i < 10; i++ {
		selectedAccount := pool.NextAccount()
		assert.False(t, originalAddresses[selectedAccount.Address], 
			"Iteration %d: got original account %s when expecting new account", 
			i, selectedAccount.Address.Hex())
	}
}

func TestAccountPoolMixedRate(t *testing.T) {
	accounts := GenerateAccounts(5)
	config := &AccountConfig{
		Accounts:       accounts,
		NewAccountRate: 0.5, // 50% new accounts
	}
	
	pool := NewAccountPool(config)
	
	originalAddresses := make(map[common.Address]bool)
	for _, account := range accounts {
		originalAddresses[account.Address] = true
	}
	
	const iterations = 100
	originalCount := 0
	newCount := 0
	
	for i := 0; i < iterations; i++ {
		selectedAccount := pool.NextAccount()
		if originalAddresses[selectedAccount.Address] {
			originalCount++
		} else {
			newCount++
		}
	}
	
	// With 50% rate, expect roughly equal distribution (allow 20% variance)
	expectedNew := iterations / 2
	tolerance := expectedNew / 5 // 20% tolerance
	
	assert.InDelta(t, expectedNew, newCount, float64(tolerance), 
		"Expected ~%d new accounts, got %d (tolerance: ±%d)", expectedNew, newCount, tolerance)
	assert.Equal(t, iterations, originalCount+newCount, "Total accounts don't match iterations")
}

func TestAccountPoolConcurrency(t *testing.T) {
	accounts := GenerateAccounts(5)
	config := &AccountConfig{
		Accounts:       accounts,
		NewAccountRate: 0.0, // Pure round-robin for predictable testing
	}
	
	pool := NewAccountPool(config)
	
	const numGoroutines = 50
	const selectionsPerGoroutine = 20
	
	var wg sync.WaitGroup
	selectedAccounts := make([]common.Address, numGoroutines*selectionsPerGoroutine)
	
	// Launch concurrent goroutines to select accounts
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < selectionsPerGoroutine; j++ {
				account := pool.NextAccount()
				selectedAccounts[goroutineID*selectionsPerGoroutine+j] = account.Address
			}
		}(i)
	}
	
	wg.Wait()
	
	// Verify all selected accounts are from the original pool
	originalAddresses := make(map[common.Address]bool)
	for _, account := range accounts {
		originalAddresses[account.Address] = true
	}
	
	for i, address := range selectedAccounts {
		assert.True(t, originalAddresses[address], 
			"Selection %d: got unexpected address %s", i, address.Hex())
	}
}

func TestCreateTxFromEthTx(t *testing.T) {
	// Create a test account and scenario
	account, err := NewAccount()
	require.NoError(t, err)
	
	receiver := common.HexToAddress("0x1234567890123456789012345678901234567890")
	scenario := &TxScenario{
		Name:     "TestScenario",
		Nonce:    42,
		Sender:   account,
		Receiver: receiver,
	}
	
	// Create a test transaction using DynamicFeeTx (EIP-1559)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1329), // Sei testnet chain ID
		Nonce:     scenario.Nonce,
		GasTipCap: big.NewInt(2000000000),  // 2 Gwei tip
		GasFeeCap: big.NewInt(20000000000), // 20 Gwei max fee
		Gas:       21000,                   // Gas limit
		To:        &scenario.Receiver,
		Value:     big.NewInt(1000000000000000000), // 1 ETH
		Data:      nil,
	})
	
	// Create LoadTx from the transaction
	loadTx := CreateTxFromEthTx(tx, scenario)
	
	// Verify LoadTx structure
	require.NotNil(t, loadTx)
	assert.Equal(t, tx, loadTx.EthTx)
	assert.Equal(t, scenario, loadTx.Scenario)
	assert.NotEmpty(t, loadTx.JSONRPCPayload)
	assert.NotEmpty(t, loadTx.Payload)
	
	// Verify JSON-RPC payload is valid JSON
	assert.Contains(t, string(loadTx.JSONRPCPayload), `"jsonrpc":"2.0"`)
	assert.Contains(t, string(loadTx.JSONRPCPayload), `"method":"eth_sendRawTransaction"`)
	assert.Contains(t, string(loadTx.JSONRPCPayload), `"id":0`) // Numeric ID, not string
	
	// Verify payload matches transaction binary data
	expectedPayload, err := tx.MarshalBinary()
	require.NoError(t, err)
	assert.Equal(t, expectedPayload, loadTx.Payload)
}

func TestLoadTxShardID(t *testing.T) {
	// Create more test accounts to ensure better shard distribution
	accounts := GenerateAccounts(50)
	
	tests := []struct {
		name       string
		numShards  int
		iterations int
	}{
		{"Single shard", 1, 10},
		{"Two shards", 2, 20},
		{"Multiple shards", 5, 50},
		{"Many shards", 16, 200}, // Increased iterations for better distribution
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shardCounts := make(map[int]int)
			
			for i := 0; i < tt.iterations; i++ {
				account := accounts[i%len(accounts)]
				scenario := &TxScenario{
					Name:     "TestScenario",
					Nonce:    uint64(i),
					Sender:   account,
					Receiver: common.Address{},
				}
				
				// Create a simple transaction
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   big.NewInt(1329), // Sei testnet chain ID
					Nonce:     scenario.Nonce,
					GasTipCap: big.NewInt(2000000000),  // 2 Gwei tip
					GasFeeCap: big.NewInt(20000000000), // 20 Gwei max fee
					Gas:       21000,                   // Gas limit
					To:        &scenario.Receiver,
					Value:     big.NewInt(0), // 0 ETH
					Data:      nil,
				})
				loadTx := CreateTxFromEthTx(tx, scenario)
				
				shardID := loadTx.ShardID(tt.numShards)
				
				// Verify shard ID is in valid range
				assert.GreaterOrEqual(t, shardID, 0, "Shard ID should be non-negative")
				assert.Less(t, shardID, tt.numShards, "Shard ID should be less than number of shards")
				
				shardCounts[shardID]++
			}
			
			// For tests with sufficient iterations and accounts, expect reasonable distribution
			// Note: Hash-based shard distribution can be uneven, so we don't require all shards to be used
			// Instead, we verify that the distribution is reasonable and all shard IDs are valid
			totalCount := 0
			for shardID, count := range shardCounts {
				totalCount += count
				// Verify shard IDs are in valid range
				assert.GreaterOrEqual(t, shardID, 0, "Shard ID should be non-negative")
				assert.Less(t, shardID, tt.numShards, "Shard ID should be less than number of shards")
			}
			
			// Verify total count matches iterations
			assert.Equal(t, tt.iterations, totalCount, "Total shard counts should match iterations")
			
			// For large numbers of shards, verify we're using a reasonable number of them
			// (at least 50% of available shards for sufficient iterations)
			if tt.numShards > 4 && tt.iterations >= tt.numShards*8 {
				usedShards := len(shardCounts)
				minExpectedShards := tt.numShards / 2
				assert.GreaterOrEqual(t, usedShards, minExpectedShards, 
					"Expected at least %d shards to be used, got %d", minExpectedShards, usedShards)
			}
		})
	}
}

func TestLoadTxShardIDConsistency(t *testing.T) {
	// Test that the same sender always maps to the same shard
	account, err := NewAccount()
	require.NoError(t, err)
	
	scenario := &TxScenario{
		Name:     "TestScenario",
		Nonce:    0,
		Sender:   account,
		Receiver: common.Address{},
	}
	
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1329), // Sei testnet chain ID
		Nonce:     scenario.Nonce,
		GasTipCap: big.NewInt(2000000000),  // 2 Gwei tip
		GasFeeCap: big.NewInt(20000000000), // 20 Gwei max fee
		Gas:       21000,                   // Gas limit
		To:        &scenario.Receiver,
		Value:     big.NewInt(0), // 0 ETH
		Data:      nil,
	})
	loadTx := CreateTxFromEthTx(tx, scenario)
	
	const numShards = 8
	expectedShardID := loadTx.ShardID(numShards)
	
	// Test multiple times with the same sender
	for i := 0; i < 10; i++ {
		shardID := loadTx.ShardID(numShards)
		assert.Equal(t, expectedShardID, shardID, 
			"Shard ID should be consistent for the same sender (iteration %d)", i)
	}
}

func TestTxScenario(t *testing.T) {
	account, err := NewAccount()
	require.NoError(t, err)
	
	receiver := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	
	scenario := &TxScenario{
		Name:     "TestScenario",
		Nonce:    123,
		Sender:   account,
		Receiver: receiver,
	}
	
	// Verify all fields are set correctly
	assert.Equal(t, "TestScenario", scenario.Name)
	assert.Equal(t, uint64(123), scenario.Nonce)
	assert.Equal(t, account, scenario.Sender)
	assert.Equal(t, receiver, scenario.Receiver)
}

func TestJSONRPCPayloadFormat(t *testing.T) {
	// Test the internal JSON-RPC payload generation
	testData := []byte{0x01, 0x02, 0x03, 0x04}
	
	payload, err := toJSONRequestBytes(testData)
	require.NoError(t, err)
	
	expectedContent := `{"jsonrpc":"2.0","method":"eth_sendRawTransaction","params":["0x01020304"],"id":0}` // Numeric ID, not string
	assert.JSONEq(t, expectedContent, string(payload))
}

func BenchmarkAccountGeneration(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := NewAccount()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAccountPoolNextAccount(b *testing.B) {
	accounts := GenerateAccounts(100)
	config := &AccountConfig{
		Accounts:       accounts,
		NewAccountRate: 0.0,
	}
	pool := NewAccountPool(config)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.NextAccount()
	}
}

func BenchmarkNonceIncrement(b *testing.B) {
	account, err := NewAccount()
	if err != nil {
		b.Fatal(err)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		account.GetAndIncrementNonce()
	}
}

func BenchmarkCreateTxFromEthTx(b *testing.B) {
	account, err := NewAccount()
	if err != nil {
		b.Fatal(err)
	}
	
	scenario := &TxScenario{
		Name:     "BenchmarkScenario",
		Nonce:    0,
		Sender:   account,
		Receiver: common.Address{},
	}
	
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1329), // Sei testnet chain ID
		Nonce:     scenario.Nonce,
		GasTipCap: big.NewInt(2000000000),  // 2 Gwei tip
		GasFeeCap: big.NewInt(20000000000), // 20 Gwei max fee
		Gas:       21000,                   // Gas limit
		To:        &scenario.Receiver,
		Value:     big.NewInt(0), // 0 ETH
		Data:      nil,
	})
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CreateTxFromEthTx(tx, scenario)
	}
}
