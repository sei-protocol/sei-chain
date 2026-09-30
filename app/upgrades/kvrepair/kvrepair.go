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
	"io"
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

// Repair is one reviewed set of entries that runs at Height on ChainID, with
// target values read at ReadHeight. Source records where the values came from
// and has no effect on execution.
type Repair struct {
	Name       string  `json:"name"`
	ChainID    string  `json:"chain_id"`
	Height     int64   `json:"height"`
	ReadHeight int64   `json:"read_height"`
	Source     string  `json:"source,omitempty"`
	Entries    []Entry `json:"entries"`
}

// Entry sets Key in Store to New, or deletes Key when New is nil. An empty New
// or Old is an empty value, which differs from an absent key. Old and OldAbsent
// state the value the key must hold before the write, unless the key already
// holds New.
type Entry struct {
	Store     string    `json:"store"`
	Key       HexBytes  `json:"key"`
	New       *HexBytes `json:"new"`
	Old       *HexBytes `json:"old,omitempty"`
	OldAbsent bool      `json:"old_absent,omitempty"`
}

// UnmarshalJSON decodes an entry and refuses unknown fields. It requires the
// new field, where null deletes the key, and it refuses a null old.
func (e *Entry) UnmarshalJSON(data []byte) error {
	var fields struct {
		Store     string          `json:"store"`
		Key       HexBytes        `json:"key"`
		New       json.RawMessage `json:"new"`
		Old       json.RawMessage `json:"old"`
		OldAbsent bool            `json:"old_absent"`
	}
	if err := decodeStrict(data, &fields); err != nil {
		return err
	}
	switch {
	case fields.New == nil:
		return fmt.Errorf("entry for key %x: new is missing; use null to delete the key", []byte(fields.Key))
	case isJSONNull(fields.Old):
		return fmt.Errorf(`entry for key %x: old is null; omit it, or use "old_absent": true`, []byte(fields.Key))
	}
	entry := Entry{Store: fields.Store, Key: fields.Key, OldAbsent: fields.OldAbsent}
	if !isJSONNull(fields.New) {
		newValue := HexBytes{}
		if err := json.Unmarshal(fields.New, &newValue); err != nil {
			return err
		}
		entry.New = &newValue
	}
	if fields.Old != nil {
		oldValue := HexBytes{}
		if err := json.Unmarshal(fields.Old, &oldValue); err != nil {
			return err
		}
		entry.Old = &oldValue
	}
	*e = entry
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func (e Entry) hasOld() bool {
	return e.Old != nil || e.OldAbsent
}

// HexBytes is a byte string that encodes as hex in JSON, with or without a 0x
// prefix. An empty string decodes to an empty, non-nil value.
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
	// KVStore.Set panics on a nil value, and an empty value is valid state.
	*b = append(HexBytes{}, decoded...)
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
	var r Repair
	if err := decodeStrict(data, &r); err != nil {
		return Repair{}, err
	}
	return r, nil
}

// decodeStrict decodes data as exactly one JSON value into v, and refuses
// unknown fields, field names that checkFieldNames refuses, and any data after
// the value.
func decodeStrict(data []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after the JSON value")
	}
	return checkFieldNames(json.NewDecoder(bytes.NewReader(data)))
}

// checkFieldNames reads one JSON value from d and returns an error when an
// object in it names a field twice, or names a field with anything other than
// lowercase ASCII letters, digits, and underscores.
func checkFieldNames(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return err
			}
			name, _ := token.(string)
			// encoding/json matches field names without regard to case, so a
			// second spelling of a name would set the same field.
			if !isLowercaseName(name) {
				return fmt.Errorf("field name %q must use lowercase ASCII letters, digits, and underscores", name)
			}
			if seen[name] {
				return fmt.Errorf("field %q appears twice in one object", name)
			}
			seen[name] = true
			if err := checkFieldNames(d); err != nil {
				return err
			}
		}
	case json.Delim('['):
		for d.More() {
			if err := checkFieldNames(d); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	_, err = d.Token()
	return err
}

func isLowercaseName(name string) bool {
	for _, c := range []byte(name) {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return name != ""
}

func (r Repair) validate(keys map[string]*sdk.KVStoreKey) error {
	switch {
	case r.Name == "":
		return errors.New("name is empty")
	case r.ChainID == "":
		return errors.New("chain_id is empty")
	case r.Height <= 0:
		return fmt.Errorf("height %d is not positive", r.Height)
	case r.ReadHeight <= 0:
		return fmt.Errorf("read_height %d is not positive", r.ReadHeight)
	case r.ReadHeight >= r.Height:
		return fmt.Errorf("read_height %d is not below height %d", r.ReadHeight, r.Height)
	case len(r.Entries) == 0:
		return errors.New("no entries")
	}
	// Between ReadHeight and Height-1 a key can change, so a new value read at
	// ReadHeight can be stale. An old value turns a stale entry into a halt;
	// without one the entry would overwrite the newer value.
	valuesAreCurrent := r.ReadHeight == r.Height-1
	seen := map[string]bool{}
	for i, e := range r.Entries {
		if _, ok := keys[e.Store]; !ok {
			return fmt.Errorf("entry %d: unknown store %q", i, e.Store)
		}
		if len(e.Key) == 0 {
			return fmt.Errorf("entry %d: key is empty", i)
		}
		if e.Old != nil && e.OldAbsent {
			return fmt.Errorf("entry %d: old and old_absent are both set", i)
		}
		if !valuesAreCurrent && !e.hasOld() {
			return fmt.Errorf("entry %d: no old value, and the new values were read at %d, not %d",
				i, r.ReadHeight, r.Height-1)
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
// error when a key that does not hold its new value holds something other than
// its old value, or when a read-back does not match the write.
func (h Handler) ExecuteHandler(ctx sdk.Context) error {
	var repaired, alreadyCorrect int
	for i, e := range h.repair.Entries {
		store := ctx.KVStore(h.keys[e.Store])
		current := store.Get(e.Key)
		if holdsTarget(current, e.New) {
			alreadyCorrect++
		} else if err := checkOld(current, e); err != nil {
			return fmt.Errorf("kvrepair %s: entry %d (store %s key %x): %w",
				h.repair.Name, i, e.Store, []byte(e.Key), err)
		} else {
			repaired++
		}
		// Entries that already hold their target are written too, so that a
		// correct node and a repaired node commit the same changeset.
		if e.New == nil {
			store.Delete(e.Key)
		} else {
			store.Set(e.Key, *e.New)
		}
		if got := store.Get(e.Key); !holdsTarget(got, e.New) {
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

func checkOld(current []byte, e Entry) error {
	switch {
	case e.OldAbsent && current != nil:
		return fmt.Errorf("expected the key to be absent, found %x", current)
	case e.Old != nil && (current == nil || !bytes.Equal(current, *e.Old)):
		return fmt.Errorf("expected old value %x, found %x", []byte(*e.Old), current)
	}
	return nil
}
