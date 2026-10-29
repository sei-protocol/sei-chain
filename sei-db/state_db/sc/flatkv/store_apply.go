package flatkv

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"unsafe"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// ApplyChangeSets buffers changeSets as part of the block at version, which the next Commit (or
// CommitPendingBlock) writes. Nothing reaches the stores before then, so reads do not observe buffered
// writes.
func (s *CommitStore) ApplyChangeSets(version int64, changeSets []*proto.NamedChangeSet) error {
	if s.readOnly {
		return errReadOnly
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Blocks are contiguous and the first block is 1, so writes always land at committedVersion+1. See the
	// Commit contract: a store whose history starts higher is seeded by SetInitialVersion.
	// An empty batch for a block that is already committed is accepted and does nothing.
	if version > 0 && version == s.committedVersion {
		if len(changeSets) == 0 {
			// An empty batch would leave the sealed block exactly as it is, so a stale height is
			// harmless here. No caller produces one today: every writer stamps its batch at the height
			// after the one the store has committed. This stands as tolerance for a caller that has
			// lost track of the height, not as a path taken in normal operation.
			return nil
		}
		// Writes are a different matter: they would belong to a block that is already sealed, and there
		// is nowhere to put them.
		return fmt.Errorf("flatkv: apply version %d is already committed and this batch has %d changesets",
			version, len(changeSets))
	}
	if version != s.committedVersion+1 {
		return fmt.Errorf("flatkv: apply version %d must be committed version %d plus one",
			version, s.committedVersion)
	}
	// A single block's writes may arrive across several ApplyChangeSets calls at the same height (e.g. a
	// ModuleRouter fanning one block's changesets out to multiple routes that all target flatKV). The check
	// above already restricts those to committedVersion+1, so same-height repeats are accepted and no other
	// height can reach here.
	s.pendingChangeSets = append(s.pendingChangeSets, changeSets...)
	s.pendingBlockHeight = version
	return nil
}

// stageMutations writes one block's mutations into the four data stores, where the block's seal picks them up, and
// returns them grouped by database for hashing, in dataDBDirs order. A database that already holds the block is
// neither written nor given any mutations to hash. The caller holds mu, or has the store to itself.
func (s *CommitStore) stageMutations(
	version int64,
	mutations []mutation.Mutation,
	// The replay skip list. Nil outside a startup replay, which means every store needs every block.
	alreadyHave map[string]int64,
) (staged []lthash.DatabaseMutations, err error) {
	obs := s.observeOp("StageMutations", otelMetrics.ApplyChangesetsLatency, "mutations", len(mutations))
	defer obs.done(&err, nil)

	s.phaseTimer.SetPhase("stage_mutations_route")
	routed, err := routeMutations(mutations, version, alreadyHave)
	if err != nil {
		return nil, fmt.Errorf("route mutations: %w", err)
	}

	s.phaseTimer.SetPhase("stage_mutations_write")
	if err := s.writeToStores(routed); err != nil {
		return nil, fmt.Errorf("write to stores: %w", err)
	}

	staged = make([]lthash.DatabaseMutations, len(dataDBDirs))
	for i, dir := range dataDBDirs {
		staged[i] = lthash.DatabaseMutations{DBName: dir, Mutations: routed[i]}
	}

	logger.Debug("FlatKV block staged",
		"version", version,
		"mutations", len(mutations),
		"elapsed", obs.elapsed())
	return staged, nil
}

// routeMutations splits mutations by the database each key belongs to, indexed in dataDBDirs order, keeping their
// order within each database. Every key is routed before anything is returned, so a malformed key fails the block
// before any store is written.
func routeMutations(
	mutations []mutation.Mutation,
	version int64,
	// The replay skip list. A database listed at or above version gets no mutations.
	alreadyHave map[string]int64,
) ([][]mutation.Mutation, error) {
	targets := make([]int8, len(mutations))
	chunks := routeChunkCount(len(mutations))
	counts := make([][]int, chunks)
	errs := make([]error, chunks)
	forEachRouteChunk(len(mutations), chunks, func(chunk int, start int, end int) {
		count := make([]int, len(dataDBDirs))
		for n := start; n < end; n++ {
			dir, err := routePhysicalKey(mutations[n].Key)
			if err != nil {
				errs[chunk] = fmt.Errorf("route key %x: %w", mutations[n].Key, err)
				return
			}
			if alreadyHave[dir] >= version {
				// A store already holds the block only when a startup replay is catching the stores up to each
				// other, where its hash already includes the block and writing it again would count it twice.
				//
				// TODO: currently, WAL replay may replay blocks already in some stores. In the future when WAL
				// replay is external, we may be able to simplify this code since we will be able to assume that
				// all stores start at the same block.
				targets[n] = -1
				continue
			}
			i := dataDBIndex[dir]
			targets[n] = int8(i) //nolint:gosec // G115: i indexes dataDBDirs, which has four entries.
			count[i]++
		}
		counts[chunk] = count
	})
	// Chunks cover ascending ranges, so the first chunk's error is the first malformed key.
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	// offsets[chunk][i] is where chunk's first mutation for database i lands in routed[i].
	offsets := make([][]int, chunks)
	totals := make([]int, len(dataDBDirs))
	for chunk, count := range counts {
		offsets[chunk] = slices.Clone(totals)
		for i, c := range count {
			totals[i] += c
		}
	}
	routed := make([][]mutation.Mutation, len(dataDBDirs))
	for i, total := range totals {
		if total > 0 {
			routed[i] = make([]mutation.Mutation, total)
		}
	}
	forEachRouteChunk(len(mutations), chunks, func(chunk int, start int, end int) {
		next := offsets[chunk]
		for n := start; n < end; n++ {
			i := targets[n]
			if i < 0 {
				continue
			}
			routed[i][next[i]] = mutations[n]
			next[i]++
		}
	})
	return routed, nil
}

// dataDBIndex maps each data database directory to its index in dataDBDirs.
var dataDBIndex = func() map[string]int {
	index := make(map[string]int, len(dataDBDirs))
	for i, dir := range dataDBDirs {
		index[dir] = i
	}
	return index
}()

const (
	// minRouteChunk is the fewest mutations routeMutations gives a goroutine of its own.
	minRouteChunk = 2048
	// maxRouteChunks bounds the goroutines routeMutations runs at once.
	maxRouteChunks = 16
)

// routeChunkCount returns how many chunks routeMutations splits n mutations into.
func routeChunkCount(n int) int {
	return max(1, min(maxRouteChunks, n/minRouteChunk))
}

// forEachRouteChunk calls visit once per chunk over contiguous ascending ranges of [0, n), concurrently when there
// is more than one chunk.
func forEachRouteChunk(n int, chunks int, visit func(chunk int, start int, end int)) {
	if chunks == 1 {
		visit(0, 0, n)
		return
	}
	size := (n + chunks - 1) / chunks
	var wg sync.WaitGroup
	for chunk := range chunks {
		start := min(chunk*size, n)
		end := min(start+size, n)
		wg.Go(func() { visit(chunk, start, end) })
	}
	wg.Wait()
}

// writeToStores writes each database's share of one block, indexed in dataDBDirs order.
func (s *CommitStore) writeToStores(routed [][]mutation.Mutation) error {
	// The four databases are independent view managers with independent locks, so their writes run
	// concurrently rather than one store's fan-out waiting on the last.
	errs := make([]error, len(dataDBDirs))
	var wg sync.WaitGroup
	wg.Add(len(dataDBDirs))
	for i, dir := range dataDBDirs {
		s.miscPool.Submit(func() {
			defer wg.Done()
			errs[i] = writeStore(s.ctx, s.viewManagerFor(dir), dir, routed[i])
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// writeStore writes the new value of each mutation into one database.
func writeStore(
	ctx context.Context,
	store view.ViewManager,
	dbDir string,
	mutations []mutation.Mutation,
) error {
	if len(mutations) == 0 {
		return nil
	}
	writes := make([]view.Write, len(mutations))
	for i := range mutations {
		key := mutations[i].Key
		// Not copied: mutation keys are never modified, and the view copies any key it keeps past retirement.
		writes[i] = view.Write{
			Key:   unsafe.String(unsafe.SliceData(key), len(key)), //nolint:gosec // see above
			Value: mutations[i].NewValue,
		}
	}
	if err := store.BatchSet(writes); err != nil {
		return fmt.Errorf("write %s values: %w", dbDir, err)
	}
	addKVPairs(ctx, dbDir, len(writes))
	return nil
}

// moduleOfKey extracts the owning module from a physical key. Injected into the
// lthash HashCalculator so it can bucket pairs by module without importing ktype
// (ktype already imports lthash).
func moduleOfKey(physicalKey []byte) (string, error) {
	module, _, err := ktype.StripModulePrefix(physicalKey)
	if err != nil {
		return "", fmt.Errorf("strip the module prefix from key %x: %w", physicalKey, err)
	}
	return module, nil
}
