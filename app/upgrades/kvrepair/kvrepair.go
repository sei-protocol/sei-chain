// Package kvrepair applies reviewed key/value repairs to module stores as hard
// fork handlers. Each repair is a JSON file under repairs/ that names one chain,
// one height, and the entries to write or delete at that height.
package kvrepair

import (
	"embed"
	"fmt"
	"io/fs"
	"path"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/app/upgrades"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/kvrepair"
)

var logger = seilog.NewLogger("app", "upgrades", "kvrepair")

// repairsDir holds the repair files compiled into the binary.
const repairsDir = "repairs"

//go:embed all:repairs
var embeddedRepairs embed.FS

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

// Load reads and validates every .json file under repairs/ in fsys. It also
// refuses a repair name used twice, and a key that two files repair at the
// same height.
func Load(fsys fs.FS, keys map[string]*sdk.KVStoreKey) ([]kvrepair.Repair, error) {
	files, err := fs.ReadDir(fsys, repairsDir)
	if err != nil {
		return nil, err
	}
	storeExists := func(name string) bool {
		_, ok := keys[name]
		return ok
	}
	var repairs []kvrepair.Repair
	names := map[string]string{}
	targets := map[string]string{}
	for _, file := range files {
		if file.IsDir() || path.Ext(file.Name()) != ".json" {
			continue
		}
		filePath := path.Join(repairsDir, file.Name())
		data, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return nil, err
		}
		r, err := kvrepair.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filePath, err)
		}
		if err := r.Validate(storeExists); err != nil {
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

// Handler implements upgrades.HardForkHandler for one Repair.
type Handler struct {
	repair kvrepair.Repair
	keys   map[string]*sdk.KVStoreKey
}

// NewHandler returns the hard fork handler that applies r.
func NewHandler(r kvrepair.Repair, keys map[string]*sdk.KVStoreKey) upgrades.HardForkHandler {
	return Handler{repair: r, keys: keys}
}

func (h Handler) GetName() string { return "kvrepair-" + h.repair.Name }

func (h Handler) GetTargetChainID() string { return h.repair.ChainID }

func (h Handler) GetTargetHeight() int64 { return h.repair.Height }

// ExecuteHandler writes every entry and reads each one back. It returns an
// error when a key that does not hold its new value holds something other than
// its old value, or when a read-back does not match the write. Values compare
// with kvrepair.ValuesEqual.
func (h Handler) ExecuteHandler(ctx sdk.Context) error {
	var repaired, alreadyCorrect int
	for i, e := range h.repair.Entries {
		store := ctx.KVStore(h.keys[e.Store])
		current := store.Get(e.Key)
		if holdsTarget(e, current) {
			alreadyCorrect++
		} else if err := checkOld(e, current); err != nil {
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
		if got := store.Get(e.Key); !holdsTarget(e, got) {
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

// holdsTarget reports whether current reads the same as the entry's new value.
func holdsTarget(e kvrepair.Entry, current []byte) bool {
	// Not bytes.Equal: a FlatKV account row returns a zero nonce for an account
	// whose nonce was deleted while another field remains, where memiavl returns
	// nil.
	return kvrepair.ValuesEqual(e.Store, e.Key, current, optional(e.New))
}

func checkOld(e kvrepair.Entry, current []byte) error {
	switch {
	case e.OldAbsent && !kvrepair.ReadsAsAbsent(e.Store, e.Key, current):
		return fmt.Errorf("expected the key to be absent, found %x", current)
	case e.Old != nil && !kvrepair.ValuesEqual(e.Store, e.Key, current, *e.Old):
		return fmt.Errorf("expected old value %x, found %x", []byte(*e.Old), current)
	}
	return nil
}

// optional returns the bytes of v, or nil when v is nil.
func optional(v *kvrepair.HexBytes) []byte {
	if v == nil {
		return nil
	}
	return *v
}
