package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	store "github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/x/seinet/keeper"
	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

func setupKeeper(t *testing.T) (*keeper.Keeper, sdk.Context, *mockBankKeeper) {
	t.Helper()

	storeKey := sdk.NewKVStoreKey(types.StoreKey)

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, sdk.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())

	registry := codectypes.NewInterfaceRegistry()
	types.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	bankKeeper := &mockBankKeeper{}
	accountKeeper := mockAccountKeeper{}

	k := keeper.NewKeeper(cdc, storeKey, bankKeeper, accountKeeper)

	return &k, ctx, bankKeeper
}

type mockBankKeeper struct {
	accountToModuleTransfers []accountToModuleTransfer
	moduleToAccountTransfers []moduleToAccountTransfer
}

type accountToModuleTransfer struct {
	sender sdk.AccAddress
	module string
	amount sdk.Coins
}

type moduleToAccountTransfer struct {
	module    string
	recipient sdk.AccAddress
	amount    sdk.Coins
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(_ sdk.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error {
	m.accountToModuleTransfers = append(m.accountToModuleTransfers, accountToModuleTransfer{
		sender: sender,
		module: module,
		amount: amt,
	})
	return nil
}

func (m *mockBankKeeper) SendCoinsFromModuleToAccount(_ sdk.Context, module string, recipient sdk.AccAddress, amt sdk.Coins) error {
	m.moduleToAccountTransfers = append(m.moduleToAccountTransfers, moduleToAccountTransfer{
		module:    module,
		recipient: recipient,
		amount:    amt,
	})
	return nil
}

func (m *mockBankKeeper) GetAllBalances(_ sdk.Context, _ sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins()
}

type mockAccountKeeper struct{}

func (mockAccountKeeper) GetModuleAddress(moduleName string) sdk.AccAddress {
	return authtypes.NewModuleAddress(moduleName)
}

func TestDepositToVault(t *testing.T) {
	k, ctx, bankKeeper := setupKeeper(t)
	srv := keeper.NewMsgServerImpl(*k)

	depositor := sdk.AccAddress([]byte("addr1---------------"))
	msg := types.NewMsgDepositToVault(
		depositor.String(),
		"100usei",
	)

	_, err := srv.DepositToVault(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)

	require.Len(t, bankKeeper.accountToModuleTransfers, 1)
	transfer := bankKeeper.accountToModuleTransfers[0]
	require.Equal(t, depositor, transfer.sender)
	require.Equal(t, types.SeinetVaultAccount, transfer.module)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("usei", 100)), transfer.amount)
}

func TestExecutePaywordSettlement(t *testing.T) {
	k, ctx, _ := setupKeeper(t)
	srv := keeper.NewMsgServerImpl(*k)

	executor := sdk.AccAddress([]byte("addr2---------------"))
	recipient := sdk.AccAddress([]byte("addr3---------------"))

	msg := types.NewMsgExecutePaywordSettlement(
		executor.String(),
		recipient.String(),
		"testpayword",
		"abcd1234",
		"50usei",
	)

	_, err := srv.ExecutePaywordSettlement(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
}
