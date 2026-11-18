package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClosestUpgradeNameRereadsDoneUpgradesOnNewHeight(t *testing.T) {
	a := Setup(t, false, false, false)
	checkCtx := a.GetCheckCtx()
	require.Equal(t, LatestUpgrade, a.closestUpgradeName(checkCtx, 1))

	a.UpgradeKeeper.SetDone(checkCtx.WithBlockHeight(5), "v1")
	require.Equal(t, LatestUpgrade, a.closestUpgradeName(checkCtx, 1), "same height reuses the cached set")
	require.Equal(t, "v1", a.closestUpgradeName(checkCtx.WithBlockHeight(checkCtx.BlockHeight()+1), 1))
}
