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

	err := run([]string{"-from", "v6.6", "-to", "v6.7", "-root", root}, &out)
	require.NoError(t, err)
	require.Contains(t, out.String(), filepath.Join(root, "upgrade_v67_test.go"))
}

func TestRunRequiresBothVersions(t *testing.T) {
	err := run([]string{"-from", "v6.6"}, &bytes.Buffer{})
	require.ErrorContains(t, err, "both -from and -to are required")
}
