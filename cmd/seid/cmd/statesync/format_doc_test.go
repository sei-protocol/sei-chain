package statesync

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// FORMAT.md names the limits of manifest.go, the vectors file of formatVersion, and test functions of this
// package that exist.
func TestFormatDocMatchesTheCode(t *testing.T) {
	data, err := os.ReadFile("FORMAT.md")
	require.NoError(t, err)
	doc := string(data)

	require.Contains(t, doc, fmt.Sprintf("# State sync archive format, version %d", formatVersion))
	require.Contains(t, doc, "("+filepath.ToSlash(formatVectorsFile)+")")
	for row, size := range map[string]string{
		"manifest entry": fmt.Sprintf("%d MiB", maxManifestSize>>20),
		"proof entry":    fmt.Sprintf("%d MiB", maxProofSize>>20),
		"row chunk":      fmt.Sprintf("%d MiB", maxRowChunkSize>>20),
		"key":            fmt.Sprintf("%d KiB", maxStateKeySize>>10),
		"value":          fmt.Sprintf("%d MiB", maxStateValueSize>>20),
	} {
		limit := regexp.MustCompile(fmt.Sprintf(`\| %s +\| %s +\|`, regexp.QuoteMeta(row), regexp.QuoteMeta(size)))
		require.True(t, limit.MatchString(doc), "FORMAT.md does not give the %s limit as %s", row, size)
	}

	tests, err := filepath.Glob("*_test.go")
	require.NoError(t, err)
	var sources strings.Builder
	for _, name := range tests {
		src, err := os.ReadFile(name) //nolint:gosec // name is a test file of this package.
		require.NoError(t, err)
		sources.Write(src)
	}
	named := regexp.MustCompile("(?m)^\\| `(Test\\w+)`").FindAllStringSubmatch(doc, -1)
	require.NotEmpty(t, named)
	for _, m := range named {
		require.Contains(t, sources.String(), "func "+m[1]+"(t *testing.T)", "FORMAT.md names %s, which is not a test of this package", m[1])
	}
}
