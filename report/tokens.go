package report

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Token struct {
	Address    string `json:"address"`
	Label      string `json:"label"`
	Type       string `json:"type"`
	CodeID     uint64 `json:"codeId"`
	Creator    string `json:"creator"`
	Admin      string `json:"admin"`
	HasAdmin   bool   `json:"hasAdmin"`
	Pointer    string `json:"pointer"`
	HasPointer bool   `json:"hasPointer"`
}

type ownerWriter func(sdk.Context, sdk.AccAddress, *Token, *os.File) error
type pointerLookup func(sdk.Context, string) (common.Address, uint16, bool)

type AccountsResponse struct {
	Accounts []string `json:"accounts"`
}

type TokensResponse struct {
	Tokens []string `json:"tokens"`
}

type OwnerResponse struct {
	Owner string `json:"owner"`
}

type BalanceResponse struct {
	Balance string `json:"balance"`
}

type TokenOwner struct {
	Owner   string `json:"address"`
	TokenID string `json:"tokenId"`
	Token   string `json:"token"`
	Balance string `json:"balance"`
}

func (s *service) queryContract(addr sdk.AccAddress, ctx sdk.Context, query []byte, target interface{}) error {
	res, err := s.wk.QuerySmart(ctx, addr, query)
	if err != nil {
		return err
	}
	return json.Unmarshal(res, target)
}

func (s *service) writeCW721Owners(ctx sdk.Context, addr sdk.AccAddress, t *Token, f *os.File) error {
	var resp TokensResponse
	if err := s.queryContract(addr, ctx, []byte(`{"all_tokens":{}}`), &resp); err != nil {
		ctx.Logger().Error("error querying cw721 contract", "error", err)
		return nil
	}
	for _, tokenID := range resp.Tokens {
		var ownerResp OwnerResponse
		query := []byte(fmt.Sprintf(`{"owner_of":{"token_id":"%s"}}`, tokenID))
		if err := s.queryContract(addr, ctx, query, &ownerResp); err != nil {
			ctx.Logger().Error("error querying cw721 contract", "error", err, "tokenId", tokenID)
			continue
		}
		if _, err := f.WriteString(jsonRow(&TokenOwner{
			Owner:   ownerResp.Owner,
			Token:   t.Address,
			TokenID: tokenID,
		})); err != nil {
			return err
		}
	}
	return nil
}

func (s *service) writeCW20Owners(ctx sdk.Context, addr sdk.AccAddress, t *Token, f *os.File) error {
	var resp AccountsResponse
	var owners []string

	// get first page
	if err := s.queryContract(addr, ctx, []byte(`{"all_tokens":{}}`), &resp); err != nil {
		return err
	}
	owners = append(owners, resp.Accounts...)

	// get rest of pages if any
	for len(resp.Accounts) > 0 {
		paginationKey := resp.Accounts[len(resp.Accounts)-1]
		query := []byte(fmt.Sprintf(`{"all_tokens":{ "start_after": "%s"}}`, paginationKey))
		if err := s.queryContract(addr, ctx, query, &resp); err != nil {
			return err
		}
		owners = append(owners, resp.Accounts...)
	}

	// collect balances
	for _, ownerAddress := range owners {
		var bal BalanceResponse
		query := []byte(fmt.Sprintf(`{"balance":{"address":"%s"}}`, ownerAddress))
		if err := s.queryContract(addr, ctx, query, &bal); err != nil {
			return err
		}
		if _, err := f.WriteString(jsonRow(&TokenOwner{
			Owner:   ownerAddress,
			Token:   t.Address,
			Balance: bal.Balance,
		})); err != nil {
			return err
		}
	}

	return nil
}

func (s *service) exportTokens(ctx sdk.Context) error {

	tokenFile := s.openFile("tokens.txt")
	cw20TokenOwners := s.openFile("owners_cw20.txt")
	cw721TokenOwners := s.openFile("owners_cw721.txt")
	cw404TokenOwners := s.openFile("owners_cw404.txt")
	cw1155TokenOwners := s.openFile("owners_cw1155.txt")

	defer func() {
		tokenFile.Close()
		cw20TokenOwners.Close()
		cw721TokenOwners.Close()
		cw404TokenOwners.Close()
		cw1155TokenOwners.Close()
	}()

	ownerFiles := map[string]*os.File{
		"cw20":   cw20TokenOwners,
		"cw721":  cw721TokenOwners,
		"cw404":  cw404TokenOwners,
		"cw1155": cw1155TokenOwners,
	}

	ownerWriters := map[string]ownerWriter{
		"cw20":  s.writeCW20Owners,
		"cw721": s.writeCW721Owners,
	}

	pointerFunc := map[string]pointerLookup{
		"cw20":   s.ek.GetERC20CW20Pointer,
		"cw721":  s.ek.GetERC721CW721Pointer,
		"cw1155": s.ek.GetERC1155CW1155Pointer,
	}

	badPayload := []byte(`{"invalid": "payload"}`)
	errorMatch := regexp.MustCompile(`^.*type\s+([a-zA-Z0-9_]+)::`)

	s.wk.IterateCodeInfos(ctx, func(codeID uint64, codeInfo types.CodeInfo) bool {
		first := true
		var contractType string
		var isToken bool
		s.wk.IterateContractsByCode(ctx, codeID, func(addr sdk.AccAddress) bool {
			if first {
				contractType, isToken = s.extractType(addr, ctx, badPayload, errorMatch)
				if !isToken {
					return true
				}
				first = false
			}

			contractInfo := s.wk.GetContractInfo(ctx, addr)
			t := &Token{
				Address:  addr.String(),
				Type:     contractType,
				CodeID:   codeID,
				Creator:  contractInfo.Creator,
				Admin:    contractInfo.Admin,
				HasAdmin: len(contractInfo.Admin) > 0,
			}
			if lookup, ok := pointerFunc[contractType]; ok {
				pointer, _, found := lookup(ctx, addr.String())
				if found {
					t.Pointer = pointer.Hex()
					t.HasPointer = true
				}
			}

			if _, ok := ownerWriters[contractType]; ok {
				if err := ownerWriters[contractType](ctx, addr, t, ownerFiles[contractType]); err != nil {
					ctx.Logger().Error("error writing owners", "error", err)
				}
			}

			if _, err := tokenFile.WriteString(jsonRow(t)); err != nil {
				ctx.Logger().Error("error writing token", "error", err)
			}

			return false
		})
		return false
	})
	return nil
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

func (s *service) extractType(addr sdk.AccAddress, ctx sdk.Context, badPayload []byte, errorMatch *regexp.Regexp) (string, bool) {
	_, err := s.wk.QuerySmart(ctx, addr, badPayload)
	match := errorMatch.FindStringSubmatch(err.Error())
	contractType := "unknown"
	if len(match) > 1 {
		contractType = match[1]
	}
	return normalizeType(contractType)
}
