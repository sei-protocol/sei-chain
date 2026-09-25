package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// mustCloseTestObject is an object registered with MustClose() by these tests. It holds a pointer so the runtime
// does not batch it into a shared allocation, where its cleanup might never run.
type mustCloseTestObject struct {
	// Unused; present only to give the object a pointer.
	payload *int
}

// A marker closed with an object other than the one it was issued for is reported.
func TestCloseMarkerRejectsAnotherOwner(t *testing.T) {
	owner := &mustCloseTestObject{}
	marker := MustClose(owner, "test object")

	require.Panics(t, func() { marker.Close(&mustCloseTestObject{}) })
	require.False(t, marker.IsClosed())

	marker.Close(owner)
	require.True(t, marker.IsClosed())
}

// Every copy of a marker reports the close made through any of them.
func TestCloseMarkerCopiesShareState(t *testing.T) {
	owner := &mustCloseTestObject{}
	marker := MustClose(owner, "test object")
	markerCopy := marker

	markerCopy.Close(owner)
	require.True(t, marker.IsClosed())
}

// A marker that MustClose() did not issue refuses to be used.
func TestCloseMarkerNotIssuedPanics(t *testing.T) {
	var marker CloseMarker[mustCloseTestObject]
	require.Panics(t, func() { marker.Close(&mustCloseTestObject{}) })
	require.Panics(t, func() { marker.IsClosed() })
}
