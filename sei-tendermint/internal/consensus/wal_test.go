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
	got := dumpWAL(t,wal)
	require.NoError(t, utils.TestDiff(msgs, got[1:]))
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

func TestWAL_ReadLastMsgs(t *testing.T) {
	cfg := getConfig(t)
	runStateUntilBlock(t, cfg, 3)
	wal, err := openWAL(cfg.Consensus.WalFile())
	if err != nil {
		t.Fatal(err)
	}
	defer wal.Close()

	gotHeight, msgs, err := wal.ReadLastHeightMsgs()
	require.NoError(t, err)
	require.True(t, gotHeight>3)
	if len(msgs)>0 {
		rs, ok := msgs[0].any.(tmtypes.EventDataRoundState)
		require.True(t, ok, "expected message of type EventDataRoundState")
		require.Equal(t, rs.Height, gotHeight, "wrong height")
	}
}
