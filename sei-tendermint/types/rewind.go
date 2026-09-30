package types

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/tmhash"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// rewindsDir holds the rewind files compiled into the binary.
const rewindsDir = "rewinds"

//go:embed all:rewinds
var embeddedRewinds embed.FS

// Rewind records that ChainID restarted from the state at SafeHeight and
// abandoned the blocks in Discarded, which it had committed above SafeHeight.
type Rewind struct {
	ChainID    string           `json:"chain_id"`
	Source     string           `json:"source,omitempty"`
	SafeHeight int64            `json:"safe_height"`
	Discarded  []DiscardedBlock `json:"discarded"`
}

// DiscardedBlock is a block that a rewind abandoned.
type DiscardedBlock struct {
	Height int64            `json:"height"`
	Hash   tmbytes.HexBytes `json:"hash"`
}

// ErrDiscardedBlock is returned for a block that a compiled rewind abandoned.
var ErrDiscardedBlock = errors.New("block was discarded by a rewind")

// lastDiscardedHeight returns the height of the highest block r abandoned.
func (r Rewind) lastDiscardedHeight() int64 {
	return r.SafeHeight + int64(len(r.Discarded))
}

// rewindTable holds the rewinds that the lookups read, by chain ID.
type rewindTable struct {
	byChain map[string][]Rewind
}

var rewinds = utils.NewRWMutex(&rewindTable{byChain: mustLoadRewinds()})

// IsDiscardedBlock reports whether a compiled rewind abandoned the block with
// hash at height on chainID.
func IsDiscardedBlock(chainID string, height int64, hash []byte) bool {
	r, ok := rewindCovering(chainID, height)
	return ok && bytes.Equal(r.Discarded[height-r.SafeHeight-1].Hash, hash)
}

// InRewoundWindow reports whether height on chainID is above the safe height
// of a compiled rewind and at or below the last block it discarded.
func InRewoundWindow(chainID string, height int64) bool {
	_, ok := rewindCovering(chainID, height)
	return ok
}

func rewindCovering(chainID string, height int64) (Rewind, bool) {
	for table := range rewinds.RLock() {
		for _, r := range table.byChain[chainID] {
			if height > r.SafeHeight && height <= r.lastDiscardedHeight() {
				return r, true
			}
		}
	}
	return Rewind{}, false
}

// ReplaceRewinds installs rs in place of the compiled rewinds and returns a
// function that restores the previous set. It is for tests.
func ReplaceRewinds(rs []Rewind) (restore func()) {
	table, err := indexRewinds(rs)
	if err != nil {
		panic(err)
	}
	var previous map[string][]Rewind
	for t := range rewinds.Lock() {
		previous, t.byChain = t.byChain, table
	}
	return func() {
		for t := range rewinds.Lock() {
			t.byChain = previous
		}
	}
}

func mustLoadRewinds() map[string][]Rewind {
	rs, err := LoadRewinds(embeddedRewinds)
	if err != nil {
		panic(fmt.Errorf("rewinds: %w", err))
	}
	table, err := indexRewinds(rs)
	if err != nil {
		panic(fmt.Errorf("rewinds: %w", err))
	}
	return table
}

// LoadRewinds reads and validates every .json file under rewinds/ in fsys.
func LoadRewinds(fsys fs.FS) ([]Rewind, error) {
	files, err := fs.ReadDir(fsys, rewindsDir)
	if err != nil {
		return nil, err
	}
	var rs []Rewind
	for _, file := range files {
		if file.IsDir() || path.Ext(file.Name()) != ".json" {
			continue
		}
		filePath := path.Join(rewindsDir, file.Name())
		r, err := readRewindFile(fsys, filePath)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filePath, err)
		}
		rs = append(rs, r)
	}
	if _, err := indexRewinds(rs); err != nil {
		return nil, err
	}
	return rs, nil
}

func readRewindFile(fsys fs.FS, filePath string) (Rewind, error) {
	data, err := fs.ReadFile(fsys, filePath)
	if err != nil {
		return Rewind{}, err
	}
	var r Rewind
	if err := decodeStrictJSON(data, &r); err != nil {
		return Rewind{}, err
	}
	return r, nil
}

func indexRewinds(rs []Rewind) (map[string][]Rewind, error) {
	table := make(map[string][]Rewind, len(rs))
	for _, r := range rs {
		if err := validateRewind(r); err != nil {
			return nil, err
		}
		for _, other := range table[r.ChainID] {
			if r.SafeHeight < other.lastDiscardedHeight() && other.SafeHeight < r.lastDiscardedHeight() {
				return nil, fmt.Errorf("%s: rewinds from %d and %d overlap", r.ChainID, r.SafeHeight, other.SafeHeight)
			}
		}
		table[r.ChainID] = append(table[r.ChainID], r)
	}
	return table, nil
}

func validateRewind(r Rewind) error {
	switch {
	case r.ChainID == "":
		return errors.New("rewind has no chain ID")
	case r.SafeHeight <= 0:
		return fmt.Errorf("%s: safe height %d is not positive", r.ChainID, r.SafeHeight)
	case len(r.Discarded) == 0:
		return fmt.Errorf("%s: rewind from %d discards no blocks", r.ChainID, r.SafeHeight)
	}
	for i, d := range r.Discarded {
		if want := r.SafeHeight + 1 + int64(i); d.Height != want {
			return fmt.Errorf("%s: discarded block %d has height %d, want %d", r.ChainID, i, d.Height, want)
		}
		if len(d.Hash) != tmhash.Size {
			return fmt.Errorf("%s: discarded block at height %d has a %d-byte hash", r.ChainID, d.Height, len(d.Hash))
		}
	}
	return nil
}
