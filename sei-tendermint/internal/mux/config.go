package mux

import (
	"github.com/tendermint/tendermint/internal/mux/pb"
	"github.com/tendermint/tendermint/internal/protoutils"
)


type Handshake struct {
	Kinds map[StreamKind]*StreamKindConfig
}

var HandshakeConv = protoutils.Conv[*Handshake,*pb.Handshake]{
	Encode: func(h *Handshake) *pb.Handshake {
		var kinds []*pb.StreamKindConfig
		for kind,c := range h.Kinds {
			kinds = append(kinds, &pb.StreamKindConfig {
				Kind: uint64(kind),
				MaxConnects: c.MaxConnects,
				MaxAccepts: c.MaxAccepts,
			})
		}
		return &pb.Handshake{Kinds: kinds} 
	},
	Decode: func(x *pb.Handshake) (*Handshake,error) { 
		kinds := map[StreamKind]*StreamKindConfig{}
		for _,pc := range x.Kinds {
			kinds[StreamKind(pc.Kind)] = &StreamKindConfig {
				MaxConnects: pc.MaxConnects,
				MaxAccepts: pc.MaxAccepts,
			}
		}
		return &Handshake {Kinds:kinds},nil
	},
}

