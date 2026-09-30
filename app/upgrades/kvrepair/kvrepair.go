// Package kvrepair applies reviewed key/value repairs to module stores as hard
// fork handlers. Each repair is a JSON file under repairs/ that names one chain,
// one height, and the entries to write or delete at that height.
package kvrepair

import (
	"bytes"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/app/upgrades"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

var logger = seilog.NewLogger("app", "upgrades", "kvrepair")

// repairsDir holds the repair files compiled into the binary.
const repairsDir = "repairs"

//go:embed all:repairs
var embeddedRepairs embed.FS

// Repair is one reviewed set of entries that runs at Height on ChainID. Source
// records where the values came from and has no effect on execution.
type Repair struct {
	Name    string  `json:"name"`
	ChainID string  `json:"chain_id"`
	Height  int64   `json:"height"`
	Source  string  `json:"source,omitempty"`
	Entries []Entry `json:"entries"`
}

// Entry sets Key in Store to Value, or deletes Key when Value is nil. Expect
// and ExpectAbsent state the value the entry must find before it writes, unless
// the key already holds its target.
type Entry struct {
	Store        string    `json:"store"`
	Key          HexBytes  `json:"key"`
	Value        *HexBytes `json:"value"`
	Expect       *HexBytes `json:"expect,omitempty"`
	ExpectAbsent bool      `json:"expect_absent,omitempty"`
}

// HexBytes is a byte string that encodes as hex in JSON, with or without a 0x
// prefix.
type HexBytes []byte

func (b HexBytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(hex.EncodeToString(b))
}

func (b *HexBytes) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X"))
	if err != nil {
		return fmt.Errorf("invalid hex %q: %w", s, err)
	}
	*b = decoded
	return nil
}

// Register validates every embedded repair against the store keys and
// registers one handler for each. It panics on an invalid repair file.
func Register(m *upgrades.HardForkManager, keys map[string]*sdk.KVStoreKey) {
	repairs, err := Load(embeddedRepairs, keys)
	if err != nil {
		panic(fmt.Errorf("kvrepair: %w", err))
	}
	for _, r := range repairs {
		m.RegisterHandler(NewHandler(r, keys))
	}
}

// Load reads and validates every .json file under repairs/ in fsys.
func Load(fsys fs.FS, keys map[string]*sdk.KVStoreKey) ([]Repair, error) {
	files, err := fs.ReadDir(fsys, repairsDir)
	if err != nil {
		return nil, err
	}
	var repairs []Repair
	names := map[string]string{}
	targets := map[string]string{}
	for _, file := range files {
		if file.IsDir() || path.Ext(file.Name()) != ".json" {
			continue
		}
		filePath := path.Join(repairsDir, file.Name())
		r, err := readRepair(fsys, filePath)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filePath, err)
		}
		if err := r.validate(keys); err != nil {
			return nil, fmt.Errorf("%s: %w", filePath, err)
		}
		if other, ok := names[r.Name]; ok {
			return nil, fmt.Errorf("%s: repair name %q already used by %s", filePath, r.Name, other)
		}
		names[r.Name] = filePath
		for _, e := range r.Entries {
			target := fmt.Sprintf("%s/%d/%s/%x", r.ChainID, r.Height, e.Store, []byte(e.Key))
			if other, ok := targets[target]; ok {
				return nil, fmt.Errorf("%s: store %s key %x at height %d is also repaired by %s",
					filePath, e.Store, []byte(e.Key), r.Height, other)
			}
			targets[target] = filePath
		}
		repairs = append(repairs, r)
	}
	return repairs, nil
}

func readRepair(fsys fs.FS, filePath string) (Repair, error) {
	data, err := fs.ReadFile(fsys, filePath)
	if err != nil {
		return Repair{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var r Repair
	if err := decoder.Decode(&r); err != nil {
		return Repair{}, err
	}
	return r, nil
}

func (r Repair) validate(keys map[string]*sdk.KVStoreKey) error {
	switch {
	case r.Name == "":
		return errors.New("name is empty")
	case r.ChainID == "":
		return errors.New("chain_id is empty")
	case r.Height <= 0:
		return fmt.Errorf("height %d is not positive", r.Height)
	case len(r.Entries) == 0:
		return errors.New("no entries")
	}
	seen := map[string]bool{}
	for i, e := range r.Entries {
		if _, ok := keys[e.Store]; !ok {
			return fmt.Errorf("entry %d: unknown store %q", i, e.Store)
		}
		if len(e.Key) == 0 {
			return fmt.Errorf("entry %d: key is empty", i)
		}
		if e.Value != nil && len(*e.Value) == 0 {
			return fmt.Errorf("entry %d: value is empty; omit it to delete the key", i)
		}
		if e.Expect != nil && e.ExpectAbsent {
			return fmt.Errorf("entry %d: expect and expect_absent are both set", i)
		}
		id := e.Store + "/" + hex.EncodeToString(e.Key)
		if seen[id] {
			return fmt.Errorf("entry %d: store %s key %x appears twice", i, e.Store, []byte(e.Key))
		}
		seen[id] = true
	}
	return nil
}

// Handler implements upgrades.HardForkHandler for one Repair.
type Handler struct {
	repair Repair
	keys   map[string]*sdk.KVStoreKey
}

// NewHandler returns the hard fork handler that applies r.
func NewHandler(r Repair, keys map[string]*sdk.KVStoreKey) upgrades.HardForkHandler {
	return Handler{repair: r, keys: keys}
}

func (h Handler) GetName() string { return "kvrepair-" + h.repair.Name }

func (h Handler) GetTargetChainID() string { return h.repair.ChainID }

func (h Handler) GetTargetHeight() int64 { return h.repair.Height }

// ExecuteHandler writes every entry and reads each one back. It returns an
// error when an entry that does not hold its target finds a value other than
// the one it expects, or when a read-back does not match the write.
func (h Handler) ExecuteHandler(ctx sdk.Context) error {
	var repaired, alreadyCorrect int
	for i, e := range h.repair.Entries {
		store := ctx.KVStore(h.keys[e.Store])
		current := store.Get(e.Key)
		if holdsTarget(current, e.Value) {
			alreadyCorrect++
		} else if err := checkExpected(current, e); err != nil {
			return fmt.Errorf("kvrepair %s: entry %d (store %s key %x): %w",
				h.repair.Name, i, e.Store, []byte(e.Key), err)
		} else {
			repaired++
		}
		// Entries that already hold their target are written too, so that a
		// correct node and a repaired node commit the same changeset.
		if e.Value == nil {
			store.Delete(e.Key)
		} else {
			store.Set(e.Key, *e.Value)
		}
		if got := store.Get(e.Key); !holdsTarget(got, e.Value) {
			return fmt.Errorf("kvrepair %s: entry %d (store %s key %x): read back %x after the write",
				h.repair.Name, i, e.Store, []byte(e.Key), got)
		}
	}
	logger.Info("applied kv repair",
		"repair", h.repair.Name,
		"height", ctx.BlockHeight(),
		"repaired", repaired,
		"already_correct", alreadyCorrect,
	)
	return nil
}

func holdsTarget(current []byte, target *HexBytes) bool {
	if target == nil {
		return current == nil
	}
	return current != nil && bytes.Equal(current, *target)
}

func checkExpected(current []byte, e Entry) error {
	switch {
	case e.ExpectAbsent && current != nil:
		return fmt.Errorf("expected no value, found %x", current)
	case e.Expect != nil && (current == nil || !bytes.Equal(current, *e.Expect)):
		return fmt.Errorf("expected %x, found %x", []byte(*e.Expect), current)
	}
	return nil
}
