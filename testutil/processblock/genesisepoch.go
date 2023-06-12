package processblock

import "time"

func (a *App) FastEpoch() {
	epoch := a.EpochKeeper.GetEpoch(a.Ctx())
	epoch.EpochDuration = 5 * time.Second
	a.EpochKeeper.SetEpoch(a.Ctx(), epoch)
}
