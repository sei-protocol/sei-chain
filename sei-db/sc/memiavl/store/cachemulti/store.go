package cachemulti

import (
	"io"

	"github.com/cosmos/cosmos-sdk/store/cachemulti"
	"github.com/cosmos/cosmos-sdk/store/types"
	dbm "github.com/tendermint/tm-db"
)

var NoopCloser io.Closer = CloserFunc(func() error { return nil })

type CloserFunc func() error

func (fn CloserFunc) Close() error {
	return fn()
}

// Store wraps sdk's cachemulti.Store to add io.Closer interface
type Store struct {
	cachemulti.Store
	io.Closer
}

func NewStore(
	db dbm.DB, stores map[types.StoreKey]types.CacheWrapper, keys map[string]types.StoreKey,
	traceWriter io.Writer, traceContext types.TraceContext, listeners map[types.StoreKey][]types.WriteListener,
	closer io.Closer,
) Store {
	if closer == nil {
		closer = NoopCloser
	}
	return Store{
		Store:  cachemulti.NewStore(db, stores, keys, traceWriter, traceContext, listeners),
		Closer: closer,
	}
}
