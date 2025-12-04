package consensus

import (
	"path"

	"testing"
	"time"

	"github.com/tendermint/tendermint/crypto/merkle"
	"github.com/tendermint/tendermint/internal/consensus/types"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	tmtypes "github.com/tendermint/tendermint/types"
)

func TestWAL_AppendRead(t *testing.T) {
	wal, err := openWAL(path.Join(t.TempDir(), "testwal"))
	require.NoError(t, err)
	defer wal.Close()

	msgs := utils.Slice(
		NewWALMessage(tmtypes.EventDataRoundState{Height: 1, Round: 1, Step: ""}),
		NewWALMessage(timeoutInfo{Duration: time.Second, Height: 1, Round: 1, Step: types.RoundStepPropose}),
		NewWALMessage(EndHeightMessage{1}),
	)
	for _, msg := range msgs {
		require.NoError(t, wal.Append(msg))
	}
	require.NoError(t, wal.Sync())
	ok, err := wal.SeekEndHeight(0)
	require.NoError(t, err)
	require.True(t, ok)
	for _, want := range msgs {
		got, err := wal.Read()
		require.NoError(t, err)
		require.NoError(t, utils.TestDiff(want, got))
	}
}

func TestWAL_ErrBadSize(t *testing.T) {
	wal, err := openWAL(path.Join(t.TempDir(), "testlog"))
	require.NoError(t, err)

	// 1) Write returns an error if msg is too big
	msg := &BlockPartMessage{
		Height: 1,
		Round:  1,
		Part: &tmtypes.Part{
			Index: 1,
			Bytes: make([]byte, 1),
			Proof: merkle.Proof{
				Total:    1,
				Index:    1,
				LeafHash: make([]byte, maxMsgSizeBytes-30),
			},
		},
	}

	if err := wal.Append(NewWALMessage(msgInfo{Msg: msg})); !utils.ErrorAs[ErrBadSize](err).IsPresent() {
		t.Fatalf("wal.Append(<big msg>): %v, want ErrBadSize", err)
	}
}

func TestWAL_SeekEndHeight(t *testing.T) {
	cfg := getConfig(t)
	runStateUntilBlock(t, cfg, 6)
	wal, err := openWAL(cfg.Consensus.WalFile())
	if err != nil {
		t.Fatal(err)
	}
	defer wal.Close()

	h := int64(3)
	found, err := wal.SeekEndHeight(h)
	require.NoError(t, err)
	require.True(t, found)

	msg, err := wal.Read()
	require.NoError(t, err, "expected to decode a message")
	rs, ok := msg.any.(tmtypes.EventDataRoundState)
	require.True(t, ok, "expected message of type EventDataRoundState")
	require.Equal(t, rs.Height, h+1, "wrong height")
}
