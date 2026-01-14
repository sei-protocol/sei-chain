package types

import (
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pb"
	"github.com/tendermint/tendermint/internal/protoutils"
)

// TimeConv is the protobuf converter for time.Time.
var TimeConv = protoutils.Conv[time.Time, *pb.Timestamp]{
	// implementation based on
	// https://github.com/pbbuffers/protobuf-go/blob/v1.36.5/types/known/timestamppb/timestamp.pb.go
	Encode: func(t time.Time) *pb.Timestamp {
		return &pb.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		}
	},
	Decode: func(p *pb.Timestamp) (time.Time, error) {
		if p.Seconds == nil {
			return utils.Zero[time.Time](), errors.New("seconds: missing")
		}
		if p.Nanos == nil {
			return utils.Zero[time.Time](), errors.New("nanos: missing")
		}
		return time.Unix(*p.Seconds, int64(*p.Nanos)).UTC(), nil
	},
}

// DurationConv is the protobuf converter for time.Duration.
var DurationConv = protoutils.Conv[time.Duration, *pb.Duration]{
	// implementation based on
	// https://github.com/pbbuffers/protobuf-go/blob/v1.36.5/types/known/durationpb/duration.pb.go
	Encode: func(d time.Duration) *pb.Duration {
		nanos := d.Nanoseconds()
		secs := nanos / 1e9
		nanos -= secs * 1e9
		return &pb.Duration{
			Seconds: proto.Int64(secs),
			Nanos:   proto.Int32(int32(nanos)),
		}
	},
	Decode: func(p *pb.Duration) (time.Duration, error) {
		if p.Seconds == nil {
			return utils.Zero[time.Duration](), errors.New("seconds: missing")
		}
		if p.Nanos == nil {
			return utils.Zero[time.Duration](), errors.New("nanos: missing")
		}
		secs := *p.Seconds
		nanos := *p.Nanos
		d := time.Duration(secs) * time.Second
		overflow := d/time.Second != time.Duration(secs)
		d += time.Duration(nanos) * time.Nanosecond
		overflow = overflow || (secs < 0 && nanos < 0 && d > 0)
		overflow = overflow || (secs > 0 && nanos > 0 && d < 0)
		if overflow {
			return utils.Zero[time.Duration](), errors.New("overflow")
		}
		return d, nil
	},
}
