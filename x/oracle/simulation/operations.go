package simulation

// DONTCOVER

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/simapp/helpers"
	simappparams "github.com/cosmos/cosmos-sdk/simapp/params"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

// Simulation operation weights constants
//
//nolint:gosec
const (
	OpWeightMsgAggregateExchangeRateVote = "op_weight_msg_exchange_rate_aggregate_vote"
	OpWeightMsgDelegateFeedConsent       = "op_weight_msg_exchange_feed_consent"
)

var voteHashMap = make(map[string]string)

// WeightedOperations returns all the operations from the module with their respective weights
func WeightedOperations(
	appParams simtypes.AppParams,
	cdc codec.JSONCodec,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
) simulation.WeightedOperations {
	var (
		weightMsgAggregateExchangeRateVote int
		weightMsgDelegateFeedConsent       int
	)

	appParams.GetOrGenerate(cdc, OpWeightMsgAggregateExchangeRateVote, &weightMsgAggregateExchangeRateVote, nil,
		func(_ *rand.Rand) {
			weightMsgAggregateExchangeRateVote = simappparams.DefaultWeightMsgSend * 2
		},
	)

	appParams.GetOrGenerate(cdc, OpWeightMsgDelegateFeedConsent, &weightMsgDelegateFeedConsent, nil,
		func(_ *rand.Rand) {
			weightMsgDelegateFeedConsent = simappparams.DefaultWeightMsgSetWithdrawAddress
		},
	)

	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(
			weightMsgAggregateExchangeRateVote,
			SimulateMsgAggregateExchangeRateVote(ak, bk, k),
		),
		simulation.NewWeightedOperation(
			weightMsgDelegateFeedConsent,
			SimulateMsgDelegateFeedConsent(ak, bk, k),
		),
	}
}

// SimulateMsgAggregateExchangeRateVote generates a MsgAggregateExchangeRateVote with random values.
//nolint: funlen
func SimulateMsgAggregateExchangeRateVote(ak types.AccountKeeper, bk types.BankKeeper, k keeper.Keeper) simtypes.Operation {
	return func(
		r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		address := sdk.ValAddress(simAccount.Address)

		// ensure the validator exists
		val := k.StakingKeeper.Validator(ctx, address)
		if val == nil || !val.IsBonded() {
			return simtypes.NoOpMsg(types.ModuleName, types.TypeMsgAggregateExchangeRateVote, "unable to find validator"), nil, nil
		}

		// ensure vote hash exists
		exchangeRatesStr, ok := voteHashMap[address.String()]
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, types.TypeMsgAggregateExchangeRateVote, "vote hash not exists"), nil, nil
		}

		feederAddr := k.GetFeederDelegation(ctx, address)
		feederSimAccount, _ := simtypes.FindAccount(accs, feederAddr)
		feederAccount := ak.GetAccount(ctx, feederAddr)
		spendableCoins := bk.SpendableCoins(ctx, feederAddr)

		fees, err := simtypes.RandomFees(r, ctx, spendableCoins)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, types.TypeMsgAggregateExchangeRateVote, "unable to generate fees"), nil, err
		}

		msg := types.NewMsgAggregateExchangeRateVote(exchangeRatesStr, feederAddr, address)

		txGen := simappparams.MakeTestEncodingConfig().TxConfig
		tx, err := helpers.GenTx(
			txGen,
			[]sdk.Msg{msg},
			fees,
			helpers.DefaultGenTxGas,
			chainID,
			[]uint64{feederAccount.GetAccountNumber()},
			[]uint64{feederAccount.GetSequence()},
			feederSimAccount.PrivKey,
		)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msg.Type(), "unable to generate mock tx"), nil, err
		}

		_, _, err = app.Deliver(txGen.TxEncoder(), tx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msg.Type(), "unable to deliver tx"), nil, err
		}

		return simtypes.NewOperationMsg(msg, true, "", nil), nil, nil
	}
}

// SimulateMsgDelegateFeedConsent generates a MsgDelegateFeedConsent with random values.
//nolint: funlen
func SimulateMsgDelegateFeedConsent(ak types.AccountKeeper, bk types.BankKeeper, k keeper.Keeper) simtypes.Operation {
	return func(
		r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		delegateAccount, _ := simtypes.RandomAcc(r, accs)
		valAddress := sdk.ValAddress(simAccount.Address)
		delegateValAddress := sdk.ValAddress(delegateAccount.Address)
		account := ak.GetAccount(ctx, simAccount.Address)

		// ensure the validator exists
		val := k.StakingKeeper.Validator(ctx, valAddress)
		if val == nil {
			return simtypes.NoOpMsg(types.ModuleName, types.TypeMsgDelegateFeedConsent, "unable to find validator"), nil, nil
		}

		// ensure the target address is not a validator
		val2 := k.StakingKeeper.Validator(ctx, delegateValAddress)
		if val2 != nil {
			return simtypes.NoOpMsg(types.ModuleName, types.TypeMsgDelegateFeedConsent, "unable to delegate to validator"), nil, nil
		}

		spendableCoins := bk.SpendableCoins(ctx, account.GetAddress())
		fees, err := simtypes.RandomFees(r, ctx, spendableCoins)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, types.TypeMsgAggregateExchangeRateVote, "unable to generate fees"), nil, err
		}

		msg := types.NewMsgDelegateFeedConsent(valAddress, delegateAccount.Address)

		txGen := simappparams.MakeTestEncodingConfig().TxConfig
		tx, err := helpers.GenTx(
			txGen,
			[]sdk.Msg{msg},
			fees,
			helpers.DefaultGenTxGas,
			chainID,
			[]uint64{account.GetAccountNumber()},
			[]uint64{account.GetSequence()},
			simAccount.PrivKey,
		)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msg.Type(), "unable to generate mock tx"), nil, err
		}

		_, _, err = app.Deliver(txGen.TxEncoder(), tx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msg.Type(), "unable to deliver tx"), nil, err
		}

		return simtypes.NewOperationMsg(msg, true, "", nil), nil, nil
	}
}
