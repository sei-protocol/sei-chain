package oracle

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
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

// CheckOracleSpamming check whether the msgs are spamming purpose or not
func (spd SpammingPreventionDecorator) CheckOracleSpamming(ctx sdk.Context, msgs []sdk.Msg) error {
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

			if err := spd.oracleKeeper.CheckAndSetSpamPreventionCounter(ctx, valAddr); err != nil {
				return err
			}
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
		case *types.MsgDelegateFeedConsent:
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
