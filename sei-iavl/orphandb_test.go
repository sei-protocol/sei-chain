package iavl

import (
	"fmt"
	"io/ioutil"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOrphanDBSaveGet(t *testing.T) {
	dir := t.TempDir()
	db := NewOrphanDB(&Options{
		NumOrphansPerFile: 2,
		OrphanDirectory:   dir,
	})
	err := db.SaveOrphans(123, map[string]int64{
		"o1": 123,
		"o2": 123,
		"o3": 123,
	})
	require.Nil(t, err)
	files, err := ioutil.ReadDir(path.Join(dir, fmt.Sprintf("%d", 123)))
	require.Nil(t, err)
	require.Equal(t, 2, len(files)) // 3 orphans would result in 2 files
	orphans := db.GetOrphans(123)
	require.Equal(t, map[string]int64{
		"o1": 123,
		"o2": 123,
		"o3": 123,
	}, orphans)
	orphans = db.GetOrphans(456) // not exist
	require.Equal(t, map[string]int64{}, orphans)

	// flush cache
	db = NewOrphanDB(&Options{
		NumOrphansPerFile: 2,
		OrphanDirectory:   dir,
	})
	orphans = db.GetOrphans(123) // would load from disk
	require.Equal(t, map[string]int64{
		"o1": 123,
		"o2": 123,
		"o3": 123,
	}, orphans)
}

func TestOrphanDelete(t *testing.T) {
	dir := t.TempDir()
	db := NewOrphanDB(&Options{
		NumOrphansPerFile: 2,
		OrphanDirectory:   dir,
	})
	err := db.SaveOrphans(123, map[string]int64{
		"o1": 123,
		"o2": 123,
		"o3": 123,
	})
	require.Nil(t, err)
	err = db.DeleteOrphans(123)
	require.Nil(t, err)
	orphans := db.GetOrphans(123) // not exist in cache
	require.Equal(t, map[string]int64{}, orphans)

	// flush cache
	db = NewOrphanDB(&Options{
		NumOrphansPerFile: 2,
		OrphanDirectory:   dir,
	})
	orphans = db.GetOrphans(123) // would load from disk
	require.Equal(t, map[string]int64{}, orphans)
}
