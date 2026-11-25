package upgradenames

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
)

func TestReleaseIsSortedAndEndsAtLatest(t *testing.T) {
	names := Release()
	require.NotEmpty(t, names)
	require.True(t, slices.IsSortedFunc(names, semver.Compare))
	require.Equal(t, names[len(names)-1], Latest())
}

func TestReleaseReturnsACopy(t *testing.T) {
	names := Release()
	names[0] = "overwritten"
	require.NotEqual(t, "overwritten", Release()[0])
}

func TestParseSplitsOnNewlinesAndCommas(t *testing.T) {
	require.Equal(t, []string{"1.0.2beta", "v5.0.0", "v6.0.0"}, Parse("v6.0.0,v5.0.0\n1.0.2beta"))
}
