package report

import (
	"encoding/csv"
	"fmt"
	"regexp"

	"github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (s *csvService) exportAccountAssetsCSV(ctx sdk.Context) error {
	file, writer := s.openCSVFile("account_asset.csv")
	defer file.Close()
	defer writer.Flush()

	// Write CSV header
	if err := writer.Write([]string{"account_id", "asset_id", "balance", "token_id"}); err != nil {
		return err
	}

	// Export coin balances
	if err := s.exportCoinBalancesCSV(ctx, writer); err != nil {
		return err
	}

	// Export CW20 token balances
	if err := s.exportCW20BalancesCSV(ctx, writer); err != nil {
		return err
	}

	// Export CW721 token ownership
	return s.exportCW721OwnersCSV(ctx, writer)
}

func (s *csvService) exportCoinBalancesCSV(ctx sdk.Context, writer *csv.Writer) error {
	balances := s.bk.GetAccountsBalances(ctx)

	for _, b := range balances {
		for _, c := range b.Coins {
			key := fmt.Sprintf("%s-%s", b.Address, c.Denom)

			s.mu.Lock()
			if s.seenBalances[key] {
				s.mu.Unlock()
				continue
			}
			s.seenBalances[key] = true
			s.mu.Unlock()

			row := []string{
				b.Address, // account_id (will be resolved to actual ID in PostgreSQL)
				c.Denom,   // asset_id (will be resolved to actual ID in PostgreSQL)
				c.Amount.String(),
				"", // empty token_id for fungible tokens
			}

			if err := writer.Write(row); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *csvService) exportCW20BalancesCSV(ctx sdk.Context, writer *csv.Writer) error {
	badPayload := []byte(`{"invalid": "payload"}`)
	errorMatch := regexp.MustCompile(`^.*type\s+([a-zA-Z0-9_]+)::`)

	s.wk.IterateCodeInfos(ctx, func(codeID uint64, codeInfo types.CodeInfo) bool {
		first := true
		var contractType string
		var isToken bool
		s.wk.IterateContractsByCode(ctx, codeID, func(addr sdk.AccAddress) bool {
			if first {
				contractType, isToken = s.extractType(addr, ctx, badPayload, errorMatch)
				if !isToken || contractType != "cw20" {
					return true
				}
				first = false
			}

			// Only process CW20 tokens
			if contractType == "cw20" {
				if err := s.writeCW20BalancesCSV(ctx, addr, addr.String(), writer); err != nil {
					ctx.Logger().Error("error exporting CW20 balances", "contract", addr.String(), "error", err)
				}
			}

			return false
		})
		return false
	})

	return nil
}

func (s *csvService) writeCW20BalancesCSV(ctx sdk.Context, addr sdk.AccAddress, tokenAddress string, writer *csv.Writer) error {
	var resp AccountsResponse
	var owners []string

	// Get all accounts
	if err := s.queryContract(addr, ctx, []byte(`{"all_accounts":{}}`), &resp); err != nil {
		return err
	}
	owners = append(owners, resp.Accounts...)

	// Handle pagination
	for len(resp.Accounts) > 0 {
		paginationKey := resp.Accounts[len(resp.Accounts)-1]
		query := []byte(fmt.Sprintf(`{"all_accounts":{"start_after":"%s"}}`, paginationKey))
		if err := s.queryContract(addr, ctx, query, &resp); err != nil {
			break
		}
		if len(resp.Accounts) == 0 {
			break
		}
		owners = append(owners, resp.Accounts...)
	}

	// Get balance for each owner
	for _, owner := range owners {
		var balanceResp BalanceResponse
		query := []byte(fmt.Sprintf(`{"balance":{"address":"%s"}}`, owner))
		if err := s.queryContract(addr, ctx, query, &balanceResp); err != nil {
			ctx.Logger().Error("error querying CW20 balance", "error", err, "owner", owner)
			continue
		}

		if balanceResp.Balance == "0" {
			continue // Skip zero balances
		}

		key := fmt.Sprintf("%s-%s", owner, tokenAddress)

		s.mu.Lock()
		if s.seenBalances[key] {
			s.mu.Unlock()
			continue
		}
		s.seenBalances[key] = true
		s.mu.Unlock()

		row := []string{
			owner,        // account_id
			tokenAddress, // asset_id
			balanceResp.Balance,
			"", // empty token_id for fungible tokens
		}

		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

func (s *csvService) exportCW721OwnersCSV(ctx sdk.Context, writer *csv.Writer) error {
	badPayload := []byte(`{"invalid": "payload"}`)
	errorMatch := regexp.MustCompile(`^.*type\s+([a-zA-Z0-9_]+)::`)

	s.wk.IterateCodeInfos(ctx, func(codeID uint64, codeInfo types.CodeInfo) bool {
		first := true
		var contractType string
		var isToken bool
		s.wk.IterateContractsByCode(ctx, codeID, func(addr sdk.AccAddress) bool {
			if first {
				contractType, isToken = s.extractType(addr, ctx, badPayload, errorMatch)
				if !isToken || contractType != "cw721" {
					return true
				}
				first = false
			}

			// Only process CW721 tokens
			if contractType == "cw721" {
				if err := s.writeCW721OwnersCSV(ctx, addr, addr.String(), writer); err != nil {
					ctx.Logger().Error("error exporting CW721 owners", "contract", addr.String(), "error", err)
				}
			}

			return false
		})
		return false
	})

	return nil
}

func (s *csvService) writeCW721OwnersCSV(ctx sdk.Context, addr sdk.AccAddress, tokenAddress string, writer *csv.Writer) error {
	var resp TokensResponse
	var tokens []string

	// Get all tokens
	if err := s.queryContract(addr, ctx, []byte(`{"all_tokens":{}}`), &resp); err != nil {
		return err
	}
	tokens = append(tokens, resp.Tokens...)

	// Handle pagination
	for len(resp.Tokens) > 0 {
		paginationKey := resp.Tokens[len(resp.Tokens)-1]
		query := []byte(fmt.Sprintf(`{"all_tokens":{ "start_after": "%s"}}`, paginationKey))
		if err := s.queryContract(addr, ctx, query, &resp); err != nil {
			break
		}
		if len(resp.Tokens) == 0 {
			break
		}
		tokens = append(tokens, resp.Tokens...)
	}

	// Get owner for each token
	for _, tokenID := range tokens {
		var ownerResp OwnerResponse
		query := []byte(fmt.Sprintf(`{"owner_of":{"token_id":"%s"}}`, tokenID))
		if err := s.queryContract(addr, ctx, query, &ownerResp); err != nil {
			ctx.Logger().Error("error querying CW721 owner", "error", err, "tokenId", tokenID)
			continue
		}

		key := fmt.Sprintf("%s-%s-%s", ownerResp.Owner, tokenAddress, tokenID)

		s.mu.Lock()
		if s.seenBalances[key] {
			s.mu.Unlock()
			continue
		}
		s.seenBalances[key] = true
		s.mu.Unlock()

		row := []string{
			ownerResp.Owner, // account_id
			tokenAddress,    // asset_id
			"",              // empty balance for NFTs
			tokenID,         // token_id for NFTs
		}

		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}
