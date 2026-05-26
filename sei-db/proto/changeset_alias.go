package proto

import (
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// On this shadow branch the generated changeset.pb.go has not yet been moved
// into sei-db/proto (that happens on main as part of the proto migration).
// Several test helpers and call sites expect proto.KVPair / proto.ChangeSet
// to be available alongside proto.NamedChangeSet; expose them here as type
// aliases of the existing sei-iavl types that NamedChangeSet.Changeset
// already references. This is compile-time only and preserves wire
// compatibility — NamedChangeSet.Changeset is still iavl.ChangeSet.
type (
	KVPair    = iavl.KVPair
	ChangeSet = iavl.ChangeSet
)
