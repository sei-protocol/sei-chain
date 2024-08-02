package dex

import (
	"errors"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/utils"
)

// TickSizeMultipleDecorator check if the place order tx's price is multiple of
// tick size
type TickSizeMultipleDecorator struct {
	dexKeeper keeper.Keeper
}

// NewTickSizeMultipleDecorator returns new ticksize multiple check decorator instance
func NewTickSizeMultipleDecorator(dexKeeper keeper.Keeper) TickSizeMultipleDecorator {
	return TickSizeMultipleDecorator{
		dexKeeper: dexKeeper,
	}
}

// AnteHandle is the interface called in RunTx() function, which CheckTx() calls with the runTxModeCheck mode
func (tsmd TickSizeMultipleDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	if ctx.IsReCheckTx() {
		return next(ctx, tx, simulate)
	}
	if !simulate {
		if ctx.IsCheckTx() {
			err := tsmd.CheckTickSizeMultiple(ctx, tx.GetMsgs())
			if err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}

// CheckTickSizeMultiple checks whether the msgs comply with ticksize
func (tsmd TickSizeMultipleDecorator) CheckTickSizeMultiple(ctx sdk.Context, msgs []sdk.Msg) error {
	for _, msg := range msgs {
		switch msg.(type) { //nolint:gocritic,gosimple // the linter is telling us we can make this faster, and this should be addressed later.
		case *types.MsgPlaceOrders:
			msgPlaceOrders := msg.(*types.MsgPlaceOrders) //nolint:gosimple // the linter is telling us we can make this faster, and this should be addressed later.
			contractAddr := msgPlaceOrders.ContractAddr
			for _, order := range msgPlaceOrders.Orders {
				priceTickSize, found := tsmd.dexKeeper.GetPriceTickSizeForPair(ctx, contractAddr,
					types.Pair{
						PriceDenom: order.PriceDenom,
						AssetDenom: order.AssetDenom,
					})
				if !found {
					return sdkerrors.Wrapf(sdkerrors.ErrKeyNotFound, "the pair {price:%s,asset:%s} has no price ticksize configured", order.PriceDenom, order.AssetDenom)
				}
				if !IsDecimalMultipleOf(order.Price, priceTickSize) {
					// Allow Market Orders with Price 0
					if !(IsMarketOrder(order) && order.Price.IsZero()) {
						return sdkerrors.Wrapf(errors.New("ErrPriceNotMultipleOfTickSize"), "price needs to be non-zero and multiple of price tick size")
					}
				}
				quantityTickSize, found := tsmd.dexKeeper.GetQuantityTickSizeForPair(ctx, contractAddr,
					types.Pair{
						PriceDenom: order.PriceDenom,
						AssetDenom: order.AssetDenom,
					})
				if !found {
					return sdkerrors.Wrapf(sdkerrors.ErrKeyNotFound, "the pair {price:%s,asset:%s} has no quantity ticksize configured", order.PriceDenom, order.AssetDenom)
				}
				if !IsDecimalMultipleOf(order.Quantity, quantityTickSize) {
					return sdkerrors.Wrapf(errors.New("ErrQuantityNotMultipleOfTickSize"), "quantity needs to be non-zero and multiple of quantity tick size")
				}
			}
			continue
		default:
			// e.g. liquidation order don't come with price so always pass this check
			return nil
		}
	}

	return nil
}

// Check whether order is market order type
func IsMarketOrder(order *types.Order) bool {
	return order.OrderType == types.OrderType_MARKET || order.OrderType == types.OrderType_FOKMARKET || order.OrderType == types.OrderType_FOKMARKETBYVALUE
}

// Check whether decimal a is multiple of decimal b
func IsDecimalMultipleOf(a, b sdk.Dec) bool {
	if a.LT(b) {
		return false
	}
	quotient := sdk.NewDecFromBigInt(a.Quo(b).RoundInt().BigInt())
	return quotient.Mul(b).Equal(a)
}

const DexGasFeeUnit = "usei"

type CheckDexGasDecorator struct {
	dexKeeper       keeper.Keeper
	checkTxMemState *dexcache.MemState
}

func NewCheckDexGasDecorator(dexKeeper keeper.Keeper, checkTxMemState *dexcache.MemState) CheckDexGasDecorator {
	return CheckDexGasDecorator{
		dexKeeper:       dexKeeper,
		checkTxMemState: checkTxMemState,
	}
}

// for a TX that contains dex gas-incurring messages, check if it provides enough gas based on dex params
func (d CheckDexGasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	if ctx.IsReCheckTx() {
		return next(ctx, tx, simulate)
	}
	params := d.dexKeeper.GetParams(ctx)
	dexGasRequired := uint64(0)
	var memState *dexcache.MemState
	if ctx.IsCheckTx() {
		memState = d.checkTxMemState
	} else {
		memState = utils.GetMemState(ctx.Context())
	}
	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {
		case *types.MsgPlaceOrders:
			numDependencies := len(memState.GetContractToDependencies(ctx, m.ContractAddr, d.dexKeeper.GetContractWithoutGasCharge))
			dexGasRequired += params.DefaultGasPerOrder * uint64(len(m.Orders)*numDependencies)
			for _, order := range m.Orders {
				dexGasRequired += params.DefaultGasPerOrderDataByte * uint64(len(order.Data))
			}
		case *types.MsgCancelOrders:
			numDependencies := len(memState.GetContractToDependencies(ctx, m.ContractAddr, d.dexKeeper.GetContractWithoutGasCharge))
			dexGasRequired += params.DefaultGasPerCancel * uint64(len(m.Cancellations)*numDependencies)
		}
	}
	if dexGasRequired == 0 {
		return next(ctx, tx, simulate)
	}
	dexFeeRequired := sdk.NewDecFromBigInt(new(big.Int).SetUint64(dexGasRequired)).Mul(params.SudoCallGasPrice).RoundInt()
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}
	for _, fee := range feeTx.GetFee() {
		if fee.Denom == DexGasFeeUnit && fee.Amount.GTE(dexFeeRequired) {
			return next(ctx, tx, simulate)
		}
	}
	return ctx, sdkerrors.ErrInsufficientFee
}

func (d CheckDexGasDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	deps := []sdkacltypes.AccessOperation{}
	for _, msg := range tx.GetMsgs() {
		// Error checking will be handled in AnteHandler
		switch msg.(type) {
		case *types.MsgPlaceOrders, *types.MsgCancelOrders:
			deps = append(deps, []sdkacltypes.AccessOperation{
				// read the dex contract info
				{
					ResourceType:       sdkacltypes.ResourceType_KV_DEX_CONTRACT,
					AccessType:         sdkacltypes.AccessType_READ,
					IdentifierTemplate: "*",
				},
			}...)
		default:
			continue
		}
	}
	return next(append(txDeps, deps...), tx, txIndex)
}
