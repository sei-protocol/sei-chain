package query

import (
	"context"
	"sort"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
)

type priceQuantity struct {
	price    sdk.Dec
	quantity sdk.Dec
}

// Note that this simulation is only accurate if it's called as part of the main Sei process (e.g. in Begin/EndBlock, transaction handler
// or contract querier), because it needs to access dex's in-memory state.
func (k KeeperWrapper) GetOrderSimulation(c context.Context, req *types.QueryOrderSimulationRequest) (*types.QueryOrderSimulationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
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
				eligibleOrderBookPriceToQuantity[lb.GetPrice().String()] = lb.GetOrderEntry().Quantity
			}
		}
	} else {
		for _, sb := range k.GetAllShortBookForPair(ctx, req.ContractAddr, req.Order.PriceDenom, req.Order.AssetDenom) {
			if req.Order.Price.IsZero() || req.Order.Price.GTE(sb.GetPrice()) {
				eligibleOrderBookPriceToQuantity[sb.GetPrice().String()] = sb.GetOrderEntry().Quantity
			}
		}
	}

	// exclude liquidity to be cancelled
	pair := types.Pair{PriceDenom: req.Order.PriceDenom, AssetDenom: req.Order.AssetDenom}
	for _, cancel := range dexutils.GetMemState(ctx.Context()).GetBlockCancels(ctx, types.ContractAddress(req.ContractAddr), pair).Get() {
		var cancelledAllocation *types.Allocation
		var found bool
		if cancel.PositionDirection == types.PositionDirection_LONG {
			cancelledAllocation, found = k.GetLongAllocationForOrderID(ctx, req.ContractAddr, cancel.PriceDenom, cancel.AssetDenom, cancel.Price, cancel.Id)
		} else {
			cancelledAllocation, found = k.GetShortAllocationForOrderID(ctx, req.ContractAddr, cancel.PriceDenom, cancel.AssetDenom, cancel.Price, cancel.Id)
		}
		if !found {
			continue
		}
		if q, ok := eligibleOrderBookPriceToQuantity[cancel.Price.String()]; ok {
			eligibleOrderBookPriceToQuantity[cancel.Price.String()] = q.Sub(cancelledAllocation.Quantity)
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
	for _, order := range dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(req.ContractAddr), pair).GetSortedMarketOrders(orderDirection) {
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
