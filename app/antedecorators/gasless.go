package antedecorators

import (
	"bytes"
	"encoding/hex"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

type GaslessDecorator struct {
	wrapped      []sdk.AnteFullDecorator
	oracleKeeper oraclekeeper.Keeper
	dexKeeper    dexkeeper.Keeper
}

func NewGaslessDecorator(wrapped []sdk.AnteFullDecorator, oracleKeeper oraclekeeper.Keeper, dexKeeper dexkeeper.Keeper) GaslessDecorator {
	return GaslessDecorator{wrapped: wrapped, oracleKeeper: oracleKeeper, dexKeeper: dexKeeper}
}

func (gd GaslessDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	originalGasMeter := ctx.GasMeter()
	// eagerly set infinite gas meter so that queries performed by isTxGasless will not incur gas cost
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeter())

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}
	gas := feeTx.GetGas()
	// If non-zero gas limit is provided by the TX, we then consider it exempt from the gasless TX, and then prioritize it accordingly
	isGasless, err := isTxGasless(tx, ctx, gd.oracleKeeper, gd.dexKeeper)
	if err != nil {
		return ctx, err
	}
	if gas > 0 || !isGasless {
		ctx = ctx.WithGasMeter(originalGasMeter)
		// if not gasless, then we use the wrappers

		// AnteHandle always takes a `next` so we need a no-op to execute only one handler at a time
		terminatorHandler := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			return ctx, nil
		}
		// iterating instead of recursing the handler for readability
		for _, handler := range gd.wrapped {
			ctx, err = handler.AnteHandle(ctx, tx, simulate, terminatorHandler)
			if err != nil {
				return ctx, err
			}
		}
		return next(ctx, tx, simulate)
	}

	return next(ctx, tx, simulate)
}

func (gd GaslessDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	deps := []sdkacltypes.AccessOperation{}
	terminatorDeps := func(txDeps []sdkacltypes.AccessOperation, _ sdk.Tx, _ int) ([]sdkacltypes.AccessOperation, error) {
		return txDeps, nil
	}
	for _, depGen := range gd.wrapped {
		deps, _ = depGen.AnteDeps(deps, tx, txIndex, terminatorDeps)
	}
	for _, msg := range tx.GetMsgs() {
		// Error checking will be handled in AnteHandler
		switch m := msg.(type) {
		case *oracletypes.MsgAggregateExchangeRateVote:
			valAddr, _ := sdk.ValAddressFromBech32(m.Validator)
			deps = append(deps, []sdkacltypes.AccessOperation{
				// validate feeder
				// read feeder delegation for val addr - READ
				{
					ResourceType:       sdkacltypes.ResourceType_KV_ORACLE_FEEDERS,
					AccessType:         sdkacltypes.AccessType_READ,
					IdentifierTemplate: hex.EncodeToString(oracletypes.GetFeederDelegationKey(valAddr)),
				},
				// read validator from staking - READ
				{
					ResourceType:       sdkacltypes.ResourceType_KV_STAKING_VALIDATOR,
					AccessType:         sdkacltypes.AccessType_READ,
					IdentifierTemplate: hex.EncodeToString(stakingtypes.GetValidatorKey(valAddr)),
				},
				// check exchange rate vote exists - READ
				{
					ResourceType:       sdkacltypes.ResourceType_KV_ORACLE_AGGREGATE_VOTES,
					AccessType:         sdkacltypes.AccessType_READ,
					IdentifierTemplate: hex.EncodeToString(oracletypes.GetAggregateExchangeRateVoteKey(valAddr)),
				},
			}...)
		default:
			continue
		}
	}

	return next(append(txDeps, deps...), tx, txIndex)
}

func isTxGasless(tx sdk.Tx, ctx sdk.Context, oracleKeeper oraclekeeper.Keeper, dexKeeper dexkeeper.Keeper) (bool, error) {
	if len(tx.GetMsgs()) == 0 {
		// empty TX shouldn't be gasless
		return false, nil
	}
	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {
		case *dextypes.MsgPlaceOrders:
			if !dexPlaceOrdersIsGasless(m) {
				return false, nil
			}

		case *dextypes.MsgCancelOrders:
			if !dexCancelOrdersIsGasless(ctx, m, dexKeeper) {
				return false, nil
			}
		case *oracletypes.MsgAggregateExchangeRateVote:
			isGasless, err := oracleVoteIsGasless(m, ctx, oracleKeeper)
			if err != nil || !isGasless {
				return false, err
			}
		default:
			return false, nil
		}
	}
	return true, nil
}

func dexPlaceOrdersIsGasless(msg *dextypes.MsgPlaceOrders) bool {
	return true
}

func dexCancelOrdersIsGasless(ctx sdk.Context, msg *dextypes.MsgCancelOrders, keeper dexkeeper.Keeper) bool {
	return allSignersWhitelisted(ctx, msg, keeper)
}

func allSignersWhitelisted(ctx sdk.Context, msg *dextypes.MsgCancelOrders, keeper dexkeeper.Keeper) bool {
	whitelist := keeper.GetWhitelistedGaslessCancelAddresses(ctx)
	for _, signer := range msg.GetSigners() {
		isWhitelisted := false
		for _, whitelisted := range whitelist {
			if bytes.Compare(signer, whitelisted) == 0 { //nolint:gosimple
				isWhitelisted = true
				break
			}
		}
		if !isWhitelisted {
			return false
		}
	}
	return true
}

func oracleVoteIsGasless(msg *oracletypes.MsgAggregateExchangeRateVote, ctx sdk.Context, keeper oraclekeeper.Keeper) (bool, error) {
	feederAddr, err := sdk.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		return false, err
	}

	valAddr, err := sdk.ValAddressFromBech32(msg.Validator)
	if err != nil {
		return false, err
	}

	err = keeper.ValidateFeeder(ctx, feederAddr, valAddr)
	if err != nil {
		return false, err
	}

	// this returns an error IFF there is no vote present
	// this also gets cleared out after every vote window, so if there is no vote present, we may want to allow gasless tx
	_, err = keeper.GetAggregateExchangeRateVote(ctx, valAddr)
	if err == nil {
		// if there is no error that means there is a vote present, so we don't allow gasless tx
		err = sdkerrors.Wrap(oracletypes.ErrAggregateVoteExist, valAddr.String())
		return false, err
	}
	// otherwise we allow it
	return true, nil
}
