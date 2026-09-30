package types

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"

	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// appHashOverridesDir holds the override files compiled into the binary.
const appHashOverridesDir = "apphash_overrides"

//go:embed all:apphash_overrides
var embeddedAppHashOverrides embed.FS

// AppHashOverride accepts Replacement as the app hash of the state committed at
// Height on ChainID, where the chain's headers record Recorded.
type AppHashOverride struct {
	ChainID     string           `json:"-"`
	Height      int64            `json:"height"`
	Recorded    tmbytes.HexBytes `json:"recorded"`
	Replacement tmbytes.HexBytes `json:"replacement"`
}

// appHashOverrideFile is one JSON file under apphash_overrides/. Source records
// where the rows came from and has no effect.
type appHashOverrideFile struct {
	ChainID   string            `json:"chain_id"`
	Source    string            `json:"source,omitempty"`
	Overrides []AppHashOverride `json:"overrides"`
}

type appHashOverrideKey struct {
	chainID string
	height  int64
}

// appHashOverrideTable is the override table that AppHashMatches reads.
type appHashOverrideTable struct {
	byKey map[appHashOverrideKey]AppHashOverride
}

var appHashOverrides = utils.NewRWMutex(&appHashOverrideTable{byKey: mustLoadAppHashOverrides()})

// AppHashMatches reports whether computed is an acceptable app hash for the
// state committed at height on chainID, where the chain records recorded. It
// accepts equal hashes, and a pair that a compiled override names for that
// chain and height.
func AppHashMatches(chainID string, height int64, recorded, computed []byte) bool {
	if bytes.Equal(recorded, computed) {
		return true
	}
	for table := range appHashOverrides.RLock() {
		o, ok := table.byKey[appHashOverrideKey{chainID, height}]
		return ok && bytes.Equal(o.Recorded, recorded) && bytes.Equal(o.Replacement, computed)
	}
	panic("unreachable")
}

// ReplaceAppHashOverrides installs overrides in place of the compiled table
// and returns a function that restores the previous table. It is for tests.
func ReplaceAppHashOverrides(overrides []AppHashOverride) (restore func()) {
	table, err := indexAppHashOverrides(overrides)
	if err != nil {
		panic(err)
	}
	var previous map[appHashOverrideKey]AppHashOverride
	for t := range appHashOverrides.Lock() {
		previous, t.byKey = t.byKey, table
	}
	return func() {
		for t := range appHashOverrides.Lock() {
			t.byKey = previous
		}
	}
}

func mustLoadAppHashOverrides() map[appHashOverrideKey]AppHashOverride {
	overrides, err := LoadAppHashOverrides(embeddedAppHashOverrides)
	if err != nil {
		panic(fmt.Errorf("app hash overrides: %w", err))
	}
	table, err := indexAppHashOverrides(overrides)
	if err != nil {
		panic(fmt.Errorf("app hash overrides: %w", err))
	}
	return table
}

// LoadAppHashOverrides reads and validates every .json file under
// apphash_overrides/ in fsys.
func LoadAppHashOverrides(fsys fs.FS) ([]AppHashOverride, error) {
	files, err := fs.ReadDir(fsys, appHashOverridesDir)
	if err != nil {
		return nil, err
	}
	var overrides []AppHashOverride
	for _, file := range files {
		if file.IsDir() || path.Ext(file.Name()) != ".json" {
			continue
		}
		filePath := path.Join(appHashOverridesDir, file.Name())
		parsed, err := readAppHashOverrideFile(fsys, filePath)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filePath, err)
		}
		overrides = append(overrides, parsed...)
	}
	if _, err := indexAppHashOverrides(overrides); err != nil {
		return nil, err
	}
	return overrides, nil
}

// decodeStrictJSON decodes data as exactly one JSON value into v, and refuses
// unknown fields and any data after the value.
func decodeStrictJSON(data []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after the JSON value")
	}
	return nil
}

func readAppHashOverrideFile(fsys fs.FS, filePath string) ([]AppHashOverride, error) {
	data, err := fs.ReadFile(fsys, filePath)
	if err != nil {
		return nil, err
	}
	var file appHashOverrideFile
	if err := decodeStrictJSON(data, &file); err != nil {
		return nil, err
	}
	if file.ChainID == "" {
		return nil, errors.New("chain_id is empty")
	}
	if len(file.Overrides) == 0 {
		return nil, errors.New("no overrides")
	}
	for i := range file.Overrides {
		file.Overrides[i].ChainID = file.ChainID
	}
	return file.Overrides, nil
}

func indexAppHashOverrides(overrides []AppHashOverride) (map[appHashOverrideKey]AppHashOverride, error) {
	table := make(map[appHashOverrideKey]AppHashOverride, len(overrides))
	for _, o := range overrides {
		switch {
		case o.ChainID == "":
			return nil, fmt.Errorf("override at height %d has no chain ID", o.Height)
		case o.Height <= 0:
			return nil, fmt.Errorf("%s: height %d is not positive", o.ChainID, o.Height)
		case len(o.Recorded) == 0 || len(o.Replacement) == 0:
			return nil, fmt.Errorf("%s: height %d: recorded and replacement must both be set", o.ChainID, o.Height)
		case bytes.Equal(o.Recorded, o.Replacement):
			return nil, fmt.Errorf("%s: height %d: recorded equals replacement", o.ChainID, o.Height)
		}
		key := appHashOverrideKey{o.ChainID, o.Height}
		if _, ok := table[key]; ok {
			return nil, fmt.Errorf("%s: height %d has more than one override", o.ChainID, o.Height)
		}
		table[key] = o
	}
	return table, nil
}
