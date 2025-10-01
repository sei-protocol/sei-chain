package multiversion_test

import (
	"testing"

	mv "github.com/cosmos/cosmos-sdk/store/multiversion"
	"github.com/stretchr/testify/require"
)

func TestMultiversionItemGetLatest(t *testing.T) {
	mvItem := mv.NewMultiVersionItem()
	// We have no value, should get found == false and a nil value
	value, found := mvItem.GetLatest()
	require.False(t, found)
	require.Nil(t, value)

	// assert that we find a value after it's set
	one := []byte("one")
	mvItem.Set(1, 0, one)
	value, found = mvItem.GetLatest()
	require.True(t, found)
	require.Equal(t, one, value.Value())

	// assert that we STILL get the "one" value since it is the latest
	zero := []byte("zero")
	mvItem.Set(0, 0, zero)
	value, found = mvItem.GetLatest()
	require.True(t, found)
	require.Equal(t, one, value.Value())
	require.Equal(t, 1, value.Index())
	require.Equal(t, 0, value.Incarnation())

	// we should see a deletion as the latest now, aka nil value and found == true
	mvItem.Delete(2, 0)
	value, found = mvItem.GetLatest()
	require.True(t, found)
	require.True(t, value.IsDeleted())
	require.Nil(t, value.Value())

	// Overwrite the deleted value with some data
	two := []byte("two")
	mvItem.Set(2, 3, two)
	value, found = mvItem.GetLatest()
	require.True(t, found)
	require.Equal(t, two, value.Value())
	require.Equal(t, 2, value.Index())
	require.Equal(t, 3, value.Incarnation())
}

func TestMultiversionItemGetByIndex(t *testing.T) {
	mvItem := mv.NewMultiVersionItem()
	// We have no value, should get found == false and a nil value
	value, found := mvItem.GetLatestBeforeIndex(9)
	require.False(t, found)
	require.Nil(t, value)

	// assert that we find a value after it's set
	one := []byte("one")
	mvItem.Set(1, 0, one)
	// should not be found because we specifically search "LESS THAN"
	value, found = mvItem.GetLatestBeforeIndex(1)
	require.False(t, found)
	require.Nil(t, value)
	// querying from "two" should be found
	value, found = mvItem.GetLatestBeforeIndex(2)
	require.True(t, found)
	require.Equal(t, one, value.Value())

	// verify that querying for an earlier index returns nil
	value, found = mvItem.GetLatestBeforeIndex(0)
	require.False(t, found)
	require.Nil(t, value)

	// assert that we STILL get the "one" value when querying with a later index
	zero := []byte("zero")
	mvItem.Set(0, 0, zero)
	// verify that querying for zero should ALWAYS return nil
	value, found = mvItem.GetLatestBeforeIndex(0)
	require.False(t, found)
	require.Nil(t, value)

	value, found = mvItem.GetLatestBeforeIndex(2)
	require.True(t, found)
	require.Equal(t, one, value.Value())
	// verify we get zero when querying with index 1
	value, found = mvItem.GetLatestBeforeIndex(1)
	require.True(t, found)
	require.Equal(t, zero, value.Value())

	// we should see a deletion as the latest now, aka nil value and found == true, but index 4 still returns `one`
	mvItem.Delete(4, 0)
	value, found = mvItem.GetLatestBeforeIndex(4)
	require.True(t, found)
	require.Equal(t, one, value.Value())
	// should get deletion item for a later index
	value, found = mvItem.GetLatestBeforeIndex(5)
	require.True(t, found)
	require.True(t, value.IsDeleted())

	// verify that we still read the proper underlying item for an older index
	value, found = mvItem.GetLatestBeforeIndex(3)
	require.True(t, found)
	require.Equal(t, one, value.Value())

	// Overwrite the deleted value with some data and verify we read it properly
	four := []byte("four")
	mvItem.Set(4, 0, four)
	// also reads the four
	value, found = mvItem.GetLatestBeforeIndex(6)
	require.True(t, found)
	require.Equal(t, four, value.Value())
	// still reads the `one`
	value, found = mvItem.GetLatestBeforeIndex(4)
	require.True(t, found)
	require.Equal(t, one, value.Value())
}

func TestMultiversionItemEstimate(t *testing.T) {
	mvItem := mv.NewMultiVersionItem()
	// We have no value, should get found == false and a nil value
	value, found := mvItem.GetLatestBeforeIndex(9)
	require.False(t, found)
	require.Nil(t, value)

	// assert that we find a value after it's set
	one := []byte("one")
	mvItem.Set(1, 0, one)
	// should not be found because we specifically search "LESS THAN"
	value, found = mvItem.GetLatestBeforeIndex(1)
	require.False(t, found)
	require.Nil(t, value)
	// querying from "two" should be found
	value, found = mvItem.GetLatestBeforeIndex(2)
	require.True(t, found)
	require.False(t, value.IsEstimate())
	require.Equal(t, one, value.Value())
	// set as estimate
	mvItem.SetEstimate(1, 2)
	// should not be found because we specifically search "LESS THAN"
	value, found = mvItem.GetLatestBeforeIndex(1)
	require.False(t, found)
	require.Nil(t, value)
	// querying from "two" should be found as ESTIMATE
	value, found = mvItem.GetLatestBeforeIndex(2)
	require.True(t, found)
	require.True(t, value.IsEstimate())
	require.Equal(t, 1, value.Index())
	require.Equal(t, 2, value.Incarnation())

	// verify that querying for an earlier index returns nil
	value, found = mvItem.GetLatestBeforeIndex(0)
	require.False(t, found)
	require.Nil(t, value)

	// assert that we STILL get the "one" value when querying with a later index
	zero := []byte("zero")
	mvItem.Set(0, 0, zero)
	// verify that querying for zero should ALWAYS return nil
	value, found = mvItem.GetLatestBeforeIndex(0)
	require.False(t, found)
	require.Nil(t, value)

	value, found = mvItem.GetLatestBeforeIndex(2)
	require.True(t, found)
	require.True(t, value.IsEstimate())
	// verify we get zero when querying with index 1
	value, found = mvItem.GetLatestBeforeIndex(1)
	require.True(t, found)
	require.Equal(t, zero, value.Value())
	// reset one to no longer be an estiamte
	mvItem.Set(1, 0, one)
	// we should see a deletion as the latest now, aka nil value and found == true, but index 4 still returns `one`
	mvItem.Delete(4, 1)
	value, found = mvItem.GetLatestBeforeIndex(4)
	require.True(t, found)
	require.Equal(t, one, value.Value())
	// should get deletion item for a later index
	value, found = mvItem.GetLatestBeforeIndex(5)
	require.True(t, found)
	require.True(t, value.IsDeleted())
	require.Equal(t, 4, value.Index())
	require.Equal(t, 1, value.Incarnation())

	// verify that we still read the proper underlying item for an older index
	value, found = mvItem.GetLatestBeforeIndex(3)
	require.True(t, found)
	require.Equal(t, one, value.Value())

	// Overwrite the deleted value with an estimate and verify we read it properly
	mvItem.SetEstimate(4, 0)
	// also reads the four
	value, found = mvItem.GetLatestBeforeIndex(6)
	require.True(t, found)
	require.True(t, value.IsEstimate())
	require.False(t, value.IsDeleted())
	// still reads the `one`
	value, found = mvItem.GetLatestBeforeIndex(4)
	require.True(t, found)
	require.Equal(t, one, value.Value())
}

func TestMultiversionItemRemove(t *testing.T) {
	mvItem := mv.NewMultiVersionItem()

	mvItem.Set(1, 0, []byte("one"))
	mvItem.Set(2, 0, []byte("two"))

	mvItem.Remove(2)
	value, found := mvItem.GetLatest()
	require.True(t, found)
	require.Equal(t, []byte("one"), value.Value())
}

func TestMultiversionItemGetLatestNonEstimate(t *testing.T) {
	mvItem := mv.NewMultiVersionItem()

	mvItem.SetEstimate(3, 0)

	value, found := mvItem.GetLatestNonEstimate()
	require.False(t, found)
	require.Nil(t, value)

	mvItem.Set(1, 0, []byte("one"))
	value, found = mvItem.GetLatestNonEstimate()
	require.True(t, found)
	require.Equal(t, []byte("one"), value.Value())

}
