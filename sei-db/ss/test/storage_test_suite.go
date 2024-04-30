package sstest

import (
	"fmt"
	"sync"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/ss/types"
	"github.com/stretchr/testify/suite"
	"golang.org/x/exp/slices"
)

const (
	storeKey1 = "store1"
)

// StorageTestSuite defines a reusable test suite for all storage backends.
type StorageTestSuite struct {
	suite.Suite

	NewDB          func(dir string) (types.StateStore, error)
	EmptyBatchSize int
	SkipTests      []string
}

func (s *StorageTestSuite) TestDatabaseClose() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	s.Require().NoError(db.Close())

	// close should not be idempotent
	s.Require().Panics(func() { _ = db.Close() })
}

func (s *StorageTestSuite) TestDatabaseLatestVersion() {
	tempDir := s.T().TempDir()
	db, err := s.NewDB(tempDir)
	s.Require().NoError(err)

	lv, err := db.GetLatestVersion()
	s.Require().NoError(err)
	s.Require().Zero(lv)

	i := int64(1)
	for ; i <= 1001; i++ {
		err = db.SetLatestVersion(i)
		s.Require().NoError(err)

		lv, err = db.GetLatestVersion()
		s.Require().NoError(err)
		s.Require().Equal(i, lv)
	}

	// Test even after closing and reopening, the latest version is maintained
	err = db.Close()
	s.Require().NoError(err)

	newDB, err := s.NewDB(tempDir)
	s.Require().NoError(err)
	defer newDB.Close()

	lv, err = newDB.GetLatestVersion()
	s.Require().NoError(err)
	s.Require().Equal(i-1, lv)

}

func (s *StorageTestSuite) TestDatabaseVersionedKeys() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(FillData(db, 1, 100))

	for i := int64(1); i <= 100; i++ {
		bz, err := db.Get(storeKey1, i, []byte("key000"))
		s.Require().NoError(err)
		s.Require().Equal(fmt.Sprintf("val000-%03d", i), string(bz))
	}
}

func (s *StorageTestSuite) TestDatabaseGetVersionedKey() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	key := []byte("key")
	val := []byte("value001")

	// store a key at version 1
	s.Require().NoError(DBApplyChangeset(db, 1, storeKey1, [][]byte{key}, [][]byte{val}))

	// assume chain progresses to version 10 w/o any changes to key
	bz, err := db.Get(storeKey1, 10, key)
	s.Require().NoError(err)
	s.Require().Equal(val, bz)

	ok, err := db.Has(storeKey1, 10, key)
	s.Require().NoError(err)
	s.Require().True(ok)

	// chain progresses to version 11 with an update to key
	newVal := []byte("value011")
	s.Require().NoError(DBApplyChangeset(db, 11, storeKey1, [][]byte{key}, [][]byte{newVal}))

	bz, err = db.Get(storeKey1, 10, key)
	s.Require().NoError(err)
	s.Require().Equal(val, bz)

	ok, err = db.Has(storeKey1, 10, key)
	s.Require().NoError(err)
	s.Require().True(ok)

	for i := int64(11); i <= 14; i++ {
		bz, err = db.Get(storeKey1, i, key)
		s.Require().NoError(err)
		s.Require().Equal(newVal, bz)

		ok, err = db.Has(storeKey1, i, key)
		s.Require().NoError(err)
		s.Require().True(ok)
	}

	// chain progresses to version 15 with a delete to key
	s.Require().NoError(DBApplyChangeset(db, 15, storeKey1, [][]byte{key}, [][]byte{nil}))

	// all queries up to version 14 should return the latest value
	for i := int64(1); i <= 14; i++ {
		bz, err = db.Get(storeKey1, i, key)
		s.Require().NoError(err)
		s.Require().NotNil(bz)

		ok, err = db.Has(storeKey1, i, key)
		s.Require().NoError(err)
		s.Require().True(ok)
	}

	// all queries after version 15 should return nil
	for i := int64(15); i <= 17; i++ {
		bz, err = db.Get(storeKey1, i, key)
		s.Require().NoError(err)
		s.Require().Nil(bz)

		ok, err = db.Has(storeKey1, i, key)
		s.Require().NoError(err)
		s.Require().False(ok)
	}
}

func (s *StorageTestSuite) TestDatabaseVersionZero() {
	// Db should write all keys at version 0 at version 1
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(DBApplyChangeset(db, 0, storeKey1, [][]byte{[]byte("key001")}, [][]byte{[]byte("value001")}))
	s.Require().NoError(DBApplyChangeset(db, 0, storeKey1, [][]byte{[]byte("key002")}, [][]byte{[]byte("value002")}))
	s.Require().NoError(DBApplyChangeset(db, 0, storeKey1, [][]byte{[]byte("key003")}, [][]byte{[]byte("value003")}))

	// Get at version 0 should return error
	bz, _ := db.Get(storeKey1, 0, []byte("key001"))
	s.Require().Nil(bz)

	bz, _ = db.Get(storeKey1, 0, []byte("key002"))
	s.Require().Nil(bz)

	bz, _ = db.Get(storeKey1, 0, []byte("key002"))
	s.Require().Nil(bz)

	// Retrieve each key at version 1
	bz, err = db.Get(storeKey1, 1, []byte("key001"))
	s.Require().NoError(err)
	s.Require().Equal([]byte("value001"), bz)

	bz, err = db.Get(storeKey1, 1, []byte("key002"))
	s.Require().NoError(err)
	s.Require().Equal([]byte("value002"), bz)

	bz, err = db.Get(storeKey1, 1, []byte("key003"))
	s.Require().NoError(err)
	s.Require().Equal([]byte("value003"), bz)

}

func (s *StorageTestSuite) TestDatabaseApplyChangeset() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(FillData(db, 100, 1))

	cs := &iavl.ChangeSet{}
	cs.Pairs = []*iavl.KVPair{}

	// Deletes
	keys := [][]byte{}
	vals := [][]byte{}
	for i := 0; i < 100; i++ {
		if i%10 == 0 {
			keys = append(keys, []byte(fmt.Sprintf("key%03d", i)))
			vals = append(vals, nil)
		}
	}
	s.Require().NoError(DBApplyChangeset(db, 1, storeKey1, keys, vals))

	lv, err := db.GetLatestVersion()
	s.Require().NoError(err)
	s.Require().Equal(int64(1), lv)

	for i := 0; i < 100; i++ {
		ok, err := db.Has(storeKey1, 1, []byte(fmt.Sprintf("key%03d", i)))
		s.Require().NoError(err)

		if i%10 == 0 {
			s.Require().False(ok)
		} else {
			s.Require().True(ok)
		}
	}
}

func (s *StorageTestSuite) TestDatabaseIteratorEmptyDomain() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	iter, err := db.Iterator(storeKey1, 1, []byte{}, []byte{})
	s.Require().Error(err)
	s.Require().Nil(iter)
}

func (s *StorageTestSuite) TestDatabaseIteratorClose() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	iter, err := db.Iterator(storeKey1, 1, []byte("key000"), nil)
	s.Require().NoError(err)
	iter.Close()

	s.Require().False(iter.Valid())
	s.Require().Panics(func() { iter.Close() })
}

func (s *StorageTestSuite) TestDatabaseIteratorDomain() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	testCases := map[string]struct {
		start, end []byte
	}{
		"start without end domain": {
			start: []byte("key010"),
		},
		"start and end domain": {
			start: []byte("key010"),
			end:   []byte("key020"),
		},
	}

	for name, tc := range testCases {
		s.Run(name, func() {
			iter, err := db.Iterator(storeKey1, 1, tc.start, tc.end)
			s.Require().NoError(err)

			defer iter.Close()

			start, end := iter.Domain()
			s.Require().Equal(tc.start, start)
			s.Require().Equal(tc.end, end)
		})
	}
}

func (s *StorageTestSuite) TestDatabaseIterator() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(FillData(db, 100, 1))

	// iterator without an end key over multiple versions
	for v := int64(1); v < 5; v++ {
		itr, err := db.Iterator(storeKey1, v, []byte("key000"), nil)
		s.Require().NoError(err)

		defer itr.Close()

		var i, count int
		for ; itr.Valid(); itr.Next() {
			s.Require().Equal([]byte(fmt.Sprintf("key%03d", i)), itr.Key(), string(itr.Key()))
			s.Require().Equal([]byte(fmt.Sprintf("val%03d-%03d", i, 1)), itr.Value())

			i++
			count++
		}
		s.Require().Equal(100, count)
		s.Require().NoError(itr.Error())

		// seek past domain, which should make the iterator invalid and produce an error
		itr.Next()
		s.Require().False(itr.Valid())
	}

	// iterator with a start and end domain over multiple versions
	for v := int64(1); v < 5; v++ {
		itr2, err := db.Iterator(storeKey1, v, []byte("key010"), []byte("key019"))
		s.Require().NoError(err)

		defer itr2.Close()

		i, count := 10, 0
		for ; itr2.Valid(); itr2.Next() {
			s.Require().Equal([]byte(fmt.Sprintf("key%03d", i)), itr2.Key())
			s.Require().Equal([]byte(fmt.Sprintf("val%03d-%03d", i, 1)), itr2.Value())

			i++
			count++
		}
		s.Require().Equal(9, count)
		s.Require().NoError(itr2.Error())

		// seek past domain, which should make the iterator invalid and produce an error
		itr2.Next()
		s.Require().False(itr2.Valid())
	}

	// start must be <= end
	iter3, err := db.Iterator(storeKey1, 1, []byte("key020"), []byte("key019"))
	s.Require().Error(err)
	s.Require().Nil(iter3)
}
func (s *StorageTestSuite) TestDatabaseIteratorRangedDeletes() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(DBApplyChangeset(db, 1, storeKey1, [][]byte{[]byte("key001")}, [][]byte{[]byte("value001")}))
	s.Require().NoError(DBApplyChangeset(db, 1, storeKey1, [][]byte{[]byte("key002")}, [][]byte{[]byte("value001")}))
	s.Require().NoError(DBApplyChangeset(db, 5, storeKey1, [][]byte{[]byte("key002")}, [][]byte{[]byte("value002")}))
	s.Require().NoError(DBApplyChangeset(db, 10, storeKey1, [][]byte{[]byte("key002")}, [][]byte{nil}))

	itr, err := db.Iterator(storeKey1, 11, []byte("key001"), nil)
	s.Require().NoError(err)

	defer itr.Close()

	// there should only be one valid key in the iterator -- key001
	var count int
	for ; itr.Valid(); itr.Next() {
		s.Require().Equal([]byte("key001"), itr.Key())
		count++
	}
	s.Require().Equal(1, count)
	s.Require().NoError(itr.Error())
}

func (s *StorageTestSuite) TestDatabaseIteratorDeletes() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(DBApplyChangeset(db, 1, storeKey1, [][]byte{[]byte("key001")}, [][]byte{[]byte("value001")}))
	s.Require().NoError(DBApplyChangeset(db, 1, storeKey1, [][]byte{[]byte("key002")}, [][]byte{[]byte("value002")}))
	s.Require().NoError(DBApplyDeleteChangeset(db, 5, storeKey1, [][]byte{[]byte("key001")}))

	itr, err := db.Iterator(storeKey1, 11, []byte("key001"), nil)
	s.Require().NoError(err)

	// there should be only one valid key in the iterator
	var count = 0
	for ; itr.Valid(); itr.Next() {
		s.Require().Equal([]byte("key002"), itr.Key())
		count++
	}
	s.Require().Equal(1, count)
	s.Require().NoError(itr.Error())
	itr.Close()

	s.Require().NoError(DBApplyChangeset(db, 10, storeKey1, [][]byte{[]byte("key001")}, [][]byte{[]byte("value001")}))
	itr, err = db.Iterator(storeKey1, 11, []byte("key001"), nil)
	s.Require().NoError(err)

	// there should be two valid keys in the iterator
	count = 0
	for ; itr.Valid(); itr.Next() {
		count++
	}
	s.Require().Equal(2, count)
	s.Require().NoError(itr.Error())
	itr.Close()
}

func (s *StorageTestSuite) TestDatabaseIteratorMultiVersion() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(FillData(db, 10, 50))

	// for versions 50-100, only update even keys
	for v := int64(51); v <= 100; v++ {
		keys := [][]byte{}
		vals := [][]byte{}
		for i := 0; i < 10; i++ {
			if i%2 == 0 {
				keys = append(keys, []byte(fmt.Sprintf("key%03d", i)))
				vals = append(vals, []byte(fmt.Sprintf("val%03d-%03d", i, v)))
			}
		}
		s.Require().NoError(DBApplyChangeset(db, v, storeKey1, keys, vals))
	}

	itr, err := db.Iterator(storeKey1, 69, []byte("key000"), nil)
	s.Require().NoError(err)

	defer itr.Close()

	// All keys should be present; All odd keys should have a value that reflects
	// version 49, and all even keys should have a value that reflects the desired
	// version, 69.
	var i, count int
	for ; itr.Valid(); itr.Next() {
		s.Require().Equal([]byte(fmt.Sprintf("key%03d", i)), itr.Key(), string(itr.Key()))

		if i%2 == 0 {
			s.Require().Equal([]byte(fmt.Sprintf("val%03d-%03d", i, 69)), itr.Value())
		} else {
			s.Require().Equal([]byte(fmt.Sprintf("val%03d-%03d", i, 50)), itr.Value())
		}

		i = (i + 1) % 10
		count++
	}
	s.Require().Equal(10, count)
	s.Require().NoError(itr.Error())
}

// Tests bug where iterator loops continuously
func (s *StorageTestSuite) TestDatabaseBugInitialReverseIteration() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	// Forward Iteration
	// Less than iterator version
	s.Require().NoError(DBApplyChangeset(db, 2, storeKey1, [][]byte{[]byte("keyA")}, [][]byte{[]byte("value001")}))
	s.Require().NoError(DBApplyChangeset(db, 3, storeKey1, [][]byte{[]byte("keyB")}, [][]byte{[]byte("value002")}))
	s.Require().NoError(DBApplyChangeset(db, 4, storeKey1, [][]byte{[]byte("keyC")}, [][]byte{[]byte("value003")}))

	s.Require().NoError(DBApplyChangeset(db, 8, storeKey1, [][]byte{[]byte("keyD")}, [][]byte{[]byte("value007")}))
	s.Require().NoError(DBApplyChangeset(db, 9, storeKey1, [][]byte{[]byte("keyE")}, [][]byte{[]byte("value008")}))
	s.Require().NoError(DBApplyChangeset(db, 10, storeKey1, [][]byte{[]byte("keyF")}, [][]byte{[]byte("value009")}))
	s.Require().NoError(DBApplyChangeset(db, 11, storeKey1, [][]byte{[]byte("keyH")}, [][]byte{[]byte("value010")}))

	itr, err := db.ReverseIterator(storeKey1, 5, []byte("keyA"), nil)
	s.Require().NoError(err)

	defer itr.Close()

	count := 0
	for ; itr.Valid(); itr.Next() {
		count++
	}

	s.Require().Equal(3, count)
}

func (s *StorageTestSuite) TestDatabaseBugInitialForwardIteration() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	// Forward Iteration
	// Less than iterator version
	s.Require().NoError(DBApplyChangeset(db, 8, storeKey1, [][]byte{[]byte("keyA")}, [][]byte{[]byte("value001")}))
	s.Require().NoError(DBApplyChangeset(db, 9, storeKey1, [][]byte{[]byte("keyB")}, [][]byte{[]byte("value002")}))
	s.Require().NoError(DBApplyChangeset(db, 10, storeKey1, [][]byte{[]byte("keyC")}, [][]byte{[]byte("value003")}))
	s.Require().NoError(DBApplyChangeset(db, 11, storeKey1, [][]byte{[]byte("keyD")}, [][]byte{[]byte("value004")}))

	s.Require().NoError(DBApplyChangeset(db, 2, storeKey1, [][]byte{[]byte("keyD")}, [][]byte{[]byte("value007")}))
	s.Require().NoError(DBApplyChangeset(db, 3, storeKey1, [][]byte{[]byte("keyE")}, [][]byte{[]byte("value008")}))
	s.Require().NoError(DBApplyChangeset(db, 4, storeKey1, [][]byte{[]byte("keyF")}, [][]byte{[]byte("value009")}))
	s.Require().NoError(DBApplyChangeset(db, 5, storeKey1, [][]byte{[]byte("keyH")}, [][]byte{[]byte("value010")}))

	itr, err := db.Iterator(storeKey1, 6, nil, []byte("keyZ"))
	s.Require().NoError(err)

	defer itr.Close()

	count := 0
	for ; itr.Valid(); itr.Next() {
		count++
	}

	s.Require().Equal(4, count)
}

func (s *StorageTestSuite) TestDatabaseBugInitialForwardIterationHigher() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	// Less than iterator version
	s.Require().NoError(DBApplyChangeset(db, 9, storeKey1, [][]byte{[]byte("keyB")}, [][]byte{[]byte("value002")}))
	s.Require().NoError(DBApplyChangeset(db, 10, storeKey1, [][]byte{[]byte("keyC")}, [][]byte{[]byte("value003")}))
	s.Require().NoError(DBApplyChangeset(db, 11, storeKey1, [][]byte{[]byte("keyD")}, [][]byte{[]byte("value004")}))

	s.Require().NoError(DBApplyChangeset(db, 12, storeKey1, [][]byte{[]byte("keyD")}, [][]byte{[]byte("value007")}))
	s.Require().NoError(DBApplyChangeset(db, 13, storeKey1, [][]byte{[]byte("keyE")}, [][]byte{[]byte("value008")}))
	s.Require().NoError(DBApplyChangeset(db, 14, storeKey1, [][]byte{[]byte("keyF")}, [][]byte{[]byte("value009")}))
	s.Require().NoError(DBApplyChangeset(db, 15, storeKey1, [][]byte{[]byte("keyH")}, [][]byte{[]byte("value010")}))

	itr, err := db.Iterator(storeKey1, 6, nil, []byte("keyZ"))
	s.Require().NoError(err)

	defer itr.Close()

	count := 0
	for ; itr.Valid(); itr.Next() {
		count++
	}

	s.Require().Equal(0, count)
}

func (s *StorageTestSuite) TestDatabaseBugInitialReverseIterationHigher() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	// Reverse Iteration
	// Less than iterator version
	s.Require().NoError(DBApplyChangeset(db, 12, storeKey1, [][]byte{[]byte("keyB")}, [][]byte{[]byte("value001")}))
	s.Require().NoError(DBApplyChangeset(db, 13, storeKey1, [][]byte{[]byte("keyC")}, [][]byte{[]byte("value002")}))
	s.Require().NoError(DBApplyChangeset(db, 14, storeKey1, [][]byte{[]byte("keyD")}, [][]byte{[]byte("value003")}))

	s.Require().NoError(DBApplyChangeset(db, 8, storeKey1, [][]byte{[]byte("keyE")}, [][]byte{[]byte("value007")}))
	s.Require().NoError(DBApplyChangeset(db, 9, storeKey1, [][]byte{[]byte("keyF")}, [][]byte{[]byte("value008")}))
	s.Require().NoError(DBApplyChangeset(db, 10, storeKey1, [][]byte{[]byte("keyG")}, [][]byte{[]byte("value009")}))
	s.Require().NoError(DBApplyChangeset(db, 11, storeKey1, [][]byte{[]byte("keyH")}, [][]byte{[]byte("value010")}))

	itr, err := db.ReverseIterator(storeKey1, 5, []byte("keyA"), nil)
	s.Require().NoError(err)

	defer itr.Close()

	count := 0
	for ; itr.Valid(); itr.Next() {
		count++
	}

	s.Require().Equal(0, count)
}

func (s *StorageTestSuite) TestDatabaseIteratorNoDomain() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(FillData(db, 10, 50))

	// create an iterator over the entire domain
	itr, err := db.Iterator(storeKey1, 50, nil, nil)
	s.Require().NoError(err)

	defer itr.Close()

	var i, count int
	for ; itr.Valid(); itr.Next() {
		s.Require().Equal([]byte(fmt.Sprintf("key%03d", i)), itr.Key(), string(itr.Key()))
		s.Require().Equal([]byte(fmt.Sprintf("val%03d-%03d", i, 50)), itr.Value())

		i++
		count++
	}
	s.Require().Equal(10, count)
	s.Require().NoError(itr.Error())
}

func (s *StorageTestSuite) TestDatabasePrune() {
	if slices.Contains(s.SkipTests, s.T().Name()) {
		s.T().SkipNow()
	}

	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(FillData(db, 10, 50))

	// Verify earliest version is 0
	earliestVersion, err := db.GetEarliestVersion()
	s.Require().NoError(err)
	s.Require().Equal(int64(0), earliestVersion)

	// prune the first 25 versions
	s.Require().NoError(db.Prune(25))

	// Verify earliest version is 26 (first 25 pruned)
	earliestVersion, err = db.GetEarliestVersion()
	s.Require().NoError(err)
	s.Require().Equal(int64(26), earliestVersion)

	latestVersion, err := db.GetLatestVersion()
	s.Require().NoError(err)
	s.Require().Equal(int64(50), latestVersion)

	// Ensure all keys are no longer present up to and including version 25 and
	// all keys are present after version 25.
	for v := int64(1); v <= 50; v++ {
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key%03d", i)
			val := fmt.Sprintf("val%03d-%03d", i, v)

			bz, err := db.Get(storeKey1, v, []byte(key))
			s.Require().NoError(err)
			if v <= 25 {
				s.Require().Nil(bz)
			} else {
				s.Require().Equal([]byte(val), bz)
			}
		}
	}

	itr, err := db.Iterator(storeKey1, 25, []byte("key000"), nil)
	s.Require().NoError(err)
	s.Require().False(itr.Valid())

	// prune the latest version which should prune the entire dataset
	s.Require().NoError(db.Prune(50))

	// Verify earliest version is 51 (first 50 pruned)
	earliestVersion, err = db.GetEarliestVersion()
	s.Require().NoError(err)
	s.Require().Equal(int64(51), earliestVersion)

	for v := int64(1); v <= 50; v++ {
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key%03d", i)

			bz, err := db.Get(storeKey1, v, []byte(key))
			s.Require().NoError(err)
			s.Require().Nil(bz)
		}
	}
}

func (s *StorageTestSuite) TestDatabasePruneAndTombstone() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	// write a key at three different versions 1, 100 and 200
	s.Require().NoError(DBApplyChangeset(db, 100, storeKey1, [][]byte{[]byte("key000")}, [][]byte{[]byte("value001")}))
	s.Require().NoError(DBApplyChangeset(db, 200, storeKey1, [][]byte{[]byte("key000")}, [][]byte{nil}))

	// prune version 150
	s.Require().NoError(db.Prune(150))

	bz, err := db.Get(storeKey1, 160, []byte("key000"))
	s.Require().NoError(err)
	s.Require().Equal([]byte("value001"), bz)
}

func (s *StorageTestSuite) TestDatabasePruneKeepRecent() {
	if slices.Contains(s.SkipTests, s.T().Name()) {
		s.T().SkipNow()
	}

	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	key := []byte("key000")

	// write a key at three different versions 1, 100 and 200
	s.Require().NoError(DBApplyChangeset(db, 1, storeKey1, [][]byte{key}, [][]byte{[]byte("value001")}))
	s.Require().NoError(DBApplyChangeset(db, 100, storeKey1, [][]byte{key}, [][]byte{[]byte("value002")}))
	s.Require().NoError(DBApplyChangeset(db, 200, storeKey1, [][]byte{key}, [][]byte{[]byte("value003")}))

	// prune version 50
	s.Require().NoError(db.Prune(50))

	// ensure queries for versions 50 and older return nil
	bz, err := db.Get(storeKey1, 49, key)
	s.Require().Nil(err)
	s.Require().Nil(bz)

	itr, err := db.Iterator(storeKey1, 49, nil, nil)
	s.Require().NoError(err)
	s.Require().False(itr.Valid())

	defer itr.Close()

	// ensure the value previously at version 1 is still there for queries greater than 50
	bz, err = db.Get(storeKey1, 51, key)
	s.Require().NoError(err)
	s.Require().Equal([]byte("value001"), bz)

	// ensure the correct value at a greater height
	bz, err = db.Get(storeKey1, 200, key)
	s.Require().NoError(err)
	s.Require().Equal([]byte("value003"), bz)

	// prune latest height and ensure we have the previous version when querying above it
	s.Require().NoError(db.Prune(200))

	bz, err = db.Get(storeKey1, 201, key)
	s.Require().NoError(err)
	s.Require().Equal([]byte("value003"), bz)
}

func (s *StorageTestSuite) TestDatabaseReverseIterator() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(FillData(db, 100, 1))

	// reverse iterator without an end key
	iter, err := db.ReverseIterator(storeKey1, 1, []byte("key000"), nil)
	s.Require().NoError(err)

	defer iter.Close()

	i, count := 99, 0
	for ; iter.Valid(); iter.Next() {
		s.Require().Equal([]byte(fmt.Sprintf("key%03d", i)), iter.Key())
		s.Require().Equal([]byte(fmt.Sprintf("val%03d-001", i)), iter.Value())

		i--
		count++
	}
	s.Require().Equal(100, count)
	s.Require().NoError(iter.Error())

	// seek past domain, which should make the iterator invalid and produce an error
	iter.Next()
	s.Require().False(iter.Valid())

	// reverse iterator with with a start and end domain
	iter2, err := db.ReverseIterator(storeKey1, 1, []byte("key010"), []byte("key019"))
	s.Require().NoError(err)

	defer iter2.Close()

	i, count = 18, 0
	for ; iter2.Valid(); iter2.Next() {
		s.Require().Equal([]byte(fmt.Sprintf("key%03d", i)), iter2.Key())
		s.Require().Equal([]byte(fmt.Sprintf("val%03d-001", i)), iter2.Value())

		i--
		count++
	}
	s.Require().Equal(9, count)
	s.Require().NoError(iter2.Error())

	// seek past domain, which should make the iterator invalid and produce an error
	iter2.Next()
	s.Require().False(iter2.Valid())

	// start must be <= end
	iter3, err := db.ReverseIterator(storeKey1, 1, []byte("key020"), []byte("key019"))
	s.Require().Error(err)
	s.Require().Nil(iter3)
}

func (s *StorageTestSuite) TestParallelWrites() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	latestVersion := 10
	kvCount := 100

	wg := sync.WaitGroup{}
	triggerStartCh := make(chan bool)

	// start 10 goroutines that write to the database
	for i := 0; i < latestVersion; i++ {
		wg.Add(1)
		go func(i int) {
			<-triggerStartCh
			defer wg.Done()
			keys := [][]byte{}
			vals := [][]byte{}
			for j := 0; j < kvCount; j++ {
				keys = append(keys, []byte(fmt.Sprintf("key-%d-%03d", i, j)))
				vals = append(vals, []byte(fmt.Sprintf("val-%d-%03d", i, j)))
			}
			s.Require().NoError(DBApplyChangeset(db, int64(i+1), storeKey1, keys, vals))
		}(i)

	}

	// start the goroutines
	close(triggerStartCh)
	wg.Wait()

	// check that all the data is there
	for i := 0; i < latestVersion; i++ {
		for j := 0; j < kvCount; j++ {
			version := int64(i + 1)
			key := fmt.Sprintf("key-%d-%03d", i, j)
			val := fmt.Sprintf("val-%d-%03d", i, j)

			v, err := db.Get(storeKey1, version, []byte(key))
			s.Require().NoError(err)
			s.Require().Equal([]byte(val), v)
		}
	}
}

func (s *StorageTestSuite) TestParallelWriteAndPruning() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	latestVersion := 100
	kvCount := 100
	prunePeriod := 5

	wg := sync.WaitGroup{}
	triggerStartCh := make(chan bool)

	// start a goroutine that write to the database
	wg.Add(1)
	go func() {
		<-triggerStartCh
		defer wg.Done()
		for i := 0; i < latestVersion; i++ {
			keys := [][]byte{}
			vals := [][]byte{}
			for j := 0; j < kvCount; j++ {
				keys = append(keys, []byte(fmt.Sprintf("key-%d-%03d", i, j)))
				vals = append(vals, []byte(fmt.Sprintf("val-%d-%03d", i, j)))
			}
			s.Require().NoError(DBApplyChangeset(db, int64(i+1), storeKey1, keys, vals))
		}
	}()

	// start a goroutine that prunes the database
	wg.Add(1)
	go func() {
		<-triggerStartCh
		defer wg.Done()
		for i := 10; i < latestVersion; i += prunePeriod {
			for {
				v, err := db.GetLatestVersion()
				s.Require().NoError(err)
				if v > int64(i) {
					s.Require().NoError(db.Prune(v - 1))
					break
				}
			}
		}
	}()

	// wait for the goroutines
	close(triggerStartCh)
	wg.Wait()

	// check if the data is pruned
	version := int64(latestVersion - prunePeriod)
	val, err := db.Get(storeKey1, version, []byte(fmt.Sprintf("key-%d-%03d", version-1, 0)))
	s.Require().Nil(err)
	s.Require().Nil(val)

	version = int64(latestVersion)
	val, err = db.Get(storeKey1, version, []byte(fmt.Sprintf("key-%d-%03d", version-1, 0)))
	s.Require().NoError(err)
	s.Require().Equal([]byte(fmt.Sprintf("val-%d-%03d", version-1, 0)), val)
}

func (s *StorageTestSuite) TestDatabaseParallelDeleteIteration() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(FillData(db, 100, 100))

	wg := sync.WaitGroup{}
	triggerStartCh := make(chan bool)

	latestVersion := 100
	kvCount := 100

	// start a goroutine that deletes from the database at latest Version
	wg.Add(1)
	go func() {
		<-triggerStartCh
		defer wg.Done()

		for j := 0; j < kvCount; j++ {
			if j%10 == 0 {
				key := []byte(fmt.Sprintf("key%03d", j))
				s.Require().NoError(DBApplyChangeset(db, int64(latestVersion), storeKey1, [][]byte{key}, [][]byte{nil}))
			}
		}
	}()

	// start a goroutine that iterates over the database
	wg.Add(1)
	go func() {
		<-triggerStartCh
		defer wg.Done()
		// iterator without an end key over multiple versions
		for v := int64(1); v < 5; v++ {
			itr, err := db.Iterator(storeKey1, v, []byte("key000"), nil)
			s.Require().NoError(err)

			defer itr.Close()

			var i, count int
			for ; itr.Valid(); itr.Next() {
				s.Require().Equal([]byte(fmt.Sprintf("key%03d", i)), itr.Key(), string(itr.Key()))
				s.Require().Equal([]byte(fmt.Sprintf("val%03d-%03d", i, v)), itr.Value())

				i++
				count++
			}
			s.Require().Equal(100, count)
			s.Require().NoError(itr.Error())

			// seek past domain, which should make the iterator invalid and produce an error
			itr.Next()
			s.Require().False(itr.Valid())
		}
	}()

	// wait for the goroutines
	close(triggerStartCh)
	wg.Wait()

	// Verify deletes
	for j := 0; j < 100; j++ {
		ok, err := db.Has(storeKey1, int64(latestVersion), []byte(fmt.Sprintf("key%03d", j)))
		s.Require().NoError(err)

		if j%10 == 0 {
			s.Require().False(ok)
		} else {
			s.Require().True(ok)
		}
	}
}

func (s *StorageTestSuite) TestDatabaseParallelWriteDelete() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(FillData(db, 100, 1))

	wg := sync.WaitGroup{}
	triggerStartCh := make(chan bool)

	latestVersion := int64(2)

	// start a goroutine that writes to the database at latestVersion
	wg.Add(1)
	go func() {
		<-triggerStartCh
		defer wg.Done()

		cs := &iavl.ChangeSet{}
		cs.Pairs = []*iavl.KVPair{}

		for i := 0; i < 50; i++ {
			// Apply changeset for each key separately
			key := []byte(fmt.Sprintf("key%03d", i))
			val := []byte(fmt.Sprintf("val%03d", i))
			s.Require().NoError(DBApplyChangeset(db, latestVersion, storeKey1, [][]byte{key}, [][]byte{val}))
		}
	}()

	// start a goroutine that deletes from the database
	wg.Add(1)
	go func() {
		<-triggerStartCh
		defer wg.Done()

		cs := &iavl.ChangeSet{}
		cs.Pairs = []*iavl.KVPair{}

		for i := 50; i < 100; i++ {
			// Apply changeset for each key separately
			key := []byte(fmt.Sprintf("key%03d", i))
			s.Require().NoError(DBApplyChangeset(db, latestVersion, storeKey1, [][]byte{key}, [][]byte{nil}))
		}
	}()

	// wait for the goroutines
	close(triggerStartCh)
	wg.Wait()

	// Verify writes and deletes on latest version
	for j := 0; j < 100; j++ {
		ok, err := db.Has(storeKey1, latestVersion, []byte(fmt.Sprintf("key%03d", j)))
		s.Require().NoError(err)

		if j >= 50 {
			s.Require().False(ok)
		} else {
			s.Require().True(ok)
		}
	}
}

func (s *StorageTestSuite) TestParallelIterationAndPruning() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(FillData(db, 10, 50))

	latestVersion := 50
	numHeightsPruned := 20
	prunePeriod := 5

	wg := sync.WaitGroup{}
	triggerStartCh := make(chan bool)

	// start a goroutine that prunes the database
	wg.Add(1)
	go func() {
		<-triggerStartCh
		defer wg.Done()
		for i := 10; i <= latestVersion-numHeightsPruned; i += prunePeriod {
			s.Require().NoError(db.Prune(int64(i)))
		}
	}()

	// start a goroutine that iterates over the database
	wg.Add(1)
	go func() {
		<-triggerStartCh
		defer wg.Done()
		// iterator without an end key over multiple versions
		for v := int64(latestVersion - numHeightsPruned + 1); v < int64(latestVersion); v++ {
			itr, err := db.Iterator(storeKey1, v, []byte("key000"), nil)
			s.Require().NoError(err)

			defer itr.Close()

			var i, count int
			for ; itr.Valid(); itr.Next() {
				s.Require().Equal([]byte(fmt.Sprintf("key%03d", i)), itr.Key(), string(itr.Key()))
				s.Require().Equal([]byte(fmt.Sprintf("val%03d-%03d", i, v)), itr.Value())

				i++
				count++
			}
			s.Require().Equal(10, count)
			s.Require().NoError(itr.Error())

			// seek past domain, which should make the iterator invalid and produce an error
			itr.Next()
			s.Require().False(itr.Valid())
		}
	}()

	// wait for the goroutines
	close(triggerStartCh)
	wg.Wait()

	// Ensure all keys are no longer present up to latestVersion - 20 and
	// all keys are present after
	for v := int64(1); v <= int64(latestVersion); v++ {
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key%03d", i)
			val := fmt.Sprintf("val%03d-%03d", i, v)

			bz, err := db.Get(storeKey1, v, []byte(key))
			s.Require().NoError(err)
			if v <= int64(latestVersion-numHeightsPruned) {
				s.Require().Nil(bz)
			} else {
				s.Require().Equal([]byte(val), bz)
			}
		}
	}
}

func (s *StorageTestSuite) TestDatabaseParallelIterationVersions() {
	db, err := s.NewDB(s.T().TempDir())
	s.Require().NoError(err)
	defer db.Close()

	s.Require().NoError(FillData(db, 10, 100))

	wg := sync.WaitGroup{}
	triggerStartCh := make(chan bool)

	latestVersion := 100
	kvCount := 10

	// start multiple goroutines that iterate over different version of the database
	for v := 1; v < latestVersion; v++ {
		wg.Add(1)
		go func(v int) {
			<-triggerStartCh
			defer wg.Done()

			itr, err := db.Iterator(storeKey1, int64(v), []byte("key000"), nil)
			s.Require().NoError(err)

			defer itr.Close()

			var i, count int
			for ; itr.Valid(); itr.Next() {
				s.Require().Equal([]byte(fmt.Sprintf("key%03d", i)), itr.Key(), string(itr.Key()))
				s.Require().Equal([]byte(fmt.Sprintf("val%03d-%03d", i, v)), itr.Value())

				i++
				count++
			}
			s.Require().Equal(kvCount, count)
			s.Require().NoError(itr.Error())

			// seek past domain, which should make the iterator invalid and produce an error
			itr.Next()
			s.Require().False(itr.Valid())

		}(v)
	}

	// wait for the goroutines
	close(triggerStartCh)
	wg.Wait()
}
