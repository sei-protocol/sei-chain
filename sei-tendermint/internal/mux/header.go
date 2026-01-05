package mux

import (
	"fmt"
	"github.com/tendermint/tendermint/internal/mux/pb"
	"github.com/tendermint/tendermint/libs/utils"
)

/*
id%2==0 => connected stream
id%2==1 => accepted stream
*/

type FrameOpen struct {
	StreamID StreamID
	StreamKind StreamKind
	MaxMsgSize uint64
	WindowEnd uint64
}

type FrameResize struct {
	StreamID StreamID
	WindowEnd uint64
}

type FrameMsg struct {
	StreamID StreamID
	PayloadSize uint64
	MsgEnd bool
}

type FrameClose struct {
	StreamID StreamID
}

type Frame interface {
	isFrame()
}

func (*FrameOpen) isFrame() {}
func (*FrameResize) isFrame() {}
func (*FrameMsg) isFrame() {}
func (*FrameClose) isFrame() {}

func EncodeFrame(f Frame) *pb.Frame {
	switch f := f.(type) {
	case *FrameOpen:
		return &pb.Frame {
			StreamId: uint64(f.StreamID),
			StreamKind: utils.Alloc(uint64(f.StreamKind)),
			MaxMsgSize: utils.Alloc(f.MaxMsgSize),
			WindowEnd: utils.Alloc(f.WindowEnd),
		}
	case *FrameResize:
		return &pb.Frame {
			StreamId: uint64(f.StreamID),
			WindowEnd: utils.Alloc(f.WindowEnd),
		}
	case *FrameMsg:
		x := &pb.Frame {
			StreamId: uint64(f.StreamID),
			PayloadSize: utils.Alloc(uint64(f.PayloadSize)),
		}
		if f.MsgEnd {
			x.MsgEnd = utils.Alloc(true)
		}
		return x
	case *FrameClose:
		return &pb.Frame {
			StreamId: uint64(f.StreamID),
			StreamEnd: utils.Alloc(true),
		}
	default:
		panic(fmt.Errorf("unknown frame type %T",f))
	}
}

func DecodeFrame(x *pb.Frame) Frame {
	switch {
	case x.StreamKind!=nil:
		return &FrameOpen {
			StreamID: StreamID(x.StreamId),
			StreamKind: StreamKind(x.GetStreamKind()),
			MaxMsgSize: x.GetMaxMsgSize(),
			WindowEnd: x.GetWindowEnd(),
		}
	case x.WindowEnd!=nil:
		return &FrameResize {
			StreamID: StreamID(x.StreamId),
			WindowEnd: x.GetWindowEnd(),
		}
	case x.GetStreamEnd():
		return &FrameClose{
			StreamID: StreamID(x.StreamId),
		}
	default:
		return &FrameMsg {
			StreamID: StreamID(x.StreamId),
			PayloadSize: x.GetPayloadSize(),
			MsgEnd: x.GetMsgEnd(),
		}
	}
}
