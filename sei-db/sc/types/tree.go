package types

import (
	"io"

	ics23 "github.com/confio/ics23/go"
	dbm "github.com/tendermint/tm-db"
)

type Tree interface {
	Get(key []byte) []byte

	Has(key []byte) bool

	Set(key, value []byte)

	Remove(key []byte)

	Version() int64

	RootHash() []byte

	Iterator(start, end []byte, ascending bool) dbm.Iterator

	GetProof(key []byte) *ics23.CommitmentProof

	io.Closer
}
