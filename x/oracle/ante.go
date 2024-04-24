package oracle

import (
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

// SpammingPreventionDecorator will check if the transaction's gas is smaller than
// configured hard cap
type SpammingPreventionDecorator struct {
	oracleKeeper keeper.Keeper
}

// NewSpammingPreventionDecorator returns new spamming prevention decorator instance
func NewSpammingPreventionDecorator(oracleKeeper keeper.Keeper) SpammingPreventionDecorator {
	return SpammingPreventionDecorator{
		oracleKeeper: oracleKeeper,
	}
}

// AnteHandle handles msg tax fee checking
func (spd SpammingPreventionDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	if ctx.IsReCheckTx() {
		return next(ctx, tx, simulate)
	}

	if !simulate {
		if ctx.IsCheckTx() {
			err := spd.CheckOracleSpamming(ctx, tx.GetMsgs())
			if err != nil {
				return ctx, err
			}
		}
	}

	return next(ctx, tx, simulate)
}

func (spd SpammingPreventionDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	deps := []sdkacltypes.AccessOperation{}
	for _, msg := range tx.GetMsgs() {
		// Error checking will be handled in AnteHandler
		switch m := msg.(type) {
		case *types.MsgAggregateExchangeRateVote:
			valAddr, _ := sdk.ValAddressFromBech32(m.Validator)
			deps = append(deps, []sdkacltypes.AccessOperation{
				// validate feeder
				// read feeder delegation for val addr - READ
				{
					ResourceType:       sdkacltypes.ResourceType_KV_ORACLE_FEEDERS,
					AccessType:         sdkacltypes.AccessType_READ,
					IdentifierTemplate: hex.EncodeToString(types.GetFeederDelegationKey(valAddr)),
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
					IdentifierTemplate: hex.EncodeToString(types.GetAggregateExchangeRateVoteKey(valAddr)),
				},
			}...)
		default:
			continue
		}
	}

	return next(append(txDeps, deps...), tx, txIndex)
}

// CheckOracleSpamming check whether the msgs are spamming purpose or not
func (spd SpammingPreventionDecorator) CheckOracleSpamming(ctx sdk.Context, msgs []sdk.Msg) error {
	curHeight := ctx.BlockHeight()
	for _, msg := range msgs {
		switch msg := msg.(type) {
		case *types.MsgAggregateExchangeRateVote:
			feederAddr, err := sdk.AccAddressFromBech32(msg.Feeder)
			if err != nil {
				return err
			}

			valAddr, err := sdk.ValAddressFromBech32(msg.Validator)
			if err != nil {
				return err
			}

			err = spd.oracleKeeper.ValidateFeeder(ctx, feederAddr, valAddr)
			if err != nil {
				return err
			}

			spamPreventionCounterHeight := spd.oracleKeeper.GetSpamPreventionCounter(ctx, valAddr)
			if spamPreventionCounterHeight == curHeight {
				return sdkerrors.Wrap(sdkerrors.ErrAlreadyExists, fmt.Sprintf("the validator has already submitted a vote at the current height=%d", curHeight))
			}
			spd.oracleKeeper.SetSpamPreventionCounter(ctx, valAddr)
			continue
		default:
			return nil
		}
	}

	return nil
}

type VoteAloneDecorator struct{}

func NewOracleVoteAloneDecorator() VoteAloneDecorator {
	return VoteAloneDecorator{}
}

// AnteHandle handles msg tax fee checking
func (VoteAloneDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	oracleVote := false
	otherMsg := false
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *types.MsgAggregateExchangeRateVote:
			oracleVote = true

		default:
			otherMsg = true
		}
	}

	if oracleVote && otherMsg {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "oracle votes cannot be in the same tx as other messages")
	}

	return next(ctx, tx, simulate)
}

func (VoteAloneDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	// requires no dependencies
	return next(txDeps, tx, txIndex)
}
