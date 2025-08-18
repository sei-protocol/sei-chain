package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
)

// Simplified interfaces for testing
type BankKeeperI interface {
	GetAllBalances(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins
}

type AccountKeeperI interface {
	IterateAccounts(ctx sdk.Context, cb func(account authtypes.AccountI) (stop bool))
	GetAccount(ctx sdk.Context, addr sdk.AccAddress) authtypes.AccountI
}

type EVMKeeperI interface {
	GetEVMAddress(ctx sdk.Context, seiAddr sdk.AccAddress) (common.Address, bool)
	GetEVMAddressOrDefault(ctx sdk.Context, seiAddr sdk.AccAddress) common.Address
	GetNonce(ctx sdk.Context, addr common.Address) uint64
}

type WasmKeeperI interface {
	IterateCodeInfos(ctx sdk.Context, cb func(uint64, wasmtypes.CodeInfo) bool)
	IterateContractsByCode(ctx sdk.Context, codeID uint64, cb func(sdk.AccAddress) bool)
	GetContractInfo(ctx sdk.Context, addr sdk.AccAddress) *wasmtypes.ContractInfo
	QuerySmart(ctx sdk.Context, addr sdk.AccAddress, query []byte) ([]byte, error)
}

// Mock implementations
type MockBankKeeper struct {
	mock.Mock
}

func (m *MockBankKeeper) GetAllBalances(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins {
	args := m.Called(ctx, addr)
	return args.Get(0).(sdk.Coins)
}

type MockAccountKeeper struct {
	mock.Mock
}

func (m *MockAccountKeeper) IterateAccounts(ctx sdk.Context, cb func(account authtypes.AccountI) (stop bool)) {
	args := m.Called(ctx, cb)
	// The mock will call the callback with test accounts
	_ = args
}

func (m *MockAccountKeeper) GetAccount(ctx sdk.Context, addr sdk.AccAddress) authtypes.AccountI {
	args := m.Called(ctx, addr)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(authtypes.AccountI)
}

type MockEVMKeeper struct {
	mock.Mock
}

func (m *MockEVMKeeper) GetEVMAddress(ctx sdk.Context, seiAddr sdk.AccAddress) (common.Address, bool) {
	args := m.Called(ctx, seiAddr)
	return args.Get(0).(common.Address), args.Get(1).(bool)
}

func (m *MockEVMKeeper) GetEVMAddressOrDefault(ctx sdk.Context, seiAddr sdk.AccAddress) common.Address {
	args := m.Called(ctx, seiAddr)
	return args.Get(0).(common.Address)
}

func (m *MockEVMKeeper) GetNonce(ctx sdk.Context, addr common.Address) uint64 {
	args := m.Called(ctx, addr)
	return args.Get(0).(uint64)
}

type MockWasmKeeper struct {
	mock.Mock
}

func (m *MockWasmKeeper) IterateCodeInfos(ctx sdk.Context, cb func(uint64, wasmtypes.CodeInfo) bool) {
	args := m.Called(ctx, cb)
	_ = args
}

func (m *MockWasmKeeper) IterateContractsByCode(ctx sdk.Context, codeID uint64, cb func(sdk.AccAddress) bool) {
	args := m.Called(ctx, codeID, cb)
	_ = args
}

func (m *MockWasmKeeper) GetContractInfo(ctx sdk.Context, addr sdk.AccAddress) *wasmtypes.ContractInfo {
	args := m.Called(ctx, addr)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*wasmtypes.ContractInfo)
}

func (m *MockWasmKeeper) QuerySmart(ctx sdk.Context, addr sdk.AccAddress, query []byte) ([]byte, error) {
	args := m.Called(ctx, addr, query)
	return args.Get(0).([]byte), args.Error(1)
}

// Testable service that uses interfaces instead of concrete keepers
type testableService struct {
	bk BankKeeperI
	ak AccountKeeperI
	ek EVMKeeperI
	wk WasmKeeperI

	ctx       sdk.Context
	outputDir string
	status    string
}

func NewTestService(bk BankKeeperI, ak AccountKeeperI, ek EVMKeeperI, wk WasmKeeperI, outputDir string) *testableService {
	return &testableService{
		bk:        bk,
		ak:        ak,
		ek:        ek,
		wk:        wk,
		outputDir: outputDir,
		status:    "ready",
	}
}

func (s *testableService) setStatus(status string) {
	s.status = status
}

func (s *testableService) classifyAccount(addr string) string {
	if strings.Contains(addr, "bonding") || strings.HasPrefix(addr, "sei1fl48vsnmsdzcv85q5d2q4z5ajdha8yu3h6cprl") {
		return "bonding_pool"
	}
	if strings.Contains(addr, "burn") || strings.HasPrefix(addr, "sei1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqnrql8a") {
		return "burn_address"
	}
	if len(addr) == 62 {
		return "contract"
	}
	return "user"
}

func (s *testableService) getCW20Decimals(contractAddr sdk.AccAddress, ctx sdk.Context) string {
	var decimalsResp struct {
		Decimals int `json:"decimals"`
	}
	if err := s.queryContract(contractAddr, ctx, []byte(`{"decimals":{}}`), &decimalsResp); err != nil {
		return ""
	}
	return fmt.Sprintf("%d", decimalsResp.Decimals)
}

// getTokenLabel gets the display label for CW20/CW721 tokens (test version)
func (s *testableService) getTokenLabel(ctx sdk.Context, contractAddr sdk.AccAddress, tokenType string) string {
	if tokenType == "cw20" {
		var tokenInfo struct {
			Name string `json:"name"`
		}
		if err := s.queryContract(contractAddr, ctx, []byte(`{"token_info":{}}`), &tokenInfo); err == nil {
			return tokenInfo.Name
		}
	} else if tokenType == "cw721" {
		var contractInfoQuery struct {
			Name string `json:"name"`
		}
		if err := s.queryContract(contractAddr, ctx, []byte(`{"contract_info":{}}`), &contractInfoQuery); err == nil {
			return contractInfoQuery.Name
		}
	}
	return "" // Return empty if query fails
}

// getTokenPointer gets EVM pointer address for tokens (test version)
func (s *testableService) getTokenPointer(ctx sdk.Context, contractAddr sdk.AccAddress, tokenType string) string {
	// For test purposes, return empty string
	return ""
}

func (s *testableService) processCW20Balances(ctx sdk.Context, contractAddr sdk.AccAddress, balanceWriter *csv.Writer) {
	fmt.Printf("DEBUG Processing CW20 balances for contract: %s\n", contractAddr.String())

	var resp AccountsResponse
	var owners []string

	// Get all accounts
	if err := s.queryContract(contractAddr, ctx, []byte(`{"all_accounts":{}}`), &resp); err != nil {
		fmt.Printf("DEBUG Error querying all_accounts for CW20 %s: %v\n", contractAddr.String(), err)
		return
	}
	owners = append(owners, resp.Accounts...)
	fmt.Printf("DEBUG Found %d initial CW20 accounts for %s\n", len(resp.Accounts), contractAddr.String())

	// Handle pagination
	for len(resp.Accounts) > 0 {
		paginationKey := resp.Accounts[len(resp.Accounts)-1]
		query := []byte(fmt.Sprintf(`{"all_accounts":{"start_after":"%s"}}`, paginationKey))
		if err := s.queryContract(contractAddr, ctx, query, &resp); err != nil {
			fmt.Printf("DEBUG Pagination error for CW20 %s: %v\n", contractAddr.String(), err)
			break
		}
		if len(resp.Accounts) == 0 {
			break
		}
		owners = append(owners, resp.Accounts...)
		fmt.Printf("DEBUG Found %d more CW20 accounts (total: %d) for %s\n", len(resp.Accounts), len(owners), contractAddr.String())
	}

	fmt.Printf("DEBUG Processing CW20 contract %s with %d total accounts\n", contractAddr.String(), len(owners))

	// Get balance for each owner
	for _, owner := range owners {
		var balanceResp BalanceResponse
		query := []byte(fmt.Sprintf(`{"balance":{"address":"%s"}}`, owner))
		if err := s.queryContract(contractAddr, ctx, query, &balanceResp); err != nil {
			fmt.Printf("DEBUG Error querying CW20 balance for %s on %s: %v\n", owner, contractAddr.String(), err)
			continue
		}

		if balanceResp.Balance == "0" {
			continue // Skip zero balances
		}

		// Write balance to CSV
		balanceWriter.Write([]string{
			owner,                 // account_id
			contractAddr.String(), // asset_id
			balanceResp.Balance,   // balance
			"",                    // empty token_id for fungible tokens
		})

		fmt.Printf("DEBUG Writing CW20 balance: account=%s, asset=%s, balance=%s\n",
			owner, contractAddr.String(), balanceResp.Balance)
	}
}

func (s *testableService) processCW721Balances(ctx sdk.Context, contractAddr sdk.AccAddress, balanceWriter *csv.Writer) {
	fmt.Printf("DEBUG Processing CW721 balances for contract: %s\n", contractAddr.String())

	var resp TokensResponse
	var allTokens []string

	if err := s.queryContract(contractAddr, ctx, []byte(`{"all_tokens":{}}`), &resp); err != nil {
		fmt.Printf("DEBUG Error querying CW721 contract %s: %v\n", contractAddr.String(), err)
		return
	}

	allTokens = append(allTokens, resp.Tokens...)

	for len(resp.Tokens) > 0 {
		paginationKey := resp.Tokens[len(resp.Tokens)-1]
		query := []byte(fmt.Sprintf(`{"all_tokens":{"start_after":"%s"}}`, paginationKey))
		if err := s.queryContract(contractAddr, ctx, query, &resp); err != nil {
			fmt.Printf("DEBUG Error querying page of cw721 contract: %v, start_after: %s\n", err, paginationKey)
			break
		}
		allTokens = append(allTokens, resp.Tokens...)
	}

	fmt.Printf("DEBUG Processing CW721 contract %s with %d tokens\n", contractAddr.String(), len(allTokens))

	for _, tokenID := range allTokens {
		var ownerResp OwnerResponse
		query := []byte(fmt.Sprintf(`{"owner_of":{"token_id":"%s"}}`, tokenID))
		if err := s.queryContract(contractAddr, ctx, query, &ownerResp); err != nil {
			fmt.Printf("DEBUG Error querying owner for token %s: %v\n", tokenID, err)
			continue
		}

		fmt.Printf("DEBUG Writing CW721 balance: account=%s, asset=%s, balance=1, tokenID=%s\n", ownerResp.Owner, contractAddr.String(), tokenID)
		balanceWriter.Write([]string{
			ownerResp.Owner,
			contractAddr.String(),
			"1",
			tokenID,
		})
	}
}

func (s *testableService) queryContract(addr sdk.AccAddress, ctx sdk.Context, query []byte, target interface{}) error {
	result, err := s.wk.QuerySmart(ctx, addr, query)
	if err != nil {
		return err
	}
	return json.Unmarshal(result, target)
}

func (s *testableService) extractType(addr sdk.AccAddress, ctx sdk.Context, badPayload []byte) (string, bool) {
	_, err := s.wk.QuerySmart(ctx, addr, badPayload)
	if err != nil {
		return normalizeType(err.Error())
	}
	return "", false
}

// Copy the Start method from the main service but adapted for testable service
func (s *testableService) Start(ctx sdk.Context) error {
	s.setStatus("processing")
	s.ctx = ctx

	if err := os.MkdirAll(s.outputDir, 0755); err != nil && !os.IsExist(err) {
		return err
	}

	// Create CSV writers
	accountFile, err := os.Create(filepath.Join(s.outputDir, "accounts.csv"))
	if err != nil {
		return err
	}
	defer accountFile.Close()

	assetFile, err := os.Create(filepath.Join(s.outputDir, "assets.csv"))
	if err != nil {
		return err
	}
	defer assetFile.Close()

	balanceFile, err := os.Create(filepath.Join(s.outputDir, "account_asset.csv"))
	if err != nil {
		return err
	}
	defer balanceFile.Close()

	accountWriter := csv.NewWriter(accountFile)
	assetWriter := csv.NewWriter(assetFile)
	balanceWriter := csv.NewWriter(balanceFile)

	defer accountWriter.Flush()
	defer assetWriter.Flush()
	defer balanceWriter.Flush()

	// Write CSV headers
	accountWriter.Write([]string{"account_id", "evm_address", "nonce", "account_number", "sequence", "bucket"})
	assetWriter.Write([]string{"name", "type", "label", "code_id", "creator", "admin", "has_admin", "pointer", "decimals"})
	balanceWriter.Write([]string{"account_id", "asset_id", "balance", "token_id"})

	// Track unique assets to avoid duplicates
	seenAssets := make(map[string]bool)

	// Process accounts and their balances
	fmt.Printf("DEBUG Starting account iteration\n")
	s.ak.IterateAccounts(ctx, func(account authtypes.AccountI) bool {
		addr := account.GetAddress()

		// Write account data
		evmAddr := s.ek.GetEVMAddressOrDefault(ctx, addr)
		bucket := s.classifyAccount(addr.String())

		accountWriter.Write([]string{
			addr.String(),
			evmAddr.Hex(),
			fmt.Sprintf("%d", s.ek.GetNonce(ctx, evmAddr)),
			fmt.Sprintf("%d", account.GetAccountNumber()),
			fmt.Sprintf("%d", account.GetSequence()),
			bucket,
		})

		// Get and write balances for this account
		balances := s.bk.GetAllBalances(ctx, addr)
		for _, balance := range balances {
			// Add native asset if not seen
			if !seenAssets[balance.Denom] {
				assetWriter.Write([]string{
					balance.Denom, // name
					"native",      // type
					"",            // label (empty for native)
					"",            // code_id (empty for native)
					"",            // creator (empty for native)
					"",            // admin (empty for native)
					"false",       // has_admin (false for native)
					"",            // pointer (empty for native)
					"6",           // decimals (default for native)
				})
				seenAssets[balance.Denom] = true
				fmt.Printf("DEBUG Created native asset: %s\n", balance.Denom)
			}

			// Write balance
			balanceWriter.Write([]string{
				addr.String(),
				balance.Denom,
				balance.Amount.String(),
				"", // No token ID for native tokens
			})
		}

		return false // Continue iteration
	})

	// Process CW20 and CW721 contracts
	fmt.Printf("DEBUG Starting contract iteration\n")
	s.wk.IterateCodeInfos(ctx, func(codeID uint64, info wasmtypes.CodeInfo) bool {
		fmt.Printf("DEBUG CodeID: %d\n", codeID)

		badPayload := []byte(`{"bad_query":{}}`)
		s.wk.IterateContractsByCode(ctx, codeID, func(contractAddr sdk.AccAddress) bool {
			contractType, isToken := s.extractType(contractAddr, ctx, badPayload)
			if !isToken {
				return false // Continue to next contract
			}

			fmt.Printf("DEBUG Contract %s: type=%s, isToken=%t\n", contractAddr.String(), contractType, isToken)

			// Add contract asset if not seen
			if !seenAssets[contractAddr.String()] {
				// Get contract info for creator/admin (same as real service)
				contractInfo := s.wk.GetContractInfo(ctx, contractAddr)
				
				// Get token label (same as real service)
				label := s.getTokenLabel(ctx, contractAddr, contractType)
				
				// Get pointer address (same as real service)
				pointer := s.getTokenPointer(ctx, contractAddr, contractType)
				
				// Get decimals based on token type (same as real service)
				decimals := "0" // Default for CW721 (NFTs don't have decimals)
				if contractType == "cw20" {
					if dec := s.getCW20Decimals(contractAddr, ctx); dec != "" {
						decimals = dec
					} else {
						decimals = "6" // Default fallback for CW20
					}
				}
				
				// Determine admin info (same as real service)
				admin := ""
				hasAdmin := "false"
				if contractInfo != nil && contractInfo.Admin != "" {
					admin = contractInfo.Admin
					hasAdmin = "true"
				}
				
				creator := ""
				if contractInfo != nil {
					creator = contractInfo.Creator
				}
				
				assetWriter.Write([]string{
					contractAddr.String(),           // name
					contractType,                    // type
					label,                          // label
					fmt.Sprintf("%d", codeID),      // code_id
					creator,                        // creator
					admin,                          // admin
					hasAdmin,                       // has_admin
					pointer,                        // pointer
					decimals,                       // decimals
				})
				seenAssets[contractAddr.String()] = true
				fmt.Printf("DEBUG Created %s asset: %s\n", contractType, contractAddr.String())
			}

			// Process token balances based on type
			if contractType == "cw20" {
				s.processCW20Balances(ctx, contractAddr, balanceWriter)
			} else if contractType == "cw721" {
				s.processCW721Balances(ctx, contractAddr, balanceWriter)
			}

			return false // Continue to next contract
		})

		return false // Continue to next code ID
	})

	fmt.Printf("DEBUG Processing complete\n")
	s.setStatus("completed")
	return nil
}

func TestStreamingCSVService(t *testing.T) {
	// Test setup
	tempDir, err := os.MkdirTemp("", "csv_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Generate test data
	accounts := generateTestAccounts(50)
	denoms := []string{"usei", "uatom", "uosmo"}
	balances := generateTestBalances(accounts, denoms)

	// Create mocks
	mockBK := &MockBankKeeper{}
	mockAK := &MockAccountKeeper{}
	mockEK := &MockEVMKeeper{}
	mockWK := &MockWasmKeeper{}

	// Setup account keeper mock
	mockAK.On("IterateAccounts", mock.Anything, mock.AnythingOfType("func(types.AccountI) bool")).Run(func(args mock.Arguments) {
		cb := args.Get(1).(func(authtypes.AccountI) bool)
		for _, account := range accounts {
			if cb(account) {
				break
			}
		}
	})

	// Setup bank keeper mock
	for _, account := range accounts {
		accountBalances := balances[account.GetAddress().String()]
		mockBK.On("GetAllBalances", mock.Anything, account.GetAddress()).Return(accountBalances)
	}

	// Setup EVM keeper mocks
	for i, account := range accounts {
		evmAddr := common.HexToAddress(fmt.Sprintf("0x%040d", i))
		mockEK.On("GetEVMAddressOrDefault", mock.Anything, account.GetAddress()).Return(evmAddr)
		mockEK.On("GetNonce", mock.Anything, evmAddr).Return(uint64(0))
	}

	// Setup wasm keeper mocks for contract iteration (no contracts in this test)
	mockWK.On("IterateCodeInfos", mock.Anything, mock.AnythingOfType("func(uint64, types.CodeInfo) bool")).Return(nil)

	// Create test service
	svc := NewTestService(mockBK, mockAK, mockEK, mockWK, tempDir)

	// Run the service
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
	err = svc.Start(ctx)
	require.NoError(t, err)

	// Verify output files exist
	accountsFile := filepath.Join(tempDir, "accounts.csv")
	assetsFile := filepath.Join(tempDir, "assets.csv")
	balancesFile := filepath.Join(tempDir, "account_asset.csv")

	require.FileExists(t, accountsFile)
	require.FileExists(t, assetsFile)
	require.FileExists(t, balancesFile)

	// Verify accounts.csv
	accountRecords := readCSV(t, accountsFile)
	require.Len(t, accountRecords, 51) // 50 accounts + header
	require.Equal(t, []string{"account_id", "evm_address", "nonce", "account_number", "sequence", "bucket"}, accountRecords[0])

	// Verify we have all accounts
	accountAddrs := make([]string, 0, 50)
	for i := 1; i < len(accountRecords); i++ {
		accountAddrs = append(accountAddrs, accountRecords[i][0])
	}
	sort.Strings(accountAddrs)

	expectedAddrs := make([]string, 50)
	for i := 0; i < 50; i++ {
		expectedAddrs[i] = sdk.AccAddress(fmt.Sprintf("account%02d____________", i)).String()
	}
	sort.Strings(expectedAddrs)
	require.Equal(t, expectedAddrs, accountAddrs)

	// Verify assets.csv
	var assetsFileHandle *os.File
	assetsFileHandle, err = os.Open(assetsFile)
	require.NoError(t, err)
	defer assetsFileHandle.Close()

	assetsReader := csv.NewReader(assetsFileHandle)
	assetsRecords, err := assetsReader.ReadAll()
	require.NoError(t, err)

	// Check header
	expectedAssetsHeader := []string{"name", "type", "label", "code_id", "creator", "admin", "has_admin", "pointer", "decimals"}
	assert.Equal(t, expectedAssetsHeader, assetsRecords[0])

	// Should have 4 rows: header + 3 assets (usei, uatom, uosmo)
	assert.Equal(t, 4, len(assetsRecords))

	// Check that all expected assets are present with correct types
	assetNames := make(map[string]string)
	for i := 1; i < len(assetsRecords); i++ {
		record := assetsRecords[i]
		// New format: name, type, label, code_id, creator, admin, has_admin, pointer, decimals
		assetNames[record[0]] = record[1] // name -> type
	}

	assert.Equal(t, "native", assetNames["usei"])
	assert.Equal(t, "native", assetNames["uatom"])
	assert.Equal(t, "native", assetNames["uosmo"])

	// Verify balances.csv
	balanceRecords := readCSV(t, balancesFile)
	require.Greater(t, len(balanceRecords), 1) // At least header + some balances
	require.Equal(t, []string{"account_id", "asset_id", "balance", "token_id"}, balanceRecords[0])

	// Count expected balances
	expectedBalanceCount := 0
	for _, coins := range balances {
		expectedBalanceCount += len(coins)
	}
	require.Len(t, balanceRecords, expectedBalanceCount+1) // +1 for header

	// Verify balance data integrity
	balanceMap := make(map[string]map[string]string) // account -> denom -> balance
	for i := 1; i < len(balanceRecords); i++ {
		record := balanceRecords[i]
		account := record[0]
		denom := record[1]
		balance := record[2]

		if balanceMap[account] == nil {
			balanceMap[account] = make(map[string]string)
		}
		balanceMap[account][denom] = balance
	}

	// Verify balances match expected
	for accountStr, expectedCoins := range balances {
		actualBalances := balanceMap[accountStr]
		require.Len(t, actualBalances, len(expectedCoins))

		for _, coin := range expectedCoins {
			require.Equal(t, coin.Amount.String(), actualBalances[coin.Denom])
		}
	}

	// Verify all mocks were called as expected
	mockAK.AssertExpectations(t)
	mockBK.AssertExpectations(t)
	mockEK.AssertExpectations(t)
	mockWK.AssertExpectations(t)
}

func TestDetermineBucket(t *testing.T) {
	svc := &testableService{}

	tests := []struct {
		address  string
		expected string
	}{
		{"sei1bonding123", "bonding_pool"},
		{"sei1burn456", "burn_address"},
		{"sei12345678901234567890123456789012345678901234567890123456789", "contract"}, // exactly 62 chars
		{"sei1normaladdress", "user"},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			result := svc.classifyAccount(tt.address)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		isToken  bool
	}{
		{"cw20-base", "cw20", true},
		{"CW20-BASE", "cw20", true},
		{"  cw721-metadata-onchain  ", "cw721", true},
		{"cw1155", "cw1155", true},
		{"cw404", "cw404", true},
		{"some-other-contract", "some-other-contract", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, isToken := normalizeType(tt.input)
			require.Equal(t, tt.expected, result)
			require.Equal(t, tt.isToken, isToken)
		})
	}
}

func TestCW721TokenExportOnly(t *testing.T) {
	tempDir := t.TempDir()

	// Mock keepers
	mockBK := &MockBankKeeper{}
	mockAK := &MockAccountKeeper{}
	mockEK := &MockEVMKeeper{}
	mockWK := &MockWasmKeeper{}

	// Generate test accounts
	accounts := generateTestAccounts(2)
	owner1 := accounts[0].GetAddress()
	owner2 := accounts[1].GetAddress()

	// CW721 contract address only
	cw721Contract, _ := sdk.AccAddressFromBech32("sei1cw721contractaddress12345678901234567890123456789012345678")

	// Setup account keeper mock
	mockAK.On("IterateAccounts", mock.Anything, mock.AnythingOfType("func(types.AccountI) bool")).Run(func(args mock.Arguments) {
		cb := args.Get(1).(func(authtypes.AccountI) bool)
		for _, account := range accounts {
			if cb(account) {
				break
			}
		}
	})

	// Setup bank keeper mock - give each account some native tokens
	for _, account := range accounts {
		nativeBalances := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000)))
		mockBK.On("GetAllBalances", mock.Anything, account.GetAddress()).Return(nativeBalances)
	}

	// Setup EVM keeper mocks
	for i, account := range accounts {
		evmAddr := common.HexToAddress(fmt.Sprintf("0x%040d", i))
		mockEK.On("GetEVMAddressOrDefault", mock.Anything, account.GetAddress()).Return(evmAddr)
		mockEK.On("GetNonce", mock.Anything, evmAddr).Return(uint64(0))
	}

	// Mock wasm keeper for contract iteration - ONLY CW721 (code ID 1)
	mockWK.On("IterateCodeInfos", mock.Anything, mock.AnythingOfType("func(uint64, types.CodeInfo) bool")).Run(func(args mock.Arguments) {
		callback := args.Get(1).(func(uint64, wasmtypes.CodeInfo) bool)
		// Only Code ID 1 - CW721
		callback(1, wasmtypes.CodeInfo{})
	}).Return(nil)

	// Mock contract iteration for CW721 (code ID 1)
	mockWK.On("IterateContractsByCode", mock.Anything, uint64(1), mock.AnythingOfType("func(types.AccAddress) bool")).Run(func(args mock.Arguments) {
		callback := args.Get(2).(func(sdk.AccAddress) bool)
		callback(cw721Contract)
	}).Return(nil)

	// Mock CW721 GetContractInfo
	cw721ContractInfo := &wasmtypes.ContractInfo{
		CodeID:  1,
		Creator: "sei1creator789",
		Admin:   "sei1admin012",
	}
	mockWK.On("GetContractInfo", mock.Anything, cw721Contract).Return(cw721ContractInfo)

	// Mock CW721 contract_info query for label
	cw721ContractInfoQuery := []byte(`{"contract_info":{}}`)
	cw721ContractInfoResponse := `{"name":"Test CW721 Collection"}`
	mockWK.On("QuerySmart", mock.Anything, cw721Contract, cw721ContractInfoQuery).Return([]byte(cw721ContractInfoResponse), nil)

	// Mock CW721 contract type detection - should return CW721 error
	cw721BadQuery := []byte(`{"bad_query":{}}`)
	mockWK.On("QuerySmart", mock.Anything, cw721Contract, cw721BadQuery).Return([]byte{}, fmt.Errorf("Error parsing into type cw721_base::msg::QueryMsg: unknown variant `bad_query`, expected one of: owner_of, all_tokens, num_tokens, nft_info, all_nft_info, tokens, contract_info"))

	// Mock CW721 all_tokens query
	cw721AllTokensQuery := []byte(`{"all_tokens":{}}`)
	cw721AllTokensResponse := `{"tokens":["dragon_001","dragon_002"]}`
	mockWK.On("QuerySmart", mock.Anything, cw721Contract, cw721AllTokensQuery).Return([]byte(cw721AllTokensResponse), nil)

	// Mock CW721 owner_of queries
	cw721Owner1Query := []byte(`{"owner_of":{"token_id":"dragon_001"}}`)
	cw721Owner1Response := fmt.Sprintf(`{"owner":"%s"}`, owner1.String())
	mockWK.On("QuerySmart", mock.Anything, cw721Contract, cw721Owner1Query).Return([]byte(cw721Owner1Response), nil)

	cw721Owner2Query := []byte(`{"owner_of":{"token_id":"dragon_002"}}`)
	cw721Owner2Response := fmt.Sprintf(`{"owner":"%s"}`, owner2.String())
	mockWK.On("QuerySmart", mock.Anything, cw721Contract, cw721Owner2Query).Return([]byte(cw721Owner2Response), nil)

	// Mock CW721 pagination query (return empty to stop pagination)
	cw721PaginationQuery := []byte(`{"all_tokens":{"start_after":"dragon_002"}}`)
	cw721PaginationResponse := `{"tokens":[]}`
	mockWK.On("QuerySmart", mock.Anything, cw721Contract, cw721PaginationQuery).Return([]byte(cw721PaginationResponse), nil)

	// Create service using the testable service
	service := NewTestService(mockBK, mockAK, mockEK, mockWK, tempDir)

	// Run the service
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
	err := service.Start(ctx)
	require.NoError(t, err)

	// Verify output files exist
	accountFile := filepath.Join(tempDir, "accounts.csv")
	assetFile := filepath.Join(tempDir, "assets.csv")
	balanceFile := filepath.Join(tempDir, "account_asset.csv")

	require.FileExists(t, accountFile)
	require.FileExists(t, assetFile)
	require.FileExists(t, balanceFile)

	// Read and verify assets.csv
	assetRecords := readCSV(t, assetFile)
	assert.Len(t, assetRecords, 3) // Header + usei + CW721
	assert.Equal(t, []string{"name", "type", "label", "code_id", "creator", "admin", "has_admin", "pointer", "decimals"}, assetRecords[0])

	// Verify we have the expected asset types
	assetTypes := make(map[string]string)
	for _, record := range assetRecords[1:] {
		assetTypes[record[0]] = record[1]
	}
	assert.Equal(t, "native", assetTypes["usei"])
	assert.Equal(t, "cw721", assetTypes[cw721Contract.String()], "CW721 contract should be detected as cw721 type")

	// Read and verify account_asset.csv (balances)
	balanceRecords := readCSV(t, balanceFile)
	assert.Equal(t, []string{"account_id", "asset_id", "balance", "token_id"}, balanceRecords[0])

	// Count different balance types
	nativeBalanceCount := 0
	cw721BalanceCount := 0

	for _, record := range balanceRecords[1:] {
		switch record[1] {
		case "usei":
			nativeBalanceCount++
			assert.Equal(t, "1000000", record[2])
			assert.Equal(t, "", record[3]) // Empty token_id for native
		case cw721Contract.String():
			cw721BalanceCount++
			assert.Equal(t, "1", record[2])   // NFT balance is always 1
			assert.NotEqual(t, "", record[3]) // token_id should be populated for CW721
			// Verify token IDs are correct
			assert.Contains(t, []string{"dragon_001", "dragon_002"}, record[3])
		}
	}

	assert.Equal(t, 2, nativeBalanceCount, "Should have 2 native balance records")
	assert.Equal(t, 2, cw721BalanceCount, "Should have 2 CW721 balance records")

	// Verify all mocks were called as expected
	mockAK.AssertExpectations(t)
	mockBK.AssertExpectations(t)
	mockEK.AssertExpectations(t)
	mockWK.AssertExpectations(t)

	t.Log(" CW721-only token export test passed - CW721 detection and token ownership export works correctly!")
}

func TestCW20TokenExportOnly(t *testing.T) {
	tempDir := t.TempDir()

	// Mock keepers
	mockBK := &MockBankKeeper{}
	mockAK := &MockAccountKeeper{}
	mockEK := &MockEVMKeeper{}
	mockWK := &MockWasmKeeper{}

	// Generate test accounts
	accounts := generateTestAccounts(3)
	owner1 := accounts[0].GetAddress()
	owner2 := accounts[1].GetAddress()
	owner3 := accounts[2].GetAddress()

	// CW20 contract address only
	cw20Contract, _ := sdk.AccAddressFromBech32("sei1cw20contractaddress123456789012345678901234567890123456789")

	// Setup account keeper mock
	mockAK.On("IterateAccounts", mock.Anything, mock.AnythingOfType("func(types.AccountI) bool")).Run(func(args mock.Arguments) {
		cb := args.Get(1).(func(authtypes.AccountI) bool)
		for _, account := range accounts {
			if cb(account) {
				break
			}
		}
	})

	// Setup bank keeper mock - give each account some native tokens
	for _, account := range accounts {
		nativeBalances := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000)))
		mockBK.On("GetAllBalances", mock.Anything, account.GetAddress()).Return(nativeBalances)
	}

	// Setup EVM keeper mocks
	for i, account := range accounts {
		evmAddr := common.HexToAddress(fmt.Sprintf("0x%040d", i))
		mockEK.On("GetEVMAddressOrDefault", mock.Anything, account.GetAddress()).Return(evmAddr)
		mockEK.On("GetNonce", mock.Anything, evmAddr).Return(uint64(0))
	}

	// Mock wasm keeper for contract iteration - ONLY CW20 (code ID 1)
	mockWK.On("IterateCodeInfos", mock.Anything, mock.AnythingOfType("func(uint64, types.CodeInfo) bool")).Run(func(args mock.Arguments) {
		callback := args.Get(1).(func(uint64, wasmtypes.CodeInfo) bool)
		// Only Code ID 1 - CW20
		callback(1, wasmtypes.CodeInfo{})
	}).Return(nil)

	// Mock contract iteration for CW20 (code ID 1)
	mockWK.On("IterateContractsByCode", mock.Anything, uint64(1), mock.AnythingOfType("func(types.AccAddress) bool")).Run(func(args mock.Arguments) {
		callback := args.Get(2).(func(sdk.AccAddress) bool)
		callback(cw20Contract)
	}).Return(nil)

	// Mock CW20 contract type detection - should return CW20 error
	cw20BadQuery := []byte(`{"bad_query":{}}`)
	mockWK.On("QuerySmart", mock.Anything, cw20Contract, cw20BadQuery).Return([]byte{}, fmt.Errorf("Error parsing into type cw20_base::msg::QueryMsg: unknown variant `bad_query`"))

	// Mock GetContractInfo for enhanced asset generation
	contractInfo := &wasmtypes.ContractInfo{
		Creator: "sei1creator123",
		Admin:   "sei1admin456",
	}
	mockWK.On("GetContractInfo", mock.Anything, cw20Contract).Return(contractInfo)

	// Mock CW20 token_info query for label
	cw20TokenInfoQuery := []byte(`{"token_info":{}}`)
	cw20TokenInfoResponse := `{"name":"Test CW20 Token"}`
	mockWK.On("QuerySmart", mock.Anything, cw20Contract, cw20TokenInfoQuery).Return([]byte(cw20TokenInfoResponse), nil)

	// Mock CW20 decimals query
	cw20DecimalsQuery := []byte(`{"decimals":{}}`)
	cw20DecimalsResponse := `{"decimals":18}`
	mockWK.On("QuerySmart", mock.Anything, cw20Contract, cw20DecimalsQuery).Return([]byte(cw20DecimalsResponse), nil)

	// Mock CW20 all_accounts query - return 3 token holders
	cw20AllAccountsQuery := []byte(`{"all_accounts":{}}`)
	cw20AllAccountsResponse := fmt.Sprintf(`{"accounts":["%s","%s","%s"]}`, owner1.String(), owner2.String(), owner3.String())
	mockWK.On("QuerySmart", mock.Anything, cw20Contract, cw20AllAccountsQuery).Return([]byte(cw20AllAccountsResponse), nil)

	// Mock CW20 pagination query (return empty to stop pagination)
	cw20PaginationQuery := []byte(fmt.Sprintf(`{"all_accounts":{"start_after":"%s"}}`, owner3.String()))
	cw20PaginationResponse := `{"accounts":[]}`
	mockWK.On("QuerySmart", mock.Anything, cw20Contract, cw20PaginationQuery).Return([]byte(cw20PaginationResponse), nil)

	// Mock CW20 balance queries for each holder
	cw20Balance1Query := []byte(fmt.Sprintf(`{"balance":{"address":"%s"}}`, owner1.String()))
	cw20Balance1Response := `{"balance":"500000"}`
	mockWK.On("QuerySmart", mock.Anything, cw20Contract, cw20Balance1Query).Return([]byte(cw20Balance1Response), nil)

	cw20Balance2Query := []byte(fmt.Sprintf(`{"balance":{"address":"%s"}}`, owner2.String()))
	cw20Balance2Response := `{"balance":"300000"}`
	mockWK.On("QuerySmart", mock.Anything, cw20Contract, cw20Balance2Query).Return([]byte(cw20Balance2Response), nil)

	cw20Balance3Query := []byte(fmt.Sprintf(`{"balance":{"address":"%s"}}`, owner3.String()))
	cw20Balance3Response := `{"balance":"750000"}`
	mockWK.On("QuerySmart", mock.Anything, cw20Contract, cw20Balance3Query).Return([]byte(cw20Balance3Response), nil)

	// Create service using the testable service
	service := NewTestService(mockBK, mockAK, mockEK, mockWK, tempDir)

	// Run the service
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
	err := service.Start(ctx)
	require.NoError(t, err)

	// Verify output files exist
	accountFile := filepath.Join(tempDir, "accounts.csv")
	assetFile := filepath.Join(tempDir, "assets.csv")
	balanceFile := filepath.Join(tempDir, "account_asset.csv")

	require.FileExists(t, accountFile)
	require.FileExists(t, assetFile)
	require.FileExists(t, balanceFile)

	// Read and verify assets.csv
	assetRecords := readCSV(t, assetFile)
	assert.Len(t, assetRecords, 3) // Header + usei + CW20
	assert.Equal(t, []string{"name", "type", "label", "code_id", "creator", "admin", "has_admin", "pointer", "decimals"}, assetRecords[0])

	// Verify we have the expected asset types
	assetTypes := make(map[string]string)
	for _, record := range assetRecords[1:] {
		assetTypes[record[0]] = record[1]
	}
	assert.Equal(t, "native", assetTypes["usei"])
	assert.Equal(t, "cw20", assetTypes[cw20Contract.String()], "CW20 contract should be detected as cw20 type")

	// Read and verify account_asset.csv (balances)
	balanceRecords := readCSV(t, balanceFile)
	assert.Equal(t, []string{"account_id", "asset_id", "balance", "token_id"}, balanceRecords[0])

	// Count different balance types and verify amounts
	nativeBalanceCount := 0
	cw20BalanceCount := 0
	expectedCW20Balances := map[string]string{
		owner1.String(): "500000",
		owner2.String(): "300000",
		owner3.String(): "750000",
	}

	for _, record := range balanceRecords[1:] {
		switch record[1] {
		case "usei":
			nativeBalanceCount++
			assert.Equal(t, "1000000", record[2])
			assert.Equal(t, "", record[3]) // Empty token_id for native
		case cw20Contract.String():
			cw20BalanceCount++
			assert.Equal(t, "", record[3]) // Empty token_id for CW20

			// Verify the balance amount is correct for this account
			expectedBalance, exists := expectedCW20Balances[record[0]]
			assert.True(t, exists, "Account %s should be a CW20 token holder", record[0])
			assert.Equal(t, expectedBalance, record[2], "CW20 balance should match expected amount for account %s", record[0])

			t.Logf("Found CW20 balance: account=%s, contract=%s, balance=%s",
				record[0], record[1], record[2])
		}
	}

	assert.Equal(t, 3, nativeBalanceCount, "Should have 3 native balance records")
	assert.Equal(t, 3, cw20BalanceCount, "Should have 3 CW20 balance records")

	// Verify all mocks were called as expected
	mockAK.AssertExpectations(t)
	mockBK.AssertExpectations(t)
	mockEK.AssertExpectations(t)
	mockWK.AssertExpectations(t)

	t.Log("✅ CW20-only token export test passed - CW20 detection and balance export works correctly!")
}

func readCSV(t *testing.T, filename string) [][]string {
	file, err := os.Open(filename)
	require.NoError(t, err)
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	require.NoError(t, err)

	return records
}

func generateTestAccounts(count int) []authtypes.AccountI {
	accounts := make([]authtypes.AccountI, count)
	for i := 0; i < count; i++ {
		addr := sdk.AccAddress(fmt.Sprintf("account%02d____________", i))
		account := authtypes.NewBaseAccount(addr, nil, 0, uint64(i*10))
		accounts[i] = account
	}
	return accounts
}

func generateTestBalances(accounts []authtypes.AccountI, denoms []string) map[string]sdk.Coins {
	balances := make(map[string]sdk.Coins)

	for i, account := range accounts {
		coins := sdk.Coins{}
		for j, denom := range denoms {
			// Create varied balances: some accounts have all coins, some have subset
			if (i+j)%3 != 0 { // Skip some balances to create variety
				amount := sdk.NewInt(int64((i + 1) * (j + 1) * 1000))
				coins = coins.Add(sdk.NewCoin(denom, amount))
			}
		}
		balances[account.GetAddress().String()] = coins
	}

	return balances
}
