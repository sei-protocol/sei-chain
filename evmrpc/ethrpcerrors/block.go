package ethrpcerrors

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
)

// CodePrunedHistory is the code go-ethereum's history.PrunedHistoryError carries.
const CodePrunedHistory = 4444

// Message literals go-ethereum's backends define inline rather than as exported sentinels.
const (
	msgHeaderNotFound        = "header not found"
	msgHeaderForHashNotFound = "header for hash not found"
	msgUnknownBlock          = "unknown block"
	msgPrunedHistory         = "pruned history unavailable"
	msgMissingTrieNode       = "missing trie node"
	msgBeyondHead            = "request beyond head block"
)

// Reasons a block, its receipts or its state cannot be served. None of them reaches a client;
// callers select on them with errors.Is to pick the rendering their endpoint family needs.
var (
	ErrBlockAboveLatest = errors.New("block above safe latest")
	ErrBlockUnknownHash = errors.New("no block with this hash")
	ErrBlockNotFound    = errors.New("no block at this height")
	ErrHistoryPruned    = errors.New("history pruned")
	ErrStatePruned      = errors.New("state pruned")
)

// BlockUnavailable reports a block, receipt set or state version this node cannot serve. Its
// message and code are the ones go-ethereum's state-backed endpoints return for the same
// condition; block-fetch endpoints answer null instead (see IsBlockMissing) and eth_getLogs has
// its own text (see ForLogs). It must be the top-level return value, or the code falls back to
// -32000 and the message gains the wrapper's prefix.
type BlockUnavailable struct {
	reason   error
	height   int64
	hash     common.Hash
	earliest int64
	latest   int64
}

var (
	_ rpc.Error     = (*BlockUnavailable)(nil)
	_ rpc.DataError = (*BlockUnavailable)(nil)
)

// BlockAboveLatest reports a height above the safe latest height.
func BlockAboveLatest(height, latest int64) *BlockUnavailable {
	return &BlockUnavailable{reason: ErrBlockAboveLatest, height: height, latest: latest}
}

// BlockUnknownHash reports a hash no block carries.
func BlockUnknownHash(hash common.Hash) *BlockUnavailable {
	return &BlockUnavailable{reason: ErrBlockUnknownHash, hash: hash}
}

// BlockNotFound reports a height inside the served window that the block store has no block for.
func BlockNotFound(height int64) *BlockUnavailable {
	return &BlockUnavailable{reason: ErrBlockNotFound, height: height}
}

// HistoryPruned reports a block or receipt set below the earliest height this node keeps.
func HistoryPruned(height, earliest int64) *BlockUnavailable {
	return &BlockUnavailable{reason: ErrHistoryPruned, height: height, earliest: earliest}
}

// StatePruned reports a height whose state this node no longer holds or never held. earliest is
// 0 when no earliest state height is known.
func StatePruned(height, earliest int64) *BlockUnavailable {
	return &BlockUnavailable{reason: ErrStatePruned, height: height, earliest: earliest}
}

func (e *BlockUnavailable) Error() string {
	switch e.reason {
	case ErrBlockUnknownHash:
		return msgHeaderForHashNotFound
	case ErrHistoryPruned:
		return msgPrunedHistory
	case ErrStatePruned:
		// go-ethereum's message names the missing node and state root; the height is the
		// closest identifier Sei has for the state that is gone.
		msg := fmt.Sprintf("%s: state at height %d is not available", msgMissingTrieNode, e.height)
		if e.earliest > 0 {
			msg += fmt.Sprintf("; earliest available is %d", e.earliest)
		}
		return msg
	default:
		return msgHeaderNotFound
	}
}

// ErrorCode returns the JSON-RPC error code.
func (e *BlockUnavailable) ErrorCode() int {
	if e.reason == ErrHistoryPruned {
		return CodePrunedHistory
	}
	return CodeDefault
}

// ErrorData returns nil; go-ethereum attaches no data to this class of error.
func (e *BlockUnavailable) ErrorData() interface{} { return nil }

// Unwrap returns the reason, so errors.Is can select on it.
func (e *BlockUnavailable) Unwrap() error { return e.reason }

// Detail returns the operator-facing description of the condition with the heights involved.
func (e *BlockUnavailable) Detail() string {
	switch e.reason {
	case ErrBlockAboveLatest:
		return fmt.Sprintf("requested height %d is not yet available; safe latest is %d", e.height, e.latest)
	case ErrBlockUnknownHash:
		return fmt.Sprintf("no block with hash %s", e.hash.Hex())
	case ErrBlockNotFound:
		return fmt.Sprintf("no block at height %d", e.height)
	case ErrHistoryPruned:
		return fmt.Sprintf("history at height %d has been pruned; earliest available is %d", e.height, e.earliest)
	case ErrStatePruned:
		if e.earliest > 0 {
			return fmt.Sprintf("state at height %d has been pruned; earliest available is %d", e.height, e.earliest)
		}
		return fmt.Sprintf("state at height %d does not exist", e.height)
	default:
		return e.reason.Error()
	}
}

// IsBlockMissing reports whether err says the requested block does not exist from the caller's
// point of view: above the safe latest height, unknown by hash, or absent from the block store.
// Endpoints that return a block, or something inside one, answer such a request with null.
func IsBlockMissing(err error) bool {
	return errors.Is(err, ErrBlockAboveLatest) || errors.Is(err, ErrBlockUnknownHash) || errors.Is(err, ErrBlockNotFound)
}

// ForLogs renders err the way go-ethereum's eth_getLogs does: "unknown block" when the requested
// block does not exist. Any other error, including pruned history, is returned unchanged.
func ForLogs(err error) error {
	if IsBlockMissing(err) {
		return &Error{code: CodeDefault, message: msgUnknownBlock}
	}
	return err
}

// ForState renders err for a state-backed endpoint such as eth_call or eth_getBalance. go-ethereum
// keeps headers when it prunes history, so those endpoints only ever report the state as missing;
// a pruned block here is reported the same way. Any other error is returned unchanged.
func ForState(err error) error {
	var e *BlockUnavailable
	if errors.As(err, &e) && e.reason == ErrHistoryPruned {
		return StatePruned(e.height, e.earliest)
	}
	return err
}

// BeyondHead is eth_feeHistory's rejection of a newest block above the head.
func BeyondHead(requested, head int64) error {
	return &Error{code: CodeDefault, message: fmt.Sprintf("%s: requested %d, head %d", msgBeyondHead, requested, head)}
}
