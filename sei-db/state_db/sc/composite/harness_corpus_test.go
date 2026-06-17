package composite

// FlatKV archive-validation harness (Arm A) — corpus reader.
// Loads a corpus-gen corpus (bdchatham-designs/.../tools/corpus-gen) and lowers each block to
// the proto.NamedChangeSet the migration consumes. Test-only.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// --- corpus-gen JSON schema (see tools/corpus-gen/README.md) ---

type harnessManifest struct {
	SchemaVersion int    `json:"schema_version"`
	Scenario      string `json:"scenario"`
	Seed          int64  `json:"seed"`
	Schedule      struct {
		KeysToMigratePerBlock int    `json:"keys_to_migrate_per_block"`
		OneBatchPerBlock      bool   `json:"one_batch_per_block"`
		CanonicalKeyOrder     string `json:"canonical_key_order"`
	} `json:"schedule"`
	BoundaryHeight int64  `json:"boundary_height"`
	NBlocks        int    `json:"n_blocks"`
	CorpusSHA256   string `json:"corpus_sha256"`
}

type harnessKV struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Delete bool   `json:"delete"`
}

type harnessBlock struct {
	Height         int64 `json:"height"`
	NamedChangeSet struct {
		Name  string      `json:"name"`
		Pairs []harnessKV `json:"pairs"`
	} `json:"named_changeset"`
}

type harnessAssertions struct {
	BoundaryHeight int64              `json:"boundary_height"`
	FinalHeight    int64              `json:"final_height"`
	ExpectedState  map[string]string  `json:"expected_state"` // hex key -> hex value: the v0 TRUTH oracle
	EdgeBlocks     map[string][]int64 `json:"edge_blocks"`    // edge -> heights (failure-matrix map)
}

// harnessCorpus is a loaded corpus-gen corpus: a fixed-schedule sequence of per-block EVM-key
// mutations plus the expected LOGICAL state (the independent v0 fold) used as the truth oracle.
type harnessCorpus struct {
	Manifest   harnessManifest
	Boundary   []harnessKV // state at H (no deletes -> Import path)
	Blocks     []harnessBlock
	Assertions harnessAssertions
}

func loadHarnessCorpus(dir string) (*harnessCorpus, error) {
	c := &harnessCorpus{}
	if err := readJSON(filepath.Join(dir, "manifest.json"), &c.Manifest); err != nil {
		return nil, err
	}
	var boundary struct {
		Height int64       `json:"height"`
		Pairs  []harnessKV `json:"pairs"`
	}
	if err := readJSON(filepath.Join(dir, "boundary.json"), &boundary); err != nil {
		return nil, err
	}
	c.Boundary = boundary.Pairs
	if err := readJSON(filepath.Join(dir, "assertions.json"), &c.Assertions); err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(dir, "blocks", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files) // filenames are zero-padded heights -> ascending order
	for _, f := range files {
		var blk harnessBlock
		if err := readJSON(f, &blk); err != nil {
			return nil, err
		}
		c.Blocks = append(c.Blocks, blk)
	}
	if len(c.Blocks) != c.Manifest.NBlocks {
		return nil, fmt.Errorf("corpus %s: manifest n_blocks=%d but found %d block files (incomplete corpus)",
			dir, c.Manifest.NBlocks, len(c.Blocks))
	}
	return c, nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// toNamedChangeSet lowers a corpus block to the proto.NamedChangeSet the migration consumes.
func (blk harnessBlock) toNamedChangeSet() (*proto.NamedChangeSet, error) {
	return lowerPairs(blk.NamedChangeSet.Name, blk.NamedChangeSet.Pairs)
}

// lowerPairs lowers hex-encoded corpus pairs into a named changeset. An empty value string decodes
// to nil bytes (a delete or storage-zero).
func lowerPairs(name string, in []harnessKV) (*proto.NamedChangeSet, error) {
	pairs := make([]*proto.KVPair, 0, len(in))
	for _, p := range in {
		key, err := hex.DecodeString(p.Key)
		if err != nil {
			return nil, fmt.Errorf("bad key hex %q: %w", p.Key, err)
		}
		var val []byte
		if p.Value != "" {
			if val, err = hex.DecodeString(p.Value); err != nil {
				return nil, fmt.Errorf("bad value hex %q: %w", p.Value, err)
			}
		}
		pairs = append(pairs, &proto.KVPair{Delete: p.Delete, Key: key, Value: val})
	}
	return &proto.NamedChangeSet{Name: name, Changeset: proto.ChangeSet{Pairs: pairs}}, nil
}
