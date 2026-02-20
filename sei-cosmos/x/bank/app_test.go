package bank_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
)

type (
	expectedBalance struct {
		addr  sdk.AccAddress
		coins sdk.Coins
	}

	appTestCase struct {
		desc             string
		expSimPass       bool
		expPass          bool
		msgs             []sdk.Msg
		accNums          []uint64
		accSeqs          []uint64
		privKeys         []cryptotypes.PrivKey
		expectedBalances []expectedBalance
	}
)

var (
	priv1 = secp256k1.GenPrivKey()
	addr1 = sdk.AccAddress(priv1.PubKey().Address())
	priv2 = secp256k1.GenPrivKey()
	addr2 = sdk.AccAddress(priv2.PubKey().Address())
	addr3 = sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	priv4 = secp256k1.GenPrivKey()
	addr4 = sdk.AccAddress(priv4.PubKey().Address())

	coins     = sdk.Coins{sdk.NewInt64Coin("foocoin", 10)}
	halfCoins = sdk.Coins{sdk.NewInt64Coin("foocoin", 5)}

	sendMsg1 = types.NewMsgSend(addr1, addr2, coins)

	multiSendMsg1 = &types.MsgMultiSend{
		Inputs:  []types.Input{types.NewInput(addr1, coins)},
		Outputs: []types.Output{types.NewOutput(addr2, coins)},
	}
	multiSendMsg2 = &types.MsgMultiSend{
		Inputs: []types.Input{types.NewInput(addr1, coins)},
		Outputs: []types.Output{
			types.NewOutput(addr2, halfCoins),
			types.NewOutput(addr3, halfCoins),
		},
	}
	multiSendMsg3 = &types.MsgMultiSend{
		Inputs: []types.Input{
			types.NewInput(addr1, coins),
			types.NewInput(addr4, coins),
		},
		Outputs: []types.Output{
			types.NewOutput(addr2, coins),
			types.NewOutput(addr3, coins),
		},
	}
	multiSendMsg4 = &types.MsgMultiSend{
		Inputs: []types.Input{
			types.NewInput(addr2, coins),
		},
		Outputs: []types.Output{
			types.NewOutput(addr1, coins),
		},
	}
)

func TestSendNotEnoughBalance(t *testing.T) {
	acc := &authtypes.BaseAccount{
		Address: addr1.String(),
	}

	genAccs := []authtypes.GenesisAccount{acc}
	a := app.SetupWithGenesisAccounts(t, genAccs)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr1, sdk.NewCoins(sdk.NewInt64Coin("foocoin", 67))))

	a.Commit(context.Background())

	res1 := a.AccountKeeper.GetAccount(ctx, addr1)
	require.NotNil(t, res1)
	require.Equal(t, acc, res1.(*authtypes.BaseAccount))

	origAccNum := res1.GetAccountNumber()
	origSeq := res1.GetSequence()

	sendMsg := types.NewMsgSend(addr1, addr2, sdk.Coins{sdk.NewInt64Coin("foocoin", 100)})
	header := tmproto.Header{Height: a.LastBlockHeight() + 1}
	txGen := app.MakeEncodingConfig().TxConfig
	_, _, err := app.SignCheckDeliver(t, txGen, a.BaseApp, header, []sdk.Msg{sendMsg}, "", []uint64{origAccNum}, []uint64{origSeq}, false, false, priv1)
	require.Error(t, err)

	app.CheckBalance(t, a, addr1, sdk.Coins{sdk.NewInt64Coin("foocoin", 67)})

	res2 := a.AccountKeeper.GetAccount(a.NewContext(true, tmproto.Header{}), addr1)
	require.NotNil(t, res2)

	require.Equal(t, res2.GetAccountNumber(), origAccNum)
	require.Equal(t, res2.GetSequence(), origSeq+1)
}

func TestSendReceiverNotInAllowList(t *testing.T) {
	acc := &authtypes.BaseAccount{
		Address: addr1.String(),
	}

	genAccs := []authtypes.GenesisAccount{acc}
	a := app.SetupWithGenesisAccounts(t, genAccs)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})
	testDenom := "testDenom"
	factoryDenom := fmt.Sprintf("factory/%s/%s", addr1.String(), testDenom)

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr1, sdk.NewCoins(sdk.NewInt64Coin(factoryDenom, 100))))
	a.BankKeeper.SetDenomAllowList(ctx, factoryDenom,
		types.AllowList{Addresses: []string{addr1.String()}})

	a.Commit(context.Background())

	res1 := a.AccountKeeper.GetAccount(ctx, addr1)
	require.NotNil(t, res1)
	require.Equal(t, acc, res1.(*authtypes.BaseAccount))

	origAccNum := res1.GetAccountNumber()
	origSeq := res1.GetSequence()

	sendMsg := types.NewMsgSend(addr1, addr2, sdk.Coins{sdk.NewInt64Coin(factoryDenom, 10)})
	header := tmproto.Header{Height: a.LastBlockHeight() + 1}
	txGen := app.MakeEncodingConfig().TxConfig
	_, _, err := app.SignCheckDeliver(t, txGen, a.BaseApp, header, []sdk.Msg{sendMsg}, "", []uint64{origAccNum}, []uint64{origSeq}, false, false, priv1)
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("%s is not allowed to receive funds", addr2))

	app.CheckBalance(t, a, addr1, sdk.Coins{sdk.NewInt64Coin(factoryDenom, 100)})
}

func TestSendSenderAndReceiverInAllowList(t *testing.T) {
	acc := &authtypes.BaseAccount{
		Address: addr1.String(),
	}

	genAccs := []authtypes.GenesisAccount{acc}
	a := app.SetupWithGenesisAccounts(t, genAccs)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})
	testDenom := "testDenom"
	factoryDenom := fmt.Sprintf("factory/%s/%s", addr1.String(), testDenom)

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr1, sdk.NewCoins(sdk.NewInt64Coin(factoryDenom, 100))))
	a.BankKeeper.SetDenomAllowList(ctx, factoryDenom,
		types.AllowList{Addresses: []string{addr1.String(), addr2.String()}})

	a.Commit(context.Background())

	res1 := a.AccountKeeper.GetAccount(ctx, addr1)
	require.NotNil(t, res1)
	require.Equal(t, acc, res1.(*authtypes.BaseAccount))

	origAccNum := res1.GetAccountNumber()
	origSeq := res1.GetSequence()

	sendMsg := types.NewMsgSend(addr1, addr2, sdk.Coins{sdk.NewInt64Coin(factoryDenom, 10)})
	header := tmproto.Header{Height: a.LastBlockHeight() + 1}
	txGen := app.MakeEncodingConfig().TxConfig
	_, _, err := app.SignCheckDeliver(t, txGen, a.BaseApp, header, []sdk.Msg{sendMsg}, "", []uint64{origAccNum}, []uint64{origSeq}, true, true, priv1)
	require.NoError(t, err)

	app.CheckBalance(t, a, addr1, sdk.Coins{sdk.NewInt64Coin(factoryDenom, 90)})
	app.CheckBalance(t, a, addr2, sdk.Coins{sdk.NewInt64Coin(factoryDenom, 10)})
}

func TestSendWithEmptyAllowList(t *testing.T) {
	acc := &authtypes.BaseAccount{
		Address: addr1.String(),
	}

	genAccs := []authtypes.GenesisAccount{acc}
	a := app.SetupWithGenesisAccounts(t, genAccs)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})
	testDenom := "testDenom"
	factoryDenom := fmt.Sprintf("factory/%s/%s", addr1.String(), testDenom)

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr1, sdk.NewCoins(sdk.NewInt64Coin(factoryDenom, 100))))
	a.BankKeeper.SetDenomAllowList(ctx, factoryDenom,
		types.AllowList{Addresses: []string{}})

	a.Commit(context.Background())

	res1 := a.AccountKeeper.GetAccount(ctx, addr1)
	require.NotNil(t, res1)
	require.Equal(t, acc, res1.(*authtypes.BaseAccount))

	origAccNum := res1.GetAccountNumber()
	origSeq := res1.GetSequence()

	sendMsg := types.NewMsgSend(addr1, addr2, sdk.Coins{sdk.NewInt64Coin(factoryDenom, 10)})
	header := tmproto.Header{Height: a.LastBlockHeight() + 1}
	txGen := app.MakeEncodingConfig().TxConfig
	_, _, err := app.SignCheckDeliver(t, txGen, a.BaseApp, header, []sdk.Msg{sendMsg}, "", []uint64{origAccNum}, []uint64{origSeq}, true, true, priv1)
	require.NoError(t, err)

	app.CheckBalance(t, a, addr1, sdk.Coins{sdk.NewInt64Coin(factoryDenom, 90)})
	app.CheckBalance(t, a, addr2, sdk.Coins{sdk.NewInt64Coin(factoryDenom, 10)})
}

func TestSendSenderNotInAllowList(t *testing.T) {
	acc := &authtypes.BaseAccount{
		Address: addr1.String(),
	}

	genAccs := []authtypes.GenesisAccount{acc}
	a := app.SetupWithGenesisAccounts(t, genAccs)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})
	testDenom := "testDenom"
	factoryDenom := fmt.Sprintf("factory/%s/%s", addr1.String(), testDenom)

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr1, sdk.NewCoins(sdk.NewInt64Coin(factoryDenom, 100))))
	a.BankKeeper.SetDenomAllowList(ctx, factoryDenom,
		types.AllowList{Addresses: []string{addr2.String()}})

	a.Commit(context.Background())

	res1 := a.AccountKeeper.GetAccount(ctx, addr1)
	require.NotNil(t, res1)
	require.Equal(t, acc, res1.(*authtypes.BaseAccount))

	origAccNum := res1.GetAccountNumber()
	origSeq := res1.GetSequence()

	sendMsg := types.NewMsgSend(addr1, addr2, sdk.Coins{sdk.NewInt64Coin(factoryDenom, 10)})
	header := tmproto.Header{Height: a.LastBlockHeight() + 1}
	txGen := app.MakeEncodingConfig().TxConfig
	_, _, err := app.SignCheckDeliver(t, txGen, a.BaseApp, header, []sdk.Msg{sendMsg}, "", []uint64{origAccNum}, []uint64{origSeq}, false, false, priv1)
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("%s is not allowed to send funds", addr1))

	app.CheckBalance(t, a, addr1, sdk.Coins{sdk.NewInt64Coin(factoryDenom, 100)})
}

func TestMsgMultiSendWithAccounts(t *testing.T) {
	acc := &authtypes.BaseAccount{
		Address: addr1.String(),
	}

	genAccs := []authtypes.GenesisAccount{acc}
	a := app.SetupWithGenesisAccounts(t, genAccs)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr1, sdk.NewCoins(sdk.NewInt64Coin("foocoin", 67))))

	a.Commit(context.Background())

	res1 := a.AccountKeeper.GetAccount(ctx, addr1)
	require.NotNil(t, res1)
	require.Equal(t, acc, res1.(*authtypes.BaseAccount))

	testCases := []appTestCase{
		{
			desc:       "make a valid tx",
			msgs:       []sdk.Msg{multiSendMsg1},
			accNums:    []uint64{0},
			accSeqs:    []uint64{0},
			expSimPass: true,
			expPass:    true,
			privKeys:   []cryptotypes.PrivKey{priv1},
			expectedBalances: []expectedBalance{
				{addr1, sdk.Coins{sdk.NewInt64Coin("foocoin", 57)}},
				{addr2, sdk.Coins{sdk.NewInt64Coin("foocoin", 10)}},
			},
		},
		{
			desc:       "wrong accNum should pass Simulate, but not Deliver",
			msgs:       []sdk.Msg{multiSendMsg1, multiSendMsg2},
			accNums:    []uint64{1}, // wrong account number
			accSeqs:    []uint64{1},
			expSimPass: true, // doesn't check signature
			expPass:    false,
			privKeys:   []cryptotypes.PrivKey{priv1},
		},
	}

	for _, tc := range testCases {
		header := tmproto.Header{Height: a.LastBlockHeight() + 1}
		txGen := app.MakeEncodingConfig().TxConfig
		_, _, err := app.SignCheckDeliver(t, txGen, a.BaseApp, header, tc.msgs, "", tc.accNums, tc.accSeqs, tc.expSimPass, tc.expPass, tc.privKeys...)
		if tc.expPass {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}

		for _, eb := range tc.expectedBalances {
			app.CheckBalance(t, a, eb.addr, eb.coins)
		}
	}
}

func TestMsgMultiSendMultipleOut(t *testing.T) {
	acc1 := &authtypes.BaseAccount{
		Address: addr1.String(),
	}
	acc2 := &authtypes.BaseAccount{
		Address: addr2.String(),
	}

	genAccs := []authtypes.GenesisAccount{acc1, acc2}
	a := app.SetupWithGenesisAccounts(t, genAccs)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr1, sdk.NewCoins(sdk.NewInt64Coin("foocoin", 42))))

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr2, sdk.NewCoins(sdk.NewInt64Coin("foocoin", 42))))

	a.Commit(context.Background())

	testCases := []appTestCase{
		{
			msgs:       []sdk.Msg{multiSendMsg2},
			accNums:    []uint64{0},
			accSeqs:    []uint64{0},
			expSimPass: true,
			expPass:    true,
			privKeys:   []cryptotypes.PrivKey{priv1},
			expectedBalances: []expectedBalance{
				{addr1, sdk.Coins{sdk.NewInt64Coin("foocoin", 32)}},
				{addr2, sdk.Coins{sdk.NewInt64Coin("foocoin", 47)}},
				{addr3, sdk.Coins{sdk.NewInt64Coin("foocoin", 5)}},
			},
		},
	}

	for _, tc := range testCases {
		header := tmproto.Header{Height: a.LastBlockHeight() + 1}
		txGen := app.MakeEncodingConfig().TxConfig
		_, _, err := app.SignCheckDeliver(t, txGen, a.BaseApp, header, tc.msgs, "", tc.accNums, tc.accSeqs, tc.expSimPass, tc.expPass, tc.privKeys...)
		require.NoError(t, err)

		for _, eb := range tc.expectedBalances {
			app.CheckBalance(t, a, eb.addr, eb.coins)
		}
	}
}

func TestMsgMultiSendMultipleInOut(t *testing.T) {
	acc1 := &authtypes.BaseAccount{
		Address: addr1.String(),
	}
	acc2 := &authtypes.BaseAccount{
		Address: addr2.String(),
	}
	acc4 := &authtypes.BaseAccount{
		Address: addr4.String(),
	}

	genAccs := []authtypes.GenesisAccount{acc1, acc2, acc4}
	a := app.SetupWithGenesisAccounts(t, genAccs)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr1, sdk.NewCoins(sdk.NewInt64Coin("foocoin", 42))))

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr2, sdk.NewCoins(sdk.NewInt64Coin("foocoin", 42))))

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr4, sdk.NewCoins(sdk.NewInt64Coin("foocoin", 42))))

	a.Commit(context.Background())

	testCases := []appTestCase{
		{
			msgs:       []sdk.Msg{multiSendMsg3},
			accNums:    []uint64{0, 2},
			accSeqs:    []uint64{0, 0},
			expSimPass: true,
			expPass:    true,
			privKeys:   []cryptotypes.PrivKey{priv1, priv4},
			expectedBalances: []expectedBalance{
				{addr1, sdk.Coins{sdk.NewInt64Coin("foocoin", 32)}},
				{addr4, sdk.Coins{sdk.NewInt64Coin("foocoin", 32)}},
				{addr2, sdk.Coins{sdk.NewInt64Coin("foocoin", 52)}},
				{addr3, sdk.Coins{sdk.NewInt64Coin("foocoin", 10)}},
			},
		},
	}

	for _, tc := range testCases {
		header := tmproto.Header{Height: a.LastBlockHeight() + 1}
		txGen := app.MakeEncodingConfig().TxConfig
		_, _, err := app.SignCheckDeliver(t, txGen, a.BaseApp, header, tc.msgs, "", tc.accNums, tc.accSeqs, tc.expSimPass, tc.expPass, tc.privKeys...)
		require.NoError(t, err)

		for _, eb := range tc.expectedBalances {
			app.CheckBalance(t, a, eb.addr, eb.coins)
		}
	}
}

func TestMsgMultiSendDependent(t *testing.T) {
	acc1 := authtypes.NewBaseAccountWithAddress(addr1)
	acc2 := authtypes.NewBaseAccountWithAddress(addr2)
	err := acc2.SetAccountNumber(1)
	require.NoError(t, err)

	genAccs := []authtypes.GenesisAccount{acc1, acc2}
	a := app.SetupWithGenesisAccounts(t, genAccs)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{})

	require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, addr1, sdk.NewCoins(sdk.NewInt64Coin("foocoin", 42))))

	a.Commit(context.Background())

	testCases := []appTestCase{
		{
			msgs:       []sdk.Msg{multiSendMsg1},
			accNums:    []uint64{0},
			accSeqs:    []uint64{0},
			expSimPass: true,
			expPass:    true,
			privKeys:   []cryptotypes.PrivKey{priv1},
			expectedBalances: []expectedBalance{
				{addr1, sdk.Coins{sdk.NewInt64Coin("foocoin", 32)}},
				{addr2, sdk.Coins{sdk.NewInt64Coin("foocoin", 10)}},
			},
		},
		{
			msgs:       []sdk.Msg{multiSendMsg4},
			accNums:    []uint64{1},
			accSeqs:    []uint64{0},
			expSimPass: true,
			expPass:    true,
			privKeys:   []cryptotypes.PrivKey{priv2},
			expectedBalances: []expectedBalance{
				{addr1, sdk.Coins{sdk.NewInt64Coin("foocoin", 42)}},
			},
		},
	}

	for _, tc := range testCases {
		header := tmproto.Header{Height: a.LastBlockHeight() + 1}
		txGen := app.MakeEncodingConfig().TxConfig
		_, _, err := app.SignCheckDeliver(t, txGen, a.BaseApp, header, tc.msgs, "", tc.accNums, tc.accSeqs, tc.expSimPass, tc.expPass, tc.privKeys...)
		require.NoError(t, err)

		for _, eb := range tc.expectedBalances {
			app.CheckBalance(t, a, eb.addr, eb.coins)
		}
	}
}

func TestMultiSendAllowList(t *testing.T) {
	// CoinToAllowList defines a struct to map coins to their allow lists.
	type CoinToAllowList struct {
		fundAmount sdk.Coin
		sendAmount sdk.Coin
		allowList  types.AllowList
	}

	type testCase struct {
		name                string
		coinsToAllowList    []CoinToAllowList
		sender              sdk.AccAddress
		receiver            sdk.AccAddress
		accNums             []uint64
		accSeqs             []uint64
		privKeys            []cryptotypes.PrivKey
		expectedSenderBal   sdk.Coins
		expectedReceiverBal sdk.Coins
		expectedError       bool
		expectedErrorMsg    string
	}

	senderAcc := sdk.AccAddress(priv1.PubKey().Address())
	receiverAcc := sdk.AccAddress(priv2.PubKey().Address())
	testDenom := fmt.Sprintf("factory/%s/test", senderAcc.String())
	testDenom1 := fmt.Sprintf("factory/%s/test1", senderAcc.String())
	// Define test cases
	testCases := []testCase{

		{
			name: "sender not allowed to send coins",
			coinsToAllowList: []CoinToAllowList{
				{
					fundAmount: sdk.NewInt64Coin(testDenom, 100),
					sendAmount: sdk.NewInt64Coin(testDenom, 20),
					allowList: types.AllowList{
						Addresses: []string{
							receiverAcc.String(),
						},
					},
				},
			},
			accNums:             []uint64{0, 2},
			accSeqs:             []uint64{0, 0},
			sender:              senderAcc,
			receiver:            receiverAcc,
			privKeys:            []cryptotypes.PrivKey{priv1},
			expectedSenderBal:   sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100)),
			expectedReceiverBal: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 0)),
			expectedError:       true,
			expectedErrorMsg:    fmt.Sprintf("%s is not allowed to send funds", senderAcc),
		},
		{
			name: "receiver not allowed to receive coins",
			coinsToAllowList: []CoinToAllowList{
				{
					fundAmount: sdk.NewInt64Coin(testDenom, 100),
					sendAmount: sdk.NewInt64Coin(testDenom, 20),
					allowList: types.AllowList{
						Addresses: []string{
							senderAcc.String(),
						},
					},
				},
			},
			accNums:             []uint64{0, 2},
			accSeqs:             []uint64{0, 0},
			sender:              senderAcc,
			receiver:            receiverAcc,
			privKeys:            []cryptotypes.PrivKey{priv1},
			expectedSenderBal:   sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100)),
			expectedReceiverBal: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 0)),
			expectedError:       true,
			expectedErrorMsg:    fmt.Sprintf("%s is not allowed to receive funds", receiverAcc),
		},
		{
			name: "allow list is empty (no restrictions)",
			coinsToAllowList: []CoinToAllowList{
				{
					fundAmount: sdk.NewInt64Coin(testDenom, 100),
					sendAmount: sdk.NewInt64Coin(testDenom, 20),
					allowList: types.AllowList{
						Addresses: []string{},
					},
				},
			},
			accNums:             []uint64{0, 2},
			accSeqs:             []uint64{0, 0},
			sender:              senderAcc,
			receiver:            receiverAcc,
			expectedSenderBal:   sdk.NewCoins(sdk.NewInt64Coin(testDenom, 80)),
			expectedReceiverBal: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 20)),
			privKeys:            []cryptotypes.PrivKey{priv1},
			expectedError:       false,
		},
		{
			name: "both sender and receiver are allowed to send and receive coins",
			coinsToAllowList: []CoinToAllowList{
				{
					fundAmount: sdk.NewInt64Coin(testDenom, 100),
					sendAmount: sdk.NewInt64Coin(testDenom, 25),
					allowList: types.AllowList{
						Addresses: []string{
							senderAcc.String(), receiverAcc.String(),
						},
					},
				},
			},
			accNums:             []uint64{0, 2},
			accSeqs:             []uint64{0, 0},
			sender:              senderAcc,
			receiver:            receiverAcc,
			privKeys:            []cryptotypes.PrivKey{priv1},
			expectedSenderBal:   sdk.NewCoins(sdk.NewInt64Coin(testDenom, 75)),
			expectedReceiverBal: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 25)),
			expectedError:       false,
		},
		{
			name: "both are allowed for first coin, but only sender is allowed for second coin",
			coinsToAllowList: []CoinToAllowList{
				{
					fundAmount: sdk.NewInt64Coin(testDenom, 100),
					sendAmount: sdk.NewInt64Coin(testDenom, 20),
					allowList: types.AllowList{
						Addresses: []string{
							senderAcc.String(), receiverAcc.String(),
						},
					},
				},
				{
					fundAmount: sdk.NewInt64Coin(testDenom1, 200),
					sendAmount: sdk.NewInt64Coin(testDenom1, 20),
					allowList: types.AllowList{
						Addresses: []string{
							senderAcc.String(),
						},
					},
				},
			},
			accNums:             []uint64{0, 2},
			accSeqs:             []uint64{0, 0},
			sender:              senderAcc,
			receiver:            receiverAcc,
			privKeys:            []cryptotypes.PrivKey{priv1},
			expectedError:       true,
			expectedSenderBal:   sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100), sdk.NewInt64Coin(testDenom1, 200)),
			expectedReceiverBal: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 0), sdk.NewInt64Coin(testDenom1, 0)),
			expectedErrorMsg:    fmt.Sprintf("%s is not allowed to receive funds", receiverAcc),
		},
		{
			name: "both sender and receiver are allowed to send and receive 2 coins",
			coinsToAllowList: []CoinToAllowList{
				{
					fundAmount: sdk.NewInt64Coin(testDenom, 100),
					sendAmount: sdk.NewInt64Coin(testDenom, 25),
					allowList: types.AllowList{
						Addresses: []string{
							senderAcc.String(), receiverAcc.String(),
						},
					},
				},
				{
					fundAmount: sdk.NewInt64Coin(testDenom1, 200),
					sendAmount: sdk.NewInt64Coin(testDenom1, 50),
					allowList: types.AllowList{
						Addresses: []string{
							senderAcc.String(), receiverAcc.String(),
						},
					},
				},
			},
			accNums:             []uint64{0, 2},
			accSeqs:             []uint64{0, 0},
			sender:              senderAcc,
			receiver:            receiverAcc,
			expectedSenderBal:   sdk.NewCoins(sdk.NewInt64Coin(testDenom, 75), sdk.NewInt64Coin(testDenom1, 150)),
			expectedReceiverBal: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 25), sdk.NewInt64Coin(testDenom1, 50)),
			privKeys:            []cryptotypes.PrivKey{priv1},
			expectedError:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup genesis accounts
			sender := &authtypes.BaseAccount{
				Address: tc.sender.String(),
			}
			receiver := &authtypes.BaseAccount{
				Address: tc.receiver.String(),
			}

			genAccs := []authtypes.GenesisAccount{sender, receiver}
			a := app.SetupWithGenesisAccounts(t, genAccs)
			ctx := a.BaseApp.NewContext(false, tmproto.Header{})

			msgs := make([]sdk.Msg, 0)

			for _, coinToAllowList := range tc.coinsToAllowList {
				a.BankKeeper.SetDenomAllowList(ctx, coinToAllowList.fundAmount.Denom, coinToAllowList.allowList)
				require.NoError(t, apptesting.FundAccount(a.BankKeeper, ctx, sender.GetAddress(), sdk.NewCoins(coinToAllowList.fundAmount)))
				multiSendMsg := &types.MsgMultiSend{
					Inputs: []types.Input{
						types.NewInput(sender.GetAddress(), sdk.Coins{coinToAllowList.sendAmount}),
					},
					Outputs: []types.Output{
						types.NewOutput(receiver.GetAddress(), sdk.Coins{coinToAllowList.sendAmount}),
					},
				}
				msgs = append(msgs, multiSendMsg)
			}

			a.Commit(context.Background())

			header := tmproto.Header{Height: a.LastBlockHeight() + 1}
			txGen := app.MakeEncodingConfig().TxConfig
			_, _, err := app.SignCheckDeliver(t, txGen, a.BaseApp, header, msgs, "", tc.accNums, tc.accSeqs, !tc.expectedError, !tc.expectedError, tc.privKeys...)

			if tc.expectedError {
				require.Error(t, err, "expected an error but got none")
				require.Contains(t, err.Error(), tc.expectedErrorMsg)
			} else {
				require.NoError(t, err, "did not expect an error but got one")
			}

			// Validate balances
			// Sender's balance after send should be as expected
			senderBal := a.BankKeeper.GetAllBalances(ctx, tc.sender)
			require.Equal(t, tc.expectedSenderBal, senderBal, "sender balance mismatch")

			// Receiver's balance after receive should be as expected
			receiverBal := a.BankKeeper.GetAllBalances(ctx, tc.receiver)
			require.Equal(t, tc.expectedReceiverBal, receiverBal, "receiver balance mismatch")
		})
	}
}
