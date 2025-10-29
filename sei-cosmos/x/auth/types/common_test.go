package types_test

import (
	"github.com/sei-protocol/sei-chain/app"
)

var (
	a                     = app.SetupWithDefaultHome(false, false, false)
	ecdc                  = app.MakeEncodingConfig()
	appCodec, legacyAmino = ecdc.Marshaler, ecdc.Amino
)
