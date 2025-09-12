package evm_test

import (
	"testing"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func TestX402PaymentTrigger(t *testing.T) {
	app, ctx := app.Setup(false)

	delegator := sdk.AccAddress([]byte("delegator_______"))
	validator := sdk.ValAddress([]byte("validator_______"))

	// Create MsgServer for staking
	stakingKeeper := app.StakingKeeper
	msgServer := stakingtypes.NewMsgServerImpl(stakingKeeper)

	// Construct a delegation message
	msg := &stakingtypes.MsgDelegate{
		DelegatorAddress: delegator.String(),
		ValidatorAddress: validator.String(),
		Amount:           sdk.NewCoin("usei", sdk.NewInt(1000000)),
	}

	// Trigger the x402 payment event
	_, err := msgServer.Delegate(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)

	// Print emitted events to validate x402 trigger
	for _, evt := range ctx.EventManager().Events() {
		t.Log("Event:", evt.Type)
		for _, attr := range evt.Attributes {
			t.Logf("  %s = %s", attr.Key, attr.Value)
		}
	}
}
