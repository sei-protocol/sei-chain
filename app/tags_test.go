package app_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
)

func TestSemver_Comparison(t *testing.T) {
	require.Zero(t, semver.Compare("v6.5", "v6.5.0"))
	require.Zero(t, semver.Compare("v6.5.0", "v6.5"))
}
