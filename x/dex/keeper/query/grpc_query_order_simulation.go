package query

import (
	"context"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
)

type priceQuantity struct {
	price    sdk.Dec
	quantity sdk.Dec
}

// Note that this simulation is only accurate if it's called as part of the main Sei process (e.g. in Begin/EndBlock, transaction handler
// or contract querier), because it needs to access dex's in-memory state.
func (k KeeperWrapper) GetOrderSimulation(c context.Context, req *types.QueryOrderSimulationRequest) (*types.QueryOrderSimulationResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	matchedPriceQuantities := k.getMatchedPriceQuantities(ctx, req)
	executedQuantity := sdk.ZeroDec()
	for _, pq := range matchedPriceQuantities {
		if executedQuantity.Add(pq.quantity).GTE(req.Order.Quantity) {
			executedQuantity = req.Order.Quantity
			break
		}
		executedQuantity = executedQuantity.Add(pq.quantity)
	}
	return &types.QueryOrderSimulationResponse{
		ExecutedQuantity: &executedQuantity,
	}, nil
}

func (k KeeperWrapper) getMatchedPriceQuantities(ctx sdk.Context, req *types.QueryOrderSimulationRequest) []priceQuantity {
	orderDirection := req.Order.PositionDirection
	// get existing liquidity
	eligibleOrderBookPriceToQuantity := map[string]sdk.Dec{}
	if orderDirection == types.PositionDirection_SHORT {
		for _, lb := range k.GetAllLongBookForPair(ctx, req.ContractAddr, req.Order.PriceDenom, req.Order.AssetDenom) {
			if req.Order.Price.IsZero() || req.Order.Price.LTE(lb.GetPrice()) {
				eligibleOrderBookPriceToQuantity[lb.GetPrice().String()] = lb.GetEntry().Quantity
			}
		}
	} else {
		for _, sb := range k.GetAllShortBookForPair(ctx, req.ContractAddr, req.Order.PriceDenom, req.Order.AssetDenom) {
			if req.Order.Price.IsZero() || req.Order.Price.GTE(sb.GetPrice()) {
				eligibleOrderBookPriceToQuantity[sb.GetPrice().String()] = sb.GetEntry().Quantity
			}
		}
	}

	// exclude liquidity to be cancelled
	pair := types.Pair{PriceDenom: req.Order.PriceDenom, AssetDenom: req.Order.AssetDenom}
	for _, cancel := range k.MemState.GetBlockCancels(ctx, utils.ContractAddress(req.ContractAddr), utils.GetPairString(&pair)).Get() {
		orderToBeCancelled := k.GetOrdersByIds(ctx, req.ContractAddr, []uint64{cancel.Id})
		if _, ok := orderToBeCancelled[cancel.Id]; !ok {
			continue
		}
		order := orderToBeCancelled[cancel.Id]
		if q, ok := eligibleOrderBookPriceToQuantity[order.Price.String()]; ok {
			eligibleOrderBookPriceToQuantity[order.Price.String()] = q.Sub(order.Quantity)
		}
	}

	priceQuantities := []priceQuantity{}
	for price, quantity := range eligibleOrderBookPriceToQuantity {
		if quantity.IsPositive() {
			priceQuantities = append(priceQuantities, priceQuantity{price: sdk.MustNewDecFromStr(price), quantity: quantity})
		}
	}
	sort.Slice(priceQuantities, func(i int, j int) bool {
		if orderDirection == types.PositionDirection_SHORT {
			// short order corresponds to long book which needs to be in descending order
			return priceQuantities[i].price.GT(priceQuantities[j].price)
		}
		// long order corresponds to long book which needs to be in ascending order
		return priceQuantities[i].price.LT(priceQuantities[j].price)
	})

	// exclude liquidity to be taken
	ptr := 0
	for _, order := range k.MemState.GetBlockOrders(ctx, utils.ContractAddress(req.ContractAddr), utils.GetPairString(&pair)).GetSortedMarketOrders(
		orderDirection, false,
	) {
		// If existing market order has price zero, it means it doesn't specify a worst price and will always have precedence over the simulated
		// order
		if !order.Price.IsZero() {
			// If the simulated order doesn't specify a worst price, no existing order with a worst price will take liquidity from it
			if req.Order.Price.IsZero() {
				break
			}
			if orderDirection == types.PositionDirection_LONG && order.Price.LT(req.Order.Price) {
				break
			}
			if orderDirection == types.PositionDirection_SHORT && order.Price.GT(req.Order.Price) {
				break
			}
		}
		remainingQuantity := order.Quantity
		for ptr < len(priceQuantities) {
			if remainingQuantity.LTE(priceQuantities[ptr].quantity) {
				priceQuantities[ptr].quantity = priceQuantities[ptr].quantity.Sub(remainingQuantity)
				break
			}
			remainingQuantity = remainingQuantity.Sub(priceQuantities[ptr].quantity)
			ptr++
		}
	}
	return priceQuantities[ptr:]
}
