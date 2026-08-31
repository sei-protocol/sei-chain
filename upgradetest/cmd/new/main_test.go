package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunScaffoldsTheRequestedUpgrade(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer

	err := run([]string{"-from", "v6.7", "-to", "v6.8", "-root", root}, &out)
	require.NoError(t, err)
	require.Contains(t, out.String(), filepath.Join(root, "upgrade_v68_test.go"))
}

func TestRunRequiresBothVersions(t *testing.T) {
	err := run([]string{"-from", "v6.7"}, &bytes.Buffer{})
	require.ErrorContains(t, err, "both -from and -to are required")
}
