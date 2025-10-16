package report

import (
	"encoding/csv"
	"github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"regexp"
	"strconv"
)

func (s *csvService) exportAssetsCSV(ctx sdk.Context) error {
	file, writer := s.openCSVFile("assets.csv")
	defer file.Close()
	defer writer.Flush()

	// Write CSV header
	if err := writer.Write([]string{"name", "type", "label", "code_id", "creator", "admin", "has_admin", "pointer"}); err != nil {
		return err
	}

	// Export native coins first
	balances := s.bk.GetAccountsBalances(ctx)
	seenCoins := make(map[string]bool)

	for _, b := range balances {
		for _, c := range b.Coins {
			if seenCoins[c.Denom] {
				continue
			}
			seenCoins[c.Denom] = true

			coin := s.getCoinByDenom(ctx, c.Denom)
			row := []string{
				coin.Denom,
				"native",
				"",
				"",
				"",
				"",
				strconv.FormatBool(coin.HasPointer),
				coin.Pointer,
			}

			if err := writer.Write(row); err != nil {
				return err
			}
		}
	}

	// Export tokens (CW20/CW721)
	return s.exportTokensToCSV(ctx, writer)
}

func (s *csvService) exportTokensToCSV(ctx sdk.Context, writer *csv.Writer) error {
	pointerFunc := map[string]func(sdk.Context, string) (common.Address, uint16, bool){
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

			s.mu.Lock()
			if s.seenTokens[addr.String()] {
				s.mu.Unlock()
				return false
			}
			s.seenTokens[addr.String()] = true
			s.mu.Unlock()

			contractInfo := s.wk.GetContractInfo(ctx, addr)

			token := &Token{
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
					token.Pointer = pointer.Hex()
					token.HasPointer = true
				}
			}

			if contractType == "cw20" {
				var tokenInfo struct {
					Name string `json:"name"`
				}
				if err := s.queryContract(addr, ctx, []byte(`{"token_info":{}}`), &tokenInfo); err == nil {
					token.Label = tokenInfo.Name
				}
			} else if contractType == "cw721" {
				var contractInfoQuery struct {
					Name string `json:"name"`
				}
				if err := s.queryContract(addr, ctx, []byte(`{"contract_info":{}}`), &contractInfoQuery); err == nil {
					token.Label = contractInfoQuery.Name
				}
			}

			row := []string{
				token.Address,
				token.Type,
				token.Label,
				strconv.FormatUint(token.CodeID, 10),
				token.Creator,
				token.Admin,
				strconv.FormatBool(token.HasAdmin),
				token.Pointer,
			}

			if err := writer.Write(row); err != nil {
				ctx.Logger().Error("error writing token CSV row", "error", err)
			}

			return false
		})
		return false
	})

	return nil
}

func (s *csvService) extractType(addr sdk.AccAddress, ctx sdk.Context, badPayload []byte, errorMatch *regexp.Regexp) (string, bool) {
	_, err := s.wk.QuerySmart(ctx, addr, badPayload)
	if err != nil && errorMatch.MatchString(err.Error()) {
		matches := errorMatch.FindStringSubmatch(err.Error())
		if len(matches) > 1 {
			return normalizeType(matches[1])
		}
	}
	return "", false
}
