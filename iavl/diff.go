package iavl

import (
	"github.com/sei-protocol/sei-chain/iavl/proto"
)

type (
	KVPair    = proto.KVPair
	ChangeSet = proto.ChangeSet
)

// KVPairReceiver is callback parameter of method `extractStateChanges` to receive stream of `KVPair`s.
type KVPairReceiver func(pair *KVPair) error
