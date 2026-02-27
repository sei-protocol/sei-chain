package client_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const (
	waitForEventTimeout        = 10 * time.Second
	waitForEventAttemptTimeout = 2 * time.Second
	waitForEventPollInterval   = 200 * time.Millisecond
)

func waitForOneEventEventually(
	ctx context.Context,
	t *testing.T,
	c client.EventsClient,
	query string,
) types.EventData {
	t.Helper()

	var evt types.EventData
	require.Eventually(t, func() bool {
		attemptCtx, cancel := context.WithTimeout(ctx, waitForEventAttemptTimeout)
		defer cancel()

		e, err := client.WaitForOneEvent(attemptCtx, c, query)
		if err != nil {
			// During polling, these are expected until the tx is committed and indexed.
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return false
			}
			require.NoError(t, err)
			return false
		}

		evt = e
		return true
	}, waitForEventTimeout, waitForEventPollInterval)

	return evt
}

// MakeTxKV returns a text transaction, allong with expected key, value pair
func MakeTxKV() ([]byte, []byte, []byte) {
	k := []byte(tmrand.Str(8))
	v := []byte(tmrand.Str(8))
	return k, v, append(k, append([]byte("="), v...)...)
}

func testTxEventsSent(ctx context.Context, t *testing.T, broadcastMethod string, c client.Client) {
	t.Helper()
	// make the tx
	_, _, tx := MakeTxKV()

	// send
	done := make(chan struct{})
	go func() {
		defer close(done)
		var (
			txres *coretypes.ResultBroadcastTx
			err   error
		)
		switch broadcastMethod {
		case "async":
			txres, err = c.BroadcastTxAsync(ctx, tx)
		case "sync":
			txres, err = c.BroadcastTxSync(ctx, tx)
		default:
			require.FailNowf(t, "Unknown broadcastMethod %s", broadcastMethod)
		}
		if assert.NoError(t, err) {
			assert.Equal(t, txres.Code, abci.CodeTypeOK)
		}
	}()

	// Wait for the transaction we sent to be confirmed.
	query := fmt.Sprintf(`tm.event = '%s' AND tx.hash = '%X'`,
		types.EventTxValue, types.Tx(tx).Hash())

	evt := waitForOneEventEventually(ctx, t, c, query)
	// and make sure it has the proper info
	txe, ok := evt.(types.EventDataTx)
	require.True(t, ok)

	// make sure this is the proper tx
	require.EqualValues(t, tx, txe.Tx)
	require.True(t, txe.Result.IsOK())
	<-done
}
