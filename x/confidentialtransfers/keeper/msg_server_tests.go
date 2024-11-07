package keeper

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/keeper"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/stretchr/testify/require"
)

func TestMsgServer_InitializeAccount(t *testing.T) {
	k, ctx := testkeeper.ConfidentialTransfersKeeper(t)
	msgServer := keeper.NewMsgServerImpl(*k)
	address := sdk.AccAddress("address1").String()
	denom := "testdenom"

	// Test valid request
	req := &types.MsgInitializeAccount{
		FromAddress: address,
		Denom:       denom,
		// Add other required fields
	}
	_, err := msgServer.InitializeAccount(sdk.WrapSDKContext(ctx), req)
	require.NoError(t, err)

	// Test invalid address
	req = &types.MsgInitializeAccount{
		FromAddress: "invalid_address",
		Denom:       denom,
		// Add other required fields
	}
	_, err = msgServer.InitializeAccount(sdk.WrapSDKContext(ctx), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), sdkerrors.ErrInvalidAddress.Error())
}

func TestMsgServer_Deposit(t *testing.T) {
	k, ctx := testkeeper.ConfidentialTransfersKeeper(t)
	msgServer := keeper.NewMsgServerImpl(*k)
	address := sdk.AccAddress("address1").String()
	denom := "testdenom"

	// Test valid request
	req := &types.MsgDeposit{
		FromAddress: address,
		Denom:       denom,
		Amount:      100,
	}
	_, err := msgServer.Deposit(sdk.WrapSDKContext(ctx), req)
	require.NoError(t, err)

	// Test invalid address
	req = &types.MsgDeposit{
		FromAddress: "invalid_address",
		Denom:       denom,
		Amount:      100,
	}
	_, err = msgServer.Deposit(sdk.WrapSDKContext(ctx), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), sdkerrors.ErrInvalidAddress.Error())
}

func TestMsgServer_Withdraw(t *testing.T) {
	k, ctx := testkeeper.ConfidentialTransfersKeeper(t)
	msgServer := keeper.NewMsgServerImpl(*k)
	address := sdk.AccAddress("address1").String()
	denom := "testdenom"

	// Test valid request
	req := &types.MsgWithdraw{
		FromAddress: address,
		Denom:       denom,
		Amount:      100,
		// Add other required fields
	}
	_, err := msgServer.Withdraw(sdk.WrapSDKContext(ctx), req)
	require.NoError(t, err)

	// Test invalid address
	req = &types.MsgWithdraw{
		FromAddress: "invalid_address",
		Denom:       denom,
		Amount:      100,
		// Add other required fields
	}
	_, err = msgServer.Withdraw(sdk.WrapSDKContext(ctx), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), sdkerrors.ErrInvalidAddress.Error())
}

func TestMsgServer_ApplyPendingBalance(t *testing.T) {
	k, ctx := testkeeper.ConfidentialTransfersKeeper(t)
	msgServer := keeper.NewMsgServerImpl(*k)
	address := sdk.AccAddress("address1").String()
	denom := "testdenom"

	// Test valid request
	req := &types.MsgApplyPendingBalance{
		Address: address,
		Denom:   denom,
		// Add other required fields
	}
	_, err := msgServer.ApplyPendingBalance(sdk.WrapSDKContext(ctx), req)
	require.NoError(t, err)

	// Test invalid address
	req = &types.MsgApplyPendingBalance{
		Address: "invalid_address",
		Denom:   denom,
		// Add other required fields
	}
	_, err = msgServer.ApplyPendingBalance(sdk.WrapSDKContext(ctx), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), sdkerrors.ErrInvalidAddress.Error())
}

func TestMsgServer_CloseAccount(t *testing.T) {
	k, ctx := testkeeper.ConfidentialTransfersKeeper(t)
	msgServer := keeper.NewMsgServerImpl(*k)
	address := sdk.AccAddress("address1").String()
	denom := "testdenom"

	// Test valid request
	req := &types.MsgCloseAccount{
		Address: address,
		Denom:   denom,
		// Add other required fields
	}
	_, err := msgServer.CloseAccount(sdk.WrapSDKContext(ctx), req)
	require.NoError(t, err)

	// Test invalid address
	req = &types.MsgCloseAccount{
		Address: "invalid_address",
		Denom:   denom,
		// Add other required fields
	}
	_, err = msgServer.CloseAccount(sdk.WrapSDKContext(ctx), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), sdkerrors.ErrInvalidAddress.Error())
}

func TestMsgServer_Transfer(t *testing.T) {
	k, ctx := testkeeper.ConfidentialTransfersKeeper(t)
	msgServer := keeper.NewMsgServerImpl(*k)
	fromAddress := sdk.AccAddress("address1").String()
	toAddress := sdk.AccAddress("address2").String()
	denom := "testdenom"

	// Test valid request
	req := &types.MsgTransfer{
		FromAddress: fromAddress,
		ToAddress:   toAddress,
		Denom:       denom,
		// Add other required fields
	}
	_, err := msgServer.Transfer(sdk.WrapSDKContext(ctx), req)
	require.NoError(t, err)

	// Test invalid from address
	req = &types.MsgTransfer{
		FromAddress: "invalid_address",
		ToAddress:   toAddress,
		Denom:       denom,
		// Add other required fields
	}
	_, err = msgServer.Transfer(sdk.WrapSDKContext(ctx), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), sdkerrors.ErrInvalidAddress.Error())

	// Test invalid to address
	req = &types.MsgTransfer{
		FromAddress: fromAddress,
		ToAddress:   "invalid_address",
		Denom:       denom,
		// Add other required fields
	}
	_, err = msgServer.Transfer(sdk.WrapSDKContext(ctx), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), sdkerrors.ErrInvalidAddress.Error())
}
