package oracle_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper/testutils"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
)

func TestOracleFilters(t *testing.T) {
	input, h := setup(t)

	// Case 1: non-oracle message being sent fails
	bankMsg := banktypes.MsgSend{}
	_, err := h(input.Ctx, &bankMsg)
	require.Error(t, err)

	// // Case 2: Normal MsgAggregateExchangeRateVote submission goes through keeper.Addrs
	voteMsg := types.NewMsgAggregateExchangeRateVote(randomExchangeRate.String()+utils.MicroAtomDenom, testutils.Addrs[0], testutils.ValAddrs[0])
	_, err = h(input.Ctx.WithBlockHeight(1), voteMsg)
	require.NoError(t, err)

	// Case 3: a non-validator sending an oracle message fails
	nonValidatorPub := secp256k1.GenPrivKey().PubKey()
	nonValidatorAddr := nonValidatorPub.Address()
	voteMsg = types.NewMsgAggregateExchangeRateVote(randomExchangeRate.String()+utils.MicroAtomDenom, sdk.AccAddress(nonValidatorAddr), sdk.ValAddress(nonValidatorAddr))
	_, err = h(input.Ctx, voteMsg)
	require.Error(t, err)
}

func TestFeederDelegation(t *testing.T) {
	input, h := setup(t)
	// Case 1: empty message
	delegateFeedConsentMsg := types.MsgDelegateFeedConsent{}
	_, err := h(input.Ctx, &delegateFeedConsentMsg)
	require.Error(t, err)

	// Case 2.1: Normal Vote - without delegation
	voteMsg := types.NewMsgAggregateExchangeRateVote(randomExchangeRate.String()+utils.MicroAtomDenom, testutils.Addrs[0], testutils.ValAddrs[0])
	_, err = h(input.Ctx.WithBlockHeight(1), voteMsg)
	require.NoError(t, err)

	// Case 2.2: Normal Vote - with delegation fails
	voteMsg = types.NewMsgAggregateExchangeRateVote(randomExchangeRate.String()+utils.MicroAtomDenom, testutils.Addrs[1], testutils.ValAddrs[0])
	_, err = h(input.Ctx.WithBlockHeight(1), voteMsg)
	require.Error(t, err)

	// Case 3: Normal MsgDelegateFeedConsent succeeds
	msg := types.NewMsgDelegateFeedConsent(testutils.ValAddrs[0], testutils.Addrs[1])
	_, err = h(input.Ctx, msg)
	require.NoError(t, err)

	// Case 4.1: Normal Vote - without delegation fails
	voteMsg = types.NewMsgAggregateExchangeRateVote(randomExchangeRate.String()+utils.MicroAtomDenom, testutils.Addrs[2], testutils.ValAddrs[0])
	_, err = h(input.Ctx.WithBlockHeight(1), voteMsg)
	require.Error(t, err)

	// Case 4.2: Normal Vote - with delegation succeeds
	voteMsg = types.NewMsgAggregateExchangeRateVote(randomExchangeRate.String()+utils.MicroAtomDenom, testutils.Addrs[1], testutils.ValAddrs[0])
	_, err = h(input.Ctx.WithBlockHeight(1), voteMsg)
	require.NoError(t, err)
}
