package consensus

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gogo/protobuf/proto"
	cstypes "github.com/tendermint/tendermint/internal/consensus/types"
	"github.com/tendermint/tendermint/types"

	"github.com/tendermint/tendermint/internal/libs/wal"
	"github.com/tendermint/tendermint/libs/utils"
	tmcons "github.com/tendermint/tendermint/proto/tendermint/consensus"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

const (
	// time.Time + max consensus msg size
	maxMsgSizeBytes = maxMsgSize + 24

	// how often the WAL should be sync'd during period sync'ing
	walDefaultFlushInterval = 2 * time.Second
)

//--------------------------------------------------------
// types and functions for savings consensus messages

// MsgToProto takes a consensus message type and returns the proto defined
// consensus message.
func MsgToProto(msg Message) *tmcons.Message {
	switch msg := msg.(type) {
	case *NewRoundStepMessage:
		return &tmcons.Message{Sum: &tmcons.Message_NewRoundStep{NewRoundStep: msg.ToProto()}}
	case *NewValidBlockMessage:
		return &tmcons.Message{Sum: &tmcons.Message_NewValidBlock{NewValidBlock: msg.ToProto()}}
	case *ProposalMessage:
		return &tmcons.Message{Sum: &tmcons.Message_Proposal{Proposal: msg.ToProto()}}
	case *ProposalPOLMessage:
		return &tmcons.Message{Sum: &tmcons.Message_ProposalPol{ProposalPol: msg.ToProto()}}
	case *BlockPartMessage:
		return &tmcons.Message{Sum: &tmcons.Message_BlockPart{BlockPart: msg.ToProto()}}
	case *VoteMessage:
		return &tmcons.Message{Sum: &tmcons.Message_Vote{Vote: msg.ToProto()}}
	case *HasVoteMessage:
		return &tmcons.Message{Sum: &tmcons.Message_HasVote{HasVote: msg.ToProto()}}
	case *VoteSetMaj23Message:
		return &tmcons.Message{Sum: &tmcons.Message_VoteSetMaj23{VoteSetMaj23: msg.ToProto()}}
	case *VoteSetBitsMessage:
		return &tmcons.Message{Sum: &tmcons.Message_VoteSetBits{VoteSetBits: msg.ToProto()}}
	default:
		panic(fmt.Errorf("consensus: message not recognized: %T", msg))
	}
}

// MsgFromProto takes a consensus proto message and returns the native go type.
func MsgFromProto(msg *tmcons.Message) (Message, error) {
	switch msg := msg.Sum.(type) {
	case *tmcons.Message_NewRoundStep:
		return newRoundStepMessageFromProto(msg.NewRoundStep)
	case *tmcons.Message_NewValidBlock:
		return newValidBlockMessageFromProto(msg.NewValidBlock)
	case *tmcons.Message_Proposal:
		return proposalMessageFromProto(msg.Proposal)
	case *tmcons.Message_ProposalPol:
		return proposalPOLMessageFromProto(msg.ProposalPol)
	case *tmcons.Message_BlockPart:
		return blockPartMessageFromProto(msg.BlockPart)
	case *tmcons.Message_Vote:
		return voteMessageFromProto(msg.Vote)
	case *tmcons.Message_HasVote:
		return hasVoteMessageFromProto(msg.HasVote)
	case *tmcons.Message_VoteSetMaj23:
		return voteSetMaj23MessageFromProto(msg.VoteSetMaj23)
	case *tmcons.Message_VoteSetBits:
		return voteSetBitsMessageFromProto(msg.VoteSetBits)
	default:
		return nil, fmt.Errorf("consensus: message not recognized: %T", msg)
	}
}

// TimedWALMessage wraps WALMessage and adds Time for debugging purposes.
type TimedWALMessage struct {
	Time time.Time
	Msg  WALMessage
}

// EndHeightMessage marks the end of the given height inside WAL.
// @internal used by scripts/wal2json util.
type EndHeightMessage struct {
	Height int64
}

type WALMessage struct{ any }

func NewWALMessage[T msgInfo | timeoutInfo | EndHeightMessage | types.EventDataRoundState](v T) WALMessage {
	return WALMessage{v}
}

// WALtoProto takes a WAL message and return a proto walMessage and error.
func (msg WALMessage) toProto() *tmcons.WALMessage {

	switch msg := msg.any.(type) {
	case types.EventDataRoundState:
		return &tmcons.WALMessage{
			Sum: &tmcons.WALMessage_EventDataRoundState{
				EventDataRoundState: &tmproto.EventDataRoundState{
					Height: msg.Height,
					Round:  msg.Round,
					Step:   msg.Step,
				},
			},
		}
	case msgInfo:
		return &tmcons.WALMessage{
			Sum: &tmcons.WALMessage_MsgInfo{
				MsgInfo: &tmcons.MsgInfo{
					Msg:    *MsgToProto(msg.Msg),
					PeerID: string(msg.PeerID),
				},
			},
		}
	case timeoutInfo:
		return &tmcons.WALMessage{
			Sum: &tmcons.WALMessage_TimeoutInfo{
				TimeoutInfo: &tmcons.TimeoutInfo{
					Duration: msg.Duration,
					Height:   msg.Height,
					Round:    msg.Round,
					Step:     uint32(msg.Step),
				},
			},
		}

	case EndHeightMessage:
		return &tmcons.WALMessage{
			Sum: &tmcons.WALMessage_EndHeight{
				EndHeight: &tmcons.EndHeight{
					Height: msg.Height,
				},
			},
		}
	default: panic("unreachable")
	}
}

// walFromProto takes a proto wal message and return a consensus walMessage and
// error.
func walFromProto(msg *tmcons.WALMessage) (WALMessage, error) {
	switch msg := msg.Sum.(type) {
	case *tmcons.WALMessage_EventDataRoundState:
		return NewWALMessage(types.EventDataRoundState{
			Height: msg.EventDataRoundState.Height,
			Round:  msg.EventDataRoundState.Round,
			Step:   msg.EventDataRoundState.Step,
		}), nil

	case *tmcons.WALMessage_MsgInfo:
		walMsg, err := MsgFromProto(&msg.MsgInfo.Msg)
		if err != nil {
			return WALMessage{}, fmt.Errorf("msgInfo from proto error: %w", err)
		}
		return NewWALMessage(msgInfo{
			Msg:    walMsg,
			PeerID: types.NodeID(msg.MsgInfo.PeerID),
		}), nil

	case *tmcons.WALMessage_TimeoutInfo:
		tis, ok := utils.SafeCast[uint8](msg.TimeoutInfo.Step)
		// deny message based on possible overflow
		if !ok {
			return WALMessage{}, fmt.Errorf("denying message due to possible overflow")
		}

		return NewWALMessage(timeoutInfo{
			Duration: msg.TimeoutInfo.Duration,
			Height:   msg.TimeoutInfo.Height,
			Round:    msg.TimeoutInfo.Round,
			Step:     cstypes.RoundStepType(tis),
		}), nil

	case *tmcons.WALMessage_EndHeight:
		return NewWALMessage(EndHeightMessage{Height: msg.EndHeight.Height}), nil

	default:
		return WALMessage{}, fmt.Errorf("from proto: wal message not recognized: %T", msg)
	}
}

//--------------------------------------------------------

// Write ahead logger writes msgs to disk before they are processed.
// Can be used for crash-recovery and deterministic replay.
type WAL struct { inner *wal.Log }

// openWAL opens WAL in append mode. 
func openWAL(walFile string) (res *WAL, resErr error) {
	inner,err := wal.NewLog(walFile, wal.DefaultConfig())
	if err!=nil { return nil,err }
	defer func(){ if resErr!=nil { inner.Close() } }()
	wal := &WAL{inner}
	if err := wal.OpenForAppend(); err!=nil {
		return nil, fmt.Errorf("OpenForAppend(): %w",err)
	}
	size,err := inner.Size()
	if err!=nil {
		return nil,fmt.Errorf("inner.Size(): %w",err)
	}
	if size==0 {
		if err := wal.Append(NewWALMessage(EndHeightMessage{0})); err != nil {
			return nil, fmt.Errorf("Append(): %w",err)
		}
	}
	return wal,nil
}

// Sync flushes and fsync's the buffered entries to underlying files. 
func (w *WAL) Sync() error { return w.inner.Sync() }

// OpenForAppend opens WAL for appending.
func (w *WAL) OpenForAppend() error { return w.inner.OpenForAppend() }

// Close releases all underlying resources unconditionally.
// Other methods will return an error after calling Close.
func (w *WAL) Close() { w.inner.Close() }

// Append appends an entry to the WAL.
// Remember to call OpenForAppend before Append.
// You need to call Sync afterwards to ensure entry is persisted on disk.
func (w *WAL) Append(msg WALMessage) error {
	entry, err := proto.Marshal(&tmcons.TimedWALMessage{Time: time.Now(), Msg: msg.toProto()})
	if err != nil {
		panic(fmt.Errorf("proto.Marshal(): %w", err))
	}
	if len(entry) > maxMsgSizeBytes {
		return fmt.Errorf("msg is too big: %d bytes, max: %d bytes", len(entry), maxMsgSizeBytes)
	}
	return w.inner.Append(entry)
}

// Read reads an entry from the WAL.
// Remember to call SeekEndHeight before Read.
func (w *WAL) Read() (WALMessage,error) {
	entry,err := w.inner.Read()
	if err!=nil { return WALMessage{},err }
	var msgPB tmcons.TimedWALMessage
	if err := proto.Unmarshal(entry, &msgPB); err != nil {
		return WALMessage{}, fmt.Errorf("proto.Unmarshal(): %w",err)
	}
	return walFromProto(msgPB.Msg)
}

func (w *WAL) readEndHeight() (EndHeightMessage,error) {
	for {
		msg,err:=w.Read()
		if err!=nil { return EndHeightMessage{},err }
		if msg,ok:=msg.any.(EndHeightMessage); ok {
			return msg,nil
		}
	}
}

var errNotFound = errors.New("not found")

// SeekEndHeight opens WAL for reading at position AFTER EndHeightMessage{height}.
func (w *WAL) SeekEndHeight(height int64) error {
	// iterate over WAL checkpoints from the newest, assuming
	// that height is recent.
	for offset:=0;; offset-- {
		if err := w.inner.OpenForRead(offset); err!=nil {
			if utils.ErrorAs[wal.ErrBadOffset](err).IsPresent() {
				return errNotFound
			}
			return err
		}
		// find the first marker
		msg,err := w.readEndHeight()
		if err!=nil {
			// No markers at all, try older checkpoint.
			if errors.Is(err,io.EOF) { continue }
			return err 
		}
		if msg.Height<=height {
			// We have found a lower marker. It is enough to seek forward now. 
			for msg.Height<height {
				if msg,err = w.readEndHeight(); err!=nil {
					if errors.Is(err,io.EOF) { return errNotFound }
					return err
				}
			}
			if msg.Height!=height {
				return errNotFound
			}
			return nil
		}
		// Try older checkpoint.
	}
}
