package operations

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

// memiavlOverlayWrite is the last changelog write to one memiavl EVM key.
type memiavlOverlayWrite struct {
	key     []byte
	value   []byte
	deleted bool
}

// readMemiavlEVMChangelogOverlay returns the last write to every EVM key in the memiavl changelog
// above the snapshot that OpenDB(height) loads, sorted by key, and the range it read.
func readMemiavlEVMChangelogOverlay(dbDir string, height int64) ([]memiavlOverlayWrite, memiavl.TreeChangelogRange, error) {
	writes := make(map[string]memiavlOverlayWrite)
	r, err := memiavl.ReplayTreeChangelog(dbDir, height, keys.EVMStoreKey, func(_ int64, cs proto.ChangeSet) error {
		for _, pair := range cs.Pairs {
			w := memiavlOverlayWrite{key: pair.Key, value: pair.Value, deleted: pair.Delete}
			if w.deleted {
				w.value = nil
			} else if w.value == nil {
				// memiavl Tree.Set stores a nil value as an empty one.
				w.value = []byte{}
			}
			writes[string(pair.Key)] = w
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, wal.ErrCorrupt) {
			return nil, memiavl.TreeChangelogRange{}, changelogEndsMidRecordError(dbDir, err)
		}
		return nil, memiavl.TreeChangelogRange{}, fmt.Errorf("read memiavl changelog: %w", err)
	}

	overlay := make([]memiavlOverlayWrite, 0, len(writes))
	var deletes, payloadBytes int
	for _, w := range writes {
		overlay = append(overlay, w)
		payloadBytes += len(w.key) + len(w.value)
		if w.deleted {
			deletes++
		}
	}
	sort.Slice(overlay, func(i, j int) bool { return bytes.Compare(overlay[i].key, overlay[j].key) < 0 })
	digestOut.sayf("memiavl changelog overlay: snapshot=%d %s keys=%d deletes=%d key_value_bytes=%d\n",
		r.SnapshotVersion, changelogVersionsText(r), len(overlay), deletes, payloadBytes)
	return overlay, r, nil
}

// changelogVersionsText names the changelog versions r replays above its snapshot.
func changelogVersionsText(r memiavl.TreeChangelogRange) string {
	if r.Version <= r.SnapshotVersion {
		return "no changelog versions"
	}
	return fmt.Sprintf("changelog versions %d to %d", r.SnapshotVersion+1, r.Version)
}

// scanMemiavlChangelogEVMLeaves streams the leaves of the memiavl EVM snapshot at evmSnapshotDir
// with overlay applied, in ascending key order. overlay must be sorted by key.
func scanMemiavlChangelogEVMLeaves(evmSnapshotDir string, overlay []memiavlOverlayWrite, fn func(rawKey, rawVal []byte) error) error {
	emit := func(w memiavlOverlayWrite) error {
		if w.deleted {
			return nil
		}
		return fn(w.key, w.value)
	}
	next := 0
	if err := scanMemiavlSnapshotEVMLeaves(evmSnapshotDir, func(k, v []byte) error {
		for ; next < len(overlay) && bytes.Compare(overlay[next].key, k) < 0; next++ {
			if err := emit(overlay[next]); err != nil {
				return err
			}
		}
		if next < len(overlay) && bytes.Equal(overlay[next].key, k) {
			next++
			return emit(overlay[next-1])
		}
		return fn(k, v)
	}); err != nil {
		return err
	}
	for ; next < len(overlay); next++ {
		if err := emit(overlay[next]); err != nil {
			return err
		}
	}
	return nil
}
