package ante

import (
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
)

// TxFeeChecker check if the provided fee is enough and returns the effective fee and tx priority,
// the effective fee should be deducted later, and the priority should be returned in abci response.
type TxFeeChecker func(ctx sdk.Context, tx sdk.Tx, simulate bool, paramsKeeper paramskeeper.Keeper) (sdk.Coins, int64, error)

// DeductFeeDecorator deducts fees from the first signer of the tx
// If the first signer does not have the funds to pay for the fees, return with InsufficientFunds error
// Call next AnteHandler if fees successfully deducted
// CONTRACT: Tx must implement FeeTx interface to use DeductFeeDecorator
type DeductFeeDecorator struct {
	accountKeeper  AccountKeeper
	bankKeeper     types.BankKeeper
	feegrantKeeper FeegrantKeeper
	paramsKeeper   paramskeeper.Keeper
	txFeeChecker   TxFeeChecker
}

func NewDeductFeeDecorator(
	ak AccountKeeper,
	bk types.BankKeeper,
	fk FeegrantKeeper,
	paramsKeeper paramskeeper.Keeper,
	tfc TxFeeChecker,
) DeductFeeDecorator {
	if tfc == nil {
		tfc = CheckTxFeeWithValidatorMinGasPrices
	}

	return DeductFeeDecorator{
		accountKeeper:  ak,
		bankKeeper:     bk,
		feegrantKeeper: fk,
		paramsKeeper:   paramsKeeper,
		txFeeChecker:   tfc,
	}
}

func (d DeductFeeDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	feeTx, _ := tx.(sdk.FeeTx)
	deps := []sdkacltypes.AccessOperation{}

	moduleAdr := d.accountKeeper.GetModuleAddress(types.FeeCollectorName)

	deps = append(deps, []sdkacltypes.AccessOperation{
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(moduleAdr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_DEFERRED_MODULE_TX_INDEX,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateDeferredCacheModuleTxIndexedPrefix(moduleAdr, uint64(txIndex))),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_DEFERRED_MODULE_TX_INDEX,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateDeferredCacheModuleTxIndexedPrefix(moduleAdr, uint64(txIndex))),
		},
	}...)

	if feeTx.FeePayer() != nil {
		deps = append(deps,
			[]sdkacltypes.AccessOperation{
				{
					AccessType:         sdkacltypes.AccessType_READ,
					ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
					IdentifierTemplate: hex.EncodeToString(authtypes.CreateAddressStoreKeyFromBech32(feeTx.FeePayer().String())),
				},
				{
					AccessType:         sdkacltypes.AccessType_READ,
					ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
					IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(feeTx.FeePayer())),
				},
				{
					AccessType:         sdkacltypes.AccessType_WRITE,
					ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
					IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(feeTx.FeePayer())),
				},
			}...)
		if feeTx.FeeGranter() != nil {
			deps = append(deps,
				[]sdkacltypes.AccessOperation{
					// read acc
					{
						AccessType:         sdkacltypes.AccessType_READ,
						ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
						IdentifierTemplate: hex.EncodeToString(authtypes.CreateAddressStoreKeyFromBech32(feeTx.FeeGranter().String())),
					},
					// read and write feegrant
					{
						AccessType:         sdkacltypes.AccessType_READ,
						ResourceType:       sdkacltypes.ResourceType_KV_FEEGRANT_ALLOWANCE,
						IdentifierTemplate: hex.EncodeToString(feegrant.FeeAllowanceKey(feeTx.FeeGranter(), feeTx.FeePayer())),
					},
					{
						AccessType:         sdkacltypes.AccessType_WRITE,
						ResourceType:       sdkacltypes.ResourceType_KV_FEEGRANT_ALLOWANCE,
						IdentifierTemplate: hex.EncodeToString(feegrant.FeeAllowanceKey(feeTx.FeeGranter(), feeTx.FeePayer())),
					},
					// read / write bank balances
					{
						AccessType:         sdkacltypes.AccessType_READ,
						ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
						IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(feeTx.FeeGranter())),
					},
					{
						AccessType:         sdkacltypes.AccessType_WRITE,
						ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
						IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(feeTx.FeeGranter())),
					},
				}...)
		}
	}

	return next(append(txDeps, deps...), tx, txIndex)
}

func (dfd DeductFeeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	fee, priority, err := dfd.txFeeChecker(ctx, tx, simulate, dfd.paramsKeeper)
	if err != nil {
		return ctx, err
	}
	if err := dfd.checkDeductFee(ctx, tx, fee); err != nil {
		return ctx, err
	}

	newCtx := ctx.WithPriority(priority)

	return next(newCtx, tx, simulate)
}

func (dfd DeductFeeDecorator) checkDeductFee(ctx sdk.Context, sdkTx sdk.Tx, fee sdk.Coins) error {
	feeTx, ok := sdkTx.(sdk.FeeTx)
	if !ok {
		return sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	if addr := dfd.accountKeeper.GetModuleAddress(types.FeeCollectorName); addr == nil {
		return fmt.Errorf("fee collector module account (%s) has not been set", types.FeeCollectorName)
	}

	feePayer := feeTx.FeePayer()
	feeGranter := feeTx.FeeGranter()
	deductFeesFrom := feePayer

	// if feegranter set deduct fee from feegranter account.
	// this works with only when feegrant enabled.
	if feeGranter != nil {
		if dfd.feegrantKeeper == nil {
			return sdkerrors.ErrInvalidRequest.Wrap("fee grants are not enabled")
		} else if !feeGranter.Equals(feePayer) {
			err := dfd.feegrantKeeper.UseGrantedFees(ctx, feeGranter, feePayer, fee, sdkTx.GetMsgs())
			if err != nil {
				return sdkerrors.Wrapf(err, "%s does not not allow to pay fees for %s", feeGranter, feePayer)
			}
		}

		deductFeesFrom = feeGranter
	}

	deductFeesFromAcc := dfd.accountKeeper.GetAccount(ctx, deductFeesFrom)
	if deductFeesFromAcc == nil {
		return sdkerrors.ErrUnknownAddress.Wrapf("fee payer address: %s does not exist", deductFeesFrom)
	}

	// deduct the fees
	if !fee.IsZero() {
		err := DeductFees(dfd.bankKeeper, ctx, deductFeesFromAcc, fee)
		if err != nil {
			return err
		}
	}

	events := sdk.Events{
		sdk.NewEvent(
			sdk.EventTypeTx,
			sdk.NewAttribute(sdk.AttributeKeyFee, fee.String()),
			sdk.NewAttribute(sdk.AttributeKeyFeePayer, deductFeesFrom.String()),
		),
	}
	ctx.EventManager().EmitEvents(events)

	return nil
}

// DeductFees deducts fees from the given account.
func DeductFees(bankKeeper types.BankKeeper, ctx sdk.Context, acc types.AccountI, fees sdk.Coins) error {
	if !fees.IsValid() {
		return sdkerrors.Wrapf(sdkerrors.ErrInsufficientFee, "invalid fee amount: %s", fees)
	}

	err := bankKeeper.DeferredSendCoinsFromAccountToModule(ctx, acc.GetAddress(), types.FeeCollectorName, fees)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInsufficientFunds, err.Error())
	}

	return nil
}
