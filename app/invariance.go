package app

import (
	"fmt"
	"time"

	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/storev2/commitment"

	sdk "github.com/cosmos/cosmos-sdk/types"

	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func (app *App) LightInvarianceChecks(cms sdk.CommitMultiStore) {
	defer metrics.MeasureSince(
		[]string{"sei", "lightinvariance", "milliseconds"},
		time.Now().UTC(),
	)
	ckv, ok := cms.GetStore(app.BankKeeper.GetStoreKey()).(*commitment.Store)
	if !ok {
		app.Logger().Error("bank store is not a memiavl store; cannot run light invariance check")
		return
	}
	balanceChangePairs := ckv.GetChangedPairs(banktypes.BalancesPrefix)
	useiPostTotal := sdk.ZeroInt()
	useiChangedAddr := []sdk.AccAddress{}
	for _, p := range balanceChangePairs {
		if len(p.Key) < 2 {
			// invalid key; ignore
			continue
		}
		addrLen := int(p.Key[1])
		if len(p.Key) < addrLen+2 {
			// invalid key length; ignore
			continue
		}
		addr := p.Key[2 : addrLen+2]
		denom := p.Key[addrLen+2:]
		if string(denom) != sdk.MustGetBaseDenom() {
			continue
		}
		if !p.Delete {
			var balance sdk.Coin
			if err := balance.Unmarshal(p.Value); err != nil {
				app.Logger().Error(fmt.Sprintf("failed to unmarshal balance: %s", err))
				continue
			}
			if balance.Amount.IsNegative() {
				panic(fmt.Sprintf("negative balance found for addr %s: %s", sdk.AccAddress(addr).String(), balance.String()))
			}
			useiPostTotal = useiPostTotal.Add(balance.Amount)
		}
		useiChangedAddr = append(useiChangedAddr, addr)
	}
	useiPreTotal := sdk.ZeroInt()
	for _, a := range useiChangedAddr {
		key := append(banktypes.CreateAccountBalancesPrefix(a), []byte(sdk.MustGetBaseDenom())...)
		val := ckv.Get(key)
		if val == nil {
			continue
		}
		var balance sdk.Coin
		if err := balance.Unmarshal(val); err != nil {
			app.Logger().Error(fmt.Sprintf("failed to unmarshal preblock balance: %s", err))
			continue
		}
		useiPreTotal = useiPreTotal.Add(balance.Amount)
	}
	weiChangePairs := ckv.GetChangedPairs(banktypes.WeiBalancesPrefix)
	weiPostTotal := sdk.ZeroInt()
	weiChangedAddrs := []sdk.AccAddress{}
	for _, p := range weiChangePairs {
		var amt sdk.Int
		if !p.Delete {
			if err := amt.Unmarshal(p.Value); err != nil {
				app.Logger().Error(fmt.Sprintf("failed to unmarshal wei balance: %s", err))
				continue
			}
			weiPostTotal = weiPostTotal.Add(amt)
			if amt.IsNegative() {
				panic(fmt.Sprintf("negative wei balance found for addr %s: %s", sdk.AccAddress(p.Key[1:]).String(), amt.String()))
			}
		}
		weiChangedAddrs = append(weiChangedAddrs, p.Key[1:])
	}
	weiPreTotal := sdk.ZeroInt()
	for _, a := range weiChangedAddrs {
		key := append(banktypes.WeiBalancesPrefix, a...)
		val := ckv.Get(key)
		if val == nil {
			continue
		}
		var amt sdk.Int
		if err := amt.Unmarshal(val); err != nil {
			app.Logger().Error(fmt.Sprintf("failed to unmarshal preblock wei balance: %s", err))
			continue
		}
		weiPreTotal = weiPreTotal.Add(amt)
	}
	totalSupplyChangePairs := ckv.GetChangedPairs(banktypes.SupplyKey)
	supplyChanged := sdk.ZeroInt()
	preTotalSupply := sdk.ZeroInt()
	if bz := ckv.Get(append(banktypes.SupplyKey, []byte(sdk.MustGetBaseDenom())...)); bz != nil {
		var amt sdk.Int
		if err := amt.Unmarshal(bz); err != nil {
			app.Logger().Error(fmt.Sprintf("failed to unmarshal pre total supply: %s", err))
			return
		}
		preTotalSupply = amt
	}
	for _, p := range totalSupplyChangePairs {
		if string(p.Key[1:]) == sdk.MustGetBaseDenom() {
			if p.Delete {
				supplyChanged = preTotalSupply.Neg()
			} else {
				var amt sdk.Int
				if err := amt.Unmarshal(p.Value); err != nil {
					app.Logger().Error(fmt.Sprintf("failed to unmarshal total supply: %s", err))
				} else {
					supplyChanged = amt.Sub(preTotalSupply)
				}
			}
			break
		}
	}
	weiDiff := weiPostTotal.Sub(weiPreTotal)
	weiDiffInUsei, weiDiffRemainder := bankkeeper.SplitUseiWeiAmount(weiDiff)
	if !weiDiffRemainder.IsZero() {
		panic(fmt.Sprintf("non-zero wei diff found! Pre-block wei total %s, post-block wei total %s", weiPreTotal, weiPostTotal))
	}
	useiDiff := useiPreTotal.Sub(useiPostTotal).Sub(weiDiffInUsei).Add(supplyChanged)
	if !useiDiff.IsZero() {
		panic(fmt.Sprintf("unexpected usei balance total found! Pre-block usei total %s wei total %s total supply %s, post-block usei total %s wei total %s total supply %s", useiPreTotal, weiPreTotal, preTotalSupply, useiPostTotal, weiPostTotal, preTotalSupply.Add(supplyChanged)))
	}
}
