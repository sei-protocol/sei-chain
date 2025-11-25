package consensus

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gogo/protobuf/proto"
	cstypes "github.com/tendermint/tendermint/internal/consensus/types"
	"github.com/tendermint/tendermint/types"

	auto "github.com/tendermint/tendermint/internal/libs/autofile"
	"github.com/tendermint/tendermint/libs/log"
	tmos "github.com/tendermint/tendermint/libs/os"
	"github.com/tendermint/tendermint/libs/service"
	tmtime "github.com/tendermint/tendermint/libs/time"
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

	default:
		panic(fmt.Errorf("to proto: wal message not recognized: %T", msg))
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
// Simple write-ahead logger

// WAL is an interface for any write-ahead logger.
type WAL interface {
	Write(WALMessage) error
	WriteSync(WALMessage) error
	FlushAndSync() error

	SearchForEndHeight(height int64, options *WALSearchOptions) (rd io.ReadCloser, found bool, err error)

	// service methods
	Start(context.Context) error
	Stop()
	Wait()
}

// Write ahead logger writes msgs to disk before they are processed.
// Can be used for crash-recovery and deterministic replay.
// TODO: currently the wal is overwritten during replay catchup, give it a mode
// so it's either reading or appending - must read to end to start appending
// again.
type BaseWAL struct {
	service.BaseService
	logger log.Logger

	group         *auto.Group
	flushTicker   *time.Ticker
	flushInterval time.Duration
}

var _ WAL = &BaseWAL{}

// NewWAL returns a new write-ahead logger based on `baseWAL`, which implements
// WAL. It's flushed and synced to disk every 2s and once when stopped.
func NewWAL(ctx context.Context, logger log.Logger, walFile string, groupOptions ...func(*auto.Group)) (*BaseWAL, error) {
	err := tmos.EnsureDir(filepath.Dir(walFile), 0700)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure WAL directory is in place: %w", err)
	}

	group, err := auto.OpenGroup(ctx, logger, walFile, groupOptions...)
	if err != nil {
		return nil, err
	}
	wal := &BaseWAL{
		logger:        logger,
		group:         group,
		flushInterval: walDefaultFlushInterval,
	}
	wal.BaseService = *service.NewBaseService(logger, "baseWAL", wal)
	return wal, nil
}

// SetFlushInterval allows us to override the periodic flush interval for the WAL.
func (wal *BaseWAL) SetFlushInterval(i time.Duration) {
	wal.flushInterval = i
}

func (wal *BaseWAL) Group() *auto.Group {
	return wal.group
}

func (wal *BaseWAL) OnStart(ctx context.Context) error {
	size, err := wal.group.Head.Size()
	if err != nil {
		return err
	} else if size == 0 {
		if err := wal.WriteSync(NewWALMessage(EndHeightMessage{0})); err != nil {
			return err
		}
	}
	err = wal.group.Start(ctx)
	if err != nil {
		return err
	}
	wal.flushTicker = time.NewTicker(wal.flushInterval)
	go wal.processFlushTicks(ctx)
	return nil
}

func (wal *BaseWAL) processFlushTicks(ctx context.Context) {
	for {
		select {
		case <-wal.flushTicker.C:
			if err := wal.FlushAndSync(); err != nil {
				wal.logger.Error("Periodic WAL flush failed", "err", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// FlushAndSync flushes and fsync's the underlying group's data to disk.
// See auto#FlushAndSync
func (wal *BaseWAL) FlushAndSync() error {
	return wal.group.FlushAndSync()
}

// Stop the underlying autofile group.
// Use Wait() to ensure it's finished shutting down
// before cleaning up files.
func (wal *BaseWAL) OnStop() {
	wal.flushTicker.Stop()
	if err := wal.FlushAndSync(); err != nil {
		wal.logger.Error("error on flush data to disk", "error", err)
	}
	wal.group.Stop()
	wal.group.Close()
}

// Wait for the underlying autofile group to finish shutting down
// so it's safe to cleanup files.
func (wal *BaseWAL) Wait() {
	if wal.IsRunning() {
		wal.BaseService.Wait()
	}
	if wal.group.IsRunning() {
		wal.group.Wait()
	}
}

// Write is called in newStep and for each receive on the
// peerMsgQueue and the timeoutTicker.
// NOTE: does not call fsync()
func (wal *BaseWAL) Write(msg WALMessage) error {
	bytes, err := encode(&TimedWALMessage{tmtime.Now(), msg})
	if err != nil {
		return fmt.Errorf("encode(): %w", err)
	}
	// TODO(gprusak): mark wal as broken after write failure.
	// A single bad write can be effectively reverted,
	// but multiple bad writes can make it permanently corrupted.
	if _, err := wal.group.Write(bytes); err != nil {
		return fmt.Errorf("wal.group.Write(): %w", err)
	}
	return nil
}

// WriteSync is called when we receive a msg from ourselves
// so that we write to disk before sending signed messages.
// NOTE: calls fsync()
func (wal *BaseWAL) WriteSync(msg WALMessage) error {
	if err := wal.Write(msg); err != nil {
		return err
	}
	if err := wal.FlushAndSync(); err != nil {
		return fmt.Errorf(`WriteSync failed to flush consensus wal.
		WARNING: may result in creating alternative proposals / votes for the current height iff the node restarted: %w`, err)
	}
	return nil
}

// WALSearchOptions are optional arguments to SearchForEndHeight.
type WALSearchOptions struct {
	// IgnoreDataCorruptionErrors set to true will result in skipping data corruption errors.
	IgnoreDataCorruptionErrors bool
}

// SearchForEndHeight searches for the EndHeightMessage with the given height
// and returns an auto.GroupReader, whenever it was found or not and an error.
// Group reader will be nil if found equals false.
//
// CONTRACT: caller must close group reader.
func (wal *BaseWAL) SearchForEndHeight(height int64, options *WALSearchOptions) (rd io.ReadCloser, found bool, err error) {
	lastHeightFound := int64(-1)

	// NOTE: starting from the last file in the group because we're usually
	// searching for the last height. See replay.go
	min, max := wal.group.MinIndex(), wal.group.MaxIndex()
	wal.logger.Info("Searching for height", "height", height, "min", min, "max", max)
	for index := max; index >= min; index-- {
		gr, err := wal.group.NewReader(index)
		if err != nil {
			return nil, false, err
		}

		for {
			msg, err := decode(gr)
			if err == io.EOF {
				// OPTIMISATION: no need to look for height in older files if we've seen h < height
				if lastHeightFound > 0 && lastHeightFound < height {
					gr.Close()
					return nil, false, nil
				}
				// check next file
				break
			}
			if options.IgnoreDataCorruptionErrors && utils.ErrorAs[DataCorruptionError](err).IsPresent() {
				wal.logger.Error("Corrupted entry. Skipping...", "err", err)
				// do nothing
				continue
			} else if err != nil {
				gr.Close()
				return nil, false, err
			}

			if m, ok := msg.Msg.any.(EndHeightMessage); ok {
				lastHeightFound = m.Height
				if m.Height == height { // found
					wal.logger.Info("Found", "height", height, "index", index)
					return gr, true, nil
				}
			}
		}
		gr.Close()
	}

	return nil, false, nil
}

// A WALEncoder writes custom-encoded WAL messages to an output stream.
//
// Format: 4 bytes CRC sum + 4 bytes length + arbitrary-length value
// Encode writes the custom encoding of v to the stream. It returns an error if
// the encoded size of v is greater than 4MB. Any error encountered
// during the write is also returned.
func encode(v *TimedWALMessage) ([]byte, error) {
	pbMsg := &tmcons.TimedWALMessage{Time: v.Time, Msg: v.Msg.toProto()}
	data, err := proto.Marshal(pbMsg)
	if err != nil {
		panic(fmt.Errorf("encode timed wall message failure: %w", err))
	}

	crc := crc32.Checksum(data, crc32c)
	length := uint32(len(data))
	if length > maxMsgSizeBytes {
		return nil, fmt.Errorf("msg is too big: %d bytes, max: %d bytes", length, maxMsgSizeBytes)
	}
	totalLength := 8 + int(length)

	msg := make([]byte, totalLength)
	binary.BigEndian.PutUint32(msg[0:4], crc)
	binary.BigEndian.PutUint32(msg[4:8], length)
	copy(msg[8:], data)
	return msg, nil
}

// DataCorruptionError is an error that occures if data on disk was corrupted.
type DataCorruptionError struct{ error }

// A WALDecoder reads and decodes custom-encoded WAL messages from an input
// stream. See WALEncoder for the format used.
//
// It will also compare the checksums and make sure data size is equal to the
// length from the header. If that is not the case, error will be returned.
// Decode reads the next custom-encoded value from its reader and returns it.
func decode(rd io.Reader) (*TimedWALMessage, error) {
	b := make([]byte, 4)

	_, err := rd.Read(b)
	if errors.Is(err, io.EOF) {
		return nil, err
	}
	if err != nil {
		return nil, DataCorruptionError{fmt.Errorf("failed to read checksum: %w", err)}
	}
	crc := binary.BigEndian.Uint32(b)

	b = make([]byte, 4)
	_, err = rd.Read(b)
	if err != nil {
		return nil, DataCorruptionError{fmt.Errorf("failed to read length: %w", err)}
	}
	length := binary.BigEndian.Uint32(b)

	if length > maxMsgSizeBytes {
		return nil, DataCorruptionError{fmt.Errorf(
			"length %d exceeded maximum possible value of %d bytes",
			length,
			maxMsgSizeBytes)}
	}

	data := make([]byte, length)
	n, err := rd.Read(data)
	if err != nil {
		return nil, DataCorruptionError{fmt.Errorf("failed to read data: %v (read: %d, wanted: %d)", err, n, length)}
	}

	// check checksum before decoding data
	actualCRC := crc32.Checksum(data, crc32c)
	if actualCRC != crc {
		return nil, DataCorruptionError{fmt.Errorf("checksums do not match: read: %v, actual: %v", crc, actualCRC)}
	}

	var res = new(tmcons.TimedWALMessage)
	err = proto.Unmarshal(data, res)
	if err != nil {
		return nil, DataCorruptionError{fmt.Errorf("failed to decode data: %w", err)}
	}

	walMsg, err := walFromProto(res.Msg)
	if err != nil {
		return nil, DataCorruptionError{fmt.Errorf("failed to convert from proto: %w", err)}
	}
	tMsgWal := &TimedWALMessage{
		Time: res.Time,
		Msg:  walMsg,
	}

	return tMsgWal, err
}

// repairWalFile decodes messages from src (until the decoder errors) and
// writes them to dst.
func repairWalFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	// best-case repair (until first error is encountered)
	for {
		msg, err := decode(in)
		if err != nil {
			break
		}

		bytes, err := encode(msg)
		if err != nil {
			return fmt.Errorf("encode(): %w", err)
		}
		if _, err := out.Write(bytes); err != nil {
			return fmt.Errorf("out.Write(): %w", err)
		}
	}

	return nil
}

type nilWAL struct{}

var _ WAL = nilWAL{}

func (nilWAL) Write(m WALMessage) error     { return nil }
func (nilWAL) WriteSync(m WALMessage) error { return nil }
func (nilWAL) FlushAndSync() error          { return nil }
func (nilWAL) SearchForEndHeight(height int64, options *WALSearchOptions) (rd io.ReadCloser, found bool, err error) {
	return nil, false, nil
}
func (nilWAL) Start(context.Context) error { return nil }
func (nilWAL) Stop()                       {}
func (nilWAL) Wait()                       {}
