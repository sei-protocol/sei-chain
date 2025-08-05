package types

import (
	proto "github.com/gogo/protobuf/proto"
)

// Params defines the parameters for the mev module.
type Params struct {
	// Define your params fields here
}

func (m *Params) Reset()         { *m = Params{} }
func (m *Params) String() string { return proto.CompactTextString(m) }
func (*Params) ProtoMessage()    {}
