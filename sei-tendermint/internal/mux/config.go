package mux

import (
	"github.com/tendermint/tendermint/internal/mux/pb"
	"github.com/tendermint/tendermint/internal/protoutils"
)


type Handshake struct {
	MaxStreams map[StreamKind]uint64
}

var HandshakeConv = protoutils.Conv[*Handshake,*pb.Handshake]{
	Encode: func(h *Handshake) *pb.Handshake {
		var maxStreams []*pb.MaxStreams
		for kind,limit := range h.MaxStreams {
			maxStreams = append(maxStreams, &pb.MaxStreams {
				Kind: uint64(kind),
				Limit: limit,
			})
		}
		return &pb.Handshake{
			MaxStreams: maxStreams, 
		}
	},
	Decode: func(x *pb.Handshake) (*Handshake,error) { 
		maxStreams := map[StreamKind]uint64{}
		for _,ms := range x.MaxStreams {
			maxStreams[StreamKind(ms.Kind)] = ms.Limit
		}
		return &Handshake {
			MaxStreams: maxStreams,
		}, nil
	},
}

