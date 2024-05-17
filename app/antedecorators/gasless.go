package antedecorators

import (
	"encoding/hex"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

type GaslessDecorator struct {
	wrapped      []sdk.AnteFullDecorator
	oracleKeeper oraclekeeper.Keeper
}

func NewGaslessDecorator(wrapped []sdk.AnteFullDecorator, oracleKeeper oraclekeeper.Keeper) GaslessDecorator {
	return GaslessDecorator{wrapped: wrapped, oracleKeeper: oracleKeeper}
}

func (gd GaslessDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	originalGasMeter := ctx.GasMeter()
	// eagerly set infinite gas meter so that queries performed by IsTxGasless will not incur gas cost
	ctx = ctx.WithGasMeter(storetypes.NewNoConsumptionInfiniteGasMeter())

	isGasless, err := IsTxGasless(tx, ctx, gd.oracleKeeper)
	if err != nil {
		return ctx, err
	}
	if !isGasless {
		ctx = ctx.WithGasMeter(originalGasMeter)
	}
	isDeliverTx := !ctx.IsCheckTx() && !ctx.IsReCheckTx() && !simulate
	if isDeliverTx || !isGasless {
		// In the case of deliverTx, we want to deduct fees regardless of whether the tx is considered gasless or not, since
		// gasless txs will be subject to application-specific fee requirements in later stage of ante, for which the payment
		// of those app-specific fees happens here. Note that the minimum fee check in the wrapped deduct fee handler is only
		// performed if the context is for CheckTx, so the check will be skipped for deliverTx and the deduct fee handler will
		// only deduct fee without checking.
		// Otherwise (i.e. in the case of checkTx), we only want to perform fee checks and fee deduction if the tx is not considered
		// gasless, or if it specifies a non-zero gas limit even if it is considered gasless, so that the wrapped deduct fee
		// handler will assign an appropriate priority to it.
		return gd.handleWrapped(ctx, tx, simulate, next)
	}

	return next(ctx, tx, simulate)
}

func (gd GaslessDecorator) handleWrapped(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// AnteHandle always takes a `next` so we need a no-op to execute only one handler at a time
	terminatorHandler := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	}
	// iterating instead of recursing the handler for readability
	for _, handler := range gd.wrapped {
		ctx, err := handler.AnteHandle(ctx, tx, simulate, terminatorHandler)
		if err != nil {
			return ctx, err
		}
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

func IsTxGasless(tx sdk.Tx, ctx sdk.Context, oracleKeeper oraclekeeper.Keeper) (bool, error) {
	if len(tx.GetMsgs()) == 0 {
		// empty TX shouldn't be gasless
		return false, nil
	}
	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {
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
