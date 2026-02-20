package baseapp

import (
	"crypto/sha256"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func (app *BaseApp) Check(txEncoder sdk.TxEncoder, tx sdk.Tx) (sdk.GasInfo, *sdk.Result, error) {
	// runTx expects tx bytes as argument, so we encode the tx argument into
	// bytes. Note that runTx will actually decode those bytes again. But since
	// this helper is only used in tests/simulation, it's fine.
	bz, err := txEncoder(tx)
	if err != nil {
		return sdk.GasInfo{}, nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "%s", err)
	}
	ctx := app.checkState.ctx.WithTxBytes(bz).WithConsensusParams(app.GetConsensusParams(app.checkState.ctx))
	gasInfo, result, _, _, _, _, _, _, err := app.runTx(ctx, runTxModeCheck, tx, sha256.Sum256(bz)) //nolint:dogsled // Because life is worth living instead of fixing this, considering sei solo is around the corner.
	return gasInfo, result, err
}

func (app *BaseApp) Deliver(txEncoder sdk.TxEncoder, tx sdk.Tx) (sdk.GasInfo, *sdk.Result, error) {
	// See comment for Check().
	bz, err := txEncoder(tx)
	if err != nil {
		return sdk.GasInfo{}, nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "%s", err)
	}
	ctx := app.deliverState.ctx.WithTxBytes(bz).WithConsensusParams(app.GetConsensusParams(app.deliverState.ctx))
	decoded, err := app.txDecoder(bz)
	if err != nil {
		return sdk.GasInfo{}, &sdk.Result{}, err
	}
	gasInfo, result, _, _, _, _, _, _, err := app.runTx(ctx, runTxModeDeliver, decoded, sha256.Sum256(bz)) //nolint:dogsled // Because life is worth living instead of fixing this, considering sei solo is around the corner.
	return gasInfo, result, err
}

// Context with current {check, deliver}State of the app used by tests.
func (app *BaseApp) NewContext(isCheckTx bool, header tmproto.Header) sdk.Context {
	if isCheckTx {
		return sdk.NewContext(app.checkState.ms, header, true, app.logger).
			WithMinGasPrices(app.minGasPrices)
	}

	return sdk.NewContext(app.deliverState.ms, header, false, app.logger)
}

func (app *BaseApp) NewUncachedContext(isCheckTx bool, header tmproto.Header) sdk.Context {
	return sdk.NewContext(app.cms, header, isCheckTx, app.logger)
}

func (app *BaseApp) GetContextForDeliverTx(txBytes []byte) sdk.Context {
	return app.getContextForTx(runTxModeDeliver, txBytes)
}
