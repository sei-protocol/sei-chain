package keeper

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

// NewQuerier creates a querier for upgrade cli and REST endpoints
func NewQuerier(k Keeper, legacyQuerierCdc *codec.LegacyAmino) sdk.Querier {
	return func(ctx sdk.Context, path []string, req abci.RequestQuery) ([]byte, error) {
		switch path[0] {

		case types.QueryCurrent:
			return queryCurrent(ctx, req, k, legacyQuerierCdc)

		case types.QueryApplied:
			return queryApplied(ctx, req, k, legacyQuerierCdc)

		default:
			return nil, sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unknown %s query endpoint: %s", types.ModuleName, path[0])
		}
	}
}

func queryCurrent(ctx sdk.Context, _ abci.RequestQuery, k Keeper, legacyQuerierCdc *codec.LegacyAmino) ([]byte, error) {
	plan, has := k.GetUpgradePlan(ctx)
	if !has {
		return nil, nil
	}

	res, err := legacyQuerierCdc.MarshalAsJSON(&plan)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrJSONMarshal, err.Error())
	}

	return res, nil
}

func queryApplied(ctx sdk.Context, req abci.RequestQuery, k Keeper, legacyQuerierCdc *codec.LegacyAmino) ([]byte, error) {
	var params types.QueryAppliedPlanRequest

	err := legacyQuerierCdc.UnmarshalAsJSON(req.Data, &params)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrJSONUnmarshal, err.Error())
	}

	applied := k.GetDoneHeight(ctx, params.Name)
	if applied == 0 {
		return nil, nil
	}

	if applied < 0 {
		return nil, fmt.Errorf("negative applied height: %d", applied)
	}

	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, uint64(applied)) //nolint:gosec // bounds checked above

	return bz, nil
}
