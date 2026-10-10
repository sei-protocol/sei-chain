package builder

import "github.com/sei-protocol/sei-chain/giga/apphash"

var _ apphash.AppHashIterator = emptyIterator{}

// An iterator over no app hashes.
type emptyIterator struct{}

func (emptyIterator) Next() (bool, error) {
	return false, nil
}

func (emptyIterator) Entry() *apphash.AppHashData {
	return nil
}

func (emptyIterator) Close() error {
	return nil
}
