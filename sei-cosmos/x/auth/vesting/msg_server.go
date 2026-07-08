package vesting

import (
	"context"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
)

// DeprecationUpgradeName is the upgrade plan that activates the vesting
// module's deprecation on chains with pre-deprecation history. It must match
// the plan name of the release that ships the deprecation; if this change
// slips to a later release, bump this constant to that release's name.
const DeprecationUpgradeName = "v6.6"

// chainsWithVestingHistory are the public chains whose history may contain
// successful MsgCreateVestingAccount transactions. On these chains the
// deprecation activates at the height the DeprecationUpgradeName upgrade
// executes, so replaying historical blocks produces identical state, gas, and
// app hashes. On every other chain (fresh networks, tests) the deprecation is
// active from genesis.
var chainsWithVestingHistory = map[string]struct{}{
	"pacific-1":  {}, // mainnet
	"atlantic-2": {}, // testnet
	"arctic-1":   {}, // devnet
}

type msgServer struct {
	keeper.AccountKeeper
	types.BankKeeper
	upgradeKeeper types.UpgradeKeeper
}

// NewMsgServerImpl returns an implementation of the vesting MsgServer
// interface, wrapping the corresponding AccountKeeper and BankKeeper.
//
// The vesting module is deprecated: once the deprecation gate is active (see
// creationDeprecated), every handler rejects its message with
// types.ErrVestingDeprecated. Existing vesting accounts in state remain fully
// supported.
func NewMsgServerImpl(k keeper.AccountKeeper, bk types.BankKeeper, uk types.UpgradeKeeper) types.MsgServer {
	return &msgServer{AccountKeeper: k, BankKeeper: bk, upgradeKeeper: uk}
}

var _ types.MsgServer = msgServer{}

// creationDeprecated reports whether vesting account creation is disabled at
// the current block. Chains with pre-deprecation history keep the original
// behavior below the deprecation upgrade height so historical replay is
// unchanged; all other chains reject immediately.
func (s msgServer) creationDeprecated(ctx sdk.Context) bool {
	if _, ok := chainsWithVestingHistory[ctx.ChainID()]; !ok {
		return true
	}
	// The done-height lookup must not consume gas: the pre-deprecation handler
	// performed no store reads before its first bank check, so charging gas
	// here would alter gas usage of historical transactions during replay.
	gasFreeCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1))
	return s.upgradeKeeper.IsUpgradeActiveAtHeight(gasFreeCtx, DeprecationUpgradeName, ctx.BlockHeight())
}

// CreateVestingAccount is deprecated and returns types.ErrVestingDeprecated
// once the deprecation gate is active; the original behavior is preserved
// below the gate so chains with pre-deprecation history replay identically.
// Existing vesting accounts remain in state and continue to vest according to
// their schedules regardless of the gate.
func (s msgServer) CreateVestingAccount(goCtx context.Context, msg *types.MsgCreateVestingAccount) (*types.MsgCreateVestingAccountResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if s.creationDeprecated(ctx) {
		return nil, types.ErrVestingDeprecated
	}

	ak := s.AccountKeeper
	bk := s.BankKeeper

	if err := bk.IsSendEnabledCoins(ctx, msg.Amount...); err != nil {
		return nil, err
	}

	from, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		return nil, err
	}
	to, err := sdk.AccAddressFromBech32(msg.ToAddress)
	if err != nil {
		return nil, err
	}

	if bk.BlockedAddr(to) {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "%s is not allowed to receive funds", msg.ToAddress)
	}

	if acc := ak.GetAccount(ctx, to); acc != nil {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "account %s already exists", msg.ToAddress)
	}

	baseAccount := ak.NewAccountWithAddress(ctx, to)
	if _, ok := baseAccount.(*authtypes.BaseAccount); !ok {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid account type; expected: BaseAccount, got: %T", baseAccount)
	}

	var admin sdk.AccAddress
	if len(msg.Admin) > 0 {
		admin, err = sdk.AccAddressFromBech32(msg.Admin)
		if err != nil {
			return nil, err
		}
		if acc := ak.GetAccount(ctx, admin); acc == nil {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "admin account %s doesn't exist", msg.ToAddress)
		}
	}

	baseVestingAccount := types.NewBaseVestingAccount(baseAccount.(*authtypes.BaseAccount), msg.Amount.Sort(), msg.EndTime, admin)

	var acc authtypes.AccountI

	if msg.Delayed {
		acc = types.NewDelayedVestingAccountRaw(baseVestingAccount)
	} else {
		acc = types.NewContinuousVestingAccountRaw(baseVestingAccount, ctx.BlockTime().Unix())
	}

	ak.SetAccount(ctx, acc)

	err = bk.SendCoins(ctx, from, to, msg.Amount)
	if err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
		),
	)

	return &types.MsgCreateVestingAccountResponse{}, nil
}
