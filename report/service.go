package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
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

// Hardcoded address mappings for account classification
var (
	gringottsAddresses = map[string]bool{
		"sei18qgau4n88tdaxu9y2t2y2px29yvwp50mk4xctp7grwfj7fkcdn8qvs9ry8": true,
		"sei19se8ass0qvpa2cc60ehnv5dtccznnn5m505cug5tg2gwsjqw5drqm5ptnx": true,
		"sei1letzrrlgdlrpxj6z279fx85hn5u34mm9nrc9hq4e6wxz5c79je2swt6x4a": true,
		"sei1w0fvamykx7v2e6n5x0e2s39m0jz3krejjkpmgc3tmnqdf8p9fy5syg05yv": true,
	}

	seiMultisigs = map[string]bool{
		"sei1xt3u4l0nzulhqxtcqhqdmgzt0p76vlwzr84t2g": true,
		"sei1vlrvsppftvaqlf4sy5muaea8jtgs2afn7xfr0w": true,
		"sei1prndl4f7hg6nsdavrlk6a26ea9a4q4780zjfgp": true,
		"sei1xhxnad3c86q3d8ggsyu24j7r0y5k3ef4zcxtc6": true,
		"sei1rufv5d36yrc57gjjs0gfur7ltj3jnhcs2lhz88": true,
		"sei19ey2jrj5qyd68sa4a34w6v6vgf6tar0zpv0cf8": true,
		"sei13u95lctpvwzmqy3thkczrhx3t4eczx7890xzky": true,
		"sei1sdwkgny20e7t5gv0533w4re5mukuusdkhmy433": true,
		"sei1y7xkz75wpgnazl47ttm72kj06wfp9u4du3ejqt": true,
		"sei1z64wl5hfdjwwadwcgf65lkze9mznydkw5j9heh": true,
		"sei15nz8xv0efg4mlaq26cue3u808ghdy29jyd0gaz": true,
		"sei1hrps2v9kl0kmhr0jdge0whfx3ulpfzlu6ptnk4": true,
	}

	burnAddress = map[string]bool{
		"sei1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq703fpu": true,
	}

	bondingPool = map[string]bool{
		"sei1fl48vsnmsdzcv85q5d2q4z5ajdha8yu3chcelk": true,
	}
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
	err = accountWriter.Write([]string{"account_id", "evm_address", "nonce", "account_number", "sequence", "bucket"})
	if err != nil {
		return err
	}
	err = assetWriter.Write([]string{"name", "type", "label", "code_id", "creator", "admin", "has_admin", "pointer", "decimals"})
	if err != nil {
		return err
	}
	err = balanceWriter.Write([]string{"account_id", "asset_id", "balance", "token_id"})
	if err != nil {
		return err
	}

	// Track unique assets to avoid duplicates
	seenAssets := make(map[string]bool)

	// Counter for progress tracking
	accountCount := 0

	// 2. Process accounts and their balances
	fmt.Printf("EXPORT Starting account iteration\n")
	var iterationError error
	s.ak.IterateAccounts(ctx, func(account authtypes.AccountI) bool {
		accountCount++
		defer func() {
			if r := recover(); r != nil {
				iterationError = fmt.Errorf("panic during account processing: %v", r)
				fmt.Printf("EXPORT PANIC during account processing: %v\n", r)
			}
		}()

		addr := account.GetAddress()

		// Write account data
		evmAddr, associated := s.ek.GetEVMAddress(ctx, addr)
		if !associated {
			evmAddr = s.ek.GetEVMAddressOrDefault(ctx, account.GetAddress())
		}

		var isMultiSig bool
		if baseAcct, ok := account.(*authtypes.BaseAccount); ok {
			_, multiOk := baseAcct.GetPubKey().(multisig.PubKey)
			isMultiSig = multiOk
		}

		// Detect contract types
		isCWContract := false
		isEVMContract := false
		evmNonce := s.ek.GetNonce(ctx, evmAddr)

		// Check if this is a CW contract by trying to get contract info
		if contractInfo := s.wk.GetContractInfo(ctx, addr); contractInfo != nil {
			isCWContract = true
		}

		// Check if this is an EVM contract by checking if it has code
		evmCode := s.ek.GetCode(ctx, evmAddr)
		if evmCode != nil && len(evmCode) > 0 {
			isEVMContract = true
		}

		bucket := ClassifyAccount(addr.String(), associated, isMultiSig, isCWContract, isEVMContract, evmNonce)

		err = accountWriter.Write([]string{
			addr.String(),
			evmAddr.Hex(),
			fmt.Sprintf("%d", evmNonce),
			fmt.Sprintf("%d", account.GetAccountNumber()),
			fmt.Sprintf("%d", account.GetSequence()),
			bucket,
		})
		if err != nil {
			iterationError = fmt.Errorf("failed to write account %s: %w", addr.String(), err)
			fmt.Printf("EXPORT Error writing account %s: %v\n", addr.String(), err)
			return false
		}

		// Get and write balances for this account
		balances := s.bk.GetAllBalances(ctx, addr)
		for _, balance := range balances {
			// Add native asset if not seen
			if !seenAssets[balance.Denom] {
				// Get native coin pointer information
				pointer := s.getNativeCoinPointer(ctx, balance.Denom)
				err = assetWriter.Write([]string{
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
				if err != nil {
					iterationError = fmt.Errorf("failed to write native asset %s: %w", balance.Denom, err)
					fmt.Printf("EXPORT Error writing native asset %s: %v\n", balance.Denom, err)
					return false
				}
				seenAssets[balance.Denom] = true
				fmt.Printf("EXPORT Created native asset: %s\n", balance.Denom)
			}

			// Write balance
			err = balanceWriter.Write([]string{
				addr.String(),
				balance.Denom,
				balance.Amount.String(),
				"", // No token ID for native tokens
			})
			if err != nil {
				iterationError = fmt.Errorf("failed to write balance for account %s, denom %s: %w", addr.String(), balance.Denom, err)
				fmt.Printf("EXPORT Error writing balance for account %s, denom %s: %v\n", addr.String(), balance.Denom, err)
				return false
			}
		}

		// Log progress periodically to help identify where process might stop
		if accountCount%10000 == 0 {
			fmt.Printf("EXPORT Progress: processed %d accounts so far\n", accountCount)
		}

		return false // Continue iteration
	})

	// Check for iteration errors
	if iterationError != nil {
		s.setStatus("failed")
		return fmt.Errorf("account iteration failed: %w", iterationError)
	}

	// Force flush all writers after account processing
	accountWriter.Flush()
	assetWriter.Flush()
	balanceWriter.Flush()
	fmt.Printf("EXPORT Completed account iteration, processed %d accounts. Flushed all writers.\n", accountCount)

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

				err = assetWriter.Write([]string{
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
				if err != nil {
					fmt.Printf("EXPORT Error writing contract asset: %v\n", err)
					return false
				}
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

// ClassifyAccount is a pure function that classifies an account into a bucket
// based on the provided parameters. It makes no external calls.
func ClassifyAccount(addr string, associated, multisig, isCWContract, isEVMContract bool, evmNonce uint64) string {
	switch {
	case bondingPool[addr]:
		return "bonding_pool"
	case burnAddress[addr]:
		return "burn_address"
	case seiMultisigs[addr]:
		return "sei_multisig"
	case multisig:
		return "multisig"
	case gringottsAddresses[addr]:
		return "gringotts"
	case isCWContract:
		return "cw_contract"
	case isEVMContract:
		return "evm_contract"
	case associated && evmNonce > 0:
		return "associated_evm"
	case associated && evmNonce == 0:
		return "associated_sei"
	case !associated:
		return "unassociated"
	default:
		return "unknown"
	}
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
		err := balanceWriter.Write([]string{
			ownerResp.Owner,
			contractAddr.String(),
			"1", // NFT balance is always 1
			tokenID,
		})
		if err != nil {
			panic("EXPORT Error writing CW721 balance: " + err.Error())
		}
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
