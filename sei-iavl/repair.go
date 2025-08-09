package iavl

import (
	"math"

	"github.com/pkg/errors"
	dbm "github.com/tendermint/tm-db"
)

// Repair013Orphans repairs incorrect orphan entries written by IAVL 0.13 pruning. To use it, close
// a database using IAVL 0.13, make a backup copy, and then run this function before opening the
// database with IAVL 0.14 or later. It returns the number of faulty orphan entries removed. If the
// 0.13 database was written with KeepEvery:1 (the default) or the last version _ever_ saved to the
// tree was a multiple of `KeepEvery` and thus saved to disk, this repair is not necessary.
//
// Note that this cannot be used directly on Cosmos SDK databases, since they store multiple IAVL
// trees in the same underlying database via a prefix scheme.
//
// The pruning functionality enabled with Options.KeepEvery > 1 would write orphans entries to disk
// for versions that should only have been saved in memory, and these orphan entries were clamped
// to the last version persisted to disk instead of the version that generated them (so a delete at
// version 749 might generate an orphan entry ending at version 700 for KeepEvery:100). If the
// database is reopened at the last persisted version and this version is later deleted, the
// orphaned nodes can be deleted prematurely or incorrectly, causing data loss and database
// corruption.
//
// This function removes these incorrect orphan entries by deleting all orphan entries that have a
// to-version equal to or greater than the latest persisted version. Correct orphans will never
// have this, since they must have been deleted in a future (non-existent) version for that to be
// the case.
func Repair013Orphans(db dbm.DB) (uint64, error) {
	ndb := newNodeDB(db, 0, &Options{Sync: true})
	version, err := ndb.getLatestVersion()
	if err != nil {
		return 0, err
	}
	if version == 0 {
		return 0, errors.New("no versions found")
	}

	var repaired uint64
	batch := db.NewBatch()
	defer batch.Close()
	err = ndb.traverseRange(orphanKeyFormat.Key(version), orphanKeyFormat.Key(int64(math.MaxInt64)), func(k, v []byte) error {
		// Sanity check so we don't remove stuff we shouldn't
		var toVersion int64
		orphanKeyFormat.Scan(k, &toVersion)
		if toVersion < version {
			err = errors.Errorf("Found unexpected orphan with toVersion=%v, lesser than latest version %v",
				toVersion, version)
			return err
		}
		repaired++
		err = batch.Delete(k)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	err = batch.WriteSync()
	if err != nil {
		return 0, err
	}

	return repaired, nil
}
