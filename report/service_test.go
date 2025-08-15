package report

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"golang.org/x/sync/errgroup"

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
	GetERC20NativePointer(ctx sdk.Context, denom string) (common.Address, uint16, bool)
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

func (m *MockEVMKeeper) GetERC20NativePointer(ctx sdk.Context, denom string) (common.Address, uint16, bool) {
	args := m.Called(ctx, denom)
	return args.Get(0).(common.Address), args.Get(1).(uint16), args.Get(2).(bool)
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

// Test service with interfaces
type testService struct {
	bk BankKeeperI
	ak AccountKeeperI
	ek EVMKeeperI
	wk WasmKeeperI

	ctx       sdk.Context
	outputDir string
	status    string

	// CSV streaming channels
	accountWorkChan chan *AccountWork
	accountDataChan chan *AccountData
	assetDataChan   chan *AssetData
	balanceDataChan chan *BalanceData

	// Deduplication maps
	seenAssets map[string]bool
	mu         sync.Mutex
}

// Copy the service methods but use interfaces
func (s *testService) Start(ctx sdk.Context) error {
	s.setStatus("processing")
	s.ctx = ctx

	grp, gctx := errgroup.WithContext(ctx.Context())
	ctx = ctx.WithContext(gctx)

	// Start all workers
	grp.Go(func() error {
		return s.iteratorWorker(ctx)
	})

	grp.Go(func() error {
		return s.accountWorker(ctx)
	})

	grp.Go(func() error {
		return s.assetWorker(ctx)
	})

	grp.Go(func() error {
		return s.accountWriter(ctx)
	})

	grp.Go(func() error {
		return s.assetWriter(ctx)
	})

	grp.Go(func() error {
		return s.balanceWriter(ctx)
	})

	err := grp.Wait()

	if err != nil {
		s.setStatus(err.Error())
	} else {
		s.setStatus("success")
	}

	return err
}

func (s *testService) setStatus(status string) {
	s.status = status
}

// Copy all the worker methods from the main service
func (s *testService) iteratorWorker(ctx sdk.Context) error {
	defer close(s.accountWorkChan)

	s.ak.IterateAccounts(ctx, func(account authtypes.AccountI) (stop bool) {
		select {
		case s.accountWorkChan <- &AccountWork{Account: account.GetAddress()}:
		case <-ctx.Context().Done():
			return true
		}
		return false
	})
	return nil
}

func (s *testService) accountWorker(ctx sdk.Context) error {
	defer close(s.accountDataChan)
	defer close(s.assetDataChan)
	defer close(s.balanceDataChan)

	for accountWork := range s.accountWorkChan {
		select {
		case <-ctx.Context().Done():
			return ctx.Context().Err()
		default:
		}

		seiAddr := accountWork.Account
		seiAddrStr := seiAddr.String()

		// Get EVM association
		evmAddr, associated := s.ek.GetEVMAddress(ctx, seiAddr)
		if !associated {
			evmAddr = s.ek.GetEVMAddressOrDefault(ctx, seiAddr)
		}

		evmNonce := s.ek.GetNonce(ctx, evmAddr)

		// Get account info
		account := s.ak.GetAccount(ctx, seiAddr)
		sequence := uint64(0)
		if account != nil {
			sequence = account.GetSequence()
		}

		accountData := &AccountData{
			Account:    seiAddrStr,
			EVMAddress: evmAddr.Hex(),
			EVMNonce:   evmNonce,
			Sequence:   sequence,
			Associated: associated,
			Bucket:     s.determineBucket(seiAddrStr),
		}

		select {
		case s.accountDataChan <- accountData:
		case <-ctx.Context().Done():
			return ctx.Context().Err()
		}

		// Get balances for this account
		balances := s.bk.GetAllBalances(ctx, seiAddr)

		for _, coin := range balances {
			// Send asset data if we haven't seen this denom before
			s.mu.Lock()
			if !s.seenAssets[coin.Denom] {
				s.seenAssets[coin.Denom] = true
				s.mu.Unlock()

				coinData := s.getCoinByDenom(ctx, coin.Denom)
				assetData := &AssetData{
					Name:       coinData.Denom,
					Type:       "native",
					Label:      "",
					CodeID:     0,
					Creator:    "",
					Admin:      "",
					HasAdmin:   false,
					HasPointer: coinData.HasPointer,
					Pointer:    coinData.Pointer,
				}

				select {
				case s.assetDataChan <- assetData:
				case <-ctx.Context().Done():
					return ctx.Context().Err()
				}
			} else {
				s.mu.Unlock()
			}

			balanceData := &BalanceData{
				AccountID: seiAddr.String(),
				AssetID:   coin.Denom,
				Balance:   coin.Amount.String(),
				TokenID:   "",
			}

			select {
			case s.balanceDataChan <- balanceData:
			case <-ctx.Context().Done():
				return ctx.Context().Err()
			}
		}
	}
	return nil
}

func (s *testService) assetWorker(ctx sdk.Context) error {
	// For testing, we don't close the channel here since accountWorker sends to it
	// The channel will be closed when accountWorker finishes
	return nil
}

func (s *testService) accountWriter(ctx sdk.Context) error {
	file, err := os.Create(fmt.Sprintf("%s/accounts.csv", s.outputDir))
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(bufio.NewWriterSize(file, 64*1024))

	// Write header
	err = writer.Write([]string{"account", "evm_address", "evm_nonce", "sequence", "associated", "bucket"})
	if err != nil {
		return err
	}

	for accountData := range s.accountDataChan {
		err := writer.Write([]string{
			accountData.Account,
			accountData.EVMAddress,
			fmt.Sprintf("%d", accountData.EVMNonce),
			fmt.Sprintf("%d", accountData.Sequence),
			fmt.Sprintf("%t", accountData.Associated),
			accountData.Bucket,
		})
		if err != nil {
			return err
		}
	}

	writer.Flush()
	return writer.Error()
}

func (s *testService) assetWriter(ctx sdk.Context) error {
	file, err := os.Create(fmt.Sprintf("%s/assets.csv", s.outputDir))
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(bufio.NewWriterSize(file, 64*1024))

	// Write header
	err = writer.Write([]string{"name", "type", "label", "code_id", "creator", "admin", "has_admin", "has_pointer", "pointer"})
	if err != nil {
		return err
	}

	for assetData := range s.assetDataChan {
		err := writer.Write([]string{
			assetData.Name,
			assetData.Type,
			assetData.Label,
			fmt.Sprintf("%d", assetData.CodeID),
			assetData.Creator,
			assetData.Admin,
			fmt.Sprintf("%t", assetData.HasAdmin),
			fmt.Sprintf("%t", assetData.HasPointer),
			assetData.Pointer,
		})
		if err != nil {
			return err
		}
	}

	writer.Flush()
	return writer.Error()
}

func (s *testService) balanceWriter(ctx sdk.Context) error {
	file, err := os.Create(fmt.Sprintf("%s/balances.csv", s.outputDir))
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(bufio.NewWriterSize(file, 64*1024))

	// Write header
	err = writer.Write([]string{"account_id", "asset_id", "balance", "token_id"})
	if err != nil {
		return err
	}

	for balanceData := range s.balanceDataChan {
		err := writer.Write([]string{
			balanceData.AccountID,
			balanceData.AssetID,
			balanceData.Balance,
			balanceData.TokenID,
		})
		if err != nil {
			return err
		}
	}

	writer.Flush()
	return writer.Error()
}

func (s *testService) determineBucket(address string) string {
	// Simple bucket classification
	if strings.Contains(address, "bonding") {
		return "bonding_pool"
	}
	if strings.Contains(address, "burn") {
		return "burn_address"
	}
	if len(address) == 62 {
		return "contract"
	}
	return "user"
}

func (s *testService) getCoinByDenom(ctx sdk.Context, denom string) *Coin {
	coin := &Coin{
		Denom: denom,
	}

	p, _, exists := s.ek.GetERC20NativePointer(ctx, coin.Denom)
	if exists {
		coin.HasPointer = true
		coin.Pointer = p.Hex()
	}

	return coin
}

// Test data generation
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

	// Setup account getter mock
	for _, account := range accounts {
		mockAK.On("GetAccount", mock.Anything, account.GetAddress()).Return(account)
	}

	// Setup bank keeper mock
	for _, account := range accounts {
		accountBalances := balances[account.GetAddress().String()]
		mockBK.On("GetAllBalances", mock.Anything, account.GetAddress()).Return(accountBalances)
	}

	// Setup EVM keeper mocks
	for i, account := range accounts {
		evmAddr := common.HexToAddress(fmt.Sprintf("0x%040d", i))
		associated := i%2 == 0 // Half are associated
		mockEK.On("GetEVMAddress", mock.Anything, account.GetAddress()).Return(evmAddr, associated)
		if !associated {
			mockEK.On("GetEVMAddressOrDefault", mock.Anything, account.GetAddress()).Return(evmAddr)
		}
		mockEK.On("GetNonce", mock.Anything, evmAddr).Return(uint64(i * 5))
	}

	// Setup EVM pointer mocks for denoms
	for i, denom := range denoms {
		if i == 0 { // First denom has pointer
			pointer := common.HexToAddress(fmt.Sprintf("0x%040d", 1000+i))
			mockEK.On("GetERC20NativePointer", mock.Anything, denom).Return(pointer, uint16(18), true)
		} else {
			mockEK.On("GetERC20NativePointer", mock.Anything, denom).Return(common.Address{}, uint16(0), false)
		}
	}

	// Create test service
	svc := &testService{
		bk:        mockBK,
		ak:        mockAK,
		ek:        mockEK,
		wk:        mockWK,
		outputDir: tempDir,
		status:    "ready",

		accountWorkChan: make(chan *AccountWork, 1000),
		accountDataChan: make(chan *AccountData, 1000),
		assetDataChan:   make(chan *AssetData, 1000),
		balanceDataChan: make(chan *BalanceData, 1000),

		seenAssets: make(map[string]bool),
	}

	// Run the service
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
	err = svc.Start(ctx)
	require.NoError(t, err)

	// Verify output files exist
	accountsFile := filepath.Join(tempDir, "accounts.csv")
	assetsFile := filepath.Join(tempDir, "assets.csv")
	balancesFile := filepath.Join(tempDir, "balances.csv")

	require.FileExists(t, accountsFile)
	require.FileExists(t, assetsFile)
	require.FileExists(t, balancesFile)

	// Verify accounts.csv
	accountRecords := readCSV(t, accountsFile)
	require.Len(t, accountRecords, 51) // 50 accounts + header
	require.Equal(t, []string{"account", "evm_address", "evm_nonce", "sequence", "associated", "bucket"}, accountRecords[0])

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
	assetRecords := readCSV(t, assetsFile)
	require.Len(t, assetRecords, 4) // 3 denoms + header
	require.Equal(t, []string{"name", "type", "label", "code_id", "creator", "admin", "has_admin", "has_pointer", "pointer"}, assetRecords[0])

	// Verify we have all denoms
	assetNames := make([]string, 0, 3)
	for i := 1; i < len(assetRecords); i++ {
		assetNames = append(assetNames, assetRecords[i][0])
	}
	sort.Strings(assetNames)
	sort.Strings(denoms)
	require.Equal(t, denoms, assetNames)

	// Verify first denom has pointer
	for i := 1; i < len(assetRecords); i++ {
		if assetRecords[i][0] == "usei" {
			require.Equal(t, "true", assetRecords[i][7]) // has_pointer
			require.NotEmpty(t, assetRecords[i][8])      // pointer address
		}
	}

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

func TestDetermineBucket(t *testing.T) {
	svc := &testService{}

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
			result := svc.determineBucket(tt.address)
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
