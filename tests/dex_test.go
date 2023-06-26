package tests

import (
	"fmt"
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
				verify.Balance,
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
				verify.Balance,
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
				verify.Balance,
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
				verify.Balance,
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
				verify.Balance,
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
				verify.Balance,
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
				verify.Balance,
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
				verify.Balance,
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

func TestCancelOrders(t *testing.T) {
	app := processblock.NewTestApp()
	p := processblock.DexPreset(app, 3, 2)
	p.DoRegisterMarkets(app)

	for _, testCase := range []TestCase{
		{
			description: "place a limit order",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[0], 10000, p.AllDexMarkets[0].LongLimitOrder(p.SignableAccounts[0], "11", "2")),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "cancel the previosuly placed limit order",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[0], 10000, p.AllDexMarkets[0].CancelLongOrder(p.SignableAccounts[0], "11", 0)),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "place two more limit orders at the same price",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[0], 10000, p.AllDexMarkets[0].LongLimitOrder(p.SignableAccounts[0], "11", "2")),
				app.Sign(p.SignableAccounts[1], 10000, p.AllDexMarkets[0].LongLimitOrder(p.SignableAccounts[1], "11", "1")),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0, 0},
		},
		{
			description: "cancel one of the orders on the same price",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[1], 10000, p.AllDexMarkets[0].CancelLongOrder(p.SignableAccounts[1], "11", 2)),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "place two more limit orders at different prices",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[0], 10000, p.AllDexMarkets[0].LongLimitOrder(p.SignableAccounts[0], "11", "2")),
				app.Sign(p.SignableAccounts[1], 10000, p.AllDexMarkets[0].LongLimitOrder(p.SignableAccounts[1], "12", "1")),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0, 0},
		},
		{
			description: "cancel one of the orders",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[1], 10000, p.AllDexMarkets[0].CancelLongOrder(p.SignableAccounts[1], "12", 4)),
			},
			verifier: []verify.Verifier{
				verify.DexOrders,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "cancel nonexistent order",
			input: []signing.Tx{
				app.Sign(p.SignableAccounts[1], 10000, p.AllDexMarkets[0].CancelLongOrder(p.SignableAccounts[1], "11", 5)),
			},
			verifier:      []verify.Verifier{}, // no change to order book
			expectedCodes: []uint32{0},         // tx itself would succeed
		},
	} {
		fmt.Println(testCase.description)
		testCase.run(t, app)
	}
}
