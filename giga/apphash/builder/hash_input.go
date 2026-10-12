package builder

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// One of the four hashes an app hash is computed from. Each is reported one block at a time by its own method
// (i.e. one of the Report*() methods).
type hashInput uint8

const (
	blockHashInput   hashInput = 0
	stateHashInput   hashInput = 1
	budInput         hashInput = 2
	receiptHashInput hashInput = 3
	hashInputCount             = 4
)

func (i hashInput) String() string {
	switch i {
	case blockHashInput:
		return "block hash"
	case stateHashInput:
		return "state hash"
	case budInput:
		return "BUD"
	case receiptHashInput:
		return "receipt hash"
	default:
		return fmt.Sprintf("hashInput(%d)", uint8(i))
	}
}

// Returns this input's value in record.
func (i hashInput) of(record *apphash.AppHashData) [32]byte {
	switch i {
	case blockHashInput:
		return record.BlockHash()
	case stateHashInput:
		return record.StateHash()
	case budInput:
		return record.BUD()
	case receiptHashInput:
		return record.ReceiptHash()
	default:
		panic(fmt.Sprintf("unknown hash input %d", uint8(i)))
	}
}

// One input's progress through the block heights at and above the giga activation height. A report is a call to
// the method that reports the input (i.e. its Report*() method). The builder uses the tracker to reject a report
// that skips or repeats a block height, and, before setup, to check each report against the app hash stored for its
// block.
type reportTracker struct {

	// Whether the height the input's reports start from is known. The input's first report at or above the giga
	// activation height sets it, as does SetupComplete(). While false, a report at any height is accepted.
	// Afterwards, reports are required to proceed through the block heights in order, one block at a time.
	startingPointKnown bool

	// The only block height the input may report next. A report at any other height stops the builder.
	nextHeight uint64

	// Used only before setup, while the input reports blocks already in the hash vault: the stored records of
	// those blocks, which each report is checked against. Nil at any other time.
	storedRecordIterator apphash.AppHashIterator
}

// A block waiting for its inputs to be reported (i.e. passed to the Report*() methods). It is built once every
// input has been reported.
type pendingBlock struct {

	// The value of each input reported so far, indexed by hashInput.
	values [hashInputCount][32]byte

	// Whether each input has been reported, indexed by hashInput.
	reported [hashInputCount]bool
}

// Returns whether every input has been reported.
func (p *pendingBlock) complete() bool {
	for _, reported := range p.reported {
		if !reported {
			return false
		}
	}
	return true
}
