package types_test

import (
	"github.com/sei-protocol/sei-chain/app"
)

var (
	a                     = app.Setup(false, false, false)
	ecdc                  = app.MakeEncodingConfig()
	appCodec, legacyAmino = ecdc.Marshaler, ecdc.Amino
)
