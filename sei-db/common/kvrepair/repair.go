// Package kvrepair defines the repair file: one reviewed set of key/value
// writes that a hard fork handler applies to module stores at one height.
package kvrepair

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Repair is one reviewed set of entries that runs at the start of block
// TargetRepairHeight on ChainID, with values taken from the state committed at
// StateHeight. Source records where the values came from and has no effect on
// execution.
type Repair struct {
	Name               string  `json:"name"`
	ChainID            string  `json:"chain_id"`
	TargetRepairHeight int64   `json:"target_repair_height"`
	StateHeight        int64   `json:"state_height"`
	Source             string  `json:"source,omitempty"`
	Entries            []Entry `json:"entries"`
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

// HasOld reports whether the entry states the value the key must hold before
// the write.
func (e Entry) HasOld() bool {
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

// Parse decodes one repair file. It refuses unknown fields, a field name that
// is not lowercase or that appears twice in one object, and data after the
// JSON value. It does not validate the repair.
func Parse(data []byte) (Repair, error) {
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

// Validate checks the repair against the rules a node enforces before it
// registers the handler. storeExists reports whether a store name is known.
func (r Repair) Validate(storeExists func(string) bool) error {
	switch {
	case r.Name == "":
		return errors.New("name is empty")
	case r.ChainID == "":
		return errors.New("chain_id is empty")
	case r.TargetRepairHeight <= 0:
		return fmt.Errorf("target_repair_height %d is not positive", r.TargetRepairHeight)
	case r.StateHeight <= 0:
		return fmt.Errorf("state_height %d is not positive", r.StateHeight)
	case r.StateHeight >= r.TargetRepairHeight:
		return fmt.Errorf("state_height %d is not below target_repair_height %d", r.StateHeight, r.TargetRepairHeight)
	case len(r.Entries) == 0:
		return errors.New("no entries")
	}
	// Between StateHeight and TargetRepairHeight-1 a key can change, so a new
	// value taken from StateHeight can be stale. An old value turns a stale entry
	// into a halt; without one the entry would overwrite the newer value.
	valuesAreCurrent := r.StateHeight == r.TargetRepairHeight-1
	seen := map[string]bool{}
	for i, e := range r.Entries {
		if !storeExists(e.Store) {
			return fmt.Errorf("entry %d: unknown store %q", i, e.Store)
		}
		if len(e.Key) == 0 {
			return fmt.Errorf("entry %d: key is empty", i)
		}
		if e.Old != nil && e.OldAbsent {
			return fmt.Errorf("entry %d: old and old_absent are both set", i)
		}
		if !valuesAreCurrent && !e.HasOld() {
			return fmt.Errorf("entry %d: no old value, and state_height is %d, not %d",
				i, r.StateHeight, r.TargetRepairHeight-1)
		}
		id := e.Store + "/" + hex.EncodeToString(e.Key)
		if seen[id] {
			return fmt.Errorf("entry %d: store %s key %x appears twice", i, e.Store, []byte(e.Key))
		}
		seen[id] = true
	}
	return nil
}

// Encode writes r as a repair file with one entry per line.
func Encode(w io.Writer, r Repair) error {
	type field struct {
		name  string
		value any
	}
	header := []field{
		{"name", r.Name},
		{"chain_id", r.ChainID},
		{"target_repair_height", r.TargetRepairHeight},
		{"state_height", r.StateHeight},
	}
	if r.Source != "" {
		header = append(header, field{"source", r.Source})
	}
	var b bytes.Buffer
	b.WriteString("{\n")
	for _, field := range header {
		value, err := json.Marshal(field.value)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "  %q: %s,\n", field.name, value)
	}
	b.WriteString("  \"entries\": [\n")
	for i, e := range r.Entries {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		b.WriteString("    ")
		b.Write(line)
		if i < len(r.Entries)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("  ]\n}\n")
	_, err := w.Write(b.Bytes())
	return err
}
