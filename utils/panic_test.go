package utils_test

import (
       "errors"
       "testing"

       log "github.com/tendermint/tendermint/libs/log"
       sdk "github.com/cosmos/cosmos-sdk/types"
       "github.com/sei-protocol/sei-chain/utils"
       "github.com/stretchr/testify/require"
)

func TestHardFail(t *testing.T) {
	hardFailer := func() {
		panic(utils.DecorateHardFailError(errors.New("some error")))
	}
	panicHandlingFn := func() {
		defer utils.PanicHandler(func(_ any) {})()
		hardFailer()
	}
	require.Panics(t, panicHandlingFn)
}

func TestLogPanicCallback(t *testing.T) {
       ctx := sdk.Context{}.WithLogger(log.NewNopLogger())
       require.NotPanics(t, func() {
               defer utils.PanicHandler(utils.LogPanicCallback(ctx))()
               panic("test panic")
       })
}
