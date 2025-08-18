package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

	// Simple sequential processing - no complex workers or channels

	// 1. Create CSV writers
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

	// 2. Process accounts and their balances
	fmt.Printf("EXPORT Starting account iteration\n")
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
				// Get native coin pointer information
				pointer := s.getNativeCoinPointer(ctx, balance.Denom)
				assetWriter.Write([]string{
					balance.Denom, // name
					"native",      // type
					"",            // label (empty for native)
					"",            // code_id (empty for native)
					"",            // creator (empty for native)
					"",            // admin (empty for native)
					"false",       // has_admin (false for native)
					pointer,       // pointer address
					"6",           // decimals (default for native)
				})
				seenAssets[balance.Denom] = true
				fmt.Printf("EXPORT Created native asset: %s\n", balance.Denom)
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

	// 3. Process CW20 and CW721 contracts
	fmt.Printf("EXPORT Starting contract iteration\n")
	s.wk.IterateCodeInfos(ctx, func(codeID uint64, info wasmtypes.CodeInfo) bool {
		fmt.Printf("EXPORT CodeID: %d\n", codeID)

		badPayload := []byte(`{"bad_query":{}}`)
		s.wk.IterateContractsByCode(ctx, codeID, func(contractAddr sdk.AccAddress) bool {
			contractType, isToken := s.extractType(contractAddr, ctx, badPayload)
			if !isToken {
				return false // Continue to next contract
			}

			fmt.Printf("EXPORT Contract %s: type=%s, isToken=%t\n", contractAddr.String(), contractType, isToken)

			// Add contract asset if not seen
			if !seenAssets[contractAddr.String()] {
				// Get contract info for creator/admin
				contractInfo := s.wk.GetContractInfo(ctx, contractAddr)

				// Get token label
				label := s.getTokenLabel(ctx, contractAddr, contractType)

				// Get pointer address
				pointer := s.getTokenPointer(ctx, contractAddr, contractType)

				// Get decimals based on token type
				decimals := "0" // Default for CW721 (NFTs don't have decimals)
				if contractType == "cw20" {
					if dec := s.getCW20Decimals(contractAddr, ctx); dec != "" {
						decimals = dec
					} else {
						decimals = "6" // Default fallback for CW20
					}
				}

				// Determine admin info
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
					contractAddr.String(),     // name
					contractType,              // type
					label,                     // label
					fmt.Sprintf("%d", codeID), // code_id
					creator,                   // creator
					admin,                     // admin
					hasAdmin,                  // has_admin
					pointer,                   // pointer
					decimals,                  // decimals
				})
				seenAssets[contractAddr.String()] = true
				fmt.Printf("EXPORT Created %s asset: %s\n", contractType, contractAddr.String())
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

	fmt.Printf("EXPORT Processing complete\n")
	s.setStatus("completed")
	return nil
}

func (s *service) classifyAccount(addr string) string {
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

func (s *service) getCW20Decimals(contractAddr sdk.AccAddress, ctx sdk.Context) string {
	// Query CW20 for decimals
	var decimalsResp struct {
		Decimals int `json:"decimals"`
	}
	if err := s.queryContract(contractAddr, ctx, []byte(`{"decimals":{}}`), &decimalsResp); err != nil {
		return "" // Default fallback if query fails
	}
	return fmt.Sprintf("%d", decimalsResp.Decimals)
}

// getNativeCoinPointer gets pointer information for native coins
func (s *service) getNativeCoinPointer(ctx sdk.Context, denom string) string {
	np, _, exists := s.ek.GetERC20NativePointer(ctx, denom)
	if exists {
		fmt.Printf("EXPORT Getting native coin pointer for %s, exists=%v\n", denom, true)

		return np.Hex()
	}
	fmt.Printf("EXPORT Getting native coin pointer for %s, exists=%v\n", denom, false)
	return ""
}

// getTokenLabel gets the display label for CW20/CW721 tokens
func (s *service) getTokenLabel(ctx sdk.Context, contractAddr sdk.AccAddress, tokenType string) string {
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

// getTokenPointer gets EVM pointer address for tokens
func (s *service) getTokenPointer(ctx sdk.Context, contractAddr sdk.AccAddress, tokenType string) string {
	if tokenType == "cw20" {
		p, _, exists := s.ek.GetERC20CW20Pointer(ctx, contractAddr.String())
		if exists {
			return p.Hex()
		}
	} else if tokenType == "cw721" {
		p, _, exists := s.ek.GetERC721CW721Pointer(ctx, contractAddr.String())
		if exists {
			return p.Hex()
		}
	}
	return ""
}

func (s *service) processCW20Balances(ctx sdk.Context, contractAddr sdk.AccAddress, balanceWriter *csv.Writer) {
	fmt.Printf("EXPORT Processing CW20 balances for contract: %s\n", contractAddr.String())

	var resp AccountsResponse
	var owners []string

	// Get all accounts
	if err := s.queryContract(contractAddr, ctx, []byte(`{"all_accounts":{}}`), &resp); err != nil {
		fmt.Printf("EXPORT Error querying all_accounts for CW20 %s: %v\n", contractAddr.String(), err)
		return
	}
	owners = append(owners, resp.Accounts...)
	fmt.Printf("EXPORT Found %d initial CW20 accounts for %s\n", len(resp.Accounts), contractAddr.String())

	// Handle pagination
	for len(resp.Accounts) > 0 {
		paginationKey := resp.Accounts[len(resp.Accounts)-1]
		query := []byte(fmt.Sprintf(`{"all_accounts":{"start_after":"%s"}}`, paginationKey))
		if err := s.queryContract(contractAddr, ctx, query, &resp); err != nil {
			fmt.Printf("EXPORT Pagination error for CW20 %s: %v\n", contractAddr.String(), err)
			break
		}
		if len(resp.Accounts) == 0 {
			break
		}
		owners = append(owners, resp.Accounts...)
		fmt.Printf("EXPORT Found %d more CW20 accounts (total: %d) for %s\n", len(resp.Accounts), len(owners), contractAddr.String())
	}

	fmt.Printf("EXPORT Processing CW20 contract %s with %d total accounts\n", contractAddr.String(), len(owners))

	// Get balance for each owner
	for _, owner := range owners {
		var balanceResp BalanceResponse
		query := []byte(fmt.Sprintf(`{"balance":{"address":"%s"}}`, owner))
		if err := s.queryContract(contractAddr, ctx, query, &balanceResp); err != nil {
			fmt.Printf("EXPORT Error querying CW20 balance for %s on %s: %v\n", owner, contractAddr.String(), err)
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

		fmt.Printf("EXPORT Writing CW20 balance: account=%s, asset=%s, balance=%s\n",
			owner, contractAddr.String(), balanceResp.Balance)
	}
}

func (s *service) processCW721Balances(ctx sdk.Context, contractAddr sdk.AccAddress, balanceWriter *csv.Writer) {
	fmt.Printf("EXPORT Processing CW721 balances for contract: %s\n", contractAddr.String())

	// Query CW721 for all tokens using correct format
	var resp TokensResponse
	var allTokens []string

	// Query all tokens with pagination
	if err := s.queryContract(contractAddr, ctx, []byte(`{"all_tokens":{}}`), &resp); err != nil {
		fmt.Printf("EXPORT Error querying CW721 contract %s: %v\n", contractAddr.String(), err)
		return
	}

	allTokens = append(allTokens, resp.Tokens...)

	// Handle pagination
	for len(resp.Tokens) > 0 {
		paginationKey := resp.Tokens[len(resp.Tokens)-1]
		query := []byte(fmt.Sprintf(`{"all_tokens":{"start_after":"%s"}}`, paginationKey))
		if err := s.queryContract(contractAddr, ctx, query, &resp); err != nil {
			fmt.Printf("EXPORT Error querying page of cw721 contract: %v, start_after: %s\n", err, paginationKey)
			break
		}
		allTokens = append(allTokens, resp.Tokens...)
	}

	fmt.Printf("EXPORT Processing CW721 contract %s with %d tokens\n", contractAddr.String(), len(allTokens))

	// Query owner for each token and write balance records
	for _, tokenID := range allTokens {
		var ownerResp OwnerResponse
		query := []byte(fmt.Sprintf(`{"owner_of":{"token_id":"%s"}}`, tokenID))
		if err := s.queryContract(contractAddr, ctx, query, &ownerResp); err != nil {
			fmt.Printf("EXPORT Error querying owner for token %s: %v\n", tokenID, err)
			continue
		}

		fmt.Printf("EXPORT Writing CW721 balance: account=%s, asset=%s, balance=1, tokenID=%s\n", ownerResp.Owner, contractAddr.String(), tokenID)
		balanceWriter.Write([]string{
			ownerResp.Owner,
			contractAddr.String(),
			"1", // NFT balance is always 1
			tokenID,
		})
	}
}

func (s *service) extractType(addr sdk.AccAddress, ctx sdk.Context, badPayload []byte) (string, bool) {
	_, err := s.wk.QuerySmart(ctx, addr, badPayload)
	if err != nil {
		return normalizeType(err.Error())
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

func (s *service) queryContract(addr sdk.AccAddress, ctx sdk.Context, query []byte, target interface{}) error {
	result, err := s.wk.QuerySmart(ctx, addr, query)
	if err != nil {
		return err
	}
	return json.Unmarshal(result, target)
}
