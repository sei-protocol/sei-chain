package flatcache

import "github.com/sei-protocol/sei-chain/sei-iavl/proto"

var _ Cache = (*cache)(nil)

// A standard implementation of a flatcache.
type cache struct {
}

// Creates a new Cache.
func NewCache() Cache {
	return &cache{}
}

func (f *cache) BatchSet(entries []*proto.KVPair) error {
	panic("unimplemented")
}

func (f *cache) Delete(key []byte) error {
	panic("unimplemented")
}

func (f *cache) Get(key []byte) ([]byte, bool, error) {
	panic("unimplemented")
}

func (f *cache) GetPrevious(key []byte) ([]byte, bool, error) {
	panic("unimplemented")
}

func (f *cache) Set(key []byte, value []byte) error {
	panic("unimplemented")
}
