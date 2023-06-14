package tests

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/testutil/processblock/verify"
)

func TestPlaceOrders(t *testing.T) {
	app := processblock.NewTestApp()
	p := processblock.DexPreset(app, 3, 2)
	p.DoRegisterMarkets(app)

	for _, testCase := range []TestCase{
		{
			description: "send a single market buy without counterparty on orderbook",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[0], 10000, p.AllDexMarkets[0].LongMarketOrder(p.SignableAccounts[0], "11", "2")),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "send a single market sell without counterparty on orderbook",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[0], 10000, p.AllDexMarkets[0].ShortMarketOrder(p.SignableAccounts[0], "10.5", "4")),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "send a single buy limit order",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[0], 10000, p.AllDexMarkets[0].LongLimitOrder(p.SignableAccounts[0], "10.5", "5")),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "send a single sell limit order",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[1], 10000, p.AllDexMarkets[0].ShortLimitOrder(p.SignableAccounts[1], "11", "3")),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "send a single market buy without exhausting the orderbook",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[2], 10000, p.AllDexMarkets[0].LongMarketOrder(p.SignableAccounts[2], "11", "2")),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "send a single market sell without exhausting the orderbook",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[2], 10000, p.AllDexMarkets[0].ShortMarketOrder(p.SignableAccounts[2], "10.5", "4")),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "send a single market buy exhausting the orderbook",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[2], 10000, p.AllDexMarkets[0].LongMarketOrder(p.SignableAccounts[2], "12", "2")),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "send a single market sell exhausting the orderbook",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[2], 10000, p.AllDexMarkets[0].ShortMarketOrder(p.SignableAccounts[2], "10", "2")),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
	} {
		testCase.run(t, app)
	}
}

func TestPlaceImposterOrders(t *testing.T) {
	app := processblock.NewTestApp()
	p := processblock.DexPreset(app, 3, 2)
	p.DoRegisterMarkets(app)

	for _, testCase := range []TestCase{
		{
			description: "send an order for someone else",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[0], 10000, p.AllDexMarkets[0].LongMarketOrder(p.SignableAccounts[1], "11", "2")),
			},
			verifier:      []verify.Verifier{},
			expectedCodes: []uint32{8},
		},
	} {
		testCase.run(t, app)
	}
}
