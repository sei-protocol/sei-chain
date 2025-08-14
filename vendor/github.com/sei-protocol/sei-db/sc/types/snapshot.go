package types

import "io"

type Importer interface {
	AddTree(name string) error

	AddNode(node *SnapshotNode)

	io.Closer
}

type Exporter interface {
	Next() (interface{}, error)

	io.Closer
}

// SnapshotNode contains import/export node data.
type SnapshotNode struct {
	Key     []byte
	Value   []byte
	Version int64
	Height  int8
}
