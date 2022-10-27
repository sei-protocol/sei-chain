package aclstakingmapping

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/sei-protocol/sei-chain/app"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestGeneratorInvalidMessageTypes(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub)

	stakingDelegate := stakingtypes.MsgDelegate{
		DelegatorAddress: "delegator",
		ValidatorAddress: "validator",
		Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(5)},
	}
	oracleVote := oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1usei",
		Feeder:        "test",
		Validator:     "validator",
	}

	_, err := MsgDelegateDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &oracleVote)
	require.Error(t, err)
	_, err = MsgUndelegateDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &stakingDelegate)
	require.Error(t, err)
	_, err = MsgBeginRedelegateDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &stakingDelegate)
	require.Error(t, err)

}

func TestMsgDelegateGenerator(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub)

	stakingDelegate := stakingtypes.MsgDelegate{
		DelegatorAddress: "delegator",
		ValidatorAddress: "validator",
		Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(5)},
	}

	accessOps, err := MsgDelegateDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &stakingDelegate)
	require.NoError(t, err)
	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(t, err)
}

func TestMsgUndelegateGenerator(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)

	stakingUndelegate := stakingtypes.MsgUndelegate{
		DelegatorAddress: "delegator",
		ValidatorAddress: "validator",
		Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(5)},
	}

	accessOps, err := MsgUndelegateDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &stakingUndelegate)
	require.NoError(t, err)
	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(t, err)
}

func TestMsgBeginRedelegateGenerator(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)

	stakingBeginRedelegate := stakingtypes.MsgBeginRedelegate{
		DelegatorAddress:    "delegator",
		ValidatorSrcAddress: "src_validator",
		ValidatorDstAddress: "dst_validator",
		Amount:              sdk.Coin{Denom: "usei", Amount: sdk.NewInt(5)},
	}

	accessOps, err := MsgBeginRedelegateDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &stakingBeginRedelegate)
	require.NoError(t, err)
	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(t, err)
}
