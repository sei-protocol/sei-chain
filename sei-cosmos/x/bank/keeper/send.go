package keeper

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/bank/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// SendKeeper defines a module interface that facilitates the transfer of coins
// between accounts without the possibility of creating coins.
type SendKeeper interface {
	ViewKeeper

	InputOutputCoins(ctx sdk.Context, inputs []types.Input, outputs []types.Output) error
	SendCoins(ctx sdk.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsWithoutAccCreation(ctx sdk.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsAndWei(ctx sdk.Context, from sdk.AccAddress, to sdk.AccAddress, amt sdk.Int, wei sdk.Int) error
	SubUnlockedCoins(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Coins, checkNeg bool) error
	AddCoins(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Coins, checkNeg bool) error
	SubWei(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Int) error
	AddWei(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Int) error

	GetParams(ctx sdk.Context) types.Params
	SetParams(ctx sdk.Context, params types.Params)

	IsSendEnabledCoin(ctx sdk.Context, coin sdk.Coin) bool
	IsSendEnabledCoins(ctx sdk.Context, coins ...sdk.Coin) error

	BlockedAddr(addr sdk.AccAddress) bool
	RegisterRecipientChecker(RecipientChecker)
}

type RecipientChecker = func(ctx sdk.Context, recipient sdk.AccAddress) bool

var _ SendKeeper = (*BaseSendKeeper)(nil)
var OneUseiInWei sdk.Int = sdk.NewInt(1_000_000_000_000)

// BaseSendKeeper only allows transfers between accounts without the possibility of
// creating coins. It implements the SendKeeper interface.
type BaseSendKeeper struct {
	BaseViewKeeper

	cdc        codec.BinaryCodec
	ak         types.AccountKeeper
	storeKey   sdk.StoreKey
	paramSpace paramtypes.Subspace

	// list of addresses that are restricted from receiving transactions
	blockedAddrs      map[string]bool
	recipientCheckers *[]RecipientChecker
}

func NewBaseSendKeeper(
	cdc codec.BinaryCodec, storeKey sdk.StoreKey, ak types.AccountKeeper, paramSpace paramtypes.Subspace, blockedAddrs map[string]bool,
) BaseSendKeeper {

	return BaseSendKeeper{
		BaseViewKeeper:    NewBaseViewKeeper(cdc, storeKey, ak),
		cdc:               cdc,
		ak:                ak,
		storeKey:          storeKey,
		paramSpace:        paramSpace,
		blockedAddrs:      blockedAddrs,
		recipientCheckers: &[]RecipientChecker{},
	}
}

// GetParams returns the total set of bank parameters.
func (k BaseSendKeeper) GetParams(ctx sdk.Context) (params types.Params) {
	k.paramSpace.GetParamSet(ctx, &params)
	return params
}

// SetParams sets the total set of bank parameters.
func (k BaseSendKeeper) SetParams(ctx sdk.Context, params types.Params) {
	k.paramSpace.SetParamSet(ctx, &params)
}

// InputOutputCoins performs multi-send functionality. It accepts a series of
// inputs that correspond to a series of outputs. It returns an error if the
// inputs and outputs don't lineup or if any single transfer of tokens fails.
func (k BaseSendKeeper) InputOutputCoins(ctx sdk.Context, inputs []types.Input, outputs []types.Output) error {
	// Safety check ensuring that when sending coins the keeper must maintain the
	// Check supply invariant and validity of Coins.
	if err := types.ValidateInputsOutputs(inputs, outputs); err != nil {
		return err
	}

	for _, in := range inputs {
		inAddress, err := sdk.AccAddressFromBech32(in.Address)
		if err != nil {
			return err
		}

		err = k.SubUnlockedCoins(ctx, inAddress, in.Coins, true)
		if err != nil {
			return err
		}

		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				sdk.EventTypeMessage,
				sdk.NewAttribute(types.AttributeKeySender, in.Address),
			),
		)
	}

	for _, out := range outputs {
		outAddress, err := sdk.AccAddressFromBech32(out.Address)
		if err != nil {
			return err
		}
		err = k.AddCoins(ctx, outAddress, out.Coins, true)
		if err != nil {
			return err
		}

		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeTransfer,
				sdk.NewAttribute(types.AttributeKeyRecipient, out.Address),
				sdk.NewAttribute(sdk.AttributeKeyAmount, out.Coins.String()),
			),
		)

		// Create account if recipient does not exist.
		//
		// NOTE: This should ultimately be removed in favor a more flexible approach
		// such as delegated fee messages.
		accExists := k.ak.HasAccount(ctx, outAddress)
		if !accExists {
			defer telemetry.IncrCounter(1, "new", "account")
			k.ak.SetAccount(ctx, k.ak.NewAccountWithAddress(ctx, outAddress))
		}
	}

	return nil
}

// SendCoins transfers amt coins from a sending account to a receiving account.
// An error is returned upon failure.
func (k BaseSendKeeper) SendCoins(ctx sdk.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	if err := k.SendCoinsWithoutAccCreation(ctx, fromAddr, toAddr, amt); err != nil {
		return err
	}

	// Create account if recipient does not exist.
	//
	// NOTE: This should ultimately be removed in favor a more flexible approach
	// such as delegated fee messages.
	accExists := k.ak.HasAccount(ctx, toAddr)
	if !accExists {
		defer telemetry.IncrCounter(1, "new", "account")
		k.ak.SetAccount(ctx, k.ak.NewAccountWithAddress(ctx, toAddr))
	}

	return nil
}

func (k BaseSendKeeper) SendCoinsWithoutAccCreation(ctx sdk.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	return k.sendCoinsWithoutAccCreation(ctx, fromAddr, toAddr, amt, true)
}

func (k BaseSendKeeper) sendCoinsWithoutAccCreation(ctx sdk.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins, checkNeg bool) error {
	err := k.SubUnlockedCoins(ctx, fromAddr, amt, checkNeg)
	if err != nil {
		return err
	}

	err = k.AddCoins(ctx, toAddr, amt, checkNeg)
	if err != nil {
		return err
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeTransfer,
			sdk.NewAttribute(types.AttributeKeyRecipient, toAddr.String()),
			sdk.NewAttribute(types.AttributeKeySender, fromAddr.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, amt.String()),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(types.AttributeKeySender, fromAddr.String()),
		),
	})

	return nil
}

// SubUnlockedCoins removes the unlocked amt coins of the given account. An error is
// returned if the resulting balance is negative or the initial amount is invalid.
// A coin_spent event is emitted after.
func (k BaseSendKeeper) SubUnlockedCoins(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Coins, checkNeg bool) error {
	if !amt.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, amt.String())
	}

	lockedCoins := k.LockedCoins(ctx, addr)

	for _, coin := range amt {
		balance := k.GetBalance(ctx, addr, coin.Denom)
		if checkNeg {
			locked := sdk.NewCoin(coin.Denom, lockedCoins.AmountOf(coin.Denom))
			spendable := balance.Sub(locked)

			_, hasNeg := sdk.Coins{spendable}.SafeSub(sdk.Coins{coin})
			if hasNeg {
				return sdkerrors.Wrapf(sdkerrors.ErrInsufficientFunds, "%s is smaller than %s", spendable, coin)
			}
		}

		var newBalance sdk.Coin
		if checkNeg {
			newBalance = balance.Sub(coin)
		} else {
			newBalance = balance.SubUnsafe(coin)
		}

		err := k.setBalance(ctx, addr, newBalance, checkNeg)
		if err != nil {
			return err
		}
	}

	// emit coin spent event
	ctx.EventManager().EmitEvent(
		types.NewCoinSpentEvent(addr, amt),
	)
	return nil
}

// AddCoins increase the addr balance by the given amt. Fails if the provided amt is invalid.
// It emits a coin received event.
func (k BaseSendKeeper) AddCoins(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Coins, checkNeg bool) error {
	if !k.CanSendTo(ctx, addr) {
		return sdkerrors.ErrInvalidRecipient
	}
	if !amt.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, amt.String())
	}

	for _, coin := range amt {
		balance := k.GetBalance(ctx, addr, coin.Denom)
		newBalance := balance.Add(coin)

		err := k.setBalance(ctx, addr, newBalance, checkNeg)
		if err != nil {
			return err
		}
	}

	// emit coin received event
	ctx.EventManager().EmitEvent(
		types.NewCoinReceivedEvent(addr, amt),
	)

	return nil
}

// initBalances sets the balance (multiple coins) for an account by address.
// An error is returned upon failure.
func (k BaseSendKeeper) initBalances(ctx sdk.Context, addr sdk.AccAddress, balances sdk.Coins) error {
	accountStore := k.getAccountStore(ctx, addr)
	for i := range balances {
		balance := balances[i]
		if !balance.IsValid() {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, balance.String())
		}

		// Bank invariants require to not store zero balances.
		if !balance.IsZero() {
			bz := k.cdc.MustMarshal(&balance)
			accountStore.Set([]byte(balance.Denom), bz)
		}
	}

	return nil
}

// setBalance sets the coin balance for an account by address.
func (k BaseSendKeeper) setBalance(ctx sdk.Context, addr sdk.AccAddress, balance sdk.Coin, checkNeg bool) error {
	if checkNeg && !balance.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, balance.String())
	}

	accountStore := k.getAccountStore(ctx, addr)

	// Bank invariants require to not store zero balances.
	if balance.IsZero() {
		accountStore.Delete([]byte(balance.Denom))
	} else {
		bz := k.cdc.MustMarshal(&balance)
		accountStore.Set([]byte(balance.Denom), bz)
	}

	return nil
}

func (k BaseSendKeeper) setWeiBalance(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Int) error {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.WeiBalancesPrefix)
	if amt.IsZero() {
		store.Delete(addr)
		return nil
	}
	val, err := amt.Marshal()
	if err != nil {
		return err
	}
	store.Set(addr, val)
	return nil
}

// IsSendEnabledCoins checks the coins provide and returns an ErrSendDisabled if
// any of the coins are not configured for sending.  Returns nil if sending is enabled
// for all provided coin
func (k BaseSendKeeper) IsSendEnabledCoins(ctx sdk.Context, coins ...sdk.Coin) error {
	for _, coin := range coins {
		if !k.IsSendEnabledCoin(ctx, coin) {
			return sdkerrors.Wrapf(types.ErrSendDisabled, "%s transfers are currently disabled", coin.Denom)
		}
	}
	return nil
}

// IsSendEnabledCoin returns the current SendEnabled status of the provided coin's denom
func (k BaseSendKeeper) IsSendEnabledCoin(ctx sdk.Context, coin sdk.Coin) bool {
	return k.GetParams(ctx).SendEnabledDenom(coin.Denom)
}

// BlockedAddr checks if a given address is restricted from
// receiving funds.
func (k BaseSendKeeper) BlockedAddr(addr sdk.AccAddress) bool {
	return k.blockedAddrs[addr.String()]
}

func (k BaseSendKeeper) SubWei(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Int) (err error) {
	if amt.Equal(sdk.ZeroInt()) {
		return nil
	}
	defer func() {
		if err == nil {
			ctx.EventManager().EmitEvent(
				types.NewWeiSpentEvent(addr, amt),
			)
		}
	}()
	currentWeiBalance := k.GetWeiBalance(ctx, addr)
	if amt.LTE(currentWeiBalance) {
		// no need to change usei balance
		return k.setWeiBalance(ctx, addr, currentWeiBalance.Sub(amt))
	}
	currentUseiBalance := k.GetBalance(ctx, addr, sdk.MustGetBaseDenom()).Amount
	currentAggregatedBalance := currentUseiBalance.Mul(OneUseiInWei).Add(currentWeiBalance)
	postAggregatedbalance := currentAggregatedBalance.Sub(amt)
	if postAggregatedbalance.IsNegative() {
		return sdkerrors.Wrapf(sdkerrors.ErrInsufficientFunds, "%swei is smaller than %swei", currentAggregatedBalance, amt)
	}
	useiBalance, weiBalance := SplitUseiWeiAmount(postAggregatedbalance)
	if err := k.setBalance(ctx, addr, sdk.NewCoin(sdk.MustGetBaseDenom(), useiBalance), true); err != nil {
		return err
	}
	return k.setWeiBalance(ctx, addr, weiBalance)
}

func (k BaseSendKeeper) AddWei(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Int) (err error) {
	if !k.CanSendTo(ctx, addr) {
		return sdkerrors.ErrInvalidRecipient
	}
	if amt.Equal(sdk.ZeroInt()) {
		return nil
	}
	defer func() {
		if err == nil {
			ctx.EventManager().EmitEvent(
				types.NewWeiReceivedEvent(addr, amt),
			)
		}
	}()
	currentWeiBalance := k.GetWeiBalance(ctx, addr)
	postWeiBalance := currentWeiBalance.Add(amt)
	if postWeiBalance.LT(OneUseiInWei) {
		// no need to change usei balance
		return k.setWeiBalance(ctx, addr, postWeiBalance)
	}
	currentUseiBalance := k.GetBalance(ctx, addr, sdk.MustGetBaseDenom()).Amount
	useiCredit, weiBalance := SplitUseiWeiAmount(postWeiBalance)
	if err := k.setBalance(ctx, addr, sdk.NewCoin(sdk.MustGetBaseDenom(), currentUseiBalance.Add(useiCredit)), true); err != nil {
		return err
	}
	return k.setWeiBalance(ctx, addr, weiBalance)
}

func (k BaseSendKeeper) SendCoinsAndWei(ctx sdk.Context, from sdk.AccAddress, to sdk.AccAddress, amt sdk.Int, wei sdk.Int) error {
	if err := k.SubWei(ctx, from, wei); err != nil {
		return err
	}
	if err := k.AddWei(ctx, to, wei); err != nil {
		return err
	}
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeWeiTransfer,
			sdk.NewAttribute(types.AttributeKeyRecipient, to.String()),
			sdk.NewAttribute(types.AttributeKeySender, from.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, wei.String()),
		),
	})
	if amt.GT(sdk.ZeroInt()) {
		return k.SendCoinsWithoutAccCreation(ctx, from, to, sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), amt)))
	}
	return nil
}

func (k BaseSendKeeper) RegisterRecipientChecker(rc RecipientChecker) {
	*k.recipientCheckers = append(*k.recipientCheckers, rc)
}

func (k BaseSendKeeper) CanSendTo(ctx sdk.Context, recipient sdk.AccAddress) bool {
	for _, rc := range *k.recipientCheckers {
		if !rc(ctx, recipient) {
			return false
		}
	}
	return true
}

func SplitUseiWeiAmount(amt sdk.Int) (sdk.Int, sdk.Int) {
	return amt.Quo(OneUseiInWei), amt.Mod(OneUseiInWei)
}
