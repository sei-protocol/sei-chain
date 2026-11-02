package littblock

import "fmt"

// recordKind identifies a primary record or secondary lookup namespace.
type recordKind uint64

// kindRegistry contains the registered record kinds.
type kindRegistry map[recordKind]struct{}

// register registers an explicit kind ID and panics if the ID is already registered.
func (r kindRegistry) register(id uint64) recordKind {
	kind := recordKind(id)
	if _, ok := r[kind]; ok {
		panic(fmt.Errorf("conflicting record kind %d", id))
	}
	r[kind] = struct{}{}
	return kind
}

var kinds = kindRegistry{}

// These IDs are persisted in ledger keys and must not be reassigned or reused.
var (
	kindBlock     = kinds.register(0)
	kindAppQC     = kinds.register(1)
	kindLaneBlock = kinds.register(2)
	kindBlockHash = kinds.register(3)
	kindAppProp   = kinds.register(4)
	kindQC        = kinds.register(5)
)
