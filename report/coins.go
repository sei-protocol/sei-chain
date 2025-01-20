package report

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Coin struct {
	Denom      string `json:"denom"`
	HasPointer bool   `json:"hasPointer"`
	Pointer    string `json:"pointer,omitempty"`
}

type CoinBalance struct {
	Account string `json:"account"`
	Denom   string `json:"denom"`
	Amount  string `json:"amount"`
}

func (s *service) exportCoins(ctx sdk.Context) error {
	coinsFile := s.openFile("coins.txt")
	defer coinsFile.Close()
	coinBalancesFile := s.openFile("coin_balances.txt")
	defer coinBalancesFile.Close()

	balances := s.bk.GetAccountsBalances(ctx)
	var uniq = make(map[string]*Coin)
	for _, b := range balances {
		for _, c := range b.Coins {
			coin, ok := uniq[c.Denom]
			if !ok {
				cbd := s.getCoinByDenom(ctx, c.Denom)
				uniq[c.Denom] = cbd
				coin = cbd

				if _, err := coinsFile.WriteString(jsonRow(coin)); err != nil {
					return err
				}
			}
			cb := &CoinBalance{
				Account: b.Address,
				Denom:   c.Denom,
				Amount:  c.Amount.String(),
			}
			if _, err := coinBalancesFile.WriteString(jsonRow(cb)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *service) getCoinByDenom(ctx sdk.Context, denom string) *Coin {
	c := &Coin{
		Denom: denom,
	}
	p, _, exists := s.ek.GetERC20NativePointer(ctx, c.Denom)
	if exists {
		c.HasPointer = true
		c.Pointer = p.Hex()
	}
	return c
}
