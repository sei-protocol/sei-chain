package evmrpc

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/stretchr/testify/require"
)

// TestLogsErrorNotificationJSON pins the notification a logs subscription sends for a failed
// poll: the error message, and the block number when the poll skipped an oversized block.
func TestLogsErrorNotificationJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "oversized block",
			err:  &oversizedBlockError{height: 42, err: receipt.ErrTooManyLogs},
			want: `{"error":"logs of block 42 skipped: query matches too many logs","blockNumber":"0x2a"}`,
		},
		{
			name: "other error",
			err:  errors.New("receipt store unavailable"),
			want: `{"error":"receipt store unavailable"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(newLogsErrorNotification(tc.err))
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(got))
		})
	}
}
