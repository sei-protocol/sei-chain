package simulation

// DONTCOVER

import (
	"math/rand"
	"strings"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/simapp/helpers"
	simappparams "github.com/cosmos/cosmos-sdk/simapp/params"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
)

// Simulation operation weights constants
const (
	OpWeightMsgAggregateExchangeRatePrevote = "op_weight_msg_exchange_rate_aggregate_prevote"
	OpWeightMsgAggregateExchangeRateVote    = "op_weight_msg_exchange_rate_aggregate_vote"
	OpWeightMsgDelegateFeedConsent          = "op_weight_msg_exchange_feed_consent"

	salt = "1234"
)

var (
	whitelist                     = []string{utils.MicroAtomDenom}
	voteHashMap map[string]string = make(map[string]string)
)

// WeightedOperations returns all the operations from the module with their respective weights
func WeightedOperations(
	appParams simtypes.AppParams,
	cdc codec.JSONCodec,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
) simulation.WeightedOperations {
	var (
		weightMsgAggregateExchangeRatePrevote int
		weightMsgAggregateExchangeRateVote    int
		weightMsgDelegateFeedConsent          int
	)
	appParams.GetOrGenerate(cdc, OpWeightMsgAggregateExchangeRatePrevote, &weightMsgAggregateExchangeRatePrevote, nil,
		func(_ *rand.Rand) {
			weightMsgAggregateExchangeRatePrevote = simappparams.DefaultWeightMsgSend * 2
		},
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
			weightMsgAggregateExchangeRatePrevote,
			SimulateMsgAggregateExchangeRatePrevote(ak, bk, k),
		),
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

// SimulateMsgAggregateExchangeRatePrevote generates a MsgAggregateExchangeRatePrevote with random values.
// nolint: funlen
func SimulateMsgAggregateExchangeRatePrevote(ak types.AccountKeeper, bk types.BankKeeper, k keeper.Keeper) simtypes.Operation {
	return func(
		r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		address := sdk.ValAddress(simAccount.Address)

		// ensure the validator exists
		val := k.StakingKeeper.Validator(ctx, address)
		if val == nil || !val.IsBonded() {
			return simtypes.NoOpMsg(types.ModuleName, types.TypeMsgAggregateExchangeRatePrevote, "unable to find validator"), nil, nil
		}

		exchangeRatesStr := ""
		for _, denom := range whitelist {
			price := sdk.NewDecWithPrec(int64(simtypes.RandIntBetween(r, 1, 10000)), int64(1))
			exchangeRatesStr += price.String() + denom + ","
		}

		exchangeRatesStr = strings.TrimRight(exchangeRatesStr, ",")
		voteHash := types.GetAggregateVoteHash(salt, exchangeRatesStr, address)

		feederAddr := k.GetFeederDelegation(ctx, address)
		feederSimAccount, _ := simtypes.FindAccount(accs, feederAddr)

		feederAccount := ak.GetAccount(ctx, feederAddr)
		spendable := bk.SpendableCoins(ctx, feederAccount.GetAddress())

		fees, err := simtypes.RandomFees(r, ctx, spendable)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, types.TypeMsgAggregateExchangeRatePrevote, "unable to generate fees"), nil, err
		}

		msg := types.NewMsgAggregateExchangeRatePrevote(voteHash, feederAddr, address)

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

		voteHashMap[address.String()] = exchangeRatesStr

		return simtypes.NewOperationMsg(msg, true, "", nil), nil, nil
	}
}

// SimulateMsgAggregateExchangeRateVote generates a MsgAggregateExchangeRateVote with random values.
// nolint: funlen
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

		// get prevote
		_, err := k.GetAggregateExchangeRatePrevote(ctx, address)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, types.TypeMsgAggregateExchangeRateVote, "prevote not found"), nil, nil
		}

		if !k.IsPrevoteFromPreviousWindow(ctx, address) {
			return simtypes.NoOpMsg(types.ModuleName, types.TypeMsgAggregateExchangeRateVote, "reveal period of submitted vote do not match with registered prevote"), nil, nil
		}

		feederAddr := k.GetFeederDelegation(ctx, address)
		feederSimAccount, _ := simtypes.FindAccount(accs, feederAddr)
		feederAccount := ak.GetAccount(ctx, feederAddr)
		spendableCoins := bk.SpendableCoins(ctx, feederAddr)

		fees, err := simtypes.RandomFees(r, ctx, spendableCoins)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, types.TypeMsgAggregateExchangeRateVote, "unable to generate fees"), nil, err
		}

		msg := types.NewMsgAggregateExchangeRateVote(salt, exchangeRatesStr, feederAddr, address)

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
// nolint: funlen
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
