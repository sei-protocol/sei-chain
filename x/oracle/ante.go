package oracle

import (
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

// SpammingPreventionDecorator will check if the transaction's gas is smaller than
// configured hard cap
type SpammingPreventionDecorator struct {
	oracleKeeper     keeper.Keeper
	oraclePrevoteMap map[string]int64
	oracleVoteMap    map[string]int64
	mu               *sync.Mutex
}

// NewSpammingPreventionDecorator returns new spamming prevention decorator instance
func NewSpammingPreventionDecorator(oracleKeeper keeper.Keeper) SpammingPreventionDecorator {
	return SpammingPreventionDecorator{
		oracleKeeper:     oracleKeeper,
		oraclePrevoteMap: make(map[string]int64),
		oracleVoteMap:    make(map[string]int64),
		mu:               &sync.Mutex{},
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
	spd.mu.Lock()
	defer spd.mu.Unlock()

	curHeight := ctx.BlockHeight()
	for _, msg := range msgs {
		switch msg := msg.(type) {
		case *types.MsgAggregateExchangeRatePrevote:
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

			if lastSubmittedHeight, ok := spd.oraclePrevoteMap[msg.Validator]; ok && lastSubmittedHeight == curHeight {
				return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "the validator has already been submitted prevote at the current height")
			}

			spd.oraclePrevoteMap[msg.Validator] = curHeight
			continue
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

			if lastSubmittedHeight, ok := spd.oracleVoteMap[msg.Validator]; ok && lastSubmittedHeight == curHeight {
				return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "the validator has already been submitted vote at the current height")
			}

			spd.oracleVoteMap[msg.Validator] = curHeight
			continue
		case *types.MsgAggregateExchangeRateCombinedVote:
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

			if lastSubmittedHeight, ok := spd.oracleVoteMap[msg.Validator]; ok && lastSubmittedHeight == curHeight {
				return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "the validator has already been submitted vote at the current height")
			}

			spd.oracleVoteMap[msg.Validator] = curHeight
			continue
		default:
			return nil
		}
	}

	return nil
}
