// Package parity records the v2 EVM RPC's responses to a curated set of blocks
// and compares the EVM-only RPC's responses to the same requests against them.
package parity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// FixtureExt is the file extension of a recorded fixture.
const FixtureExt = ".json"

// Fixture is one curated replay: the EVM state a run of blocks starts from, the
// blocks as v2 executed and reported them, and v2's response to every request.
type Fixture struct {
	Name        string                     `json:"name"`
	Source      string                     `json:"source"`
	Description string                     `json:"description,omitempty"`
	ChainID     hexutil.Uint64             `json:"chainId"`
	Prestate    map[common.Address]Account `json:"prestate"`
	Blocks      []Block                    `json:"blocks"`
	Requests    []Request                  `json:"requests"`
}

// Account is the EVM state of one address before the fixture's first block.
type Account struct {
	Balance *hexutil.Big                `json:"balance"`
	Nonce   hexutil.Uint64              `json:"nonce,omitempty"`
	Code    hexutil.Bytes               `json:"code,omitempty"`
	Storage map[common.Hash]common.Hash `json:"storage,omitempty"`
}

// Block is one block of a fixture: its header as v2 reported it, the EVM block
// context v2 executed it under, and its transactions as raw Ethereum RLP.
type Block struct {
	Header    Header          `json:"header"`
	Execution Execution       `json:"execution"`
	Txs       []hexutil.Bytes `json:"txs"`
}

// Header is the part of a block's header that the EVM-only RPC reads from the
// consensus block rather than deriving from execution.
type Header struct {
	Number           hexutil.Uint64 `json:"number"`
	Hash             common.Hash    `json:"hash"`
	ParentHash       common.Hash    `json:"parentHash"`
	Time             time.Time      `json:"time"`
	StateRoot        common.Hash    `json:"stateRoot"`
	Miner            common.Address `json:"miner"`
	TransactionsRoot common.Hash    `json:"transactionsRoot"`
	ReceiptsRoot     common.Hash    `json:"receiptsRoot"`
}

// Execution is the EVM block context v2 executed a block under.
type Execution struct {
	Coinbase    common.Address `json:"coinbase"`
	PrevRandao  common.Hash    `json:"prevRandao"`
	GasLimit    hexutil.Uint64 `json:"gasLimit"`
	BaseFee     *hexutil.Big   `json:"baseFee"`
	BlobBaseFee *hexutil.Big   `json:"blobBaseFee"`
}

// Request is one JSON-RPC call and v2's response to it.
type Request struct {
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
	V2     Response          `json:"v2"`
}

// Response is the outcome of a JSON-RPC call: exactly one of Result and Error
// is set, and a null result is recorded as the JSON literal null.
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Tip returns the fixture's last block.
func (f *Fixture) Tip() Block {
	return f.Blocks[len(f.Blocks)-1]
}

// Validate reports whether f is complete enough to replay.
func (f *Fixture) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("fixture has no name")
	}
	if len(f.Blocks) == 0 {
		return fmt.Errorf("fixture %s has no blocks", f.Name)
	}
	for i, block := range f.Blocks {
		if block.Execution.BaseFee == nil || block.Execution.BlobBaseFee == nil {
			return fmt.Errorf("fixture %s block %d has no execution fees", f.Name, block.Header.Number)
		}
		if i > 0 && block.Header.Number != f.Blocks[i-1].Header.Number+1 {
			return fmt.Errorf("fixture %s block %d does not follow block %d", f.Name, block.Header.Number, f.Blocks[i-1].Header.Number)
		}
	}
	for addr, account := range f.Prestate {
		if account.Balance == nil {
			return fmt.Errorf("fixture %s prestate %s has no balance", f.Name, addr)
		}
	}
	return nil
}

// LoadFixtures reads every fixture in dir, ordered by file name.
func LoadFixtures(dir string) ([]*Fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var fixtures []*Fixture
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), FixtureExt) || entry.Name() == KnownDiffsFile {
			continue
		}
		fixture, err := LoadFixture(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, fixture)
	}
	slices.SortFunc(fixtures, func(a, b *Fixture) int { return strings.Compare(a.Name, b.Name) })
	return fixtures, nil
}

// LoadFixture reads and validates the fixture at path.
func LoadFixture(path string) (*Fixture, error) {
	bz, err := os.ReadFile(path) //nolint:gosec // path names a checked-in fixture.
	if err != nil {
		return nil, err
	}
	var fixture Fixture
	if err := json.Unmarshal(bz, &fixture); err != nil {
		return nil, fmt.Errorf("decode fixture %s: %w", path, err)
	}
	if err := fixture.Validate(); err != nil {
		return nil, err
	}
	return &fixture, nil
}

// WriteFixture writes f to dir as indented JSON named after f.Name.
func WriteFixture(dir string, f *Fixture) error {
	if err := f.Validate(); err != nil {
		return err
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(f); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, f.Name+FixtureExt), buf.Bytes(), 0o600)
}
