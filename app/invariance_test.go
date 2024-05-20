package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	app "github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"
)

func TestLightInvarianceChecks(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	accounts := []sdk.AccAddress{
		sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()),
		sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()),
	}
	useiCoin := func(i int64) sdk.Coin { return sdk.NewCoin("usei", sdk.NewInt(i)) }
	useiCoins := func(i int64) sdk.Coins { return sdk.NewCoins(useiCoin(i)) }
	for i, tt := range []struct {
		preUsei    []int64
		preWei     []int64
		preSupply  int64
		postUsei   []int64
		postWei    []int64
		postSupply int64
		success    bool
	}{
		{
			preUsei:    []int64{0, 0},
			preWei:     []int64{0, 0},
			preSupply:  5,
			postUsei:   []int64{1, 2},
			postWei:    []int64{0, 0},
			postSupply: 8,
			success:    true,
		},
		{
			preUsei:    []int64{2, 1},
			preWei:     []int64{0, 0},
			preSupply:  3,
			postUsei:   []int64{0, 0},
			postWei:    []int64{0, 0},
			postSupply: 0,
			success:    true,
		},
		{
			preUsei:    []int64{1, 0},
			preWei:     []int64{0, 0},
			preSupply:  10,
			postUsei:   []int64{0, 1},
			postWei:    []int64{0, 0},
			postSupply: 10,
			success:    true,
		},
		{
			preUsei:    []int64{1, 0},
			preWei:     []int64{0, 0},
			preSupply:  10,
			postUsei:   []int64{0, 0},
			postWei:    []int64{500_000_000_000, 500_000_000_000},
			postSupply: 10,
			success:    true,
		},
		{
			preUsei:    []int64{0, 0},
			preWei:     []int64{500_000_000_000, 500_000_000_000},
			preSupply:  10,
			postUsei:   []int64{1, 0},
			postWei:    []int64{0, 0},
			postSupply: 10,
			success:    true,
		},
		{
			preUsei:    []int64{0, 0},
			preWei:     []int64{1, 2},
			preSupply:  10,
			postUsei:   []int64{0, 0},
			postWei:    []int64{2, 1},
			postSupply: 10,
			success:    true,
		},
		{
			preUsei:    []int64{1, 0},
			preWei:     []int64{0, 0},
			preSupply:  10,
			postUsei:   []int64{1, 1},
			postWei:    []int64{0, 0},
			postSupply: 10,
			success:    false,
		},
		{
			preUsei:    []int64{1, 0},
			preWei:     []int64{0, 0},
			preSupply:  10,
			postUsei:   []int64{0, 0},
			postWei:    []int64{0, 0},
			postSupply: 10,
			success:    false,
		},
		{
			preUsei:    []int64{1, 0},
			preWei:     []int64{0, 0},
			preSupply:  10,
			postUsei:   []int64{0, 1},
			postWei:    []int64{500_000_000_000, 500_000_000_000},
			postSupply: 10,
			success:    false,
		},
		{
			preUsei:    []int64{1, 0},
			preWei:     []int64{500_000_000_000, 500_000_000_000},
			preSupply:  10,
			postUsei:   []int64{0, 1},
			postWei:    []int64{0, 0},
			postSupply: 10,
			success:    false,
		},
		{
			preUsei:    []int64{0, 0},
			preWei:     []int64{1, 2},
			preSupply:  10,
			postUsei:   []int64{0, 0},
			postWei:    []int64{2, 2},
			postSupply: 10,
			success:    false,
		},
		{
			preUsei:    []int64{0, 0},
			preWei:     []int64{1, 2},
			preSupply:  10,
			postUsei:   []int64{0, 0},
			postWei:    []int64{1, 1},
			postSupply: 10,
			success:    false,
		},
	} {
		fmt.Printf("Running test %d\n", i)
		testWrapper := app.NewTestWrapperWithSc(t, tm, valPub, false)
		a, ctx := testWrapper.App, testWrapper.Ctx
		for i := range tt.preUsei {
			if tt.preUsei[i] > 0 {
				a.BankKeeper.AddCoins(ctx, accounts[i], useiCoins(tt.preUsei[i]), false)
			}
			if tt.preWei[i] > 0 {
				a.BankKeeper.AddWei(ctx, accounts[i], sdk.NewInt(tt.preWei[i]))
			}
		}
		if tt.preSupply > 0 {
			a.BankKeeper.SetSupply(ctx, useiCoin(tt.preSupply))
		}
		a.SetDeliverStateToCommit()
		a.WriteState()
		a.GetWorkingHash() // flush to sc
		for i := range tt.postUsei {
			useiDiff := tt.postUsei[i] - tt.preUsei[i]
			if useiDiff > 0 {
				a.BankKeeper.AddCoins(ctx, accounts[i], useiCoins(useiDiff), false)
			} else if useiDiff < 0 {
				a.BankKeeper.SubUnlockedCoins(ctx, accounts[i], useiCoins(-useiDiff), false)
			}

			weiDiff := tt.postWei[i] - tt.preWei[i]
			if weiDiff > 0 {
				a.BankKeeper.AddWei(ctx, accounts[i], sdk.NewInt(weiDiff))
			} else if weiDiff < 0 {
				a.BankKeeper.SubWei(ctx, accounts[i], sdk.NewInt(-weiDiff))
			}
		}
		a.BankKeeper.SetSupply(ctx, useiCoin(tt.postSupply))
		a.SetDeliverStateToCommit()
		f := func() { a.LightInvarianceChecks(a.WriteState(), app.LightInvarianceConfig{SupplyEnabled: true}) }
		if tt.success {
			require.NotPanics(t, f)
		} else {
			require.Panics(t, f)
		}
		safeClose(a)
	}
}

// TODO: remove once snapshot manager can be closed gracefully in tests
func safeClose(a *app.App) {
	defer func() {
		_ = recover()
	}()
	a.Close()
}
