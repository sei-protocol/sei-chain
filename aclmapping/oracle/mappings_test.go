package acloraclemapping

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/sei-protocol/sei-chain/app"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestMsgVoteDependencyGenerator(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)

	oracleVote := oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1usei",
		Feeder:        "test",
		Validator:     "validator",
	}

	accessOps, err := MsgVoteDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &oracleVote)
	require.NoError(t, err)
	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(t, err)
}
