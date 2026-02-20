package bank_test

import (
	"strings"
	"testing"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/bank"
	bankkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

func TestInvalidMsg(t *testing.T) {
	h := bank.NewHandler(nil)

	res, err := h(sdk.NewContext(nil, tmproto.Header{}, false, nil), testdata.NewTestMsg())
	require.Error(t, err)
	require.Nil(t, res)

	_, _, log := sdkerrors.ABCIInfo(err, false)
	require.True(t, strings.Contains(log, "unrecognized bank message type"))
}

// A module account cannot be the recipient of bank sends unless it has been marked as such
func TestSendToModuleAccount(t *testing.T) {
	priv1 := secp256k1.GenPrivKey()
	addr1 := sdk.AccAddress(priv1.PubKey().Address())
	moduleAccAddr := authtypes.NewModuleAddress(stakingtypes.BondedPoolName)
	coins := sdk.Coins{sdk.NewInt64Coin("foocoin", 10)}

	tests := []struct {
		name          string
		expectedError error
		msg           *types.MsgSend
	}{
		{
			name:          "not allowed module account",
			msg:           types.NewMsgSend(addr1, moduleAccAddr, coins),
			expectedError: sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "%s is not allowed to receive funds", moduleAccAddr),
		},
		{
			name:          "allowed module account",
			msg:           types.NewMsgSend(addr1, authtypes.NewModuleAddress(stakingtypes.ModuleName), coins),
			expectedError: nil,
		},
	}

	acc1 := &authtypes.BaseAccount{
		Address: addr1.String(),
	}
	accs := authtypes.GenesisAccounts{acc1}
	balances := []types.Balance{
		{
			Address: addr1.String(),
			Coins:   coins,
		},
	}

	app := seiapp.SetupWithGenesisAccounts(t, accs, balances...)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	app.BankKeeper = bankkeeper.NewBaseKeeper(
		app.AppCodec(), app.GetKey(types.StoreKey), app.AccountKeeper, app.GetSubspace(types.ModuleName), map[string]bool{
			moduleAccAddr.String(): true,
		},
	)
	handler := bank.NewHandler(app.BankKeeper)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := handler(ctx, tc.msg)
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}
