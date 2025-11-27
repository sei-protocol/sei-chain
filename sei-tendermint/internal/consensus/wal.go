package consensus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gogo/protobuf/proto"
	cstypes "github.com/tendermint/tendermint/internal/consensus/types"
	"github.com/tendermint/tendermint/types"

	"github.com/tendermint/tendermint/internal/libs/autofile"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
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
type WALWriter struct {
	writer *autofile.LogWriter
}

// NewWAL returns a new write-ahead logger based on `baseWAL`, which implements
// WAL. It's flushed and synced to disk every 2s and once when stopped.
func NewWALWriter(walFile string, cfg *autofile.Config) *WALWriter {
	return &WALWriter{autofile.NewLogWriter(walFile, cfg)}
}

func (w *WALWriter) Run(ctx context.Context, syncInterval time.Duration) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			for {
				if err:=utils.Sleep(ctx,syncInterval); err!=nil {
					return err
				}
				if err:=w.Sync(ctx); err!=nil {
					return err
				}
			}
		})
		return w.writer.Run(ctx)
	})
}

// FlushAndSync flushes and fsync's the underlying group's data to disk.
// See auto#FlushAndSync
func (w *WALWriter) Sync(ctx context.Context) error {
	return w.writer.Sync(ctx)
}

// Write is called in newStep and for each receive on the
// peerMsgQueue and the timeoutTicker.
// NOTE: does not call fsync()
func (wal *WALWriter) Write(ctx context.Context, msg WALMessage) error {
	if wal.writer.TotalSize()== 0 {
		if err := wal.Write(NewWALMessage(EndHeightMessage{0})); err != nil {
			return nil,err
		}
	}
	data, err := proto.Marshal(&tmcons.TimedWALMessage{Time: time.Now(), Msg: msg.toProto()})
	if err != nil {
		panic(fmt.Errorf("proto.Marshal(): %w", err))
	}
	if len(data) > maxMsgSizeBytes {
		return fmt.Errorf("msg is too big: %d bytes, max: %d bytes", len(data), maxMsgSizeBytes)
	}
	return wal.writer.Write(ctx,data)
}

type WALReader struct {
	reader *autofile.LogReader
}

func (r *WALReader) Close() {
	r.reader.Close()
}

func (r *WALReader) Read() (WALMessage,error) {
	data,err := r.reader.Read()
	if err!=nil { return WALMessage{},err }
	var msgPB tmcons.TimedWALMessage
	if err := proto.Unmarshal(data, &msgPB); err != nil {
		return WALMessage{}, fmt.Errorf("proto.Unmarshal(): %w",err)
	}
	return walFromProto(msgPB.Msg)
}

func (r *WALReader) readEndHeight() (EndHeightMessage,error) {
	for {
		msg,err:=r.Read()
		if err!=nil { return EndHeightMessage{},err }
		if msg,ok:=msg.any.(EndHeightMessage); ok {
			return msg,nil
		}
	}
}

var errNotFound = errors.New("not found")

// SearchForEndHeight searches for the EndHeightMessage with the given height
// and returns an auto.GroupReader, whenever it was found or not and an error.
// Group reader will be nil if found equals false.
//
// CONTRACT: caller must close group reader.
func OpenReaderAfterHeight(walFile string, height int64) (*WALReader, error) {
	for offset:=0;; offset-- {
		logReader,err := autofile.NewLogReader(walFile,offset)
		if err!=nil {
			if utils.ErrorAs[autofile.ErrInvalidOffset](err).IsPresent() {
				return nil,errNotFound
			}
			return nil,err
		}
		r := &WALReader{reader:logReader}
		found,err := func() (bool,error) {
			// find the first marker
			msg,err := r.readEndHeight()
			if err!=nil { 
				if errors.Is(err,io.EOF) { return false,nil }
				return false,err 
			}
			if msg.Height>height {
				// first marker is too high, we need to check older files.
				return false,nil
			}
			// We have found the file which should contain the desired marker.
			for msg.Height<height {
				if msg,err = r.readEndHeight(); err!=nil {
					return false, err
				}
			}
			if msg.Height!=height {
				// The desired height marker is missing.
				return false,errNotFound
			}
			return true,nil
		}()
		if err==nil && found {
			return r,nil
		}
		r.Close()
		if err!=nil {
			return nil,err
		}
	}
}
