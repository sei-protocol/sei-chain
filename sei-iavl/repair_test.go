package iavl

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func TestRepair013Orphans(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-iavl-repair")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	// There is also 0.13-orphans-v6.db containing a database closed immediately after writing
	// version 6, which should not contain any broken orphans.
	err = copyDB("testdata/0.13-orphans.db", filepath.Join(dir, "0.13-orphans.db"))
	require.NoError(t, err)

	db, err := dbm.NewGoLevelDB("0.13-orphans", dir)
	require.NoError(t, err)

	// Repair the database.
	repaired, err := Repair013Orphans(db)
	require.NoError(t, err)
	assert.EqualValues(t, 8, repaired)

	// Load the database.
	tree, err := NewMutableTreeWithOpts(db, 0, &Options{Sync: true}, false)
	require.NoError(t, err)
	version, err := tree.Load()
	require.NoError(t, err)
	require.EqualValues(t, 6, version)

	// We now generate two empty versions, and check all persisted versions.
	_, version, err = tree.SaveVersion()
	require.NoError(t, err)
	require.EqualValues(t, 7, version)
	_, version, err = tree.SaveVersion()
	require.NoError(t, err)
	require.EqualValues(t, 8, version)

	// Check all persisted versions.
	require.Equal(t, []int{3, 6, 7, 8}, tree.AvailableVersions())
	assertVersion(t, tree, 0)
	assertVersion(t, tree, 3)
	assertVersion(t, tree, 6)
	assertVersion(t, tree, 7)
	assertVersion(t, tree, 8)

	// We then delete version 6 (the last persisted one with 0.13).
	err = tree.DeleteVersion(6)
	require.NoError(t, err)

	// Reading "rm7" (which should not have been deleted now) would panic with a broken database.
	value, err := tree.Get([]byte("rm7"))
	require.NoError(t, err)
	require.Equal(t, []byte{1}, value)

	// Check all persisted versions.
	require.Equal(t, []int{3, 7, 8}, tree.AvailableVersions())
	assertVersion(t, tree, 0)
	assertVersion(t, tree, 3)
	assertVersion(t, tree, 7)
	assertVersion(t, tree, 8)

	// Delete all historical versions, and check the latest.
	err = tree.DeleteVersion(3)
	require.NoError(t, err)
	err = tree.DeleteVersion(7)
	require.NoError(t, err)

	require.Equal(t, []int{8}, tree.AvailableVersions())
	assertVersion(t, tree, 0)
	assertVersion(t, tree, 8)
}

// assertVersion checks the given version (or current if 0) against the expected values.
func assertVersion(t *testing.T, tree *MutableTree, version int64) {
	var err error
	itree := tree.ImmutableTree
	if version > 0 {
		itree, err = tree.GetImmutable(version)
		require.NoError(t, err)
	}
	version = itree.version

	// The "current" value should have the current version for <= 6, then 6 afterwards
	value, err := itree.Get([]byte("current"))
	require.NoError(t, err)
	if version >= 6 {
		require.EqualValues(t, []byte{6}, value)
	} else {
		require.EqualValues(t, []byte{byte(version)}, value)
	}

	// The "addX" entries should exist for 1-6 in the respective versions, and the
	// "rmX" entries should have been removed for 1-6 in the respective versions.
	for i := byte(1); i < 8; i++ {
		value, err = itree.Get([]byte(fmt.Sprintf("add%v", i)))
		require.NoError(t, err)
		if i <= 6 && int64(i) <= version {
			require.Equal(t, []byte{i}, value)
		} else {
			require.Nil(t, value)
		}

		value, err = itree.Get([]byte(fmt.Sprintf("rm%v", i)))
		require.NoError(t, err)
		if i <= 6 && version >= int64(i) {
			require.Nil(t, value)
		} else {
			require.Equal(t, []byte{1}, value)
		}
	}
}

// Generate013Orphans generates a GoLevelDB orphan database in testdata/0.13-orphans.db
// for testing Repair013Orphans(). It must be run with IAVL 0.13.x.
/*func TestGenerate013Orphans(t *testing.T) {
	err := os.RemoveAll("testdata/0.13-orphans.db")
	require.NoError(t, err)
	db, err := dbm.NewGoLevelDB("0.13-orphans", "testdata")
	require.NoError(t, err)
	tree, err := NewMutableTreeWithOpts(db, dbm.NewMemDB(), 0, &Options{
		KeepEvery:  3,
		KeepRecent: 1,
		Sync:       true,
	})
	require.NoError(t, err)
	version, err := tree.Load()
	require.NoError(t, err)
	require.EqualValues(t, 0, version)

	// We generate 8 versions. In each version, we create a "addX" key, delete a "rmX" key,
	// and update the "current" key, where "X" is the current version. Values are the version in
	// which the key was last set.
	tree.Set([]byte("rm1"), []byte{1})
	tree.Set([]byte("rm2"), []byte{1})
	tree.Set([]byte("rm3"), []byte{1})
	tree.Set([]byte("rm4"), []byte{1})
	tree.Set([]byte("rm5"), []byte{1})
	tree.Set([]byte("rm6"), []byte{1})
	tree.Set([]byte("rm7"), []byte{1})
	tree.Set([]byte("rm8"), []byte{1})

	for v := byte(1); v <= 8; v++ {
		tree.Set([]byte("current"), []byte{v})
		tree.Set([]byte(fmt.Sprintf("add%v", v)), []byte{v})
		tree.Remove([]byte(fmt.Sprintf("rm%v", v)))
		_, version, err = tree.SaveVersion()
		require.NoError(t, err)
		require.EqualValues(t, v, version)
	}

	// At this point, the database will contain incorrect orphans in version 6 that, when
	// version 6 is deleted, will cause "current", "rm7", and "rm8" to go missing.
}*/

// copyDB makes a shallow copy of the source database directory.
func copyDB(src, dest string) error {
	entries, err := ioutil.ReadDir(src)
	if err != nil {
		return err
	}
	err = os.MkdirAll(dest, 0777)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		out, err := os.Create(filepath.Join(dest, entry.Name()))
		if err != nil {
			return err
		}
		defer out.Close()

		in, err := os.Open(filepath.Join(src, entry.Name()))
		defer func() {
			in.Close()
		}()
		if err != nil {
			return err
		}

		_, err = io.Copy(out, in)
		if err != nil {
			return err
		}
	}
	return nil
}
