package core

import (
	"context"
	"errors"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// ABCIQuery queries the application for some information.
// More: https://docs.tendermint.com/master/rpc/#/ABCI/abci_query
func (env *Environment) ABCIQuery(ctx context.Context, req *coretypes.RequestABCIQuery) (*coretypes.ResultABCIQuery, error) {
	app, ok := env.App.Get()
	if !ok {
		return nil, errors.New("ABCI queries are unavailable on Autobahn")
	}
	resQuery, err := app.Query(ctx, &abci.RequestQuery{
		Path:   req.Path,
		Data:   req.Data,
		Height: int64(req.Height),
		Prove:  req.Prove,
	})
	if err != nil {
		return nil, err
	}

	return &coretypes.ResultABCIQuery{Response: *resQuery}, nil
}

// ABCIInfo gets some info about the application.
// More: https://docs.tendermint.com/master/rpc/#/ABCI/abci_info
func (env *Environment) ABCIInfo(ctx context.Context) (*coretypes.ResultABCIInfo, error) {
	if evm, ok := env.EVM.Get(); ok {
		r := evm.Info()
		return &coretypes.ResultABCIInfo{Response: abci.ResponseInfo{Data: r.Data, Version: r.Version, AppVersion: r.AppVersion, LastBlockHeight: r.LastBlockHeight, LastBlockAppHash: r.LastBlockAppHash, MinimumGasPrices: r.MinimumGasPrices}}, nil
	}
	resInfo := env.abciApplication().Info()
	return &coretypes.ResultABCIInfo{Response: *resInfo}, nil
}
