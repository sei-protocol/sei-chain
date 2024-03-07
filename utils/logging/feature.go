package logging

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var EnabledFeatures = map[string]struct{}{}

func EnableFeature(feature string) {
	EnabledFeatures[feature] = struct{}{}
}

func Info(ctx sdk.Context, s string, feature string) {
	if _, ok := EnabledFeatures[feature]; !ok {
		return
	}
	ctx.Logger().Info(s)
}
