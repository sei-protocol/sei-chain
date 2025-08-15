package report

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"golang.org/x/sync/errgroup"
	"os"
	"regexp"
	"strings"
	"sync"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
)

// Main service implementation
type service struct {
	bk bankkeeper.Keeper
	ak *accountkeeper.AccountKeeper
	ek *evmkeeper.Keeper
	wk *wasmkeeper.Keeper

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

	// PostgreSQL config (optional)
	pgConfig *PostgreSQLConfig
}

// NewService creates a new report service
func NewService(
	bk bankkeeper.Keeper,
	ak *accountkeeper.AccountKeeper,
	ek *evmkeeper.Keeper,
	wk *wasmkeeper.Keeper,
	outputDir string) Service {

	return &service{
		bk:        bk,
		ak:        ak,
		ek:        ek,
		wk:        wk,
		outputDir: outputDir,
		status:    "ready",

		// Initialize channels
		accountWorkChan: make(chan *AccountWork, 1000),
		accountDataChan: make(chan *AccountData, 1000),
		assetDataChan:   make(chan *AssetData, 1000),
		balanceDataChan: make(chan *BalanceData, 1000),

		// Initialize deduplication
		seenAssets: make(map[string]bool),
	}
}

func (s *service) Name() string {
	return s.outputDir
}

func (s *service) Status() string {
	return s.status
}

func (s *service) setStatus(status string) {
	s.status = status
}

// Main entry point - starts the streaming CSV export
func (s *service) Start(ctx sdk.Context) error {
	s.setStatus("processing")
	s.ctx = ctx

	if err := os.MkdirAll(s.outputDir, 0755); err != nil && !os.IsExist(err) {
		return err
	}

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

// Worker 1: Iterator - streams accounts into work channel
func (s *service) iteratorWorker(ctx sdk.Context) error {
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

// Worker 2: Account processor - processes accounts and fetches balances
func (s *service) accountWorker(ctx sdk.Context) error {
	defer close(s.accountDataChan)
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
				AccountID: seiAddrStr,
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

// Worker 3: Asset discovery - finds CW20/CW721 tokens
func (s *service) assetWorker(ctx sdk.Context) error {
	defer close(s.assetDataChan)

	badPayload := []byte(`{"invalid_query":{}}`)
	errorMatch := CW20ErrorRegex

	s.wk.IterateCodeInfos(ctx, func(codeID uint64, codeInfo wasmtypes.CodeInfo) bool {
		select {
		case <-ctx.Context().Done():
			return true
		default:
		}

		var contractType string
		var isToken bool
		first := true

		s.wk.IterateContractsByCode(ctx, codeID, func(addr sdk.AccAddress) bool {
			if first {
				contractType, isToken = s.extractType(addr, ctx, badPayload, errorMatch)
				if !isToken {
					return true
				}
				first = false
			}

			contractInfo := s.wk.GetContractInfo(ctx, addr)

			s.mu.Lock()
			addrStr := addr.String()
			if !s.seenAssets[addrStr] {
				s.seenAssets[addrStr] = true
				s.mu.Unlock()

				assetData := &AssetData{
					Name:     addrStr,
					Type:     contractType,
					Label:    "",
					CodeID:   codeID,
					Creator:  contractInfo.Creator,
					Admin:    contractInfo.Admin,
					HasAdmin: contractInfo.Admin != "",
				}

				// Try to get token name
				if contractType == "cw20" {
					var tokenInfo struct {
						Name string `json:"name"`
					}
					if err := s.queryContract(addr, ctx, []byte(`{"token_info":{}}`), &tokenInfo); err == nil {
						assetData.Label = tokenInfo.Name
					}
				} else if contractType == "cw721" {
					var contractInfoQuery struct {
						Name string `json:"name"`
					}
					if err := s.queryContract(addr, ctx, []byte(`{"contract_info":{}}`), &contractInfoQuery); err == nil {
						assetData.Label = contractInfoQuery.Name
					}
				}

				select {
				case s.assetDataChan <- assetData:
				case <-ctx.Context().Done():
					return true
				}
			} else {
				s.mu.Unlock()
			}

			return false
		})
		return false
	})

	return nil
}

// CSV Writers
func (s *service) accountWriter(ctx sdk.Context) error {
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

func (s *service) assetWriter(ctx sdk.Context) error {
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

func (s *service) balanceWriter(ctx sdk.Context) error {
	file, err := os.Create(fmt.Sprintf("%s/account_asset.csv", s.outputDir))
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

// Helper functions
func (s *service) determineBucket(address string) string {
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

func (s *service) getCoinByDenom(ctx sdk.Context, denom string) *Coin {
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

func (s *service) queryContract(addr sdk.AccAddress, ctx sdk.Context, query []byte, target interface{}) error {
	res, err := s.wk.QuerySmart(ctx, addr, query)
	if err != nil {
		return err
	}
	return json.Unmarshal(res, target)
}

func (s *service) extractType(addr sdk.AccAddress, ctx sdk.Context, badPayload []byte, errorMatch *regexp.Regexp) (string, bool) {
	_, err := s.wk.QuerySmart(ctx, addr, badPayload)
	if err != nil && errorMatch.MatchString(err.Error()) {
		matches := errorMatch.FindStringSubmatch(err.Error())
		if len(matches) > 1 {
			return normalizeType(matches[1])
		}
	}
	return "", false
}

func normalizeType(s string) (string, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.Contains(s, "cw20") {
		return "cw20", true
	}
	if strings.Contains(s, "cw1155") {
		return "cw1155", true
	}
	if strings.Contains(s, "cw721") {
		return "cw721", true
	}
	if strings.Contains(s, "cw404") {
		return "cw404", true
	}
	return s, false
}
