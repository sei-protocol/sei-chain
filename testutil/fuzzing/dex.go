package fuzzing

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const BaselinePrice = 1234.56

var ValidAccountCorpus = []string{
	"sei1h9yjz89tl0dl6zu65dpxcqnxfhq60wxx8s5kag",
	"sei1c2q6xm0x684rshrnlg898zm3vpwz92pcfhgmws",
	"sei1ewxvf5a9wq9zk5nurtl6m9yfxpnhyp7s7uk5sl",
	"sei1lllgxa294pshcsrsrteh7sj6ey0zqgty30sl8a",
	"sei1vhn2p3xavts9swus27zz3n56tz98g3f6unavs2",
	"sei1jpkqjfydghgrc23chmnj52xln0muz09j5huhkt",
	"sei1k98zjg7scsmk6d4ye8hhrv3an6ppykvt660736",
	"sei1wxpqjzdmtjm6gwg6555n0a0aqglrvnp3pqh9hs",
	"sei1yuyyr3xg7jhk7pjkrp4j6h88t7gv35e29pfvmf",
	"sei1vjgdad5v2euf98nj3pwg5d8agflr384k0eks43",
}

var AccountCorpus = append([]string{
	"invalid",
}, ValidAccountCorpus...)

var ContractCorpus = []string{
	"invalid",
	"sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
	"sei1nc5tatafv6eyq7llkr2gv50ff9e22mnf70qgjlv737ktmt4eswrqms7u8a",
}

var (
	MicroTick  = sdk.MustNewDecFromStr("0.000001")
	MilliTick  = sdk.MustNewDecFromStr("0.001")
	WholeTick  = sdk.OneDec()
	PairCorpus = []types.Pair{
		{},
		{PriceDenom: "SEI"},
		{AssetDenom: "ATOM"},
		{
			PriceDenom: "SEI",
			AssetDenom: "ATOM",
		},
		{
			PriceDenom:       "SEI",
			AssetDenom:       "ATOM",
			PriceTicksize:    &MicroTick,
			QuantityTicksize: &MicroTick,
		},
		{
			PriceDenom:       "SEI",
			AssetDenom:       "ATOM",
			PriceTicksize:    &MilliTick,
			QuantityTicksize: &MilliTick,
		},
		{
			PriceDenom:       "SEI",
			AssetDenom:       "ATOM",
			PriceTicksize:    &WholeTick,
			QuantityTicksize: &WholeTick,
		},
		{
			PriceDenom: "USDC",
			AssetDenom: "ATOM",
		},
		{
			PriceDenom:       "USDC",
			AssetDenom:       "ATOM",
			PriceTicksize:    &MicroTick,
			QuantityTicksize: &MicroTick,
		},
		{
			PriceDenom:       "USDC",
			AssetDenom:       "ATOM",
			PriceTicksize:    &MilliTick,
			QuantityTicksize: &MilliTick,
		},
		{
			PriceDenom:       "USDC",
			AssetDenom:       "ATOM",
			PriceTicksize:    &WholeTick,
			QuantityTicksize: &WholeTick,
		},
	}
)

func GetAccount(i int) string {
	ui := uint64(i) % uint64(len(AccountCorpus))
	return AccountCorpus[int(ui)]
}

func GetValidAccount(i int) string {
	ui := uint64(i) % uint64(len(ValidAccountCorpus))
	return ValidAccountCorpus[int(ui)]
}

func GetContract(i int) string {
	ui := uint64(i) % uint64(len(ContractCorpus))
	return ContractCorpus[int(ui)]
}

func GetPair(i int) types.Pair {
	ui := uint64(i) % uint64(len(PairCorpus))
	return PairCorpus[int(ui)]
}

func GetPlacedOrders(direction types.PositionDirection, orderType types.OrderType, pair types.Pair, prices []byte, quantities []byte) []*types.Order {
	// take the shorter slice's length
	if len(prices) < len(quantities) {
		quantities = quantities[:len(prices)]
	} else {
		prices = prices[:len(quantities)]
	}
	res := []*types.Order{}
	for i, price := range prices {
		var priceDec sdk.Dec
		if direction == types.PositionDirection_LONG {
			priceDec = sdk.MustNewDecFromStr(fmt.Sprintf("%f", BaselinePrice+float64(price)))
		} else {
			priceDec = sdk.MustNewDecFromStr(fmt.Sprintf("%f", BaselinePrice-float64(price)))
		}
		quantity := sdk.NewDec(int64(quantities[i]))
		res = append(res, &types.Order{
			Id:                uint64(i),
			Status:            types.OrderStatus_PLACED,
			Price:             priceDec,
			Quantity:          quantity,
			PositionDirection: direction,
			OrderType:         orderType,
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
		})
	}
	return res
}

func GetOrderBookEntries(buy bool, priceDenom string, assetDenom string, entryWeights []byte, allAccountIndices []byte, allWeights []byte) []types.OrderBookEntry {
	res := []types.OrderBookEntry{}
	totalPriceWeights := uint64(0)
	for _, entryWeight := range entryWeights {
		totalPriceWeights += uint64(entryWeight)
	}
	if totalPriceWeights == uint64(0) {
		return res
	}
	sliceStartAccnt, sliceStartWeights := 0, 0
	cumWeights := uint64(0)
	for i, entryWeight := range entryWeights {
		var price sdk.Dec
		if buy {
			price = sdk.MustNewDecFromStr(fmt.Sprintf("%f", BaselinePrice-float64(i)))
		} else {
			price = sdk.MustNewDecFromStr(fmt.Sprintf("%f", BaselinePrice+float64(i)))
		}
		cumWeights += uint64(entryWeight)
		nextSliceStartAccnt := int(cumWeights * uint64(len(allAccountIndices)) / totalPriceWeights)
		nextSliceStartWeights := int(cumWeights * uint64(len(allWeights)) / totalPriceWeights)
		entry := types.OrderEntry{
			Price:      price,
			Quantity:   sdk.NewDec(int64(uint64((entryWeight)))),
			PriceDenom: priceDenom,
			AssetDenom: assetDenom,
			Allocations: GetAllocations(
				int64(uint64((entryWeight))),
				allAccountIndices[sliceStartAccnt:nextSliceStartAccnt],
				allWeights[sliceStartWeights:nextSliceStartWeights],
			),
		}
		if buy {
			res = append(res, &types.LongBook{
				Price: price,
				Entry: &entry,
			})
		} else {
			res = append(res, &types.ShortBook{
				Price: price,
				Entry: &entry,
			})
		}
		sliceStartAccnt, sliceStartWeights = nextSliceStartAccnt, nextSliceStartWeights
	}
	return res
}

func GetAllocations(totalQuantity int64, accountIndices []byte, weights []byte) []*types.Allocation {
	// take the shorter slice's length
	if len(accountIndices) < len(weights) {
		weights = weights[:len(accountIndices)]
	} else {
		accountIndices = accountIndices[:len(weights)]
	}
	// dedupe and aggregate
	aggregatedAccountsToWeights := map[string]uint64{}
	totalWeight := uint64(0)
	for i, accountIdx := range accountIndices {
		account := GetValidAccount(int(accountIdx))
		weight := uint64(weights[i])
		if old, ok := aggregatedAccountsToWeights[account]; !ok {
			aggregatedAccountsToWeights[account] = weight
		} else {
			aggregatedAccountsToWeights[account] = old + weight
		}
		totalWeight += weight
	}

	quantityDec := sdk.NewDec(totalQuantity)
	totalWeightDec := sdk.NewDec(int64(totalWeight))
	res := []*types.Allocation{}
	orderID := 0
	for account, weight := range aggregatedAccountsToWeights {
		var quantity sdk.Dec
		if totalWeightDec.IsZero() {
			quantity = sdk.ZeroDec()
		} else {
			quantity = quantityDec.Mul(sdk.NewDec(int64(weight))).Quo(totalWeightDec)
		}
		res = append(res, &types.Allocation{
			OrderId:  uint64(orderID),
			Account:  account,
			Quantity: quantity,
		})
		orderID++
	}
	return res
}
