package testutil

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func MessageDependencyGeneratorTestHelper() aclkeeper.DependencyGeneratorMap {
	return aclkeeper.DependencyGeneratorMap{
		types.GenerateMessageKey(&banktypes.MsgSend{}):        BankSendDepGenerator,
		types.GenerateMessageKey(&stakingtypes.MsgDelegate{}): StakingDelegateDepGenerator,
	}
}

func BankSendDepGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]acltypes.AccessOperation, error) {
	bankSend, ok := msg.(*banktypes.MsgSend)
	if !ok {
		return []acltypes.AccessOperation{}, fmt.Errorf("invalid message received for BankMsgSend")
	}
	accessOps := []acltypes.AccessOperation{
		{ResourceType: acltypes.ResourceType_KV_BANK_BALANCES, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: bankSend.FromAddress},
		{ResourceType: acltypes.ResourceType_KV_BANK_BALANCES, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: bankSend.ToAddress},
		*types.CommitAccessOp(),
	}
	return accessOps, nil
}

// this is intentionally missing a commit so it fails validation
func StakingDelegateDepGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]acltypes.AccessOperation, error) {
	stakingDelegate, ok := msg.(*stakingtypes.MsgDelegate)
	if !ok {
		return []acltypes.AccessOperation{}, fmt.Errorf("invalid message received for StakingDelegate")
	}
	accessOps := []acltypes.AccessOperation{
		{ResourceType: acltypes.ResourceType_KV_STAKING, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: stakingDelegate.DelegatorAddress},
	}
	return accessOps, nil
}
