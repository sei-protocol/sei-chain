package app

import (
	"testing"
	"time"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

func TestInitLastHeaderSetsCheckStateAndStartsEVMServers(t *testing.T) {
	app := Setup(t, false, false, false)
	header := &tmproto.Header{ChainID: "sei-test", Height: 9, Time: time.Unix(1700000000, 0).UTC()}

	app.InitLastHeader(header)

	ctx := app.GetCheckCtx()
	require.Equal(t, header.ChainID, ctx.ChainID())
	require.Equal(t, header.Height, ctx.BlockHeight())
	require.True(t, header.Time.Equal(ctx.BlockTime()))
	for name, signal := range map[string]chan struct{}{
		"http": app.httpServerStartSignal,
		"ws":   app.wsServerStartSignal,
	} {
		select {
		case <-signal:
		default:
			t.Fatalf("%s server start signal was not sent", name)
		}
	}

	// A later block must not block on the already-sent signals.
	app.signalEVMServersStart()
	require.Empty(t, app.httpServerStartSignal)
	require.Empty(t, app.wsServerStartSignal)
}
